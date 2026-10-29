package live

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const fixtureHost = "127.0.0.1:43188"

func request(b *Bridge, method, path, body, origin, content string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+fixtureHost+path, strings.NewReader(body))
	r.Host = fixtureHost
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if content != "" {
		r.Header.Set("Content-Type", content)
	}
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	return w
}
func TestHTTPHostOriginAndFields(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	b := &Bridge{Manager: m, StaticRoot: t.TempDir(), Host: fixtureHost}
	for _, test := range []struct {
		method, path, body, origin, content string
		status                              int
	}{
		{"GET", "/api/live/state", "", "", "", 200},
		{"GET", "/api/live/state", "", "https://evil.example", "", 403},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true}`, "", "application/json", 403},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true}`, "null", "application/json", 403},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true}`, "http://" + fixtureHost, "text/plain", 415},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true,"extra":1}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true,"resetAlertsEnabled":false}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":1}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/preferences", `{}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/preferences", `null`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/preferences", `{"resetAlertsEnabled":true} {}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/refresh", `{"url":"https://evil.example"}`, "http://" + fixtureHost, "application/json", 400},
		{"POST", "/api/live/test-notification", `{}`, "http://" + fixtureHost, "application/json", 409},
		{"OPTIONS", "/api/live/state", "", "", "", 405},
		{"GET", "/api/live/state?callback=x", "", "", "", 404},
		{"GET", "/../AGENTS.md", "", "", "", 404},
		{"GET", "/assets/PROVENANCE.md", "", "", "", 404},
	} {
		t.Run(test.method+test.path+test.body, func(t *testing.T) {
			w := request(b, test.method, test.path, test.body, test.origin, test.content)
			if w.Code != test.status {
				t.Fatalf("status %d, want %d", w.Code, test.status)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("privacy headers missing")
			}
		})
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/live/state", nil)
	r.Host = "evil.example"
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("host rebinding accepted")
	}
	r = httptest.NewRequest("GET", "http://"+fixtureHost+"/api/live/state", nil)
	r.Host = fixtureHost
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-site request accepted")
	}
}
func TestHTTPPreferencesTestNotificationAndRateLimit(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	refreshes := 0
	b := &Bridge{Manager: m, Host: fixtureHost, Refresh: func() { refreshes++ }}
	origin := "http://" + fixtureHost
	if request(b, "POST", "/api/live/preferences", `{"resetAlertsEnabled":true}`, origin, "application/json").Code != 200 {
		t.Fatal("enable failed")
	}
	if request(b, "POST", "/api/live/test-notification", `{}`, origin, "application/json").Code != 200 || m.Snapshot().Notification.Kind != "test" {
		t.Fatal("test not created")
	}
	if request(b, "POST", "/api/live/test-notification", `{}`, origin, "application/json").Code != 429 {
		t.Fatal("test spam not limited")
	}
	if request(b, "POST", "/api/live/refresh", `{}`, origin, "application/json").Code != 200 || refreshes != 1 {
		t.Fatal("refresh not queued")
	}
	if request(b, "POST", "/api/live/refresh", `{}`, origin, "application/json").Code != 429 {
		t.Fatal("refresh spam not limited")
	}
	if request(b, "POST", "/api/live/preferences", `{"resetAlertsEnabled":false}`, origin, "application/json").Code != 200 || m.Snapshot().Notification != nil {
		t.Fatal("close did not suppress pending native notification")
	}
}
func TestAllCurrentPageScriptsAndStylesAreServed(t *testing.T) {
	root := filepath.Join("..", "..", "preview", "community-mvp")
	index, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Manager: NewManager(t.TempDir(), nil), Host: fixtureHost, StaticRoot: root}
	if request(b, "GET", "/", "", "", "").Code != 200 {
		t.Fatal("index failed")
	}
	refs := regexp.MustCompile(`(?:src|href)="([^"#]+)"`).FindAllSubmatch(index, -1)
	checked := 0
	for _, ref := range refs {
		name := string(ref[1])
		if strings.Contains(name, ":") {
			continue
		}
		if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css") {
			continue
		}
		if request(b, "GET", "/"+strings.TrimPrefix(name, "/"), "", "", "").Code != 200 {
			t.Errorf("required page asset unavailable: %s", name)
		}
		checked++
	}
	if checked < 6 {
		t.Fatal("current page dependency check did not cover all scripts")
	}
}
func TestBridgeRefusesRemoteBindingAndOccupiedPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewManager(t.TempDir(), nil)
	if StartBridge(ctx, m, "", "0.0.0.0:43188", nil) == nil {
		t.Fatal("non-loopback listener accepted")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if StartBridge(ctx, m, "", listener.Addr().String(), nil) == nil {
		t.Fatal("occupied port not reported")
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTiboAdapterDoesNotInventUpstreamHealthOrTrustProviderVerification(t *testing.T) {
	calls := 0
	source := &TiboSource{Client: &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "tibo.modelyard.dev" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("private or arbitrary source request")
		}
		body := `{"status":"ok","checkedAt":"2026-10-25T08:00:00Z"}`
		if r.URL.Path == "/api/events" {
			body = `{"data":[{"id":1,"category":"RESET_PLANNED","published_at":"2026-10-25T07:00:00Z","summary_zh":"模拟源预告","source_url":"https://x.com/demo/status/123","verification_status":"DIRECT_VERIFIED"},{"id":2,"category":"CODEX_UPDATE"}],"total":2}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	batch, err := source.Fetch(context.Background())
	if err != nil || calls != 2 || batch.UpstreamSuccessAt != nil || len(batch.Events) != 1 || batch.Events[0].Verification != "third_party" {
		t.Fatal("adapter trusted unsupported freshness/verification")
	}
}
func TestSourceRejectsMarkupUnsafeLinksAndUnknownKinds(t *testing.T) {
	for _, link := range []string{"https://evil.example/demo/status/123", "http://x.com/demo/status/123", "https://x.com:443/demo/status/123", "https://x.com/demo/status/123?x=1", "https://user@x.com/demo/status/123", "https://x.com/demo/status/123#x", "https://x.com/settings"} {
		if validPostURL(link) {
			t.Fatal("unsafe source URL accepted")
		}
	}
	for _, text := range []string{"<img src=x onerror=alert(1)>", "hello\nworld", "hello\u202efoo"} {
		if plainSummary(text) != "" {
			t.Fatal("unsafe source text accepted")
		}
	}
	for _, raw := range []string{`{"category":"RESET_PLANNED","id":1}`, `{"category":"CODEX_UPDATE"}`, `null`, `{"category":"RESET_PLANNED","id":1,"published_at":"not-a-date"}`} {
		if _, ok := parseTiboEvent([]byte(raw)); ok {
			t.Fatal("invalid event accepted")
		}
	}
}
