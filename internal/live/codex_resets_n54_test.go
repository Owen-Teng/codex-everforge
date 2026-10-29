package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"codex-quota-monitor/internal/reset"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func monitorFixture(t *testing.T) []byte {
	t.Skip("Private external capture excluded from public source; synthetic regressions remain enabled.")
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "team", "evidence", "n54-source-research", "codex-resets-sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wrapper struct {
		Body json.RawMessage `json:"body"`
	}
	if err = json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatal(err)
	}
	return wrapper.Body
}
func TestN54RealReplay(t *testing.T) {
	now := time.Now()
	b, err := ParseCodexResets(monitorFixture(t), now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Evidence) != 3 || b.Evidence[0].PostID != "" || b.Evidence[0].RelatedPostID != "2114588174249071102" || b.Evidence[0].PublishedAt != nil || b.Sync.UpstreamCollectedAt != nil {
		t.Fatalf("opaque observation/times: %+v", b)
	}
	store, _ := messageinbox.Open(t.TempDir())
	a, err := ParseAIHOT(realAIHOTFixture(t), now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Ingest(a.Posts, *a.Sync, a.HistoryFrom, now); err != nil {
		t.Fatal(err)
	}
	if err = store.IngestEvidence(b.Evidence, *b.Sync, now); err != nil {
		t.Fatal(err)
	}
	byID := map[string]reset.Event{}
	for _, v := range store.ResetEvents() {
		byID[v.ID] = v
	}
	done := byID["reset:2114200152701575656"]
	observed := byID["reset:2114588174249071102"]
	if done.Phase != "author_completed" || len(done.PostIDs) < 3 || done.ConfirmedAt == nil {
		t.Fatalf("Oct25: %+v", done)
	}
	if observed.Phase != "externally_observed" || observed.ConfirmedAt != nil || len(observed.Attributions) != 2 {
		t.Fatalf("Oct26: %+v", observed)
	}
	if observed.AccountRecoveryVerified || observed.Independence != "unverified" {
		t.Fatal("overstated evidence")
	}
	for _, m := range store.Snapshot().Messages {
		if strings.HasPrefix(m.PostID, "observed-") {
			t.Fatal("fabricated X id")
		}
		if m.Translation != nil && m.TextCompleteness == "full" && m.Status == "active" && m.DisplayText.Basis != "source_translation" {
			t.Fatal("translation not preferred")
		}
	}
	if out := os.Getenv("N54_REPLAY_OUTPUT"); out != "" {
		if err = messageinbox.SaveJSON(out, store.Snapshot()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestN54MonitorSchemaRejectsBadClaimsAndIdentity(t *testing.T) {
	raw := monitorFixture(t)
	now := time.Now().Unix()
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	for _, kind := range []string{"question", "negative", "wrong-author", "fake-url", "future", "duplicate", "bad-version", "observed-opaque"} {
		t.Run(kind, func(t *testing.T) {
			var p map[string]any
			_ = json.Unmarshal(raw, &p)
			rows := p["data"].([]any)
			r := rows[1].(map[string]any)
			source := r["source"].(map[string]any)
			switch kind {
			case "question":
				r["text"] = "Codex limits have been reset?"
			case "negative":
				r["text"] = "It is all propagated. Not true."
			case "wrong-author":
				source["author"] = "other"
			case "fake-url":
				source["url"] = "https://evil.example/thsottiaux/status/2114237083652882932"
			case "future":
				r["announced_at"] = time.Unix(now+301, 0).UTC().Format(time.RFC3339)
			case "duplicate":
				p["data"] = append(rows, rows[0])
			case "bad-version":
				p["meta"].(map[string]any)["api_version"] = "v2"
			case "observed-opaque":
				r["source"] = map[string]any{"type": "observed"}
				r["id"] = "observed-123"
			}
			body, _ := json.Marshal(p)
			b, err := ParseCodexResets(body, now)
			if kind == "question" || kind == "negative" {
				if err != nil {
					t.Fatal(err)
				}
				if b.Evidence[1].Type != "unclassified" {
					t.Fatal("bad assertion became event")
				}
				return
			}
			if kind == "observed-opaque" {
				if err != nil || b.Evidence[1].PostID != "" || b.Evidence[1].RelatedPostID != "" {
					t.Fatal("opaque observation became post")
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
}
func TestN54MonitorCooldownRestartETagRetryAndOutage(t *testing.T) {
	raw := monitorFixture(t)
	var count atomic.Int32
	var status atomic.Int32
	status.Store(200)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if r.Method != "GET" || r.ContentLength > 0 || r.Header.Get("Authorization") != "" {
			t.Error("private upload/auth")
		}
		if n > 1 && r.Header.Get("If-None-Match") != "sample" {
			t.Error("ETag absent")
		}
		switch status.Load() {
		case 200:
			w.Header().Set("ETag", "sample")
			w.Header().Set("Cache-Control", "public, s-maxage=300")
			_, _ = w.Write(raw)
		case 429:
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(429)
		default:
			w.WriteHeader(int(status.Load()))
		}
	}))
	defer srv.Close()
	root := t.TempDir()
	cfg := map[string]any{"schemaVersion": 1, "enabled": true, "endpoint": srv.URL}
	_ = messageinbox.SaveJSON(filepath.Join(root, MonitorConfigFile), cfg)
	s, err := loadMonitor(root)
	if err != nil {
		t.Fatal(err)
	}
	s.client = srv.Client()
	now := time.Now()
	s.now = func() time.Time { return now }
	b, err := s.Fetch(context.Background())
	if err != nil || !b.PublicFeedAvailable {
		t.Fatal(err)
	}
	upstreamGenerated := *b.Sync.GeneratedAt
	now = now.Add(299 * time.Second)
	_, _ = s.Fetch(context.Background())
	if count.Load() != 1 {
		t.Fatal("cooldown bypass")
	}
	s, err = loadMonitor(root)
	if err != nil {
		t.Fatal(err)
	}
	s.client = srv.Client()
	s.now = func() time.Time { return now }
	_, _ = s.Fetch(context.Background())
	if count.Load() != 1 {
		t.Fatal("restart bypass")
	}
	now = now.Add(time.Second)
	status.Store(304)
	b, err = s.Fetch(context.Background())
	if err != nil || count.Load() != 2 || *b.Sync.GeneratedAt != upstreamGenerated || b.Sync.UpstreamCollectedAt != nil {
		t.Fatal("304/freshness mismatch", err)
	}
	now = now.Add(300 * time.Second)
	status.Store(429)
	if _, err = s.Fetch(context.Background()); err == nil {
		t.Fatal("429 accepted")
	}
	if s.NextPollTime().Unix() < now.Unix()+3600 {
		t.Fatal("Retry-After ignored")
	}
	_, _ = s.Fetch(context.Background())
	if count.Load() != 3 {
		t.Fatal("429 flood")
	}
	b, err = s.cached("test")
	if err != nil || b.PublicFeedAvailable {
		t.Fatal("failed source fresh")
	}
	now = now.Add(3600 * time.Second)
	srv.Close()
	if _, err = s.Fetch(context.Background()); err == nil {
		t.Fatal("outage ignored")
	}
	if s.HealthSnapshot().State == "available" {
		t.Fatal("outage health")
	}
}
func TestN54MonitorConfigurationAndCorruptCache(t *testing.T) {
	root := t.TempDir()
	src, err := LoadPublicResetSources(root)
	if err != nil || src == nil || !src.(*PublicResetSources).ReadWithoutAlerts() {
		t.Fatal("fresh install no public default", err)
	}
	for _, endpoint := range []string{"http://example.com/api", "https://user:pass@example.com/api", "https://example.com/api#x", "https://example.com/api?token=secret"} {
		_ = messageinbox.SaveJSON(filepath.Join(root, MonitorConfigFile), map[string]any{"schemaVersion": 1, "enabled": true, "endpoint": endpoint})
		if _, err = loadMonitor(root); err == nil {
			t.Fatal("unsafe mirror", endpoint)
		}
	}
	_ = messageinbox.SaveJSON(filepath.Join(root, MonitorConfigFile), map[string]any{"schemaVersion": 1, "enabled": false})
	src, err = LoadPublicResetSources(root)
	if err != nil || src != nil {
		t.Fatal("opt out ignored")
	}
	_ = os.Remove(filepath.Join(root, MonitorConfigFile))
	_ = os.WriteFile(filepath.Join(root, "codex-resets-poll.json"), []byte("broken"), 0600)
	if _, err = loadMonitor(root); err == nil {
		t.Fatal("corrupt cache not rejected")
	}
	_ = os.Remove(filepath.Join(root, "codex-resets-poll.json"))
	s, _ := loadMonitor(root)
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://example.com", 302) }))
	defer redirect.Close()
	s.endpoint = redirect.URL
	s.client.Transport = redirect.Client().Transport
	if _, err = s.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatal("redirect followed", err)
	}
	// Default production TLS does not trust an arbitrary mirror certificate.
	root = t.TempDir()
	s, _ = loadMonitor(root)
	s.endpoint = redirect.URL
	if _, err = s.Fetch(context.Background()); err == nil {
		t.Fatal("TLS validation bypassed")
	}
}

func TestN54MirrorSwitchPreservesReservationAndDropsETag(t *testing.T) {
	root := t.TempDir()
	now := time.Now().Unix()
	old := monitorCache{Version: 1, Endpoint: CodexResetsURL, Raw: monitorFixture(t), ETag: "old-origin", LastSuccessAt: &now, NextAllowedAt: now + 300}
	if err := messageinbox.SaveJSON(filepath.Join(root, "codex-resets-poll.json"), old); err != nil {
		t.Fatal(err)
	}
	_ = messageinbox.SaveJSON(filepath.Join(root, MonitorConfigFile), map[string]any{"schemaVersion": 1, "enabled": true, "endpoint": "https://mirror.example/api/v1/resets?limit=20"})
	s, err := loadMonitor(root)
	if err != nil || s.cache.NextAllowedAt != old.NextAllowedAt || s.cache.ETag != "" || s.cache.LastSuccessAt != nil || len(s.cache.Raw) != 0 {
		t.Fatal("mirror inherited wrong cache or erased reservation", err)
	}
}

type n54Source struct {
	batch SourceBatch
	calls int
	err   error
}

func (s *n54Source) Fetch(context.Context) (SourceBatch, error) { s.calls++; return s.batch, s.err }
func (s *n54Source) HealthSnapshot() messageinbox.SourceState {
	return messageinbox.SourceState{ID: "test", State: "offline"}
}
func n54Batch(at int64, id, typ string) SourceBatch {
	e := reset.Evidence{ID: "monitor:" + id, SourceID: CodexResetsSourceID, SourceEventID: id, RelatedPostID: id, Type: typ, ObservedAt: &at, CollectedAt: at, Status: "active", Attribution: reset.Attribution{Name: "Codex Resets", URL: "https://codex-resets.com"}, URL: "https://x.com/thsottiaux/status/" + id}
	h := messageinbox.SourceState{ID: CodexResetsSourceID, State: "available", LastSuccessAt: &at}
	part := SourceBatch{Evidence: []reset.Evidence{e}, Sync: &h, PublicFeedAvailable: true}
	return SourceBatch{SourceID: PublicResetSourcesID, SourceKind: "public_aggregator", SourceLabel: "公开重置消息", Sync: &h, Segments: []SourceBatch{part}, ResetProjection: true, PublicFeedAvailable: true}
}
func TestN54ManagerBaselineDeliveryRestartWithdrawalAndDisabled(t *testing.T) {
	root := t.TempDir()
	src := &PublicResetSources{monitor: &n54Source{}}
	m := NewManager(root, src)
	if err := m.SetAlerts(true); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m.enabledAt = now.Unix() - 3600
	b := n54Batch(now.Unix()-10, "11", "external_observation")
	m.ApplySource(b, nil, m.generation, now)
	if m.Snapshot().Notification != nil {
		t.Fatal("initial history bomb")
	}
	b = n54Batch(now.Unix(), "12", "external_observation")
	m.ApplySource(b, nil, m.generation, now)
	states := m.WorkerStates()
	if len(states) != 1 || states[0].Notification == nil || states[0].Notification.Title != "有重置的好消息啦" || !strings.Contains(states[0].Notification.Body, "观测记录") {
		t.Fatalf("no worker notice %+v", states)
	}
	id := states[0].Notification.ID
	m.NotificationEmitted(id)
	if err := m.RecordNotificationResult(id, "submitted", "consumer_accepted"); err != nil {
		t.Fatal(err)
	}
	restarted := NewManager(root, src)
	restarted.ApplySource(b, nil, restarted.generation, now)
	if restarted.Snapshot().Notification != nil {
		t.Fatal("restart duplicate")
	}
	// New source copy of same stage cannot issue a second notice.
	copy := b.Segments[0]
	copy.Sync = &messageinbox.SourceState{ID: "another", State: "available"}
	copy.Evidence = append([]reset.Evidence(nil), copy.Evidence...)
	copy.Evidence[0].ID = "other:12"
	copy.Evidence[0].SourceID = "another"
	b.Segments = append(b.Segments, copy)
	restarted.ApplySource(b, nil, restarted.generation, now)
	if restarted.Snapshot().Notification != nil {
		t.Fatal("provider duplicate")
	}
	b = n54Batch(now.Unix(), "13", "external_observation")
	restarted.ApplySource(b, nil, restarted.generation, now)
	if restarted.Snapshot().Notification == nil {
		t.Fatal("new stage missing")
	}
	b.Segments[0].Evidence[0].Type = "withdrawn"
	restarted.ApplySource(b, nil, restarted.generation, now)
	if restarted.Snapshot().Notification != nil || len(restarted.cloudQueue) != 0 {
		t.Fatal("withdrawn notice survived")
	}
	off := NewManager(t.TempDir(), src)
	b = n54Batch(now.Unix(), "14", "external_observation")
	b.RecordOnly = true
	off.ApplySource(b, nil, off.generation, now)
	if off.Snapshot().Notification != nil || len(off.InboxSnapshot().ResetEvents) != 1 || off.Snapshot().Preferences.ResetAlertsEnabled {
		t.Fatal("disabled alert semantics")
	}
}
func TestN54OnlyPublicSourceWhileAlertsOff(t *testing.T) {
	monitor := &n54Source{batch: n54Batch(time.Now().Unix(), "1", "external_observation").Segments[0]}
	internal := &n54Source{err: errors.New("internal_not_allowed")}
	s := &PublicResetSources{monitor: monitor, internal: internal}
	b, err := s.FetchWithoutAlerts(context.Background())
	if err != nil || monitor.calls != 1 || internal.calls != 0 || !b.RecordOnly {
		t.Fatal("internal opt-in invoked off", err)
	}
	monitor.err = errors.New("offline")
	b, _ = s.FetchWithoutAlerts(context.Background())
	if b.PublicFeedAvailable || b.Sync.State != "offline" {
		t.Fatal("outage hidden")
	}
}

func TestN54StagesExpiryAndQueuedRestart(t *testing.T) {
	root := t.TempDir()
	src := &PublicResetSources{monitor: &n54Source{}}
	m := NewManager(root, src)
	_ = m.SetAlerts(true)
	now := time.Now()
	m.enabledAt = now.Unix() - 3600
	b := n54Batch(now.Unix(), "1", "external_observation")
	b.Segments[0].Evidence = []reset.Evidence{}
	m.ApplySource(b, nil, m.generation, now)
	for _, phase := range []string{"author_announcement", "source_recorded_completion"} {
		b = n54Batch(now.Unix(), "2", phase)
		e := &b.Segments[0].Evidence[0]
		e.PostID = "2"
		e.RelatedPostID = ""
		at := now.Unix()
		e.PublishedAt = &at
		e.ObservedAt = nil
		m.ApplySource(b, nil, m.generation, now)
		states := m.WorkerStates()
		if len(states) != 1 || states[0].Notification == nil {
			t.Fatal("new stage missing", phase)
		}
		if phase == "author_announcement" && states[0].Notification.Title != "Tibo 又要重置啦，快点灯！" {
			t.Fatal("wrong future copy")
		}
		if phase == "source_recorded_completion" && (states[0].Notification.Kind != "announced" || strings.Contains(states[0].Notification.Title, "又要")) {
			t.Fatal("wrong past copy")
		}
		if phase == "author_announcement" {
			m.NotificationEmitted(states[0].Notification.ID)
		}
	}
	// The completed notification was queued but not emitted; retry after a worker crash.
	restarted := NewManager(root, src)
	restarted.ApplySource(b, nil, restarted.generation, now)
	states := restarted.WorkerStates()
	if states[0].Notification == nil {
		t.Fatal("queued stage lost across crash")
	}
	id := states[0].Notification.ID
	restarted.NotificationEmitted(id)
	_ = restarted.RecordNotificationResult(id, "failed", "consumer_failed")
	again := NewManager(root, src)
	again.ApplySource(b, nil, again.generation, now)
	if again.Snapshot().Notification != nil {
		t.Fatal("failed receipt retried without user request")
	}
	b = n54Batch(now.Add(-25*time.Hour).Unix(), "3", "external_observation")
	again.ApplySource(b, nil, again.generation, now)
	if again.Snapshot().Notification != nil {
		t.Fatal("expired event bomb")
	}
	b = n54Batch(now.Add(301*time.Second).Unix(), "4", "external_observation")
	again.ApplySource(b, nil, again.generation, now)
	if again.Snapshot().Notification != nil {
		t.Fatal("future event notified")
	}
}

func TestN54OfficialPublicGet(t *testing.T) {
	output := os.Getenv("N54_PUBLIC_GET_OUTPUT")
	if output == "" {
		t.Skip("explicit bounded public API check only")
	}
	s, err := loadMonitor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !b.PublicFeedAvailable || b.Sync.ID != CodexResetsSourceID {
		t.Fatal("official feed not available")
	}
	if err = messageinbox.SaveJSON(output, map[string]any{"endpoint": CodexResetsURL, "requests": 1, "mode": "single_public_get_temporary_state", "source": b.Sync, "evidence": b.Evidence, "raw": s.cache.Raw}); err != nil {
		t.Fatal(err)
	}
}
