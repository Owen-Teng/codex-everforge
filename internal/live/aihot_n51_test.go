package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func realAIHOTFixture(t *testing.T) []byte {
	t.Skip("Private external capture excluded from public source; synthetic regressions remain enabled.")
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "community", "backend", "night-n51", "aihot-recent-real.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestN51RealFixtureRetainsDay21WithoutAggregateConfirmation(t *testing.T) {
	raw := realAIHOTFixture(t)
	batch, err := ParseAIHOT(raw, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := messageinbox.Open(t.TempDir())
	if err = s.Ingest(batch.Posts, *batch.Sync, batch.HistoryFrom, time.Now()); err != nil {
		t.Fatal(err)
	}
	day, ok := s.Message("2114588174249071102")
	if !ok || day.Category != "signal" || day.AggregateStatus != "likely_completed" || day.PublishedAt == nil || !strings.Contains(day.OriginalText, "But today is a reset.") {
		t.Fatal("Day21 lost or estimated completion promoted")
	}
	if len(s.MessagesFor(batch.Posts)) < 18 {
		t.Fatal("related/context posts discarded")
	}
	parent, _ := s.Message("2114200152701575656")
	if parent.PublishedAt != nil {
		t.Fatal("context without publication time gained fabricated time")
	}
	if batch.Sync.UpstreamCheckedAt == nil || *batch.Sync.UpstreamCheckedAt == time.Now().Unix() {
		t.Fatal("collection clock substituted for upstream scan")
	}
}
func TestN51ETag600SecondsRestart304AndRetryAfter(t *testing.T) {
	raw := realAIHOTFixture(t)
	var requests atomic.Int32
	var response atomic.Int32
	response.Store(200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if n > 1 && r.Header.Get("If-None-Match") != "fixture-etag" {
			t.Error("missing ETag")
		}
		if response.Load() == 429 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
			return
		}
		if n > 1 {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "fixture-etag")
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	root := t.TempDir()
	source, _ := newAIHOTSource(root)
	now := time.Now()
	source.now = func() time.Time { return now }
	source.endpoint = server.URL
	first, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	upstream := *first.Sync.UpstreamCheckedAt
	now = now.Add(599 * time.Second)
	if _, err = source.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("manual refresh bypassed interval")
	}
	restarted, err := newAIHOTSource(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	restarted.endpoint = server.URL
	if _, err = restarted.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("restart bypassed interval")
	}
	now = now.Add(time.Second)
	second, err := restarted.Fetch(context.Background())
	if err != nil || requests.Load() != 2 {
		t.Fatal("304 failed")
	}
	if *second.Sync.UpstreamCheckedAt != upstream || *second.Sync.LastSuccessAt == upstream {
		t.Fatal("304 forged X/upstream heartbeat")
	}
	now = now.Add(600 * time.Second)
	response.Store(429)
	if _, err = restarted.Fetch(context.Background()); err == nil {
		t.Fatal("429 hidden")
	}
	after, _ := newAIHOTSource(root)
	after.now = func() time.Time { return now.Add(3599 * time.Second) }
	after.endpoint = server.URL
	if _, err = after.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 3 || after.cache.NextAllowedAt < now.Unix()+3600 {
		t.Fatal("Retry-After not durable")
	}
}
func TestN51InternalSourceAbsentDisabledAndStrictConfig(t *testing.T) {
	root := t.TempDir()
	source, err := LoadInternalPublicSource(root)
	if err != nil || source != nil {
		t.Fatal("release default enabled internal source")
	}
	for _, raw := range []string{`{"schemaVersion":1,"enabled":false}`, `{"schemaVersion":1,"enabled":true,"source":"aihot_internal","internalUseAccepted":false}`, `{"schemaVersion":1,"enabled":true,"source":"aihot_internal","internalUseAccepted":true,"url":"https://evil.invalid"}`} {
		_ = os.WriteFile(filepath.Join(root, AIHOTConfigFile), []byte(raw), 0600)
		source, err = LoadInternalPublicSource(root)
		if strings.Contains(raw, `"enabled":false`) {
			if err != nil || source != nil {
				t.Fatal("disabled config active")
			}
		} else if err == nil {
			t.Fatal("invalid/internal use gate missing")
		}
	}
	_ = os.WriteFile(filepath.Join(root, AIHOTConfigFile), []byte(`{"schemaVersion":1,"enabled":true,"source":"aihot_internal","internalUseAccepted":true}`), 0600)
	source, err = LoadInternalPublicSource(root)
	if err != nil || source == nil || source.endpoint != AIHOTURL {
		t.Fatal("explicit local config not usable")
	}
}
func publicBatch(now time.Time, posts ...messageinbox.Message) SourceBatch {
	stamp := now.Unix()
	return SourceBatch{SourceID: AIHOTSourceID, SourceKind: "public_aggregator", SourceLabel: "AIHOT 公开聚合（本机内部评估）", PublicFeedAvailable: true, UpstreamSuccessAt: &stamp, Posts: posts, Sync: &messageinbox.SourceState{ID: AIHOTSourceID, State: "available", LastAttemptAt: &stamp, LastSuccessAt: &stamp, Origin: "synthetic_test_only"}}
}
func publicPost(id, text string, at time.Time) messageinbox.Message {
	stamp := at.Unix()
	return messageinbox.Message{PostID: id, URL: "https://x.com/thsottiaux/status/" + id, Author: "thsottiaux", PublishedAt: &stamp, OriginalText: text, TextCompleteness: "full", Verification: "aggregated_source_quote", Status: "active"}
}

type publicIdentitySource struct{}

func (*publicIdentitySource) Fetch(context.Context) (SourceBatch, error) {
	return SourceBatch{}, context.Canceled
}
func (*publicIdentitySource) SourceDescriptor() (string, string) {
	return "public_aggregator", "AIHOT 测试身份"
}
func (*publicIdentitySource) PersistentSourceID() string { return AIHOTSourceID }
func TestN51SilentHistoryOutageQueueReceiptsRestartAndCrossSource(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	m := NewManager(root, &publicIdentitySource{})
	_ = m.SetAlerts(true)
	m.enabledAt = now.Add(-time.Hour).Unix()
	old := publicPost("71001", "Day 20/ Rest and reset", now.Add(-time.Minute))
	m.ApplySource(publicBatch(now, old), nil, m.generation, now)
	if m.Snapshot().Notification != nil || len(m.InboxSnapshot().Messages) != 1 {
		t.Fatal("historical first read notified or discarded")
	}
	m.ApplySource(SourceBatch{}, context.DeadlineExceeded, m.generation, now)
	p1 := publicPost("71002", "Day 21/ Wonder what happens the remaining seven days. But today is a reset.", now.Add(-time.Second))
	p2 := publicPost("71003", "We just reset Codex limits.", now)
	p3 := publicPost("71004", "Codex limits will be reset tomorrow.", now)
	m.ApplySource(publicBatch(now, old, p1, p2, p3), nil, m.generation, now)
	states := m.WorkerStates()
	if len(states) != 3 {
		t.Fatalf("reconnect queue count: %d", len(states))
	}
	ids := map[string]bool{}
	for _, state := range states {
		if state.Notification == nil || state.Notification.Kind == "recovered" || ids[state.Notification.ID] {
			t.Fatal("invalid/duplicate source notice")
		}
		ids[state.Notification.ID] = true
		m.NotificationEmitted(state.Notification.ID)
		if err := m.RecordNotificationResult(state.Notification.ID, "submitted", "windows_shell_accepted_visibility_unknown"); err != nil {
			t.Fatal(err)
		}
	}
	m.ApplySource(publicBatch(now, old, p1, p2, p3), nil, m.generation, now)
	if len(m.cloudQueue) != 0 {
		t.Fatal("same posts replayed")
	}
	resumed := NewManager(root, &publicIdentitySource{})
	later := publicPost("71005", "Day 22/ Today is a reset.", now.Add(-10*time.Second))
	resumed.ApplySource(publicBatch(now, old, p1, p2, p3, later), nil, resumed.generation, now)
	if len(resumed.cloudQueue) != 1 || messageinbox.PostID(resumed.cloudQueue[0].Event.SourceURL) != "71005" {
		t.Fatal("restart swallowed offline catchup or replayed handled posts")
	}
	resumed.NotificationEmitted(resumed.cloudQueue[0].Notification.ID)
	_ = resumed.WorkerStates()
	fresh := now.Unix()
	resumed.ApplySource(SourceBatch{SourceID: "other-cloud", SourceKind: "cloud_feed", CloudFeedAvailable: true, UpstreamSuccessAt: &fresh}, nil, resumed.generation, now)
	resumed.ApplySource(SourceBatch{SourceID: "other-cloud", SourceKind: "cloud_feed", CloudFeedAvailable: true, UpstreamSuccessAt: &fresh, Events: []Event{{ID: "different-event-id", Kind: "notice", PublishedAt: now.Unix(), SourceURL: p1.URL, Summary: "来源公告。", Verification: "author_claim"}}}, nil, resumed.generation, now)
	if len(resumed.cloudQueue) != 0 {
		t.Fatal("cross-source same PostID alerted twice")
	}
}
func TestN51DisableLateWithdrawalAndExpiredSuppression(t *testing.T) {
	now := time.Now()
	m := NewManager(t.TempDir(), &publicIdentitySource{})
	_ = m.SetAlerts(true)
	m.ApplySource(publicBatch(now), nil, m.generation, now)
	gen := m.generation
	_ = m.SetAlerts(false)
	p := publicPost("72001", "Day 22/ Today is a reset.", now)
	m.ApplySource(publicBatch(now, p), nil, gen, now)
	if m.Snapshot().Notification != nil || len(m.InboxSnapshot().Messages) != 0 {
		t.Fatal("late result escaped off")
	}
	_ = m.SetAlerts(true)
	m.ApplySource(publicBatch(now, p), nil, m.generation, now)
	if m.Snapshot().Notification != nil {
		t.Fatal("reenable backlog bomb")
	}
	old := publicPost("72002", "We just reset Codex limits.", now.Add(-25*time.Hour))
	withdrawn := publicPost("72003", "We just reset Codex limits.", now)
	withdrawn.Status = "withdrawn"
	m.ApplySource(publicBatch(now, p, old, withdrawn), nil, m.generation, now)
	if len(m.cloudQueue) != 0 {
		t.Fatal("expired/withdrawn alerted")
	}
	if len(m.InboxSnapshot().Messages) != 3 {
		t.Fatal("suppressed history lost")
	}
	data, _ := json.Marshal(m.InboxSnapshot())
	if !strings.Contains(string(data), "withdrawn") {
		t.Fatal("withdrawal evidence absent")
	}
}

func TestN51SeedPreservesCooldownAndNoSourceEnabled(t *testing.T) {
	root := t.TempDir()
	raw := realAIHOTFixture(t)
	meta, err := os.ReadFile(filepath.Join("..", "..", "community", "backend", "night-n51", "aihot-recent-real-meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, _ := newAIHOTSource(root)
	source.cache.NextAllowedAt = time.Now().Add(time.Hour).Unix()
	source.cache.Attempts = 7
	if err = source.save(); err != nil {
		t.Fatal(err)
	}
	if err = SeedAIHOT(root, raw, meta); err != nil {
		t.Fatal(err)
	}
	after, _ := newAIHOTSource(root)
	if after.cache.NextAllowedAt != source.cache.NextAllowedAt || after.cache.Attempts != 7 {
		t.Fatal("manual import reset polling reservation")
	}
	configured, err := LoadInternalPublicSource(root)
	if err != nil || configured != nil {
		t.Fatal("seed enabled source")
	}
	inbox, _ := messageinbox.Open(root)
	if inbox.Snapshot().Source.Origin != "manual_public_import" {
		t.Fatal("manual origin hidden")
	}
}
func TestN51ActualCancellationStopsRequestAndQueueGeneration(t *testing.T) {
	var requests atomic.Int32
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	root := t.TempDir()
	source, _ := newAIHOTSource(root)
	source.endpoint = server.URL
	m := NewManager(root, source)
	_ = m.SetAlerts(true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.sourceLoop(ctx) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("request did not start")
	}
	_ = m.SetAlerts(false)
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("off did not cancel active request")
	}
	cancel()
	<-done
	if requests.Load() != 1 || m.Snapshot().Notification != nil {
		t.Fatal("off allowed retry/late notice")
	}
	now := time.Now()
	manual := NewManager(t.TempDir(), &publicIdentitySource{})
	_ = manual.SetAlerts(true)
	manual.enabledAt = now.Add(-time.Hour).Unix()
	manual.ApplySource(publicBatch(now), nil, manual.generation, now)
	manual.ApplySource(publicBatch(now, publicPost("73001", "Day 22/ Today is a reset.", now)), nil, manual.generation, now)
	states := manual.WorkerStates()
	if len(states) != 1 || states[0].Notification == nil {
		t.Fatal("missing queued fixture")
	}
	_ = manual.SetAlerts(false)
	_ = manual.SetAlerts(true)
	if manual.WorkerStateCurrent(states[0]) {
		t.Fatal("prior generation notification survived off/reenable")
	}
}
func TestN51ConcurrentPollLockDoesNotDuplicateRequest(t *testing.T) {
	root := t.TempDir()
	release, err := lockPublicPoll(filepath.Join(root, "aihot-poll.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1) }))
	defer server.Close()
	source, _ := newAIHOTSource(root)
	source.endpoint = server.URL
	if _, err = source.Fetch(context.Background()); err == nil || err.Error() != "public_source_poll_in_use" {
		t.Fatal("second process/poller ignored lock")
	}
	if count.Load() != 0 {
		t.Fatal("duplicate poll went online")
	}
}
func TestN51EdgeSafetyFailureRetriesOnceThenStopsWithoutDisguise(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("User-Agent") != "CodexEverforge-InternalEvaluation/0.1" {
			t.Error("UA changed")
		}
		w.Header().Set("X-Request-Id", "fixture-request")
		w.WriteHeader(567)
	}))
	defer server.Close()
	root := t.TempDir()
	s, _ := newAIHOTSource(root)
	now := time.Now()
	s.now = func() time.Time { return now }
	s.endpoint = server.URL
	_, _ = s.Fetch(context.Background())
	now = now.Add(600 * time.Second)
	_, _ = s.Fetch(context.Background())
	after, _ := newAIHOTSource(root)
	after.now = func() time.Time { return now.Add(24 * time.Hour) }
	after.endpoint = server.URL
	_, err := after.Fetch(context.Background())
	if err == nil || err.Error() != "public_source_safety_review_required" || count.Load() != 2 || after.cache.LastRequestID != "fixture-request" {
		t.Fatal("edge safety response was retried indefinitely")
	}
}
