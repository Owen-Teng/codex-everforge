package messageinbox

import (
	"codex-quota-monitor/internal/reset"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN54EventStoreAtomicRollbackAndProjectionBudget(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root)
	now := time.Now()
	at := now.Unix()
	e := reset.Evidence{ID: "source:one", SourceID: "source", SourceEventID: "one", Type: "external_observation", ObservedAt: &at, CollectedAt: at, Status: "active", Text: "original monitor text", Attribution: reset.Attribution{Name: "Codex Resets", URL: "https://codex-resets.com"}}
	if err := s.IngestEvidence([]reset.Evidence{e}, SourceState{ID: "source"}, now); err != nil {
		t.Fatal(err)
	}
	id := s.ResetEvents()[0].NotificationID
	if err := s.EventDelivery(id, "queued", "new", now); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := reopened.FindEventDelivery(id); !ok || d.State != "queued" {
		t.Fatal("receipt lost")
	}
	before, _ := os.ReadFile(filepath.Join(root, FileName))
	copy := s.Snapshot()
	copy.ResetEvents[0].Evidence[0].Text = "tampered"
	after, _ := os.ReadFile(filepath.Join(root, FileName))
	if string(before) != string(after) || s.ResetEvents()[0].Evidence[0].Text != "original monitor text" {
		t.Fatal("projection mutated original")
	}
	// Make SaveJSON fail without changing the real on-disk state.
	s.path = filepath.Join(root, "blocked", "state.json")
	_ = os.WriteFile(filepath.Join(root, "blocked"), []byte("file"), 0600)
	e.ID = "source:two"
	e.SourceEventID = "two"
	if err := s.IngestEvidence([]reset.Evidence{e}, SourceState{ID: "source", State: "failed"}, now); err == nil {
		t.Fatal("failed save accepted")
	}
	if len(s.ResetEvents()) != 1 {
		t.Fatal("failed ingest changed memory")
	}
	if err := s.EventDelivery(id, "submitted", "x", now); err == nil {
		t.Fatal("failed receipt save accepted")
	}
	if d, _ := s.FindEventDelivery(id); d.State != "queued" {
		t.Fatal("failed receipt changed memory")
	}
	if err := s.UpdateSourceHealth(SourceState{ID: "new"}); err == nil {
		t.Fatal("failed source save accepted")
	}
	if _, ok := s.data.Sources["new"]; ok {
		t.Fatal("failed source changed memory")
	}
	s.path = filepath.Join(root, FileName)
	for i := 0; i < 60; i++ {
		e.ID = strings.Repeat("x", i+1)
		e.SourceEventID = e.ID
		e.Text = strings.Repeat("x", 16000)
		if err := s.IngestEvidence([]reset.Evidence{e}, SourceState{ID: "source"}, now); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(s.Snapshot())
	if len(raw) > 160<<10 || len(s.ResetEvents()) != 61 {
		t.Fatal("projection budget deleted durable events")
	}
	// Old clients keep their established shape and ignore optional fields.
	var legacy struct {
		SchemaVersion int       `json:"schemaVersion"`
		Messages      []Message `json:"messages"`
	}
	if json.Unmarshal(raw, &legacy) != nil || legacy.SchemaVersion != 1 {
		t.Fatal("legacy DTO broken")
	}
}
func TestN54DisplayTranslationFallbackAndEvidenceRetention(t *testing.T) {
	zh := "来源提供的中文内容"
	m := Message{OriginalText: "Original text", Status: "active", TextCompleteness: "full", Translation: &zh, Sources: []string{"aihot.news/codex-resets/v1"}}
	if d := DisplayFor(m); d.Basis != "source_translation" || d.Source != "AIHOT" || m.OriginalText != "Original text" {
		t.Fatal(d)
	}
	for _, bad := range []string{"<script>bad</script>", "text\u202e", strings.Repeat("x", 16385)} {
		m.Translation = &bad
		if DisplayFor(m).Basis != "rule_summary" {
			t.Fatal("unsafe translation")
		}
	}
	s, _ := Open(t.TempDir())
	now := time.Now()
	old := now.Add(-8 * 24 * time.Hour).Unix()
	e := reset.Evidence{ID: "old", SourceID: "s", SourceEventID: "old", Type: "external_observation", CollectedAt: old, ObservedAt: &old, Status: "active", Text: "expired text"}
	_ = s.IngestEvidence([]reset.Evidence{e}, SourceState{ID: "s"}, now)
	if len(s.ResetEvents()) != 0 {
		t.Fatal("old monitor text retained")
	}
}
