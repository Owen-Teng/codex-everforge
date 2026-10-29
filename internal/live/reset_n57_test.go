package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/quota"
	"codex-quota-monitor/internal/reset"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func n57Posts(t *testing.T, now time.Time) []messageinbox.Message {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "community", "backend", "n57", "day23-public-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Posts []messageinbox.Message `json:"posts"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	// Keep the real wording/references; translate all times equally for finite
	// eligibility tests that must remain reproducible on a later calendar date.
	delta := now.Unix() - 3600 - *fixture.Posts[2].PublishedAt
	for i := range fixture.Posts {
		p := &fixture.Posts[i]
		at := *p.PublishedAt + delta
		p.PublishedAt = &at
		p.FirstReceivedAt = now.Unix() - 600
		p.LastSeenAt = now.Unix()
	}
	return fixture.Posts
}
func n57PublicBatch(now time.Time, posts []messageinbox.Message) SourceBatch {
	at := now.Unix()
	h := messageinbox.SourceState{ID: AIHOTSourceID, State: "available", LastSuccessAt: &at}
	return SourceBatch{SourceID: PublicResetSourcesID, SourceKind: "public_aggregator", ResetProjection: true, PublicFeedAvailable: true, Sync: &messageinbox.SourceState{ID: PublicResetSourcesID, State: "available"}, Segments: []SourceBatch{{Sync: &h, Posts: posts, PublicFeedAvailable: true}}}
}
func n57LegacyCache(t *testing.T, root string, posts []messageinbox.Message, initialized bool, stageReceipt, postReceipt string) {
	t.Helper()
	rows := map[string]messageinbox.Message{}
	for _, p := range posts {
		p.Category = "related"
		if p.PostID == posts[2].PostID {
			p.Delivery.State = postReceipt
		}
		rows[p.PostID] = p
	}
	id := reset.NotificationID(posts[2].PostID + ":author_completed")
	receipts := map[string]reset.Delivery{}
	if stageReceipt != "" {
		receipts[id] = reset.Delivery{State: stageReceipt, Reason: "existing_receipt"}
	}
	if err := messageinbox.SaveJSON(filepath.Join(root, messageinbox.FileName), map[string]any{"version": 1, "revision": 9, "messages": rows, "initialized": map[string]bool{PublicResetSourcesID: initialized, "events:" + AIHOTSourceID: initialized}, "eventDeliveries": receipts}); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(filepath.Join(root, "live-preferences.json"), Preferences{true}); err != nil {
		t.Fatal(err)
	}
}
func TestN57CachedRelatedCatchupQueueAckAndRestart(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	posts := n57Posts(t, now)
	n57LegacyCache(t, root, posts, true, "", "")
	src := &PublicResetSources{}
	m := NewManager(root, src)
	b := n57PublicBatch(now, nil)
	if m.Snapshot().Notification != nil {
		t.Fatal("constructor emitted before baseline frame")
	}
	m.ApplySource(b, nil, m.generation, now)
	m.ApplySource(b, nil, m.generation, now)
	if len(m.cloudQueue) != 1 {
		t.Fatal("duplicate pending stage queued")
	}
	states := m.WorkerStates()
	if len(states) != 1 || states[0].Notification == nil || states[0].Notification.Kind != "announced" || states[0].Reset.Event.PublishedAt != *posts[2].PublishedAt || states[0].Reset.LocalRecovery != nil {
		t.Fatalf("missing public catchup %+v", states)
	}
	id := states[0].Notification.ID
	delivery, _ := m.inbox.FindEventDelivery(id)
	if delivery.State != "queued" || delivery.QueuedAt == nil || *delivery.QueuedAt != now.Unix() || delivery.EmittedAt != nil || delivery.ResultAt != nil {
		t.Fatal("invented historical emission", delivery)
	}
	// A crash before stdout leaves the queue eligible exactly once on restart.
	m = NewManager(root, src)
	m.ApplySource(b, nil, m.generation, now)
	states = m.WorkerStates()
	if len(states) != 1 || states[0].Notification == nil || states[0].Notification.ID != id {
		t.Fatal("queued restart lost")
	}
	m.NotificationEmitted(id)
	if err := m.RecordNotificationResult(id, "submitted", "host_accepted_visibility_unknown"); err != nil {
		t.Fatal(err)
	}
	m = NewManager(root, src)
	m.ApplySource(b, nil, m.generation, now)
	if m.Snapshot().Notification != nil || len(m.cloudQueue) != 0 {
		t.Fatal("submitted stage replayed")
	}
	delivery, _ = m.inbox.FindEventDelivery(id)
	if delivery.State != "submitted" || delivery.EmittedAt == nil || delivery.ResultAt == nil {
		t.Fatal("receipt incomplete", delivery)
	}
}
func TestN57CatchupRespectsBaselineExpiryReceiptsAndReenable(t *testing.T) {
	for _, mode := range []string{"first-install", "expired", "reenabled", "baseline", "submitted", "worker_emitted", "failed", "unknown", "post-baseline", "post-submitted"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			root := t.TempDir()
			posts := n57Posts(t, now)
			stageReceipt, postReceipt := "", ""
			if mode == "expired" {
				for i := range posts {
					at := *posts[i].PublishedAt - 25*3600
					posts[i].PublishedAt = &at
				}
			}
			if strings.HasPrefix(mode, "post-") {
				postReceipt = strings.TrimPrefix(mode, "post-")
			} else if mode != "first-install" && mode != "expired" && mode != "reenabled" {
				stageReceipt = mode
			}
			n57LegacyCache(t, root, posts, mode != "first-install", stageReceipt, postReceipt)
			m := NewManager(root, &PublicResetSources{})
			if mode == "reenabled" {
				if m.SetAlerts(false) != nil || m.SetAlerts(true) != nil {
					t.Fatal("toggle temp fixture")
				}
			}
			m.ApplySource(n57PublicBatch(now, nil), nil, m.generation, now)
			if m.Snapshot().Notification != nil || len(m.cloudQueue) != 0 {
				t.Fatal("silent history notified")
			}
		})
	}
}
func TestN57WithdrawalCancelsPendingCompletion(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	posts := n57Posts(t, now)
	n57LegacyCache(t, root, posts, true, "", "")
	m := NewManager(root, &PublicResetSources{})
	m.ApplySource(n57PublicBatch(now, nil), nil, m.generation, now)
	if len(m.cloudQueue) != 1 {
		t.Fatal("missing queue")
	}
	id := m.cloudQueue[0].Notification.ID
	posts[0].Status = "withdrawn"
	m.ApplySource(n57PublicBatch(now, posts[:1]), nil, m.generation, now)
	if m.Snapshot().Notification != nil || len(m.cloudQueue) != 0 {
		t.Fatal("withdrawal retained pending")
	}
	d, _ := m.inbox.FindEventDelivery(id)
	if d.State != "withdrawn" || d.EmittedAt != nil {
		t.Fatal("withdrawn receipt wrong", d)
	}
}
func TestN57RecoveryDeadlineIncreaseAndObservationGapAudit(t *testing.T) {
	for _, mode := range []string{"direct", "loading", "unknown-error"} {
		t.Run(mode, func(t *testing.T) {
			m := managerFixture(t)
			before := fixtureQuota(71, fixtureNow.Add(-120*time.Second).Unix())
			after := fixtureQuota(99, fixtureNow.Unix())
			at := *before.Windows[0].Reset + 172800
			after.Windows[0].Reset = &at
			m.ObserveQuotaAt(before, fixtureNow.Add(-120*time.Second))
			if mode == "loading" {
				m.ObserveQuotaAt(quota.Snapshot{State: "loading"}, fixtureNow.Add(-60*time.Second))
			}
			if mode == "unknown-error" {
				m.ObserveQuotaAt(quota.Snapshot{State: "error"}, fixtureNow.Add(-60*time.Second))
			}
			m.ObserveQuotaAt(after, fixtureNow)
			recovery := m.Snapshot().Reset.LocalRecovery
			if mode == "unknown-error" {
				if recovery != nil {
					t.Fatal("unknown context bridged")
				}
				return
			}
			if recovery == nil || recovery.Kind != "possible_extra" || recovery.BeforeRemaining != 71 || recovery.AfterRemaining != 99 {
				t.Fatal("future deadline increase incorrectly rejected", recovery)
			}
		})
	}
}
