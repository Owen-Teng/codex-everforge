package xactivity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const MonitorStatusFile = "monitor-status.json"
const MonitorNoticeFile = "notices.jsonl"

var errMonitorStop = errors.New("manual_stop")

type MonitorTransition struct {
	At     int64  `json:"at"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// Snapshot contains only counters, safe reason codes and timing. It never stores
// a credential, a response body or Post content. realConnection means ever
// connected in THIS session; connectionActive is the current connection state.
type MonitorSnapshot struct {
	SchemaVersion          int                 `json:"schemaVersion"`
	Mode                   string              `json:"mode"`
	SessionPolicy          string              `json:"sessionPolicy"`
	SessionID              string              `json:"sessionId"`
	PID                    int                 `json:"pid"`
	Running                bool                `json:"running"`
	State                  string              `json:"state"`
	Reason                 string              `json:"reason"`
	StartedAt              int64               `json:"startedAt"`
	UpdatedAt              int64               `json:"updatedAt"`
	DeadlineAt             int64               `json:"deadlineAt"`
	StoppedAt              *int64              `json:"stoppedAt"`
	Limits                 Config              `json:"limits"`
	Receiver               Status              `json:"receiver"`
	DeliveredNotifications int                 `json:"deliveredNotifications"`
	Decisions              map[string]int      `json:"decisions"`
	Transitions            []MonitorTransition `json:"transitions"`
}

type Monitor struct {
	Config       Config
	Root         string
	Connector    Connector
	Prepare      func(context.Context) (Config, error)
	OnTransition func(MonitorSnapshot)
	// Only injected by offline tests. The CLI uses the real bounded backoff.
	Sleep       func(context.Context, time.Duration) bool
	Observation bool
}

func monitorPathSafe(path string) bool {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) {
		return false
	}
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || monitorReparsePoint(current)) {
			return false
		}
		if err != nil && !os.IsNotExist(err) {
			return false
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return true
}

func writeMonitorJSON(path string, value any) error {
	if !monitorPathSafe(path) {
		return errors.New("invalid_state_path")
	}
	file, err := os.CreateTemp(filepath.Dir(path), "monitor-*.tmp")
	if err != nil {
		return errors.New("status_write_failed")
	}
	name := file.Name()
	defer os.Remove(name)
	_ = file.Chmod(0600)
	err = json.NewEncoder(file).Encode(value)
	if err == nil {
		err = file.Sync()
	}
	if e := file.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return errors.New("status_write_failed")
	}
	return nil
}

func ReadMonitorStatus(root string) (MonitorSnapshot, error) {
	var s MonitorSnapshot
	path := filepath.Join(root, MonitorStatusFile)
	if !monitorPathSafe(path) {
		return s, errors.New("invalid_state_path")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return MonitorSnapshot{SchemaVersion: 1, Mode: "x_activity_monitor", State: "not_started", Reason: "not_started"}, nil
	}
	if err != nil || len(data) > 65536 || json.Unmarshal(data, &s) != nil || s.SchemaVersion != 1 {
		return s, errors.New("status_unavailable")
	}
	if s.Running && time.Now().Unix()-s.UpdatedAt > 3 {
		s.Running = false
		s.State = "status_stale"
		s.Reason = "heartbeat_missing"
		s.Receiver.ConnectionActive = false
	}
	return s, nil
}

func RequestMonitorStop(root string) error {
	s, err := ReadMonitorStatus(root)
	if err != nil {
		return err
	}
	if !s.Running {
		return errors.New("not_running")
	}
	if len(s.SessionID) != 32 {
		return errors.New("status_unavailable")
	}
	if _, err := hex.DecodeString(s.SessionID); err != nil {
		return errors.New("status_unavailable")
	}
	return writeMonitorJSON(filepath.Join(root, "stop-"+s.SessionID+".json"), struct {
		SessionID string `json:"sessionId"`
	}{s.SessionID})
}

func (m Monitor) Run(parent context.Context) (MonitorSnapshot, error) {
	c := m.Config
	s := MonitorSnapshot{SchemaVersion: 1, Mode: "x_activity_monitor", State: "disabled", Reason: "disabled", Limits: c, Decisions: map[string]int{}}
	s.SessionPolicy = "finite_test"
	if m.Observation {
		s.SessionPolicy = "bounded_observation"
	}
	if !c.Enabled {
		return s, nil
	}
	if m.Connector == nil {
		return s, errors.New("not_configured")
	}
	live := m.Connector.IsLive()
	if !live {
		s.Mode = "synthetic_x_monitor"
	}
	if live && (!c.BillingApproved || !c.BillingCycleLimitConfirmed) {
		return s, errors.New("billing_not_authorized")
	}
	validatePreflight := c.ValidateLiveTest
	if m.Observation {
		validatePreflight = c.ValidateObservationPreflight
	}
	if err := validatePreflight(); err != nil {
		return s, err
	}
	if live && m.Prepare == nil {
		return s, errors.New("preflight_required")
	}
	if !monitorPathSafe(m.Root) {
		return s, errors.New("invalid_state_path")
	}
	if err := os.MkdirAll(m.Root, 0700); err != nil {
		return s, errors.New("state_unavailable")
	}
	lockPath := filepath.Join(m.Root, "monitor.lock")
	if !monitorPathSafe(lockPath) {
		return s, errors.New("invalid_state_path")
	}
	unlock, err := lockMonitor(lockPath)
	if err != nil {
		return s, errors.New("already_running_or_locked")
	}
	defer unlock()
	if closer, ok := m.Connector.(interface{ Close() }); ok {
		defer closer.Close()
	}
	for _, name := range []string{MonitorNoticeFile, MonitorStatusFile, "x-seen.json"} {
		if !monitorPathSafe(filepath.Join(m.Root, name)) {
			return s, errors.New("invalid_state_path")
		}
	}
	notices, err := os.OpenFile(filepath.Join(m.Root, MonitorNoticeFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return s, errors.New("notice_output_unavailable")
	}
	defer notices.Close()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return s, errors.New("session_unavailable")
	}
	s.SessionID = hex.EncodeToString(id[:])
	s.PID = os.Getpid()
	s.Running = true
	s.StartedAt = time.Now().Unix()
	s.DeadlineAt = time.Now().Add(time.Duration(c.MaxDurationSeconds) * time.Second).Unix()
	deadline, endDeadline := context.WithTimeout(parent, time.Duration(c.MaxDurationSeconds)*time.Second)
	defer endDeadline()
	ctx, cancel := context.WithCancelCause(deadline)
	defer cancel(nil)
	var mu sync.Mutex
	copySnapshot := func() MonitorSnapshot {
		out := s
		out.Transitions = append([]MonitorTransition(nil), s.Transitions...)
		out.Decisions = make(map[string]int, len(s.Decisions))
		for k, v := range s.Decisions {
			out.Decisions[k] = v
		}
		return out
	}
	var writeErr error
	save := func() {
		mu.Lock()
		defer mu.Unlock()
		s.UpdatedAt = time.Now().Unix()
		if e := writeMonitorJSON(filepath.Join(m.Root, MonitorStatusFile), s); e != nil {
			writeErr = e
			cancel(e)
		}
	}
	transition := func(state, reason string) {
		mu.Lock()
		changed := s.State != state || s.Reason != reason
		s.UpdatedAt = time.Now().Unix()
		s.State = state
		s.Reason = reason
		if changed {
			s.Transitions = append(s.Transitions, MonitorTransition{time.Now().Unix(), state, reason})
			if len(s.Transitions) > 32 {
				s.Transitions = s.Transitions[len(s.Transitions)-32:]
			}
		}
		out := copySnapshot()
		mu.Unlock()
		if changed && m.OnTransition != nil {
			m.OnTransition(out)
		}
	}
	transition("starting", "initializing")
	save()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	stopPath := filepath.Join(m.Root, "stop-"+s.SessionID+".json")
	defer os.Remove(stopPath)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if monitorPathSafe(stopPath) {
					if b, e := os.ReadFile(stopPath); e == nil && len(b) < 1024 {
						var request struct {
							SessionID string `json:"sessionId"`
						}
						if json.Unmarshal(b, &request) == nil && request.SessionID == s.SessionID {
							cancel(errMonitorStop)
							return
						}
					}
				}
				save()
			}
		}
	}()
	finish := func(reason string) (MonitorSnapshot, error) {
		close(done)
		wg.Wait()
		if cause := context.Cause(ctx); cause != nil {
			switch {
			case errors.Is(cause, errMonitorStop):
				reason = "manual_stop"
			case errors.Is(cause, context.DeadlineExceeded):
				reason = "time_limit"
			case errors.Is(cause, context.Canceled):
				reason = "interrupted"
			default:
				reason = "local_output_failed"
			}
		}
		mu.Lock()
		s.Running = false
		s.Receiver.ConnectionActive = false
		s.Receiver.NextRetryAt = nil
		stamp := time.Now().Unix()
		s.StoppedAt = &stamp
		mu.Unlock()
		transition("stopped", reason)
		save()
		mu.Lock()
		out := copySnapshot()
		failure := writeErr
		mu.Unlock()
		return out, failure
	}
	if m.Prepare != nil {
		transition("preflighting", "verifying_subscription_and_author")
		save()
		prepared, e := m.Prepare(ctx)
		if e != nil {
			return finish(e.Error())
		}
		// Preflight may resolve author identity, never expand session limits.
		if prepared.MaxDurationSeconds != c.MaxDurationSeconds || prepared.MaxDeliveredFrames != c.MaxDeliveredFrames || prepared.MaxAttempts != c.MaxAttempts || prepared.MaxWireBytes != c.MaxWireBytes {
			return finish("preflight_limits_changed")
		}
		c = prepared
	}
	validate := c.Validate
	if m.Observation {
		validate = c.ValidateObservation
	}
	if e := validate(); e != nil {
		return finish(e.Error())
	}
	processor, e := newProcessor(c, time.Now, filepath.Join(m.Root, "x-seen.json"), m.Observation)
	if e != nil {
		return finish(e.Error())
	}
	mu.Lock()
	s.Limits = c
	mu.Unlock()
	encoder := json.NewEncoder(notices)
	runner := Runner{Config: c, Connector: m.Connector, Processor: processor, Sleep: m.Sleep, Observation: m.Observation, OnStatus: func(status Status) {
		mu.Lock()
		s.Receiver = status
		mu.Unlock()
		// Publish one coherent stopped snapshot, after files/stop watcher finish.
		if status.State != "stopped" {
			transition(status.State, status.Reason)
		}
	}, OnDecision: func(d Decision) {
		mu.Lock()
		s.Decisions[d.Reason]++
		mu.Unlock()
		if d.Notification == nil {
			return
		}
		line := ReplayDelivery(d)
		if live {
			line = LiveDelivery(d, true)
		}
		if e := encoder.Encode(line); e != nil {
			cancel(errors.New("notice_output_failed"))
			return
		}
		if e := notices.Sync(); e != nil {
			cancel(errors.New("notice_output_failed"))
			return
		}
		mu.Lock()
		s.DeliveredNotifications++
		mu.Unlock()
	}}
	status := runner.Run(ctx)
	mu.Lock()
	s.Receiver = status
	mu.Unlock()
	reason := status.Reason
	if status.StopCondition == "attempt_limit" {
		reason = "attempt_limit_" + status.Reason
	}
	return finish(reason)
}
