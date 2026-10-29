package messageinbox

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

func previewMessage(category, text string) Message {
	stamp := int64(1793107282)
	return Message{PostID: "2114588174249071102", URL: "https://x.com/thsottiaux/status/2114588174249071102", Author: "thsottiaux", PublishedAt: &stamp, OriginalText: text, Category: category, Status: "active", TextCompleteness: "full", Verification: "aggregated_source_quote"}
}

func TestN53PreviewObservedPostsAndKnownCategories(t *testing.T) {
	cases := []struct{ name, category, text, kind, label, basis string }{
		{"day21", "signal", "Day 21/ Wonder what happens the remaining seven days. But today is a reset.", "signal", "重置未确认", "day_reset_signal"},
		{"day20", "signal", "Day 20/ Rest and reset", "signal", "重置未确认", "category_signal"},
		{"reset_count_reply", "unknown", "@larsencc @bot Not enough resets", "related", "普通互动", "interaction_reset_count"},
		{"agreement", "related", "Me too, me too", "related", "普通互动", "interaction_agreement"},
		{"mentioned_agreement", "related", "@Aoakudotcom Me too, me too", "related", "普通互动", "interaction_agreement"},
		{"deadline_signal", "signal", "By midnight PST", "signal", "时间补充", "contextual_deadline_signal"},
		{"deadline_without_context", "related", "By midnight PST", "insufficient", "信息不足", "deadline_context_missing"},
		{"deadline_unknown", "unknown", "By midnight PST", "insufficient", "信息不足", "deadline_context_missing"},
		{"deadline_notice", "notice", "By midnight PST", "notice", "重置预告", "contextual_deadline_notice"},
		{"deadline_not_confirmation", "announced", "By midnight PST", "insufficient", "信息不足", "deadline_context_missing"},
		{"explicit_notice", "notice", "We will reset Codex usage limits tomorrow.", "notice", "重置预告", "category_notice"},
		{"explicit_completed", "announced", "We have reset Codex usage limits.", "announced", "作者称已重置", "category_announced"},
		{"contextual_completed", "announced", "It is all propagated.", "announced", "作者称已重置", "category_announced"},
		{"contextual_completed_signal", "signal", "It is all propagated.", "signal", "范围未明确", "contextual_completion_signal"},
		{"completed_without_context", "related", "It is all propagated.", "insufficient", "信息不足", "completion_context_missing"},
		{"ordinary_post", "related", "We shipped a new feature.", "related", "普通动态", "category_related"},
		{"day17_legacy_related", "related", "Day 17/ Bet you thought we forgot, but no, it’s reset day today.", "insufficient", "提到重置", "observed_unscoped_reset_mention"},
		{"day18_legacy_related", "related", "Day 18/ Decisions API is GA. But yes… that’s a reset!", "insufficient", "提到重置", "observed_unscoped_reset_mention"},
		{"unclassified_reset", "unknown", "Some reset-related discussion", "insufficient", "提到重置", "unclassified_reset_mention"},
		{"unknown_future_category", "future_kind", "Anything else", "insufficient", "信息不足", "category_unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := previewMessage(tc.category, tc.text)
			p := PreviewFor(m)
			if p.Kind != tc.kind || p.Label != tc.label || p.Basis != "rule_summary:"+tc.basis {
				t.Fatalf("unexpected projection: %+v", p)
			}
			if utf8.RuneCountInString(p.Summary) > 50 || utf8.RuneCountInString(p.Label) > 10 {
				t.Fatal("list copy too long")
			}
			hasChinese := false
			for _, r := range p.Summary {
				if unicode.Is(unicode.Han, r) {
					hasChinese = true
				}
			}
			if !hasChinese {
				t.Fatal("missing Chinese summary")
			}
			if tc.category != "announced" && p.Kind == "announced" {
				t.Fatal("weak category promoted to confirmation")
			}
		})
	}
}
func TestN53PreviewDisqualifiersOverrideStrongCategory(t *testing.T) {
	cases := []struct{ text, kind string }{
		{"We have not reset Codex usage limits.", "negative"},
		{"The Codex reset has not happened yet.", "negative"},
		{"Have we reset Codex usage limits?", "question"},
		{"Have we reset Codex usage limits", "question"},
		{"If we reset Codex limits, we will announce it.", "negative"},
		{"Maybe Codex limits reset today.", "negative"},
		{"This Codex reset was a joke.", "negative"},
		{"Reset your password.", "related"},
		{"Reset the grok bot cache.", "related"},
		{"Codex limits reset every week.", "related"},
		{"Correction: the reset announcement is withdrawn.", "insufficient"},
		{"Someone said we have reset Codex limits.", "insufficient"},
		{"<script>Codex limits have been reset.</script>", "insufficient"},
		{"Reset\u202eCodex limits", "insufficient"},
		{"Codex 额度不会重置。", "negative"},
		{"Codex 额度没有重置。", "negative"},
		{"这不是重置。", "negative"},
		{"今天不重置 Codex 额度。", "negative"},
		{"有人说 Codex 额度已重置。", "insufficient"},
		{"转述：Codex 额度已重置。", "insufficient"},
		{"\"We have reset Codex limits.\"", "insufficient"},
		{"“Codex 额度已重置。”", "insufficient"},
		{"'We have reset Codex limits.'", "insufficient"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			p := PreviewFor(previewMessage("announced", tc.text))
			if p.Kind != tc.kind {
				t.Fatalf("unsafe preview: %+v", p)
			}
		})
	}
}
func TestN53PreviewStatusMissingAndExcerptPrecedence(t *testing.T) {
	cases := []struct {
		name, kind, basis string
		change            func(*Message)
	}{
		{"withdrawn", "withdrawn", "status_withdrawn", func(m *Message) { m.Status = "withdrawn"; m.OriginalText = "" }},
		{"removed", "removed", "status_removed", func(m *Message) { m.Status = "not_in_latest_snapshot"; m.OriginalText = "" }},
		{"expired", "expired", "status_expired", func(m *Message) { m.Status = "expired" }},
		{"unknown_status", "insufficient", "status_unknown", func(m *Message) { m.Status = "" }},
		{"missing_author", "insufficient", "author_missing", func(m *Message) { m.Author = "" }},
		{"other_author", "related", "other_author", func(m *Message) { m.Author = "someone_else" }},
		{"missing_body", "insufficient", "text_missing", func(m *Message) { m.OriginalText = "" }},
		{"excerpt", "insufficient", "text_incomplete", func(m *Message) { m.TextCompleteness = "excerpt" }},
		{"context", "insufficient", "text_incomplete", func(m *Message) { m.TextCompleteness = "context" }},
		{"unknown_completeness", "insufficient", "text_incomplete", func(m *Message) { m.TextCompleteness = "" }},
		{"unknown_time", "insufficient", "publication_time_missing", func(m *Message) { m.PublishedAt = nil }},
		{"unverified", "insufficient", "source_basis_missing", func(m *Message) { m.Verification = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := previewMessage("announced", "We have reset Codex limits.")
			tc.change(&m)
			p := PreviewFor(m)
			if p.Kind != tc.kind || p.Basis != "rule_summary:"+tc.basis {
				t.Fatalf("precedence failed: %+v", p)
			}
		})
	}
}
func TestN53PreviewIgnoresAggregateInferenceAndIsPure(t *testing.T) {
	m := previewMessage("signal", "Day 21/ Wonder what happens the remaining seven days. But today is a reset.")
	m.AggregateStatus = "likely_completed"
	m.AggregateAction = "confirm"
	m.Delivery.State = "submitted"
	p := PreviewFor(m)
	m.AggregateStatus = "confirmed"
	m.AggregateAction = "announce"
	m.Delivery.State = "failed"
	if PreviewFor(m) != p || p.Kind != "signal" {
		t.Fatal("aggregate inference/delivery promoted evidence")
	}
	before, _ := json.Marshal(m)
	_ = PreviewFor(m)
	after, _ := json.Marshal(m)
	if !bytes.Equal(before, after) {
		t.Fatal("projection mutated message")
	}
}
func TestN53PreviewSnapshotCopyDiskAndLegacyCompatibility(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m := previewMessage("signal", "Day 21/ Wonder what happens the remaining seven days. But today is a reset.")
	m.FirstReceivedAt = now.Unix()
	m.LastSeenAt = now.Unix()
	store.data.Messages[m.PostID] = m
	store.data.Revision = 7
	if err = SaveJSON(store.path, store.data); err != nil {
		t.Fatal(err)
	}
	diskBefore, _ := os.ReadFile(filepath.Join(root, FileName))
	memoryBefore, _ := json.Marshal(store.data)
	snapshot := store.Snapshot()
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].Preview == nil || snapshot.Messages[0].Preview.Kind != "signal" {
		t.Fatal("snapshot lacks Chinese preview")
	}
	diskAfter, _ := os.ReadFile(store.path)
	memoryAfter, _ := json.Marshal(store.data)
	if !bytes.Equal(diskBefore, diskAfter) || !bytes.Equal(memoryBefore, memoryAfter) {
		t.Fatal("display projection persisted or mutated raw data")
	}
	raw, _ := json.Marshal(snapshot.Messages[0])
	var legacy struct {
		PostID       string `json:"postId"`
		Category     string `json:"category"`
		OriginalText string `json:"originalText"`
		Status       string `json:"status"`
	}
	if err = json.Unmarshal(raw, &legacy); err != nil || legacy.PostID != m.PostID || legacy.Category != m.Category || legacy.OriginalText != m.OriginalText || legacy.Status != m.Status {
		t.Fatal("additive field broke old client")
	}
	snapshot.Messages[0].Preview.Label = "modified by caller"
	if reflect.DeepEqual(snapshot.Messages[0].Preview, store.data.Messages[m.PostID].Preview) || store.data.Messages[m.PostID].Preview != nil {
		t.Fatal("preview alias escaped into raw store")
	}
	for _, field := range []string{"postId", "category", "originalText", "status", "preview"} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatal("lost legacy field")
		}
	}
}
