package messageinbox

import (
	"codex-quota-monitor/internal/reset"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func n57Fixture(t *testing.T) []Message {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "community", "backend", "n57", "day23-public-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Posts []Message `json:"posts"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Posts
}
func TestN57SyntheticDaySeriesMigrationPreservesRawTimesAndReceipts(t *testing.T) {
	for _, receipt := range []string{"", "baseline", "submitted", "worker_emitted"} {
		t.Run(receipt, func(t *testing.T) {
			root := t.TempDir()
			posts := n57Fixture(t)
			now := time.Now().Unix()
			d := disk{Version: 1, Revision: 7, Messages: map[string]Message{}, Initialized: map[string]bool{"events:aihot.news/codex-resets/v1": true}, EventDeliveries: map[string]reset.Delivery{}}
			stage := reset.NotificationID(posts[2].PostID + ":author_completed")
			if receipt != "" {
				d.EventDeliveries[stage] = reset.Delivery{State: receipt, Reason: "prior receipt"}
			}
			for _, p := range posts {
				p.LastSeenAt = now
				d.Messages[p.PostID] = p
			}
			if err := SaveJSON(filepath.Join(root, FileName), d); err != nil {
				t.Fatal(err)
			}
			s, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			events := s.ResetEvents()
			if len(events) != 1 || events[0].Phase != "author_completed" || len(events[0].PostIDs) != 3 || events[0].NotificationID != stage || events[0].Delivery.State != receipt {
				t.Fatalf("migration %+v", events)
			}
			for _, before := range posts {
				after, _ := s.Message(before.PostID)
				if after.Category != "signal" || after.OriginalText != before.OriginalText || *after.PublishedAt != *before.PublishedAt || after.FirstReceivedAt != before.FirstReceivedAt || after.LastSeenAt != now || after.Delivery.State != before.Delivery.State {
					t.Fatalf("altered raw/receipt %+v", after)
				}
			}
			reopened, err := Open(root)
			if err != nil || reopened.data.Revision != s.data.Revision || reopened.data.ClassificationVersion != classificationVersion {
				t.Fatal("migration repeated", err)
			}
		})
	}
}
func TestN57LateContextAndRetractionReclassifyDependents(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	posts := n57Fixture(t)
	now := time.Now()
	source := SourceState{ID: "aihot.news/codex-resets/v1"}
	for _, batch := range [][]Message{{posts[2]}, {posts[1]}, {posts[0]}} {
		if err = s.Ingest(batch, source, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	events := s.ResetEvents()
	if len(events) != 1 || events[0].Phase != "author_completed" {
		t.Fatalf("late context %+v", events)
	}
	posts[0].Status = "withdrawn"
	if err = s.Ingest(posts[:1], source, nil, now); err != nil {
		t.Fatal(err)
	}
	for _, event := range s.ResetEvents() {
		if event.Phase == "author_completed" {
			t.Fatal("withdrawn context still completed")
		}
	}
	for _, id := range []string{posts[1].PostID, posts[2].PostID} {
		m, _ := s.Message(id)
		if m.Category == "signal" {
			t.Fatal("dependent retained classification")
		}
	}
}
