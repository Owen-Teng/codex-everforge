package live

import (
	"codex-quota-monitor/internal/resetcloud/protocol"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func cloudBatch(events ...Event) SourceBatch {
	b := freshBatch(events...)
	b.SourceKind = "cloud_feed"
	b.CloudFeedAvailable = true
	b.SourceLabel = "作者公开公告"
	for i := range b.Events {
		b.Events[i].Verification = "author_claim"
	}
	return b
}
func TestCloudSameSourceOutageCatchupAndExplicitOffBaseline(t *testing.T) {
	m := managerFixture(t)
	apply(m, cloudBatch())
	m.ApplySource(SourceBatch{}, errors.New("source_network"), m.generation, fixtureNow)
	stale := cloudBatch(fixtureEvent("new-offline", "announced", fixtureNow.Unix()))
	stale.UnavailableReason = "cloud_upstream_stopped_or_stale"
	apply(m, stale)
	if m.Snapshot().Notification == nil || !m.Snapshot().Reset.SourceFresh || m.Snapshot().Reset.Reason != "cloud_upstream_stopped_or_stale" {
		t.Fatal("captured claim lost or stopped upstream hidden")
	}
	apply(m, cloudBatch(fixtureEvent("new-offline", "announced", fixtureNow.Unix())))
	state := m.Snapshot()
	if state.Notification == nil || state.Notification.Kind != "announced" || state.Reset.SourceKind != "cloud_feed" {
		t.Fatal("same-source reconnect swallowed new event")
	}
	id := state.Notification.ID
	apply(m, cloudBatch(fixtureEvent("new-offline", "announced", fixtureNow.Unix())))
	if m.Snapshot().Notification.ID != id {
		t.Fatal("duplicate replayed")
	}
	suppressed := cloudBatch()
	suppressed.SuppressedIDs = []string{"new-offline"}
	apply(m, suppressed)
	if m.Snapshot().Notification != nil || m.Snapshot().Reset.Event != nil {
		t.Fatal("withdrawal left active notice")
	}
	_ = m.SetAlerts(false)
	gen := m.generation
	_ = m.SetAlerts(true)
	m.enabledAt = fixtureNow.Unix() - 3600
	m.ApplySource(cloudBatch(fixtureEvent("late", "notice", fixtureNow.Unix())), nil, gen, fixtureNow)
	if m.Snapshot().Notification != nil {
		t.Fatal("late result escaped disable")
	}
	apply(m, cloudBatch(fixtureEvent("after-enable", "notice", fixtureNow.Unix())))
	if m.Snapshot().Notification != nil {
		t.Fatal("manual reenable not silent")
	}
	switched := cloudBatch(fixtureEvent("switched", "notice", fixtureNow.Unix()))
	switched.SourceID = "new-cloud"
	apply(m, switched)
	if m.Snapshot().Notification != nil {
		t.Fatal("source switch not silent")
	}
}

func TestCloudWorkerPreservesMultipleNewClaimsAndWithdrawal(t *testing.T) {
	m := managerFixture(t)
	apply(m, cloudBatch())
	apply(m, cloudBatch(fixtureEvent("batch-one", "notice", fixtureNow.Unix()-2), fixtureEvent("batch-two", "announced", fixtureNow.Unix()-1)))
	states := m.WorkerStates()
	if len(states) != 2 || states[0].Reset.Event.ID != "batch-one" || states[1].Reset.Event.ID != "batch-two" || states[0].Notification.ID == states[1].Notification.ID {
		t.Fatal("multiple claims collapsed")
	}
	if len(m.cloudQueue) != 0 {
		t.Fatal("queue not drained")
	}
	apply(m, cloudBatch(fixtureEvent("batch-three", "notice", fixtureNow.Unix())))
	batch := cloudBatch()
	batch.SuppressedIDs = []string{"batch-three"}
	apply(m, batch)
	if len(m.cloudQueue) != 0 {
		t.Fatal("withdrawn pending claim delivered")
	}
}
func TestCloudURLModeFreshnessAndTLSBoundaries(t *testing.T) {
	for _, url := range []string{"http://example.com/v1/feed", "https://user:pass@example.com/v1/feed", "https://example.com/v1/feed?account=private"} {
		if _, err := NewCloudSource(url); err == nil {
			t.Fatal("unsafe production URL accepted")
		}
	}
	for _, url := range []string{"http://localhost:1/v1/feed", "http://example.com/v1/feed", "https://127.0.0.1/v1/feed"} {
		if _, err := NewSyntheticCloudSource(url); err == nil {
			t.Fatal("non-literal-loopback test URL accepted")
		}
	}
	empty, _ := NewCloudSource("")
	if _, err := empty.Fetch(context.Background()); err == nil || err.Error() != "cloud_endpoint_not_configured" {
		t.Fatal("missing endpoint silently fell back")
	}
	stamp := time.Now().Unix()
	feed := protocol.Feed{SchemaVersion: 1, SourceID: protocol.SourceID, GeneratedAt: stamp, ExpiresAt: stamp + 180, Synthetic: true, Events: []protocol.Event{}, Upstream: protocol.Upstream{State: "connected", Connected: true, LastWireAt: &stamp}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(feed) }))
	defer server.Close()
	source, _ := NewSyntheticCloudSource(server.URL + "/v1/feed")
	b, err := source.Fetch(context.Background())
	if err != nil || b.UnavailableReason != "" || b.SourceKind != "synthetic_cloud" {
		t.Fatal("valid fixture failed")
	}
	feed.Upstream.Connected = false
	b, err = source.Fetch(context.Background())
	if err != nil || b.UnavailableReason == "" {
		t.Fatal("HTTP200 forged upstream connection")
	}
	feed.Synthetic = false
	if _, err = source.Fetch(context.Background()); err == nil {
		t.Fatal("mixed mode accepted")
	}
	feed.Synthetic = true
	feed.ExpiresAt = stamp - 1
	if _, err = source.Fetch(context.Background()); err == nil {
		t.Fatal("expired feed accepted")
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(feed) }))
	defer tls.Close()
	production, _ := NewCloudSource(tls.URL + "/v1/feed")
	if _, err = production.Fetch(context.Background()); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL+"/v1/feed", 302) }))
	defer redirect.Close()
	source, _ = NewSyntheticCloudSource(redirect.URL + "/v1/feed")
	if _, err = source.Fetch(context.Background()); err == nil {
		t.Fatal("redirect followed")
	}
}
