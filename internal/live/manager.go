package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/quota"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	inbox             *messageinbox.Store
	inboxError        string
	cloudQueue        []queuedSourceNotice
	mu                sync.Mutex
	root              string
	source            Source
	state             State
	previous          *quota.Snapshot
	seen              map[string]int64
	baseline          bool
	enabledAt         int64
	generation        uint64
	cancelSource      context.CancelFunc
	stickyOff         bool
	lastTestID        string
	changes           chan struct{}
	runtimeWarning    string
	preferenceWarning string
	sourceID          string
	recoveryContext   string
	refresh           func()
}

type queuedSourceNotice struct {
	Event        Event
	Notification Notification
}

func NewManager(root string, source Source) *Manager {
	preferences, _ := readPreferences(root)
	m := &Manager{root: root, source: source, seen: map[string]int64{}, changes: make(chan struct{}, 1), enabledAt: time.Now().Unix()}
	m.state = State{SchemaVersion: 1, Mode: "live_local", Quota: quota.Snapshot{State: "loading", Color: "gray"},
		Usage: quota.UnavailableUsage("not_checked"), Preferences: preferences,
		Reset: ResetState{State: "source_unavailable", SourceKind: "community_relay", SourceLabel: "第三方公开消息源", Reason: "not_checked"}}
	if descriptor, ok := source.(interface{ SourceDescriptor() (string, string) }); ok {
		m.state.Reset.SourceKind, m.state.Reset.SourceLabel = descriptor.SourceDescriptor()
	}
	inbox, errInbox := messageinbox.Open(root)
	m.inbox = inbox
	if errInbox != nil {
		m.inboxError = errInbox.Error()
	}
	if persisted, ok := source.(interface{ PersistentSourceID() string }); ok && m.inbox != nil && preferences.ResetAlertsEnabled && m.inbox.Initialized(persisted.PersistentSourceID()) {
		m.sourceID = persisted.PersistentSourceID()
		m.baseline = true
		m.enabledAt = time.Now().Add(-EventTTL).Unix()
	}
	if data, err := os.ReadFile(filepath.Join(root, "live-seen.json")); err == nil && len(data) <= 256000 {
		_ = json.Unmarshal(data, &m.seen)
		if m.seen == nil {
			m.seen = map[string]int64{}
		}
	}
	// A test command already on disk predates this worker's notification host.
	// Never replay it on startup, even if it is still within the 30-second TTL.
	if data, err := os.ReadFile(filepath.Join(root, "live-test-request.json")); err == nil && len(data) <= 1024 {
		var request testRequest
		if strictJSON(data, &request) == nil {
			m.lastTestID = request.ID
		}
	}
	return m
}
func (m *Manager) Changed() <-chan struct{} { return m.changes }
func (m *Manager) signal() {
	select {
	case m.changes <- struct{}{}:
	default:
	}
}
func (m *Manager) Snapshot() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}
func (m *Manager) snapshotLocked() State {
	return m.snapshotLockedAt(time.Now())
}
func (m *Manager) snapshotLockedAt(now time.Time) State {
	if m.inbox != nil {
		snapshot := m.inboxSnapshotLockedAt(now)
		m.state.Inbox = &snapshot
	}
	var out State
	data, _ := json.Marshal(m.state)
	_ = json.Unmarshal(data, &out)
	if out.Inbox != nil && out.Inbox.Source.ID == PublicResetSourcesID && out.Inbox.Source.State == "offline" && out.Reset.SourceKind == "public_aggregator" {
		out.Reset.SourceFresh = false
		out.Reset.Reason = out.Inbox.Source.Reason
		if out.Notification == nil && out.Reset.LocalRecovery == nil {
			out.Reset.State = "source_unavailable"
		}
	}
	out.generation = m.generation
	if m.runtimeWarning != "" {
		out.Quota.ConfigWarning = m.runtimeWarning
	}
	if m.preferenceWarning != "" {
		out.Quota.ConfigWarning = m.preferenceWarning
	}
	return out
}

// WorkerStates preserves every new claim when one feed contains several. The
// existing single-Notification wire DTO remains unchanged for desktop clients.
func (m *Manager) WorkerStates() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.snapshotLocked()
	if len(m.cloudQueue) == 0 {
		return []State{state}
	}
	states := make([]State, 0, len(m.cloudQueue))
	for _, item := range m.cloudQueue {
		out := state
		e, n := item.Event, item.Notification
		out.Reset.Event = &e
		out.Notification = &n
		out.Reset.State = "notice"
		if n.Kind == "announced" {
			out.Reset.State = "waiting_local"
		}
		states = append(states, out)
	}
	m.cloudQueue = nil
	return states
}
func (m *Manager) SetRuntimeWarning(value string) {
	m.mu.Lock()
	m.runtimeWarning = value
	m.signal()
	m.mu.Unlock()
}
func (m *Manager) SetRefresh(refresh func()) { m.mu.Lock(); m.refresh = refresh; m.mu.Unlock() }
func (m *Manager) clearRecoveryLocked() {
	m.state.Reset.LocalRecovery = nil
	m.recoveryContext = ""
	if n := m.state.Notification; n != nil && n.Kind == "recovered" {
		m.state.Notification = nil
	}
	if m.state.Reset.State == "recovered" {
		m.state.Reset.State = "source_unavailable"
		if m.state.Reset.SourceFresh {
			m.state.Reset.State = "waiting_local"
		}
	}
}
func (m *Manager) setEnabledLocked(enabled bool, now time.Time) {
	if m.state.Preferences.ResetAlertsEnabled == enabled {
		return
	}
	m.state.Preferences.ResetAlertsEnabled = enabled
	m.generation++
	m.baseline = false
	m.previous = nil
	m.clearRecoveryLocked()
	m.enabledAt = now.Unix()
	m.state.Notification = nil
	m.cloudQueue = nil
	if m.cancelSource != nil {
		m.cancelSource()
		m.cancelSource = nil
	}
	if !enabled {
		m.state.Reset.SourceFresh = false
		m.state.Reset.State = "source_unavailable"
		m.state.Reset.Reason = "alerts_disabled"
		if m.inbox != nil {
			health := m.inbox.Snapshot().Source
			health.State = "disabled"
			health.Reason = "alerts_disabled"
			_ = m.inbox.UpdateSource(health)
		}
	}
	m.signal()
}
func (m *Manager) SetAlerts(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !enabled {
		m.setEnabledLocked(false, time.Now())
		m.stickyOff = true
	}
	if err := atomicJSON(filepath.Join(m.root, "live-preferences.json"), Preferences{ResetAlertsEnabled: enabled}); err != nil {
		m.preferenceWarning = "提醒设置未保存。"
		if !enabled {
			m.preferenceWarning = "本次已关，重启后设置可能恢复。"
		}
		m.signal()
		return errors.New("preferences_not_saved")
	}
	m.preferenceWarning = ""
	m.stickyOff = false
	m.setEnabledLocked(enabled, time.Now())
	return nil
}
func (m *Manager) ReloadPreferences() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stickyOff {
		return
	}
	// Keep read/apply ordered with in-process toggle writes. An earlier disk
	// read must not race a newer explicit disable and temporarily turn it on.
	value, err := readPreferences(m.root)
	if err != nil {
		m.setEnabledLocked(false, time.Now())
		return
	}
	m.setEnabledLocked(value.ResetAlertsEnabled, time.Now())
}
func (m *Manager) SetUsage(value quota.UsageStatus) {
	m.mu.Lock()
	m.state.Usage = value
	m.signal()
	m.mu.Unlock()
}
func validQuota(s quota.Snapshot, now time.Time) bool {
	if s.State != "ready" || s.Demo || s.Stale || s.Partial || s.Remaining == nil || *s.Remaining < 0 || *s.Remaining > 100 || s.AccountKey == "" || s.Bucket == "" || s.UpdatedAt <= 0 || now.Unix()-s.UpdatedAt > 300 || s.UpdatedAt > now.Unix()+5 || len(s.Windows) == 0 {
		return false
	}
	for _, w := range s.Windows {
		if w.Duration == nil || *w.Duration <= 0 || w.Reset == nil || *w.Reset <= 0 || w.Remaining < 0 || w.Remaining > 100 {
			return false
		}
	}
	return true
}
func detectRecovery(before, after quota.Snapshot, now time.Time) *Recovery {
	if before.AccountKey != after.AccountKey || before.Bucket != after.Bucket || before.Scope != after.Scope || len(before.Windows) != len(after.Windows) || after.UpdatedAt <= before.UpdatedAt || after.UpdatedAt-before.UpdatedAt > 600 || *after.Remaining <= *before.Remaining {
		return nil
	}
	cycle := false
	increase := false
	for i, a := range after.Windows {
		b := before.Windows[i]
		if a.Name != b.Name || *a.Duration != *b.Duration || *a.Reset < *b.Reset {
			return nil
		}
		if a.Remaining > b.Remaining {
			increase = true
			if now.Unix() >= *b.Reset && *a.Reset > *b.Reset {
				cycle = true
			}
		}
	}
	if !increase {
		return nil
	}
	kind := "possible_extra"
	if cycle {
		kind = "normal_cycle"
	}
	return &Recovery{ID: randomID(), ObservedAt: after.UpdatedAt, Kind: kind, Summary: "本机额度已回升。仅依据此设备的前后读数，原因尚未确认。", PreviousObservedAt: before.UpdatedAt, BeforeRemaining: *before.Remaining, AfterRemaining: *after.Remaining}
}
func (m *Manager) ObserveQuota(s quota.Snapshot) { m.ObserveQuotaAt(s, time.Now()) }
func (m *Manager) ObserveQuotaAt(s quota.Snapshot, now time.Time) {
	s = copyQuota(s)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Quota = s
	m.signal()
	if s.State == "loading" {
		return
	}
	if !m.state.Preferences.ResetAlertsEnabled || !validQuota(s, now) {
		m.previous = nil
		m.clearRecoveryLocked()
		return
	}
	currentContext := s.AccountKey + ":" + s.Bucket
	if m.recoveryContext != "" && m.recoveryContext != currentContext {
		m.clearRecoveryLocked()
	}
	if m.previous != nil {
		if recovery := detectRecovery(*m.previous, s, now); recovery != nil {
			m.state.Reset.LocalRecovery = recovery
			m.recoveryContext = currentContext
			m.state.Reset.State = "recovered"
			m.state.Notification = &Notification{ID: recovery.ID, Kind: "recovered", Title: "本机额度已回升", Body: recovery.Summary, CreatedAt: now.Unix()}
		}
	}
	copy := s
	m.previous = &copy
}
func eventKey(source, id string) string {
	sum := sha256.Sum256([]byte(source + ":" + id))
	return hex.EncodeToString(sum[:])
}
func randomID() string { var buf [16]byte; _, _ = rand.Read(buf[:]); return hex.EncodeToString(buf[:]) }
func (m *Manager) ApplySource(batch SourceBatch, fetchErr error, generation uint64, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	readOnly := false
	if permitted, ok := m.source.(interface{ ReadWithoutAlerts() bool }); ok {
		readOnly = permitted.ReadWithoutAlerts() && batch.RecordOnly
	}
	if (!m.state.Preferences.ResetAlertsEnabled && !readOnly) || generation != m.generation {
		return
	}
	stamp := now.Unix()
	m.state.Reset.LastCheckedAt = &stamp
	m.signal()
	if fetchErr != nil {
		// Keep a same-source baseline across ordinary network failures.
		if m.state.Reset.SourceKind != "cloud_feed" && m.state.Reset.SourceKind != "synthetic_cloud" && m.state.Reset.SourceKind != "public_aggregator" {
			m.baseline = false
		}
		m.state.Reset.SourceFresh = false
		m.state.Notification = nil
		m.cloudQueue = nil
		m.state.Reset.State = "source_unavailable"
		m.state.Reset.Reason = fetchErr.Error()
		if m.inbox != nil && m.state.Reset.SourceKind == "public_aggregator" {
			health := m.inbox.Snapshot().Source
			if provider, ok := m.source.(interface {
				HealthSnapshot() messageinbox.SourceState
			}); ok {
				health = provider.HealthSnapshot()
			}
			health.State = "offline"
			health.Reason = fetchErr.Error()
			_ = m.inbox.UpdateSource(health)
		}
		return
	}
	if m.sourceID != batch.SourceID {
		m.baseline = false
		m.sourceID = batch.SourceID
		m.state.Reset.Event = nil
		m.state.Notification = nil
		m.cloudQueue = nil
	}
	if batch.SourceKind != "" {
		m.state.Reset.SourceKind = batch.SourceKind
		m.state.Reset.SourceLabel = batch.SourceLabel
	}
	if batch.CheckedAt != nil {
		m.state.Reset.LastCheckedAt = batch.CheckedAt
	}
	if err := m.applyInboxLocked(&batch, now); err != nil {
		m.state.Reset.State = "source_unavailable"
		m.state.Reset.SourceFresh = false
		m.state.Reset.Reason = err.Error()
		m.state.Notification = nil
		m.cloudQueue = nil
		return
	}
	fresh := batch.SourceID != "" && batch.UpstreamSuccessAt != nil && *batch.UpstreamSuccessAt > 0 && *batch.UpstreamSuccessAt <= stamp+int64(FutureTolerance/time.Second) && stamp-*batch.UpstreamSuccessAt <= int64(SourceTTL/time.Second)
	fresh = fresh && batch.UnavailableReason == ""
	// Cloud freshness is the validated feed TTL. The X connection has its own
	// preserved reason/last-success. A captured claim survives a finite X stop.
	if batch.SourceKind == "cloud_feed" || batch.SourceKind == "synthetic_cloud" {
		fresh = batch.CloudFeedAvailable
	}
	if batch.SourceKind == "public_aggregator" {
		fresh = batch.PublicFeedAvailable
	}
	m.state.Reset.SourceFresh = fresh
	m.state.Reset.LastSuccessAt = nil
	if batch.UpstreamSuccessAt != nil {
		value := *batch.UpstreamSuccessAt
		m.state.Reset.LastSuccessAt = &value
	}
	if !fresh {
		if batch.SourceKind != "cloud_feed" && batch.SourceKind != "synthetic_cloud" && batch.SourceKind != "public_aggregator" {
			m.baseline = false
		}
		m.state.Notification = nil
		m.cloudQueue = nil
		m.state.Reset.State = "source_unavailable"
		m.state.Reset.Reason = "upstream_freshness_unknown"
		if batch.UpstreamSuccessAt != nil {
			m.state.Reset.Reason = "upstream_stale"
		}
		if batch.UnavailableReason != "" {
			m.state.Reset.Reason = batch.UnavailableReason
		}
	} else {
		m.state.Reset.State = "waiting_local"
		m.state.Reset.Reason = ""
		if batch.UnavailableReason != "" {
			m.state.Reset.State = "source_unavailable"
			m.state.Reset.Reason = batch.UnavailableReason
		}
	}
	for _, id := range batch.SuppressedIDs {
		key := eventKey(batch.SourceID, id)
		if strings.HasPrefix(id, "post-") {
			key = messageinbox.NotificationID(strings.TrimPrefix(id, "post-"))
		} else if m.state.Reset.Event != nil && m.state.Reset.Event.ID == id {
			key = canonicalEventKey(batch.SourceID, *m.state.Reset.Event)
		}
		m.seen[key] = stamp
		if m.state.Reset.Event != nil && m.state.Reset.Event.ID == id {
			m.state.Reset.Event = nil
		}
		if m.state.Notification != nil && m.state.Notification.ID == key {
			m.state.Notification = nil
		}
		kept := m.cloudQueue[:0]
		for _, item := range m.cloudQueue {
			if item.Event.ID != id {
				kept = append(kept, item)
			}
		}
		m.cloudQueue = kept
	}
	if current := m.state.Reset.Event; current != nil && stamp-current.PublishedAt > int64(EventTTL/time.Second) {
		key := canonicalEventKey(batch.SourceID, *current)
		m.state.Reset.Event = nil
		if m.state.Notification != nil && m.state.Notification.ID == key {
			m.state.Notification = nil
		}
	}
	events := append([]Event(nil), batch.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].PublishedAt < events[j].PublishedAt })
	for _, event := range events {
		if !validEvent(event) || event.Verification == "author_claim" && (batch.SourceKind != "cloud_feed" && batch.SourceKind != "synthetic_cloud") {
			continue
		}
		copy := event
		m.state.Reset.Event = &copy
		key := canonicalEventKey(batch.SourceID, event)
		_, known := m.seen[key]
		if m.inbox != nil && strings.Contains(event.SourceURL, "/thsottiaux/status/") {
			if record, ok := m.inbox.Message(messageinbox.PostID(event.SourceURL)); ok && record.Delivery.State != "" && record.Delivery.State != "queued" {
				known = true
			}
		}
		if _, legacy := m.seen[eventKey(batch.SourceID, event.ID)]; legacy {
			known = true
		}
		if batch.SourceKind == "public_aggregator" && m.inbox != nil {
			if record, ok := m.inbox.Message(messageinbox.PostID(event.SourceURL)); ok {
				if record.Delivery.State == "queued" {
					known = false
					for _, pending := range m.cloudQueue {
						if pending.Notification.ID == key {
							known = true
						}
					}
				} else if record.Delivery.State != "" {
					known = true
				}
			}
		}
		eligible := fresh && m.baseline && !known && event.PublishedAt >= m.enabledAt && event.PublishedAt <= stamp+int64(FutureTolerance/time.Second) && stamp-event.PublishedAt <= int64(EventTTL/time.Second)
		if batch.SourceKind == "public_aggregator" {
			if eligible {
				if err := m.inboxDeliveryLocked(event, "queued", "validated_public_source_event", now); err != nil {
					m.state.Reset.Reason = "inbox_delivery_save_failed"
					m.state.Notification = nil
					m.cloudQueue = nil
					return
				}
			} else if !m.baseline {
				if err := m.inboxDeliveryLocked(event, "baseline", "initial_or_reenabled_history_silent", now); err != nil {
					m.state.Reset.Reason = "inbox_delivery_save_failed"
					return
				}
			} else if stamp-event.PublishedAt > int64(EventTTL/time.Second) {
				if record, ok := m.inbox.Message(messageinbox.PostID(event.SourceURL)); ok && (record.Delivery.State == "" || record.Delivery.State == "queued") {
					_ = m.inboxDeliveryLocked(event, "expired", "publication_ttl_expired", now)
				}
			}
		}
		// A stale feed may contain an unprocessed new event. Leave it eligible
		// for one delivery after this same upstream reconnects.
		if fresh || !m.baseline {
			m.seen[key] = stamp
		}
		// No correction relation/revision fields exist in the current contract.
		if eligible && event.Kind != "correction" {
			m.state.Reset.State = "notice"
			if event.Kind == "announced" {
				m.state.Reset.State = "waiting_local"
			}
			m.state.Notification = &Notification{ID: key, Kind: event.Kind, Title: "第三方重置动态", Body: event.Summary + "（第三方来源，未经独立核验）", CreatedAt: stamp}
			if batch.SourceKind == "cloud_feed" || batch.SourceKind == "synthetic_cloud" {
				m.state.Notification.Title = "作者重置公告"
				m.state.Notification.Body = event.Summary + " " + event.SourceURL + "（作者声明，本机恢复待核对）"
				if batch.SourceKind == "synthetic_cloud" {
					m.state.Notification.Title = "云公告联调（合成）"
				}
				m.cloudQueue = append(m.cloudQueue, queuedSourceNotice{Event: event, Notification: *m.state.Notification})
			}
			if batch.SourceKind == "public_aggregator" {
				m.state.Notification.Title = "Tibo公开消息 · 聚合来源"
				m.state.Notification.Body = event.Summary + " " + event.SourceURL
				m.cloudQueue = append(m.cloudQueue, queuedSourceNotice{Event: event, Notification: *m.state.Notification})
			}
			if event.Kind == "announced" && m.refresh != nil {
				m.refresh()
			}
		}
	}
	if batch.ResetProjection {
		m.applyEventNotificationsLocked(batch, now)
	}
	if fresh {
		m.baseline = true
		if batch.SourceKind == "public_aggregator" && m.inbox != nil {
			if m.inbox.MarkInitialized(batch.SourceID) != nil {
				m.state.Reset.Reason = "inbox_checkpoint_save_failed"
				m.state.Notification = nil
				m.cloudQueue = nil
			}
		}
	}
	for key, stampSeen := range m.seen {
		if stamp-stampSeen > 7*86400 {
			delete(m.seen, key)
		}
	}
	if len(m.seen) <= 2000 {
		_ = atomicJSON(filepath.Join(m.root, "live-seen.json"), m.seen)
	}
}
func (m *Manager) TestNotification() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.state.Preferences.ResetAlertsEnabled {
		return errors.New("alerts_disabled")
	}
	request := testRequest{ID: randomID(), CreatedAt: time.Now().Unix()}
	if err := atomicJSON(filepath.Join(m.root, "live-test-request.json"), request); err != nil {
		return errors.New("test_not_saved")
	}
	m.applyTestLocked(request, time.Now())
	return nil
}

type testRequest struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"createdAt"`
}

func (m *Manager) applyTestLocked(request testRequest, now time.Time) {
	if request.ID == m.lastTestID || len(request.ID) != 32 || request.CreatedAt < now.Unix()-30 || request.CreatedAt > now.Unix()+5 {
		return
	}
	m.lastTestID = request.ID
	if !m.state.Preferences.ResetAlertsEnabled || request.CreatedAt < m.enabledAt {
		return
	}
	m.state.Notification = &Notification{ID: request.ID, Kind: "test", Title: "测试提醒", Body: "这只是通知通路测试，不表示额度已恢复或发布了重置公告。", CreatedAt: request.CreatedAt}
	m.signal()
}
func (m *Manager) reloadTest() {
	data, err := os.ReadFile(filepath.Join(m.root, "live-test-request.json"))
	if err != nil || len(data) > 1024 {
		return
	}
	var request testRequest
	if strictJSON(data, &request) != nil {
		return
	}
	m.mu.Lock()
	m.applyTestLocked(request, time.Now())
	m.mu.Unlock()
}
func (m *Manager) Run(ctx context.Context, settings *quota.Store, realUsage bool) {
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.ReloadPreferences()
				m.reloadTest()
				m.mu.Lock()
				if n := m.state.Notification; n != nil && time.Now().Unix()-n.CreatedAt > 300 {
					m.state.Notification = nil
					m.signal()
				}
				m.mu.Unlock()
			}
		}
	})
	if realUsage {
		wg.Go(func() {
			timer := time.NewTimer(0)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
				m.SetUsage((quota.Provider{}).ReadUsage(ctx, settings.Get()))
				timer.Reset(10 * time.Minute)
			}
		})
	}
	wg.Go(func() { m.sourceLoop(ctx) })
	<-ctx.Done()
	wg.Wait()
}
func (m *Manager) sourceLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	next := time.Time{}
	failures := 0
	var priorGen uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		enabled := m.state.Preferences.ResetAlertsEnabled
		gen := m.generation
		source := m.source
		if gen != priorGen {
			next = time.Time{}
			failures = 0
			priorGen = gen
		}
		readWithoutAlerts := false
		if permitted, ok := source.(interface{ ReadWithoutAlerts() bool }); ok {
			readWithoutAlerts = permitted.ReadWithoutAlerts()
		}
		if (!enabled && !readWithoutAlerts) || source == nil || time.Now().Before(next) {
			m.mu.Unlock()
			continue
		}
		fetchCtx, cancel := context.WithTimeout(ctx, sourceFetchBudget(source))
		m.cancelSource = cancel
		m.mu.Unlock()
		var batch SourceBatch
		var err error
		if !enabled {
			if reader, ok := source.(interface {
				FetchWithoutAlerts(context.Context) (SourceBatch, error)
			}); ok {
				batch, err = reader.FetchWithoutAlerts(fetchCtx)
			} else {
				batch, err = source.Fetch(fetchCtx)
			}
		} else {
			batch, err = source.Fetch(fetchCtx)
		}
		cancel()
		if ctx.Err() != nil {
			return
		}
		m.ApplySource(batch, err, gen, time.Now())
		if err != nil {
			failures++
		} else {
			failures = 0
		}
		delay := SourceDelay(failures)
		if polled, ok := source.(interface{ PollInterval() time.Duration }); ok {
			delay = polled.PollInterval()
			if failures > 0 {
				shift := min(failures-1, 4)
				delay *= time.Duration(1 << shift)
				if delay > 15*time.Minute {
					delay = 15 * time.Minute
				}
			}
		}
		next = time.Now().Add(delay)
		if scheduled, ok := source.(interface{ NextPollTime() time.Time }); ok {
			if planned := scheduled.NextPollTime(); planned.After(time.Now()) {
				next = planned
			}
		}
	}
}
func SourceDelay(failures int) time.Duration {
	delay := 2 * time.Minute
	if failures > 0 {
		shift := failures - 1
		if shift > 3 {
			shift = 3
		}
		delay *= time.Duration(1 << shift)
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
	}
	return delay
}

func copyQuota(value quota.Snapshot) quota.Snapshot {
	if value.Remaining != nil {
		v := *value.Remaining
		value.Remaining = &v
	}
	value.Choices = append([]quota.Choice(nil), value.Choices...)
	value.Windows = append([]quota.Window(nil), value.Windows...)
	for i := range value.Windows {
		w := &value.Windows[i]
		if w.Duration != nil {
			v := *w.Duration
			w.Duration = &v
		}
		if w.Reset != nil {
			v := *w.Reset
			w.Reset = &v
		}
	}
	return value
}
