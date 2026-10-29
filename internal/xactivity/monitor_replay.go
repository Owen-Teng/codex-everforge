package xactivity

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

// NewMonitorReplay is an isolated loopback fixture. It has no credential reader
// and cannot target an arbitrary endpoint. It exercises 503, disconnect,
// reconnect, duplicates, old posts and the existing labeled semantic fixtures.
func NewMonitorReplay(step time.Duration) (Config, Connector, func()) {
	c := ReplayConfig()
	c.MaxDurationSeconds = 30
	c.MaxAttempts = 3
	c.MaxDeliveredFrames = 14
	frames := ReplayFrames(c, time.Now())
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		flush, _ := w.(http.Flusher)
		selected := frames
		if attempt == 2 {
			selected = frames[:5]
		}
		for _, frame := range selected {
			if r.Context().Err() != nil {
				return
			}
			_, _ = w.Write([]byte("\r\n"))
			flush.Flush()
			for offset := 0; offset < len(frame); offset += 17 {
				end := min(offset+17, len(frame))
				if _, err := w.Write(frame[offset:end]); err != nil {
					return
				}
				flush.Flush()
			}
			_, _ = w.Write([]byte("\r\n"))
			flush.Flush()
			if !sleep(r.Context(), step) {
				return
			}
		}
	}))
	connector, _ := NewLoopbackConnector(server.URL)
	return c, connector, server.Close
}
