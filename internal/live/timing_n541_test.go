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

type timingTransport func(*http.Request) (*http.Response, error)

func (f timingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func n541Source(t *testing.T, root string, now *time.Time, transport timingTransport) *CodexResetsSource {
	t.Helper()
	s, err := loadMonitor(root)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return *now }
	s.client.Transport = transport
	return s
}
func TestN541SuccessAnd304Use300Seconds(t *testing.T) {
	raw := monitorFixture(t)
	now := time.Now()
	root := t.TempDir()
	calls := 0
	status := 200
	transport := timingTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.String() != CodexResetsURL || r.Header.Get("Cache-Control") != "" {
			t.Fatal("changed request or bypassed CDN")
		}
		if calls > 1 && r.Header.Get("If-None-Match") != "stable" {
			t.Fatal("conditional request lost")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Cache-Control": []string{"public, max-age=14400, s-maxage=300, stale-while-revalidate=300"}, "Age": []string{"274"}, "Etag": []string{"stable"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
	s := n541Source(t, root, &now, transport)
	b, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	generated := *b.Sync.GeneratedAt
	observed := *b.Evidence[0].ObservedAt
	if s.cache.NextAllowedAt != now.Unix()+300 || s.cache.ScheduleReason != "poll" {
		t.Fatal("200 used HTTP TTL as polling prohibition")
	}
	now = now.Add(299 * time.Second)
	_, _ = s.Fetch(context.Background())
	s = n541Source(t, root, &now, transport)
	_, _ = s.Fetch(context.Background())
	if calls != 1 {
		t.Fatal("manual refresh/restart bypassed reservation")
	}
	now = now.Add(time.Second)
	status = 304
	b, err = s.Fetch(context.Background())
	if err != nil || calls != 2 || s.cache.NextAllowedAt != now.Unix()+300 {
		t.Fatal("304 cadence", err)
	}
	if *b.Sync.GeneratedAt != generated || *b.Evidence[0].ObservedAt != observed || b.Sync.UpstreamCollectedAt != nil {
		t.Fatal("HTTP validation fabricated upstream time")
	}
}
func TestN541LegacyMigrationIsSelectiveAndLocked(t *testing.T) {
	for _, kind := range []string{"success-100", "success-600", "failure", "success-before-attempt", "future-success", "invalid-raw", "in-flight", "unknown-reason", "missing-endpoint", "non-four-hour", "missing-success"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			attempt := now.Unix() - 100
			if kind == "success-600" {
				attempt = now.Unix() - 600
			}
			success := attempt + 1
			old := monitorCache{Version: 1, Endpoint: CodexResetsURL, Raw: monitorFixture(t), ETag: "preserved", LastAttemptAt: &attempt, LastSuccessAt: &success, NextAllowedAt: attempt + 14400}
			switch kind {
			case "failure":
				old.Failures = 1
			case "success-before-attempt":
				success = attempt - 1
			case "future-success":
				success = now.Unix() + 60
			case "invalid-raw":
				old.Raw = json.RawMessage(`{}`)
			case "in-flight":
				old.ScheduleVersion = 1
				old.ScheduleReason = "in_flight"
			case "unknown-reason":
				old.ScheduleReason = "unknown"
			case "missing-endpoint":
				old.Endpoint = ""
			case "non-four-hour":
				old.NextAllowedAt = attempt + 7200
			case "missing-success":
				old.LastSuccessAt = nil
			}
			// Sample metadata is current: allow enough history for a valid success.
			var payload map[string]any
			_ = json.Unmarshal(old.Raw, &payload)
			if meta, ok := payload["meta"].(map[string]any); ok {
				meta["generated_at"] = time.Unix(attempt, 0).UTC().Format(time.RFC3339)
				old.Raw, _ = json.Marshal(payload)
			}
			root := t.TempDir()
			path := filepath.Join(root, "codex-resets-poll.json")
			if err := messageinbox.SaveJSON(path, old); err != nil {
				t.Fatal(err)
			}
			calls := 0
			transport := timingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("If-None-Match") != "preserved" {
					t.Fatal("ETag erased by migration")
				}
				return &http.Response{StatusCode: 304, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			s := n541Source(t, root, &now, transport)
			if s.cache.NextAllowedAt != old.NextAllowedAt {
				t.Fatal("load mutated reservation before lock")
			}
			_, _ = s.Fetch(context.Background())
			wanted := old.NextAllowedAt
			if strings.HasPrefix(kind, "success-") && kind != "success-before-attempt" {
				wanted = attempt + 300
				if kind == "success-600" {
					wanted = now.Unix() + 300
				}
			}
			if s.cache.NextAllowedAt != wanted {
				t.Fatalf("%s reservation: got %d want %d", kind, s.cache.NextAllowedAt, wanted)
			}
			if kind == "success-600" {
				if calls != 1 {
					t.Fatal("due migrated poll not sent")
				}
			} else if calls != 0 {
				t.Fatal("early network request")
			}
			var persisted monitorCache
			raw, _ := os.ReadFile(path)
			_ = json.Unmarshal(raw, &persisted)
			if persisted.NextAllowedAt != wanted {
				t.Fatal("migration not durable")
			}
		})
	}
}
func TestN541RetryReservationsSurviveRestartAndLegacyMigration(t *testing.T) {
	for _, kind := range []string{"429", "network", "legacy-failure"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			root := t.TempDir()
			calls := 0
			transport := timingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if kind == "network" {
					return nil, errors.New("offline")
				}
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			s := n541Source(t, root, &now, transport)
			if kind == "legacy-failure" {
				attempt := now.Unix() - 10
				success := attempt - 10
				_ = messageinbox.SaveJSON(filepath.Join(root, "codex-resets-poll.json"), monitorCache{Version: 1, Endpoint: CodexResetsURL, Raw: monitorFixture(t), LastAttemptAt: &attempt, LastSuccessAt: &success, NextAllowedAt: attempt + 14400, Failures: 1})
				s = n541Source(t, root, &now, transport)
			} else {
				_, _ = s.Fetch(context.Background())
			}
			reserved := s.cache.NextAllowedAt
			initialCalls := calls
			s = n541Source(t, root, &now, transport)
			_, _ = s.Fetch(context.Background())
			if calls != initialCalls || s.cache.NextAllowedAt != reserved {
				t.Fatal("retry shortened across restart")
			}
			if kind == "429" && reserved != now.Unix()+3600 {
				t.Fatal("Retry-After ignored")
			}
			if kind == "network" && reserved < now.Unix()+300 {
				t.Fatal("network backoff removed")
			}
			now = time.Unix(reserved-1, 0)
			_, _ = s.Fetch(context.Background())
			if calls != initialCalls {
				t.Fatal("retry sent one second early")
			}
		})
	}
}
func TestN541SnapshotAgesWithoutFetchAndPreservesRecovery(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	m := NewManager(root, &PublicResetSources{monitor: &n54Source{}})
	stamp := now.Unix()
	generated := stamp - 100
	health := messageinbox.SourceState{ID: CodexResetsSourceID, State: "available", LastSuccessAt: &stamp, GeneratedAt: &generated}
	_ = m.inbox.UpdateSourceHealth(health)
	_ = m.inbox.UpdateSource(messageinbox.SourceState{ID: PublicResetSourcesID, State: "available", LastSuccessAt: &stamp})
	m.state.Reset.SourceKind = "public_aggregator"
	m.state.Reset.SourceFresh = true
	before, _ := os.ReadFile(filepath.Join(root, messageinbox.FileName))
	atBoundary := m.snapshotLockedAt(now.Add(1200 * time.Second))
	if !atBoundary.Reset.SourceFresh || atBoundary.Inbox.Sources[0].State != "available" {
		t.Fatal("premature stale")
	}
	stale := m.snapshotLockedAt(now.Add(1201 * time.Second))
	if stale.Reset.SourceFresh || stale.Inbox.Source.State != "offline" || stale.Inbox.Sources[0].State != "stale" {
		t.Fatal("snapshot stayed fresh without Fetch")
	}
	if *stale.Inbox.Sources[0].GeneratedAt != generated || stale.Inbox.Sources[0].UpstreamCollectedAt != nil {
		t.Fatal("snapshot changed provenance")
	}
	after, _ := os.ReadFile(filepath.Join(root, messageinbox.FileName))
	if string(before) != string(after) {
		t.Fatal("snapshot wrote health back")
	}
	if m.inboxSnapshotLockedAt(now.Add(1201*time.Second)).Source.State != "offline" {
		t.Fatal("inbox API disagrees")
	}
	m.state.Reset.State = "recovered"
	m.state.Reset.LocalRecovery = &Recovery{ID: "synthetic-local-recovery"}
	recovered := m.snapshotLockedAt(now.Add(1201 * time.Second))
	if recovered.Reset.State != "recovered" || recovered.Reset.LocalRecovery == nil || recovered.Reset.SourceFresh {
		t.Fatal("stale public source erased independent local recovery")
	}
	m.state.Reset.LocalRecovery = nil
	// A fresh AIHOT source keeps the summary partial rather than fully offline.
	_ = m.inbox.UpdateSourceHealth(messageinbox.SourceState{ID: AIHOTSourceID, State: "available", LastSuccessAt: &stamp})
	provider := m.source.(*PublicResetSources)
	provider.internal = &n54Source{}
	m.state.Preferences.ResetAlertsEnabled = true
	if m.snapshotLockedAt(now.Add(1201*time.Second)).Inbox.Source.State != "partial" {
		t.Fatal("independent healthy source hidden")
	}
	newSuccess := now.Add(1201 * time.Second).Unix()
	health.LastSuccessAt = &newSuccess
	_ = m.inbox.UpdateSourceHealth(health)
	fresh := m.snapshotLockedAt(time.Unix(newSuccess, 0))
	if fresh.Inbox.Source.State != "available" || *fresh.Inbox.Sources[1].GeneratedAt != generated {
		t.Fatal("recovery changed provider generation time or failed to recover health")
	}
}
