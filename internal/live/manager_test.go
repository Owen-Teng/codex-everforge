package live

import (
	"codex-quota-monitor/internal/quota"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var fixtureNow = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

func managerFixture(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir(), nil)
	if err := m.SetAlerts(true); err != nil {
		t.Fatal(err)
	}
	m.enabledAt = fixtureNow.Add(-time.Hour).Unix()
	return m
}
func fixtureEvent(id, kind string, stamp int64) Event {
	return Event{ID: id, Kind: kind, PublishedAt: stamp, SourceURL: "https://x.com/demo/status/1234567", Summary: "模拟公开源：重置动态。", Verification: "third_party"}
}
func freshBatch(events ...Event) SourceBatch {
	stamp := fixtureNow.Unix()
	return SourceBatch{SourceID: "fixture-only", Events: events, UpstreamSuccessAt: &stamp}
}
func apply(m *Manager, batch SourceBatch) { m.ApplySource(batch, nil, m.generation, fixtureNow) }
func fixtureQuota(remaining int, stamp int64) quota.Snapshot {
	minutes := int64(10080)
	reset := fixtureNow.Add(time.Hour).Unix()
	return quota.Snapshot{State: "ready", Remaining: &remaining, Bucket: "codex", Scope: "7 天窗口", UpdatedAt: stamp, AccountKey: "synthetic-account-a", Windows: []quota.Window{{Name: "7 天窗口", Remaining: remaining, Duration: &minutes, Reset: &reset}}}
}
func TestSourceBaselineDedupAndAnnouncements(t *testing.T) {
	m := managerFixture(t)
	stamp := fixtureNow.Unix()
	first := fixtureEvent("old", "notice", stamp-300)
	apply(m, freshBatch(first))
	if m.Snapshot().Notification != nil {
		t.Fatal("first snapshot notified")
	}
	var refreshes atomic.Int32
	m.SetRefresh(func() { refreshes.Add(1) })
	second := fixtureEvent("new", "announced", stamp-1)
	apply(m, freshBatch(first, second))
	state := m.Snapshot()
	if state.Notification == nil || state.Notification.Kind != "announced" || state.Reset.Event.Verification != "third_party" || refreshes.Load() != 1 {
		t.Fatal("new announcement not handled")
	}
	id := state.Notification.ID
	apply(m, freshBatch(first, second))
	if m.Snapshot().Notification.ID != id || refreshes.Load() != 1 {
		t.Fatal("duplicate announcement replayed")
	}
	restarted := NewManager(m.root, nil)
	restarted.enabledAt = m.enabledAt
	apply(restarted, freshBatch(first, second))
	apply(restarted, freshBatch(first, second))
	if restarted.Snapshot().Notification != nil {
		t.Fatal("restart replayed old event")
	}
}
func TestFreshnessIndependentFromEventAge(t *testing.T) {
	m := managerFixture(t)
	old := fixtureEvent("old", "notice", fixtureNow.Add(-48*time.Hour).Unix())
	apply(m, freshBatch(old))
	if !m.Snapshot().Reset.SourceFresh || m.Snapshot().Notification != nil {
		t.Fatal("no new event incorrectly means source unhealthy")
	}
	apply(m, SourceBatch{SourceID: "fixture-only", Events: []Event{old}})
	if m.Snapshot().Reset.SourceFresh || m.Snapshot().Reset.Reason != "upstream_freshness_unknown" {
		t.Fatal("HTTP/fetch success treated as upstream proof")
	}
	stale := fixtureNow.Add(-SourceTTL - time.Second).Unix()
	batch := freshBatch(old)
	batch.UpstreamSuccessAt = &stale
	apply(m, batch)
	if m.Snapshot().Reset.SourceFresh || m.Snapshot().Reset.Reason != "upstream_stale" {
		t.Fatal("old upstream check accepted")
	}
}
func TestOldFutureWeakAndUnrelatedCorrectionsDoNotNotify(t *testing.T) {
	for _, event := range []Event{
		fixtureEvent("too-old", "notice", fixtureNow.Add(-25*time.Hour).Unix()),
		fixtureEvent("future", "announced", fixtureNow.Add(6*time.Minute).Unix()),
		fixtureEvent("correction", "correction", fixtureNow.Unix()),
		fixtureEvent("weak", "product_update", fixtureNow.Unix()),
	} {
		t.Run(event.ID, func(t *testing.T) {
			m := managerFixture(t)
			apply(m, freshBatch())
			apply(m, freshBatch(event))
			if m.Snapshot().Notification != nil {
				t.Fatal("ineligible event notified")
			}
		})
	}
}
func TestSourceFailureRecoveryAndSourceSwitchAreSilent(t *testing.T) {
	m := managerFixture(t)
	apply(m, freshBatch())
	m.ApplySource(SourceBatch{}, errors.New("source_network"), m.generation, fixtureNow)
	apply(m, freshBatch(fixtureEvent("after-outage", "notice", fixtureNow.Unix())))
	if m.Snapshot().Notification != nil {
		t.Fatal("outage backlog notified")
	}
	batch := freshBatch(fixtureEvent("other-source", "notice", fixtureNow.Unix()))
	batch.SourceID = "another-fixture"
	apply(m, batch)
	if m.Snapshot().Notification != nil {
		t.Fatal("new source failed to baseline")
	}
}
func TestDisableRejectsInflightAndReenableBaselines(t *testing.T) {
	m := managerFixture(t)
	apply(m, freshBatch())
	generation := m.generation
	if err := m.SetAlerts(false); err != nil {
		t.Fatal(err)
	}
	m.ApplySource(freshBatch(fixtureEvent("late", "announced", fixtureNow.Unix())), nil, generation, fixtureNow)
	if m.Snapshot().Notification != nil || m.Snapshot().Preferences.ResetAlertsEnabled {
		t.Fatal("late result escaped shutdown")
	}
	if err := m.SetAlerts(true); err != nil {
		t.Fatal(err)
	}
	m.enabledAt = fixtureNow.Add(-time.Hour).Unix()
	apply(m, freshBatch(fixtureEvent("re-enabled", "notice", fixtureNow.Unix())))
	if m.Snapshot().Notification != nil {
		t.Fatal("reenable replayed baseline")
	}
}
func TestSharedPreferencesAndTestRequestPropagation(t *testing.T) {
	root := t.TempDir()
	web := NewManager(root, nil)
	native := NewManager(root, nil)
	if web.Snapshot().Preferences.ResetAlertsEnabled || native.Snapshot().Preferences.ResetAlertsEnabled {
		t.Fatal("default on")
	}
	if web.SetAlerts(true) != nil {
		t.Fatal("enable failed")
	}
	native.ReloadPreferences()
	if !native.Snapshot().Preferences.ResetAlertsEnabled {
		t.Fatal("worker did not share preference")
	}
	if web.TestNotification() != nil {
		t.Fatal("test request failed")
	}
	native.reloadTest()
	first := native.Snapshot().Notification
	if first == nil || first.Kind != "test" {
		t.Fatal("test not propagated")
	}
	if len(native.seen) != 0 || native.Snapshot().Reset.Event != nil || native.Snapshot().Reset.LocalRecovery != nil {
		t.Fatal("test polluted production state")
	}
	native.reloadTest()
	if native.Snapshot().Notification.ID != first.ID {
		t.Fatal("same test ID replayed")
	}
	if web.SetAlerts(false) != nil {
		t.Fatal("disable failed")
	}
	native.ReloadPreferences()
	native.reloadTest()
	if native.Snapshot().Preferences.ResetAlertsEnabled || native.Snapshot().Notification != nil {
		t.Fatal("web close did not stop worker")
	}
	restarted := NewManager(root, nil)
	if restarted.Snapshot().Preferences.ResetAlertsEnabled {
		t.Fatal("upgrade reset choice")
	}
}
func TestOldTestRequestsAreNotReplayed(t *testing.T) {
	m := managerFixture(t)
	m.applyTestLocked(testRequest{ID: strings.Repeat("1", 32), CreatedAt: time.Now().Add(-time.Hour).Unix()}, time.Now())
	if m.Snapshot().Notification != nil {
		t.Fatal("old test request replayed")
	}
	if m.TestNotification() != nil {
		t.Fatal("fixture test request failed")
	}
	restarted := NewManager(m.root, nil)
	restarted.reloadTest()
	if restarted.Snapshot().Notification != nil {
		t.Fatal("startup replayed a recent test request")
	}
}
func TestDisableSaveFailureStaysOffInSession(t *testing.T) {
	m := managerFixture(t)
	savedRoot := m.root
	blocked := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocked, []byte("x"), 0600)
	m.root = blocked
	if m.SetAlerts(false) == nil {
		t.Fatal("save failure not reported")
	}
	m.root = savedRoot
	m.ReloadPreferences()
	state := m.Snapshot()
	if state.Preferences.ResetAlertsEnabled || state.Notification != nil || !strings.Contains(state.Quota.ConfigWarning, "本次已关") {
		t.Fatal("failed save resumed notifications")
	}
}
func TestRecoverySameContextAndNormalCycle(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(map[bool]string{false: "possible_extra", true: "normal_cycle"}[cycle], func(t *testing.T) {
			m := managerFixture(t)
			before := fixtureQuota(0, fixtureNow.Add(-time.Minute).Unix())
			after := fixtureQuota(80, fixtureNow.Unix())
			if cycle {
				old := fixtureNow.Add(-time.Second).Unix()
				before.Windows[0].Reset = &old
			}
			m.ObserveQuotaAt(before, fixtureNow.Add(-time.Minute))
			m.ObserveQuotaAt(after, fixtureNow)
			state := m.Snapshot()
			if state.Reset.LocalRecovery == nil || state.Reset.LocalRecovery.Kind != map[bool]string{false: "possible_extra", true: "normal_cycle"}[cycle] || state.Reset.LocalRecovery.BeforeRemaining != 0 || state.Notification.Kind != "recovered" {
				t.Fatal("valid recovery not classified")
			}
		})
	}
}
func TestRecoveryNeverCrossesContextsOrInvalidObservations(t *testing.T) {
	for _, mode := range []string{"account", "bucket", "partial", "failure", "missing-identity", "stale", "different-window", "missing-window"} {
		t.Run(mode, func(t *testing.T) {
			m := managerFixture(t)
			before := fixtureQuota(0, fixtureNow.Add(-time.Minute).Unix())
			after := fixtureQuota(80, fixtureNow.Unix())
			m.ObserveQuotaAt(before, fixtureNow.Add(-time.Minute))
			switch mode {
			case "account":
				after.AccountKey = "synthetic-account-b"
			case "bucket":
				after.Bucket = "other"
			case "partial":
				after.Partial = true
			case "failure":
				after.State = "error"
			case "missing-identity":
				after.AccountKey = ""
			case "stale":
				after.Stale = true
			case "different-window":
				duration := int64(300)
				after.Windows[0].Duration = &duration
			case "missing-window":
				after.Windows = nil
			}
			m.ObserveQuotaAt(after, fixtureNow)
			if m.Snapshot().Reset.LocalRecovery != nil || m.Snapshot().Notification != nil {
				t.Fatal("untrusted recovery inferred")
			}
		})
	}
}
func TestOldRecoveryClearedOnContextLossAndToggle(t *testing.T) {
	for _, mode := range []string{"account", "bucket", "error", "partial", "toggle"} {
		t.Run(mode, func(t *testing.T) {
			m := managerFixture(t)
			m.ObserveQuotaAt(fixtureQuota(0, fixtureNow.Add(-time.Minute).Unix()), fixtureNow.Add(-time.Minute))
			m.ObserveQuotaAt(fixtureQuota(80, fixtureNow.Unix()), fixtureNow)
			if m.Snapshot().Reset.LocalRecovery == nil {
				t.Fatal("fixture needs recovery")
			}
			after := fixtureQuota(90, fixtureNow.Add(time.Minute).Unix())
			switch mode {
			case "account":
				after.AccountKey = "other"
			case "bucket":
				after.Bucket = "other"
			case "error":
				after.State = "error"
			case "partial":
				after.Partial = true
			case "toggle":
				_ = m.SetAlerts(false)
				_ = m.SetAlerts(true)
			}
			if mode != "toggle" {
				m.ObserveQuotaAt(after, fixtureNow.Add(time.Minute))
			}
			state := m.Snapshot()
			if state.Reset.LocalRecovery != nil || state.Notification != nil && state.Notification.Kind == "recovered" {
				t.Fatal("old account recovery leaked")
			}
		})
	}
}
func TestQuotaAndIdentityDoNotPersistOrEnterSourceState(t *testing.T) {
	m := managerFixture(t)
	q := fixtureQuota(13, fixtureNow.Unix())
	q.AccountKey = "private-account-fingerprint-marker"
	m.ObserveQuotaAt(q, fixtureNow)
	apply(m, freshBatch(fixtureEvent("notice", "notice", fixtureNow.Unix())))
	if *m.Snapshot().Quota.Remaining != 13 {
		t.Fatal("source changed quota")
	}
	serialized, _ := json.Marshal(m.Snapshot())
	if strings.Contains(string(serialized), q.AccountKey) {
		t.Fatal("identity serialized")
	}
	files, _ := os.ReadDir(m.root)
	for _, file := range files {
		data, _ := os.ReadFile(filepath.Join(m.root, file.Name()))
		for _, name := range []string{"private-account", "remaining", "dailyUsage", "quota", "tokens", "email"} {
			if strings.Contains(string(data), name) {
				t.Fatal("private data persisted")
			}
		}
	}
}
func TestBackoffIsBounded(t *testing.T) {
	if SourceDelay(0) != 2*time.Minute || SourceDelay(3) != 8*time.Minute || SourceDelay(100) != 15*time.Minute {
		t.Fatal("source backoff wrong")
	}
}

type countingSource struct{ calls atomic.Int32 }

func (s *countingSource) Fetch(ctx context.Context) (SourceBatch, error) {
	s.calls.Add(1)
	return SourceBatch{}, errors.New("fixture-unavailable")
}
func TestSourcePollingOffAndShutdown(t *testing.T) {
	source := &countingSource{}
	m := NewManager(t.TempDir(), source)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.sourceLoop(ctx); close(done) }()
	time.Sleep(1100 * time.Millisecond)
	if source.calls.Load() != 0 {
		t.Fatal("disabled source polled")
	}
	if m.SetAlerts(true) != nil {
		t.Fatal("enable failed")
	}
	time.Sleep(1100 * time.Millisecond)
	if source.calls.Load() != 1 {
		t.Fatal("enabled source did not read once")
	}
	_ = m.SetAlerts(false)
	time.Sleep(1100 * time.Millisecond)
	if source.calls.Load() != 1 {
		t.Fatal("disabled source continued")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("source loop did not stop")
	}
}
