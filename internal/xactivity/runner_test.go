package xactivity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureRunner(t *testing.T, handler http.HandlerFunc) (Runner, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	c := ReplayConfig()
	c.MaxAttempts = 1
	connector, err := NewLoopbackConnector(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewProcessor(c, time.Now, "")
	return Runner{Config: c, Connector: connector, Processor: p, Sleep: func(ctx context.Context, d time.Duration) bool { return ctx.Err() == nil }}, server.Close
}
func TestActualHTTPFragmentationHeartbeatsAndReplayEnvelope(t *testing.T) {
	var lines []ReplayLine
	status := RunReplay(context.Background(), 0, func(line ReplayLine) { lines = append(lines, line) })
	if status.Notifications != 2 || status.Frames != 9 || status.RealConnection || status.Authentication != "synthetic_http" || len(lines) != 9 {
		t.Fatalf("unexpected replay counts: %+v", status)
	}
	notices := 0
	for _, line := range lines {
		if line.Mode != "synthetic_x_replay" || line.SchemaVersion != 1 {
			t.Fatal("wrong replay mode")
		}
		if line.Notification != nil {
			notices++
			if !strings.Contains(line.Notification.Title, "联调") || line.Notification.Kind == "recovered" {
				t.Fatal("integration notice mislabeled")
			}
		}
	}
	if notices != 2 {
		t.Fatal("missing real pipeline notices")
	}
}
func TestDisconnectReconnectDeduplicatesAndBoundsAttempts(t *testing.T) {
	var connections atomic.Int32
	now := time.Now()
	c := ReplayConfig()
	first := SyntheticFrame(c, "demo-1", "900001", "Codex limits will be reset tomorrow.", now, now)
	second := SyntheticFrame(c, "demo-2", "900002", "We just reset Codex limits.", now, now)
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, request *http.Request) {
		n := connections.Add(1)
		_, _ = w.Write(append(first, '\n'))
		if n > 1 {
			_, _ = w.Write(append(second, '\n'))
		}
	})
	defer closeServer()
	r.Config.MaxAttempts = 3
	r.Config.MaxDeliveredFrames = 3
	status := r.Run(context.Background())
	if status.Attempts != 2 || status.Notifications != 2 || status.Frames != 3 {
		t.Fatalf("reconnect/dedup failed: %+v", status)
	}
}
func TestHTTPAuthenticationFailuresStopWithoutResponseLeak(t *testing.T) {
	for _, code := range []int{401, 403, 402} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("fixture-secret-marker"))
			})
			defer closeServer()
			r.Config.MaxAttempts = 3
			status := r.Run(context.Background())
			data, _ := json.Marshal(status)
			if status.Attempts != 1 || status.Notifications != 0 || strings.Contains(string(data), "fixture-secret-marker") {
				t.Fatal("auth failure retried or leaked")
			}
		})
	}
}
func TestStreamAuthenticationErrorStopsWithoutRetryOrLeak(t *testing.T) {
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"status":401,"detail":"fixture-secret-marker"}]}`))
	})
	defer closeServer()
	r.Config.MaxAttempts = 3
	status := r.Run(context.Background())
	data, _ := json.Marshal(status)
	if status.Attempts != 1 || status.Notifications != 0 || status.Reason != "authentication_or_permission_denied" || strings.Contains(string(data), "fixture-secret-marker") {
		t.Fatal("stream auth handling failed")
	}
}
func TestHTTP429RetryAfterAndServerErrorsAreBounded(t *testing.T) {
	for _, code := range []int{429, 500} {
		r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "60"); w.WriteHeader(code) })
		r.Config.MaxAttempts = 3
		waits := []time.Duration{}
		r.Sleep = func(ctx context.Context, d time.Duration) bool { waits = append(waits, d); return true }
		status := r.Run(context.Background())
		closeServer()
		if status.Attempts != 3 || len(waits) != 2 || waits[0] < time.Minute {
			t.Fatal("retry was unbounded or ignored server wait")
		}
	}
}
func TestMalformedJSONStopsAndPartialEOFCanReconnect(t *testing.T) {
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{bad-json}`)) })
	defer closeServer()
	r.Config.MaxAttempts = 3
	status := r.Run(context.Background())
	if status.Reason != "invalid_stream_json" || status.Attempts != 1 {
		t.Fatal("malformed stream retried")
	}
	var calls atomic.Int32
	now := time.Now()
	c := ReplayConfig()
	raw := SyntheticFrame(c, "demo-ok", "900001", "We just reset Codex limits.", now, now)
	partial, closePartial := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"data":`))
		} else {
			_, _ = w.Write(raw)
		}
	})
	defer closePartial()
	partial.Config.MaxAttempts = 2
	partial.Config.MaxDeliveredFrames = 1
	if status := partial.Run(context.Background()); status.Attempts != 2 || status.Notifications != 1 {
		t.Fatal("partial-frame recovery failed")
	}
}
func TestCancellationIdleAndTimeLimitCloseOwnHTTPConnection(t *testing.T) {
	for _, mode := range []string{"cancel", "idle", "duration"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				close(started)
				<-request.Context().Done()
			})
			defer closeServer()
			r.Config.IdleTimeoutSeconds = 1
			if mode == "duration" {
				r.Config.IdleTimeoutSeconds = 120
				r.Config.MaxDurationSeconds = 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan Status, 1)
			go func() { done <- r.Run(ctx) }()
			<-started
			if mode == "cancel" {
				cancel()
			}
			select {
			case status := <-done:
				if status.Notifications != 0 || mode == "cancel" && status.Reason != "cancelled" || mode == "duration" && status.Reason != "time_limit" || mode == "idle" && status.Reason != "idle_timeout" {
					t.Fatalf("stop reason: %s", status.Reason)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not stop")
			}
		})
	}
}
func TestWireAndFrameLimits(t *testing.T) {
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat(" ", 2048))) })
	defer closeServer()
	r.Config.MaxWireBytes = 1024
	status := r.Run(context.Background())
	if status.Reason != "wire_limit" || status.Notifications != 0 {
		t.Fatal("wire budget failed")
	}
	if Backoff(1) != 2*time.Second || Backoff(100) > time.Minute {
		t.Fatal("backoff unbounded")
	}
	if retryDelay("9223372036854775807") != 24*time.Hour {
		t.Fatal("Retry-After overflow bypassed wait")
	}
}
func TestLoopbackFixtureCannotRedirectToRemoteOrUseCredentials(t *testing.T) {
	for _, endpoint := range []string{"https://api.x.com/2/activity/stream", "http://localhost:1234", "http://user@127.0.0.1:1234", "http://127.0.0.1:1234?a=secret"} {
		if _, err := NewLoopbackConnector(endpoint); err == nil {
			t.Fatal("unsafe fixture URL")
		}
	}
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://api.x.com/2/activity/stream", 302)
	})
	defer closeServer()
	if status := r.Run(context.Background()); status.Reason != "http_rejected" {
		t.Fatal("redirect followed")
	}
}

func TestLiveOutputCallbackUsesSameDTOWithoutFalseOriginOrRecovery(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	frame := SyntheticFrame(c, "demo-live-output", "900008", "We just reset Codex usage limits.", now, now)
	r, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(frame) })
	defer closeServer()
	r.Config.MaxDeliveredFrames = 1
	var lines []Delivery
	var states []string
	r.OnStatus = func(status Status) { states = append(states, status.State) }
	r.OnDecision = func(decision Decision) { lines = append(lines, LiveDelivery(decision, r.Connector.IsLive())) }
	status := r.Run(context.Background())
	if status.Notifications != 1 || len(lines) != 1 || lines[0].Notification == nil {
		t.Fatal("live path dropped notification")
	}
	if len(states) < 3 || states[0] != "connecting" || states[1] != "connected" || states[len(states)-1] != "stopped" {
		t.Fatal("connection state not visible")
	}
	line := lines[0]
	if line.Mode != "x_activity_candidate" || line.Real == nil || *line.Real || line.Source != "x_activity_stream" || line.Verification != "author_claim" || line.Notification.Kind != "announced" {
		t.Fatal("live output origin/claim invalid")
	}
	encoded, _ := json.Marshal(ReplayDelivery(Decision{Notification: line.Notification}))
	var replay map[string]any
	_ = json.Unmarshal(encoded, &replay)
	if len(replay) != 3 || replay["mode"] != "synthetic_x_replay" {
		t.Fatal("replay envelope contract expanded")
	}
}
