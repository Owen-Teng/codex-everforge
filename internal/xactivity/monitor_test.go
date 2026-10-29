package xactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type guardedLive struct{ opens int }

func (g *guardedLive) IsLive() bool { return true }
func (g *guardedLive) Open(context.Context) (*http.Response, error) {
	g.opens++
	return nil, fmt.Errorf("unexpected_open")
}

func TestMonitorDisabledAndBillingGatesDoNotPrepareOrOpen(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c := ReplayConfig()
		c.Enabled = enabled
		root := filepath.Join(t.TempDir(), "not-created")
		connector := &guardedLive{}
		prepared := 0
		s, err := (Monitor{Config: c, Root: root, Connector: connector, Prepare: func(context.Context) (Config, error) { prepared++; return c, nil }}).Run(t.Context())
		if enabled && (err == nil || err.Error() != "billing_not_authorized") {
			t.Fatal("billing gate missing")
		}
		if !enabled && (err != nil || s.State != "disabled") {
			t.Fatal("disabled gate missing")
		}
		if prepared != 0 || connector.opens != 0 {
			t.Fatal("gate allowed sensitive work")
		}
		if _, e := os.Stat(root); !os.IsNotExist(e) {
			t.Fatal("disabled/gated session touched output")
		}
	}
}

func fixtureMonitor(t *testing.T, handler http.HandlerFunc) (Monitor, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	c := ReplayConfig()
	c.MaxAttempts = 1
	c.MaxDurationSeconds = 20
	connector, err := NewLoopbackConnector(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return Monitor{Config: c, Root: t.TempDir(), Connector: connector, Sleep: func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }}, server.Close
}

func TestMonitorStopStatusHeartbeatsAndExclusiveLifecycle(t *testing.T) {
	m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("\r\n\r\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer closeServer()
	done := make(chan MonitorSnapshot, 1)
	go func() {
		s, err := m.Run(context.Background())
		if err != nil {
			t.Error(err)
		}
		done <- s
	}()
	deadline := time.Now().Add(4 * time.Second)
	for {
		s, err := ReadMonitorStatus(m.Root)
		if err != nil {
			t.Fatal(err)
		}
		if s.Receiver.LastWireAt != nil {
			if s.Receiver.LastConnectedAt == nil || s.Receiver.Frames != 0 || s.Receiver.LastActivityAt != nil || !s.Receiver.ConnectionActive || s.Mode != "synthetic_x_monitor" {
				t.Fatal("heartbeat confused with event")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("status heartbeat missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := m.Run(t.Context()); err == nil || err.Error() != "already_running_or_locked" {
		t.Fatal("concurrent session was allowed")
	}
	if err := RequestMonitorStop(m.Root); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-done:
		if s.Running || s.Reason != "manual_stop" || s.Receiver.ConnectionActive || s.StoppedAt == nil {
			t.Fatalf("not stopped: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manual stop not acknowledged")
	}
	if err := RequestMonitorStop(m.Root); err == nil || err.Error() != "not_running" {
		t.Fatal("stopped session accepts stop request")
	}
	// The persistent file is an OS lock, not an existence sentinel.
	release, err := lockMonitor(filepath.Join(m.Root, "monitor.lock"))
	if err != nil {
		t.Fatal("lock not released")
	}
	release()
}

func TestMonitorNon200AndAttemptLimitPreserveFailure(t *testing.T) {
	for _, code := range []int{401, 402, 403, 429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls atomic.Int32
			m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(code)
				_, _ = w.Write([]byte("untrusted-response-body"))
			})
			defer closeServer()
			m.Config.MaxAttempts = 3
			var delays []time.Duration
			m.Sleep = func(ctx context.Context, d time.Duration) bool { delays = append(delays, d); return ctx.Err() == nil }
			s, err := m.Run(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			want := 3
			if code == 401 || code == 402 || code == 403 {
				want = 1
			}
			if int(calls.Load()) != want || s.Receiver.Notifications != 0 || s.Running || s.Receiver.ConnectionActive {
				t.Fatalf("unbounded/false success: %+v", s)
			}
			raw, _ := os.ReadFile(filepath.Join(m.Root, MonitorStatusFile))
			if strings.Contains(string(raw), "untrusted-response-body") {
				t.Fatal("response body leaked")
			}
			if want == 3 && (!strings.HasPrefix(s.Reason, "attempt_limit_") || s.Receiver.StopCondition != "attempt_limit") {
				t.Fatal("attempt exhaustion invisible")
			}
			if code == 429 && (len(delays) != 2 || delays[0] < time.Minute || delays[1] < 2*time.Minute) {
				t.Fatal("429 minimum backoff missing")
			}
			if code == 503 && (len(delays) != 2 || delays[0] < 5*time.Second || delays[1] < 10*time.Second) {
				t.Fatal("HTTP exponential backoff missing")
			}
		})
	}
}

func TestMonitorDisconnectDedupExpiredStrictSemanticsAndNotificationOutput(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	notice := SyntheticFrame(c, "monitor-notice", "960001", "Codex usage limits will be reset tomorrow.", now, now)
	old := SyntheticFrame(c, "monitor-old", "960002", "We just reset Codex limits.", now.Add(-48*time.Hour), now)
	question := SyntheticFrame(c, "monitor-question", "960003", "Have we just reset Codex limits?", now, now)
	recoveryClaim := SyntheticFrame(c, "monitor-local", "960004", "My local Codex quota recovered.", now, now)
	announcement := SyntheticFrame(c, "monitor-announced", "960005", "We just reset all Codex limits.", now, now)
	var calls atomic.Int32
	var mu sync.Mutex
	var reconnect bool
	var invalidTransition bool
	m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) {
		frames := [][]byte{notice}
		if calls.Add(1) > 1 {
			frames = [][]byte{notice, old, question, recoveryClaim, announcement}
		}
		for _, raw := range frames {
			_, _ = w.Write(raw)
			_, _ = w.Write([]byte("\r\n"))
		}
	})
	defer closeServer()
	m.Config.MaxAttempts = 3
	m.Config.MaxDeliveredFrames = 6
	m.OnTransition = func(s MonitorSnapshot) {
		mu.Lock()
		if s.UpdatedAt == 0 || s.State == "stopped" && (s.Running || s.StoppedAt == nil) {
			invalidTransition = true
		}
		mu.Unlock()
		if s.State == "reconnecting" {
			mu.Lock()
			reconnect = s.Reason == "disconnected" && s.Receiver.NextRetryAt != nil && !s.Receiver.ConnectionActive
			mu.Unlock()
		}
	}
	s, err := m.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	didReconnect := reconnect
	badTransition := invalidTransition
	mu.Unlock()
	if badTransition || !didReconnect || s.Receiver.Attempts != 2 || s.Receiver.Frames != 6 || s.DeliveredNotifications != 2 || s.Decisions["duplicate"] != 1 || s.Decisions["old_or_future"] != 1 || s.Decisions["ambiguous_or_unrelated"] != 2 || s.Reason != "frame_limit" {
		t.Fatalf("pipeline counters: %+v", s)
	}
	data, err := os.ReadFile(filepath.Join(m.Root, MonitorNoticeFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatal("incorrect delivery count")
	}
	for _, raw := range lines {
		var d Delivery
		if json.Unmarshal([]byte(raw), &d) != nil || d.Mode != "synthetic_x_replay" || d.Real != nil || d.Source != "" || d.Verification != "" || d.Notification == nil || d.Notification.Kind == "recovered" || !strings.Contains(d.Notification.Title, "联调") {
			t.Fatal("origin/recovery confusion")
		}
	}
}

func TestMonitorDedupAcrossRestartsAndStaleStatus(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	raw := SyntheticFrame(c, "monitor-same", "970001", "We just reset Codex limits.", now, now)
	m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	defer closeServer()
	m.Config.MaxDeliveredFrames = 1
	first, err := m.Run(t.Context())
	if err != nil || first.DeliveredNotifications != 1 {
		t.Fatal("initial output missing")
	}
	second, err := m.Run(t.Context())
	if err != nil || second.DeliveredNotifications != 0 || second.Decisions["duplicate"] != 1 || first.SessionID == second.SessionID {
		t.Fatal("restart dedup failed")
	}
	second.Running = true
	second.UpdatedAt = time.Now().Unix() - 10
	if err := writeMonitorJSON(filepath.Join(m.Root, MonitorStatusFile), second); err != nil {
		t.Fatal(err)
	}
	stale, err := ReadMonitorStatus(m.Root)
	if err != nil || stale.Running || stale.State != "status_stale" {
		t.Fatal("crashed/stale status reported as running")
	}
}

func TestMonitorDeadlineIncludesPreflightAndStatusPaths(t *testing.T) {
	c := ReplayConfig()
	c.BillingApproved = true
	c.BillingCycleLimitConfirmed = true
	c.MaxDurationSeconds = 1
	connector := &guardedLive{}
	m := Monitor{Config: c, Root: t.TempDir(), Connector: connector, Prepare: func(ctx context.Context) (Config, error) {
		<-ctx.Done()
		return Config{}, fmt.Errorf("preflight_cancelled_or_deadline")
	}}
	s, err := m.Run(t.Context())
	if err != nil || s.Reason != "time_limit" || s.Running || connector.opens != 0 {
		t.Fatal("preflight outlived budget")
	}
	m.Root = "relative-root"
	if _, err := m.Run(t.Context()); err == nil || err.Error() != "invalid_state_path" {
		t.Fatal("relative state accepted")
	}
}

func TestObservationBoundsAreSeparateFromOldTestAndRemainDisabled(t *testing.T) {
	c := ReplayConfig()
	c.MaxDurationSeconds = 86400
	if c.ValidateLiveTest() == nil || c.Validate() == nil {
		t.Fatal("old test accepted observation duration")
	}
	if err := c.ValidateObservation(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{
		func() Config { v := c; v.MaxDurationSeconds++; return v }(),
		func() Config { v := c; v.MaxDeliveredFrames = 21; return v }(),
		func() Config { v := c; v.MaxAttempts = 4; return v }(),
		func() Config { v := c; v.MaxWireBytes++; return v }(),
	} {
		if bad.ValidateObservation() == nil {
			t.Fatal("observation budget expanded")
		}
	}
	c.Enabled = false
	connector := &guardedLive{}
	s, err := (Monitor{Config: c, Observation: true, Root: filepath.Join(t.TempDir(), "not-created"), Connector: connector}).Run(t.Context())
	if err != nil || s.State != "disabled" || s.SessionPolicy != "bounded_observation" || connector.opens != 0 {
		t.Fatal("observation started without explicit enable")
	}
}

func TestObservationRuntimeIsBoundedAndInterruptibleWithoutX(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	raw := SyntheticFrame(c, "observation-frame", "980001", "Codex limits will be reset tomorrow.", now, now)
	m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	defer closeServer()
	m.Observation = true
	m.Config.MaxDurationSeconds = 86400
	m.Config.MaxDeliveredFrames = 1
	s, err := m.Run(t.Context())
	if err != nil || s.SessionPolicy != "bounded_observation" || s.Reason != "frame_limit" || s.Receiver.RealConnection || s.DeliveredNotifications != 1 {
		t.Fatal("observation event limit failed")
	}
	m.Connector = func() Connector {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		t.Cleanup(server.Close)
		connector, _ := NewLoopbackConnector(server.URL)
		return connector
	}()
	m.Root = t.TempDir()
	m.Config.MaxDurationSeconds = 1
	s, err = m.Run(t.Context())
	if err != nil || s.Reason != "time_limit" || s.Running {
		t.Fatal("observation deadline failed")
	}
}
