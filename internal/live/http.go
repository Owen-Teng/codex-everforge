package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Bridge exposes no filesystem browsing, executable commands or arbitrary URLs.
type Bridge struct {
	Manager     *Manager
	StaticRoot  string
	Host        string
	Refresh     func()
	mu          sync.Mutex
	lastRefresh time.Time
	lastTest    time.Time
}

var staticFiles = map[string]string{
	"/": "index.html", "/index.html": "index.html", "/styles.css": "styles.css", "/app.js": "app.js",
	"/live.js": "live.js", "/translations-r2.js": "translations-r2.js",
	"/translations.js": "translations.js", "/domain.js": "domain.js",
	"/assets/maker.png": "assets/maker.png", "/assets/favicon.svg": "assets/favicon.svg",
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	if r.Host != b.Host {
		http.Error(w, "invalid_host", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+b.Host {
		http.Error(w, "invalid_origin", http.StatusForbidden)
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		http.Error(w, "cross_site", http.StatusForbidden)
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" {
		http.NotFound(w, r)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/live/state" {
		writeJSON(w, 200, b.Manager.Snapshot())
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/live/inbox" {
		writeJSON(w, 200, b.Manager.InboxSnapshot())
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/live/") {
		if r.Method != "POST" {
			http.Error(w, "method_not_allowed", 405)
			return
		}
		if r.Header.Get("Origin") != "http://"+b.Host {
			http.Error(w, "invalid_origin", 403)
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			http.Error(w, "json_required", 415)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
		if err != nil {
			http.Error(w, "invalid_body", 400)
			return
		}
		switch r.URL.Path {
		case "/api/live/notification-result":
			var value struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Reason string `json:"reason"`
			}
			if strictJSON(data, &value) != nil || b.Manager.RecordNotificationResult(value.ID, value.Status, value.Reason) != nil {
				http.Error(w, "invalid_notification_result", 400)
				return
			}
		case "/api/live/preferences":
			var value struct {
				ResetAlertsEnabled *bool `json:"resetAlertsEnabled"`
			}
			if strictJSON(data, &value) != nil || value.ResetAlertsEnabled == nil {
				http.Error(w, "invalid_fields", 400)
				return
			}
			if err := b.Manager.SetAlerts(*value.ResetAlertsEnabled); err != nil {
				writeJSON(w, 503, map[string]any{"error": err.Error(), "preferences": b.Manager.Snapshot().Preferences})
				return
			}
		case "/api/live/refresh", "/api/live/test-notification":
			var value map[string]json.RawMessage
			if strictJSON(data, &value) != nil || value == nil || len(value) != 0 {
				http.Error(w, "invalid_fields", 400)
				return
			}
			b.mu.Lock()
			now := time.Now()
			last := &b.lastRefresh
			delay := 5 * time.Second
			if r.URL.Path == "/api/live/test-notification" {
				last = &b.lastTest
				delay = 2 * time.Second
			}
			if now.Sub(*last) < delay {
				b.mu.Unlock()
				w.Header().Set("Retry-After", "5")
				http.Error(w, "rate_limited", 429)
				return
			}
			*last = now
			b.mu.Unlock()
			if r.URL.Path == "/api/live/refresh" {
				if b.Refresh != nil {
					b.Refresh()
				}
			} else if err := b.Manager.TestNotification(); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, b.Manager.Snapshot())
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "method_not_allowed", 405)
		return
	}
	name, ok := staticFiles[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	root, err := filepath.EvalSymlinks(b.StaticRoot)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		http.Error(w, "invalid_asset", 403)
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
func strictJSON(data []byte, out any) error {
	// These endpoints accept one flat object. Reject duplicate keys as well as
	// unknown fields, trailing JSON, null and arrays.
	probe := json.NewDecoder(bytes.NewReader(data))
	first, err := probe.Token()
	if err != nil || first != json.Delim('{') {
		return errors.New("invalid_json")
	}
	keys := map[string]bool{}
	for probe.More() {
		key, err := probe.Token()
		if err != nil {
			return errors.New("invalid_json")
		}
		name, ok := key.(string)
		if !ok || keys[name] {
			return errors.New("invalid_json")
		}
		keys[name] = true
		var raw json.RawMessage
		if probe.Decode(&raw) != nil {
			return errors.New("invalid_json")
		}
	}
	if _, err := probe.Token(); err != nil {
		return errors.New("invalid_json")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid_json")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func StartBridge(ctx context.Context, manager *Manager, root, address string, refresh func()) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return errors.New("loopback_required")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return errors.New("bridge_port_unavailable")
	}
	server := &http.Server{Handler: &Bridge{Manager: manager, StaticRoot: root, Host: address, Refresh: refresh}, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(stop)
	}()
	go func() { _ = server.Serve(listener) }()
	return nil
}
