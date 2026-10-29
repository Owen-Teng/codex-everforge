package live

import (
	"codex-quota-monitor/internal/messageinbox"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN56AIHOTReceivesItsClientBudget(t *testing.T) {
	s, err := newAIHOTSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.client.Transport = timingTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 11*time.Second {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(publicSyntheticBudgetFeed())))}, nil
	})
	combined := &PublicResetSources{internal: s}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, err := combined.Fetch(ctx)
	if err != nil || !b.PublicFeedAvailable {
		t.Fatalf("a request that fits the source's 12s client budget was rejected: %v %+v", err, b.Sync)
	}
}

func TestN56PersistentRecoveryPromisesAndLegacyState(t *testing.T) {
	for _, sourceKind := range []string{"monitor", "aihot"} {
		t.Run(sourceKind, func(t *testing.T) {
			now := time.Now()
			root := t.TempDir()
			calls := 0
			mode := "network"
			status := 0
			retry := ""
			payload := monitorFixture(t)
			interval := int64(300)
			cacheName := "codex-resets-poll.json"
			if sourceKind == "aihot" {
				payload = realAIHOTFixture(t)
				interval = 600
				cacheName = AIHOTCacheFile
			}
			transport := timingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "network" {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{retry}}, Body: io.NopCloser(strings.NewReader(string(payload)))}, nil
			})
			newSource := func() Source {
				if sourceKind == "monitor" {
					s, err := loadMonitor(root)
					if err != nil {
						t.Fatal(err)
					}
					s.now = func() time.Time { return now }
					s.client.Transport = transport
					return s
				}
				s, err := newAIHOTSource(root)
				if err != nil {
					t.Fatal(err)
				}
				s.now = func() time.Time { return now }
				s.client.Transport = transport
				return s
			}
			readCache := func() map[string]any {
				raw, err := os.ReadFile(filepath.Join(root, cacheName))
				if err != nil {
					t.Fatal(err)
				}
				var c map[string]any
				if json.Unmarshal(raw, &c) != nil {
					t.Fatal("cache invalid")
				}
				return c
			}
			s := newSource()
			for i := 0; i < 4; i++ {
				if _, err := s.Fetch(context.Background()); err == nil {
					t.Fatal("fake timeout accepted")
				}
				c := readCache()
				if int64(c["nextAllowedAt"].(float64)) != now.Unix()+interval || c["failureKind"] != "timeout" || c["scheduleReason"] != "retry" {
					t.Fatal("transport retry grew exponentially", c["nextAllowedAt"])
				}
				before := calls
				s = newSource()
				_, _ = s.Fetch(context.Background())
				if calls != before {
					t.Fatal("restart bypassed retry")
				}
				now = now.Add(time.Duration(interval) * time.Second)
			}
			mode = "http"
			status = 200
			b, err := s.Fetch(context.Background())
			if err != nil || !b.PublicFeedAvailable {
				t.Fatal("did not recover after normal retry", err)
			}
			c := readCache()
			if c["failures"].(float64) != 0 {
				t.Fatal("successful recovery kept failure count")
			}
			if b.Sync.LastSuccessAt == nil || *b.Sync.LastSuccessAt != now.Unix() {
				t.Fatal("successful read not recorded")
			}
			now = now.Add(time.Duration(interval) * time.Second)
			status = 304
			b, err = s.Fetch(context.Background())
			if err != nil || !b.PublicFeedAvailable {
				t.Fatal("304 recovery failed", err)
			}
			now = now.Add(time.Duration(interval) * time.Second)
			status = 429
			retry = "3600"
			_, _ = s.Fetch(context.Background())
			c = readCache()
			until := int64(c["nextAllowedAt"].(float64))
			if until < now.Unix()+3600 || c["failureKind"] != "http_429" || int64(c["retryAfterUntil"].(float64)) != now.Unix()+3600 {
				t.Fatal("server retry promise lost")
			}
			before := calls
			s = newSource()
			now = time.Unix(until-1, 0)
			_, _ = s.Fetch(context.Background())
			if calls != before {
				t.Fatal("429 crossed restart early")
			}
			now = time.Unix(until, 0)
			status = 503
			retry = "900"
			_, _ = s.Fetch(context.Background())
			c = readCache()
			if int64(c["nextAllowedAt"].(float64)) < now.Unix()+900 || c["failureKind"] != "http_5xx" {
				t.Fatal("server backoff lost")
			}
			// An old cache cannot prove whether a long reservation was server-directed.
			delete(c, "failureKind")
			delete(c, "retryAfterUntil")
			delete(c, "scheduleReason")
			delete(c, "scheduleVersion")
			c["failures"] = 3
			c["nextAllowedAt"] = now.Unix() + 7200
			if err = messageinbox.SaveJSON(filepath.Join(root, cacheName), c); err != nil {
				t.Fatal(err)
			}
			s = newSource()
			before = calls
			_, _ = s.Fetch(context.Background())
			if calls != before || int64(readCache()["nextAllowedAt"].(float64)) != now.Unix()+7200 {
				t.Fatal("legacy unknown reservation shortened")
			}
			// Simulated wall-clock resume: when the old promise is past, a normal poll runs.
			now = now.Add(3 * time.Hour)
			status = 200
			retry = ""
			b, err = s.Fetch(context.Background())
			if err != nil || !b.PublicFeedAvailable || calls != before+1 {
				t.Fatal("elapsed promise stayed stuck after clock advance", err)
			}
		})
	}
}

type n56BudgetSource struct {
	budget time.Duration
	fetch  func(context.Context) (SourceBatch, error)
	health messageinbox.SourceState
}

func (s *n56BudgetSource) Fetch(ctx context.Context) (SourceBatch, error) { return s.fetch(ctx) }
func (s *n56BudgetSource) FetchTimeout() time.Duration                    { return s.budget }
func (s *n56BudgetSource) HealthSnapshot() messageinbox.SourceState       { return s.health }
func TestN56SourcesRunIndependentlyAndPublishEarliestPoll(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	sawSecond := false
	at := time.Now().Unix()
	slowNext, fastNext := at+300, at+600
	slow := &n56BudgetSource{budget: 500 * time.Millisecond, health: messageinbox.SourceState{ID: CodexResetsSourceID, NextPollAt: &slowNext}}
	slow.fetch = func(ctx context.Context) (SourceBatch, error) {
		close(first)
		select {
		case <-second:
			sawSecond = true
		case <-ctx.Done():
			return SourceBatch{}, ctx.Err()
		}
		<-ctx.Done()
		return SourceBatch{}, ctx.Err()
	}
	fast := &n56BudgetSource{budget: time.Second}
	fast.fetch = func(ctx context.Context) (SourceBatch, error) {
		close(second)
		select {
		case <-first:
		case <-ctx.Done():
			return SourceBatch{}, ctx.Err()
		}
		h := messageinbox.SourceState{ID: AIHOTSourceID, State: "available", NextPollAt: &fastNext}
		return SourceBatch{Sync: &h, PublicFeedAvailable: true}, nil
	}
	combined := &PublicResetSources{monitor: slow, internal: fast}
	b, err := combined.Fetch(context.Background())
	if err != nil || !sawSecond || !b.PublicFeedAvailable || b.Sync.State != "partial" || b.Sync.NextPollAt == nil || *b.Sync.NextPollAt != slowNext {
		t.Fatal("one source blocked/canceled another, or earliest promise missing")
	}
	if sourceFetchBudget(combined) < combined.internal.(*n56BudgetSource).budget {
		t.Fatal("aggregate budget clipped child")
	}
}
func TestN56CanceledFetchDoesNotReserveOrSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, kind := range []string{"monitor", "aihot"} {
		root := t.TempDir()
		var source Source
		if kind == "monitor" {
			source, _ = loadMonitor(root)
		} else {
			source, _ = newAIHOTSource(root)
		}
		if _, err := source.Fetch(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled request lost")
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 0 {
			t.Fatal("canceled fetch wrote a false attempt")
		}
	}
}

func TestN56ServerFailureCapKeepsLongerRetryAfter(t *testing.T) {
	for _, kind := range []string{"monitor", "aihot"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			root := t.TempDir()
			retry := ""
			minimum := int64(300)
			cacheName := "codex-resets-poll.json"
			if kind == "aihot" {
				minimum = 600
				cacheName = AIHOTCacheFile
			}
			transport := timingTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{retry}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			var source Source
			if kind == "monitor" {
				s, _ := loadMonitor(root)
				s.now = func() time.Time { return now }
				s.client.Transport = transport
				source = s
			} else {
				s, _ := newAIHOTSource(root)
				s.now = func() time.Time { return now }
				s.client.Transport = transport
				source = s
			}
			readNext := func() int64 {
				raw, err := os.ReadFile(filepath.Join(root, cacheName))
				if err != nil {
					t.Fatal(err)
				}
				var cache struct {
					Next int64 `json:"nextAllowedAt"`
				}
				if json.Unmarshal(raw, &cache) != nil {
					t.Fatal("invalid cache")
				}
				return cache.Next
			}
			for attempt := 0; attempt < 8; attempt++ {
				_, _ = source.Fetch(context.Background())
				delay := readNext() - now.Unix()
				if delay < minimum || delay > 900 || attempt >= 2 && delay != 900 {
					t.Fatal("server failure exceeded 15-minute recovery cap", delay)
				}
				now = time.Unix(readNext(), 0)
			}
			retry = "3600"
			_, _ = source.Fetch(context.Background())
			if readNext() < now.Unix()+3600 {
				t.Fatal("15-minute cap shortened explicit Retry-After")
			}
		})
	}
	for _, kind := range []string{"body", "schema"} {
		for _, minimum := range []int64{300, 600} {
			if publicFailureDelay(minimum, 20, kind) != 900 {
				t.Fatal("temporary source data failure has hours-long backoff")
			}
		}
	}
	if publicFailureDelay(600, 20, "http_429") != 19200 {
		t.Fatal("unannounced 429 backoff was shortened")
	}
}

func TestN56SnapshotNextPollExcludesInactiveInternalSource(t *testing.T) {
	m := NewManager(t.TempDir(), &PublicResetSources{monitor: &n54Source{}, internal: &n54Source{}})
	now := time.Now().Unix()
	monitorNext, internalNext := now+600, now+300
	_ = m.inbox.UpdateSourceHealth(messageinbox.SourceState{ID: CodexResetsSourceID, State: "available", LastSuccessAt: &now, NextPollAt: &monitorNext, ProxyMode: "system"})
	_ = m.inbox.UpdateSourceHealth(messageinbox.SourceState{ID: AIHOTSourceID, State: "available", LastSuccessAt: &now, NextPollAt: &internalNext})
	_ = m.inbox.UpdateSource(messageinbox.SourceState{ID: PublicResetSourcesID, State: "available", NextPollAt: &internalNext})
	off := m.InboxSnapshot()
	if off.Source.NextPollAt == nil || *off.Source.NextPollAt != monitorNext {
		t.Fatal("inactive AIHOT determines public-only schedule")
	}
	m.state.Preferences.ResetAlertsEnabled = true
	on := m.InboxSnapshot()
	if on.Source.NextPollAt == nil || *on.Source.NextPollAt != internalNext {
		t.Fatal("summary did not include active earliest reservation")
	}
	for _, source := range on.Sources {
		if source.ID == CodexResetsSourceID && source.ProxyMode != "system" {
			t.Fatal("non-sensitive proxy diagnostic lost")
		}
	}
}

func publicSyntheticBudgetFeed() []byte {
 stamp := time.Now().UTC().Format(time.RFC3339)
 raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "timezone": "Asia/Shanghai", "checkedAt": stamp, "historyFrom": stamp, "count": 0, "events": []any{}, "activities": []any{}, "monitor": map[string]any{"status": "healthy", "lastCollectedAt": stamp}})
 return raw
}
