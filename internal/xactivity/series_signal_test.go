package xactivity

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservedSeriesTextIsOnlyAScopeUnknownSignal(t *testing.T) {
	for _, text := range []string{"Day 20/ Rest and reset", "Day 20/\nRest and reset", "Day 21 / Rest and reset."} {
		if Classify(text) != "signal" {
			t.Fatal("observed series signature missed")
		}
	}
	for _, text := range []string{
		"By midnight PST", "Rest and reset", "We will reset something", "Day 20/ Rest and reset?",
		"Day 20/ Not rest and reset", "Day 20/ Rest and reset, only one account",
		"Day 20/ Rest and reset in a test environment", "Someone said Day 20/ Rest and reset",
		"\"Day 20/ Rest and reset\"", "Day 20/ Rest and reset. This is false.",
	} {
		if Classify(text) != "" {
			t.Fatalf("unrelated/ambiguous series accepted: %q", text)
		}
	}
	c := ReplayConfig()
	now := time.Now()
	p, _ := NewProcessor(c, func() time.Time { return now }, "")
	fixture := SyntheticFrame(c, "series-fixture", "990001", "Day 20/ Rest and reset", now, now)
	d := p.Process(fixture)
	if d.Reason != "signal" || d.Notification == nil || d.Notification.Kind != "notice" || !strings.Contains(d.Notification.Title, "范围未明确") || !strings.Contains(d.Notification.Body, "尚不能认定为 Codex") || strings.Contains(d.Notification.Title, "额度已重置") {
		t.Fatalf("signal overstated: %+v", d)
	}
	if strings.Contains(d.Notification.Body, "midnight") || strings.Contains(d.Notification.Body, "PST") {
		t.Fatal("reply was interpreted as a reset deadline")
	}
	for _, test := range []struct {
		reason string
		raw    []byte
	}{
		{"target_mismatch", mutatedFrame(fixture, func(data map[string]any) { data["payload"].(map[string]any)["author_id"] = "8888" })},
		{"quoted_or_reposted", mutatedFrame(fixture, func(data map[string]any) {
			data["payload"].(map[string]any)["referenced_tweets"] = []map[string]string{{"type": "quoted", "id": "111"}}
		})},
		{"old_or_future", SyntheticFrame(c, "series-old", "990002", "Day 20/ Rest and reset", now.Add(-6*time.Minute), now)},
	} {
		fresh, _ := NewProcessor(c, func() time.Time { return now }, "")
		result := fresh.Process(test.raw)
		if result.Reason != test.reason || result.Notification != nil {
			t.Fatal("series bypassed envelope/freshness checks")
		}
	}
}

func TestSeriesSignalThroughOfflineMonitorKeepsOriginAndDedup(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	signal := SyntheticFrame(c, "series-flow", "991001", "Day 20/ Rest and reset", now, now)
	reply := SyntheticFrame(c, "series-reply", "991002", "By midnight PST", now, now)
	question := SyntheticFrame(c, "series-question", "991003", "Day 20/ Rest and reset?", now, now)
	old := SyntheticFrame(c, "series-expired", "991004", "Day 20/ Rest and reset", now.Add(-6*time.Minute), now)
	other := mutatedFrame(signal, func(data map[string]any) { data["payload"].(map[string]any)["author_id"] = "8888" })
	clear := SyntheticFrame(c, "series-clear", "991005", "We just reset all Codex limits.", now, now)
	m, closeServer := fixtureMonitor(t, func(w http.ResponseWriter, r *http.Request) {
		for _, raw := range [][]byte{signal, signal, reply, question, old, other, clear} {
			_, _ = w.Write(raw)
			_, _ = w.Write([]byte("\r\n"))
		}
	})
	defer closeServer()
	m.Config.MaxDeliveredFrames = 7
	s, err := m.Run(t.Context())
	if err != nil || s.DeliveredNotifications != 2 || s.Decisions["signal"] != 1 || s.Decisions["duplicate"] != 1 || s.Decisions["target_mismatch"] != 1 || s.Decisions["old_or_future"] != 1 {
		t.Fatalf("series pipeline: %+v %v", s, err)
	}
	file, err := os.Open(filepath.Join(m.Root, MonitorNoticeFile))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	for _, kind := range []string{"notice", "announced"} {
		var line Delivery
		if decoder.Decode(&line) != nil || line.Real != nil || line.Mode != "synthetic_x_replay" || line.Notification == nil || line.Notification.Kind != kind {
			t.Fatal("series output mislabeled")
		}
		if kind == "notice" && !strings.Contains(line.Notification.Body, "范围未明确") {
			t.Fatal("unknown scope missing")
		}
	}
}
