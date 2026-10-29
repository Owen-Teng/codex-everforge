package xactivity

// Synthetic fixtures use the documented envelope and a real loopback HTTP
// stream. Only this output adapter adds the integration-test title/mode.
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"time"
)

func ReplayConfig() Config {
	c := DefaultConfig()
	c.Enabled = true
	c.AuthorID = "999000111"
	c.SubscriptionID = "999000222"
	return c
}
func SyntheticFrame(c Config, eventID, postID, text string, postAt, deliveryAt time.Time) []byte {
	value := map[string]any{"data": map[string]any{"event_uuid": eventID, "created_at": deliveryAt.UTC().Format(time.RFC3339Nano), "event_type": "post.create", "tag": c.Tag,
		"filter": map[string]string{"user_id": c.AuthorID}, "payload": map[string]any{"id": postID, "author_id": c.AuthorID, "created_at": postAt.UTC().Format(time.RFC3339Nano), "text": text}}}
	raw, _ := json.Marshal(value)
	return raw
}
func ReplayFrames(c Config, now time.Time) [][]byte {
	old := SyntheticFrame(c, "demo-old", "900001", "Codex usage limits will be reset tomorrow.", now.Add(-48*time.Hour), now)
	notice := SyntheticFrame(c, "demo-notice", "900002", "Codex usage limits will be reset tomorrow.", now, now)
	negative := SyntheticFrame(c, "demo-negative", "900003", "Codex usage limits have not been reset.", now, now)
	quoted := SyntheticFrame(c, "demo-quote", "900004", "We just reset Codex usage limits.", now, now)
	var value map[string]any
	_ = json.Unmarshal(quoted, &value)
	value["data"].(map[string]any)["payload"].(map[string]any)["referenced_tweets"] = []map[string]string{{"type": "quoted", "id": "700000"}}
	quoted, _ = json.Marshal(value)
	unrelated := SyntheticFrame(c, "demo-feature", "900005", "New Codex feature launch.", now, now)
	announced := SyntheticFrame(c, "demo-announced", "900006", "We just reset Codex usage limits.", now, now)
	other := SyntheticFrame(c, "demo-other", "900007", "We just reset Codex usage limits.", now, now)
	_ = json.Unmarshal(other, &value)
	value["data"].(map[string]any)["payload"].(map[string]any)["author_id"] = "999999"
	other, _ = json.Marshal(value)
	frames := [][]byte{old, notice, notice, negative, quoted, unrelated, announced, other, []byte(`{}`)}
	// A new explicit replay is a new synthetic session. Keep the in-session
	// duplicate identical, while avoiding old-ID suppression by a running host.
	prefix := strconv.FormatInt(now.UnixNano(), 10)
	for i, raw := range frames {
		if i == 8 {
			continue
		}
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		data := item["data"].(map[string]any)
		post := data["payload"].(map[string]any)
		index := i
		if i == 2 {
			index = 1
		}
		data["event_uuid"] = "demo-" + prefix + "-" + strconv.Itoa(index)
		post["id"] = prefix + strconv.Itoa(index)
		frames[i], _ = json.Marshal(item)
	}
	return frames
}
func RunReplay(ctx context.Context, step time.Duration, emit func(ReplayLine)) Status {
	c := ReplayConfig()
	frames := ReplayFrames(c, time.Now())
	c.MaxDeliveredFrames = len(frames)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flush, _ := w.(http.Flusher)
		for _, frame := range frames {
			if r.Context().Err() != nil {
				return
			}
			_, _ = w.Write([]byte("\r\n"))
			flush.Flush()
			// Deliberate packet fragmentation; the receiver cannot assume one
			// transport read is one event or that blank heartbeats are posts.
			for offset := 0; offset < len(frame); offset += 17 {
				end := offset + 17
				if end > len(frame) {
					end = len(frame)
				}
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
	defer server.Close()
	connector, _ := NewLoopbackConnector(server.URL)
	processor, _ := NewProcessor(c, time.Now, "")
	return (Runner{Config: c, Connector: connector, Processor: processor, OnDecision: func(decision Decision) {
		if emit != nil {
			emit(ReplayDelivery(decision))
		}
	}}).Run(ctx)
}
