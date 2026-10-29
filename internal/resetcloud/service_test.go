package resetcloud

import (
	"codex-quota-monitor/internal/xactivity"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersistedEventsWithdrawalAndTruthfulHealth(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path, true, clock)
	if err != nil {
		t.Fatal(err)
	}
	c := xactivity.ReplayConfig()
	p, _ := xactivity.NewProcessor(c, clock, "")
	d := p.Process(xactivity.SyntheticFrame(c, "fixture-event", "900001", "We just reset Codex limits.", now, now))
	if err = s.Ingest(d); err != nil {
		t.Fatal(err)
	}
	if err = s.Ingest(d); err != nil {
		t.Fatal(err)
	}
	f := s.Feed()
	if len(f.Events) != 1 || f.Revision != 1 || f.Events[0].Verification != "author_claim" || f.Events[0].Kind == "recovered" {
		t.Fatal("ingestion/dedupe failed")
	}
	stamp := now.Unix()
	_ = s.RecordStatus("", xactivity.Status{State: "connected", ConnectionActive: true, LastWireAt: &stamp})
	if !s.Feed().Upstream.Connected {
		t.Fatal("fresh heartbeat lost")
	}
	now = now.Add(91 * time.Second)
	if s.Feed().Upstream.Connected {
		t.Fatal("service time forged upstream freshness")
	}
	if err = s.Withdraw(f.Events[0].ID); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, true, clock)
	if err != nil {
		t.Fatal(err)
	}
	f = reopened.Feed()
	if f.Upstream.Connected || f.Events[0].Version != 2 || f.Events[0].Status != "withdrawn" || f.Revision != 2 {
		t.Fatal("restart resurrected upstream/event")
	}
	if _, err = Open(path, false, clock); err == nil {
		t.Fatal("synthetic state accepted as production")
	}
	now = now.Add(24 * time.Hour)
	if len(reopened.Feed().Events) != 0 {
		t.Fatal("expired event exposed")
	}
}
func TestAuthorizationRestartCannotRenewOrResetBudget(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "state.json")
	s, _ := Open(path, false, func() time.Time { return now })
	c := xactivity.DefaultConfig()
	if err := s.ReserveGrant("fixture-grant-1", now.Unix()+100, c); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordStatus("fixture-grant-1", xactivity.Status{State: "stopped", Attempts: 2, Frames: 3, WireBytes: 400}); err != nil {
		t.Fatal(err)
	}
	_ = s.RecordStatus("fixture-grant-1", xactivity.Status{Attempts: 0, Frames: 0})
	reopened, err := Open(path, false, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	grant := reopened.disk.Grants["fixture-grant-1"]
	if grant.UsedAttempts != 2 || grant.UsedFrames != 3 || grant.UsedWireBytes != 400 || grant.ReservedAttempts != 3 || grant.ReservedFrames != 20 {
		t.Fatal("budget regressed")
	}
	if reopened.ReserveGrant("fixture-grant-1", now.Unix()+110, c) == nil {
		t.Fatal("restart renewed authorization")
	}
	if reopened.ReserveGrant("expired-grant", now.Unix()-1, c) == nil {
		t.Fatal("expired authorization accepted")
	}
	if reopened.ReserveGrant("long-grant", now.Unix()+121, c) == nil {
		t.Fatal("deadline expanded duration")
	}
	recorder := httptest.NewRecorder()
	reopened.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/feed", nil))
	if recorder.Code != 200 || strings.Contains(recorder.Body.String(), "fixture-grant") {
		t.Fatal("feed unavailable or leaked private grant")
	}
}
func TestReadOnlyHTTPAndDisabledStartupNeverReadsSecrets(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"), false, nil)
	c := Config{X: xactivity.DefaultConfig(), SecretFile: "/does/not/exist"}
	s.RunAuthorized(context.Background(), c)
	if s.Feed().Upstream.Reason != "disabled" {
		t.Fatal("disabled tried secret/preflight")
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/v1/feed", 200}, {"GET", "/v1/health", 200}, {"POST", "/v1/feed", 405}, {"POST", "/admin/import", 404}, {"GET", "/config", 404}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if tc.status == 200 {
			var value any
			if json.Unmarshal(w.Body.Bytes(), &value) != nil {
				t.Fatal("invalid response")
			}
		}
	}
	path := filepath.Join(t.TempDir(), "broken.json")
	_ = os.WriteFile(path, []byte(`{"schemaVersion":9}`), 0600)
	if _, err := Open(path, false, nil); err == nil {
		t.Fatal("corrupt state reset silently")
	}
}
