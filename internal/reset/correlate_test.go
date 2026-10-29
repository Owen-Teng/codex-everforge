package reset

import "testing"

func stamp(v int64) *int64 { return &v }
func TestN54ChainAndEvidenceDistinction(t *testing.T) {
	p := map[string]Post{
		"20":   {ID: "20", Author: "thsottiaux", Text: "Day 20 / Rest and reset.", Category: "signal", Status: "active"},
		"time": {ID: "time", Author: "thsottiaux", Text: "By midnight PST.", Category: "signal", Status: "active", PublishedAt: stamp(100), Links: []Link{{"20", "thsottiaux", "reply"}}},
		"done": {ID: "done", Author: "thsottiaux", Text: "It is all propagated. Enjoy some mad codexing tomorrow. Fun week ahead too.", Category: "related", Status: "active", PublishedAt: stamp(120), AggregateStatus: "confirmed", AggregateAction: "confirm", ConfirmationBasis: "source_post", Links: []Link{{"time", "thsottiaux", "quote"}}},
		"21":   {ID: "21", Author: "thsottiaux", Text: "Day 21 / But today is a reset.", Category: "signal", Status: "active", PublishedAt: stamp(200), AggregateStatus: "likely_completed", AggregateAction: "announce"},
	}
	e := Evidence{ID: "monitor:21", SourceID: "monitor", SourceEventID: "observed-21", RelatedPostID: "21", Type: "external_observation", ObservedAt: stamp(220), CollectedAt: 300, Status: "active", Attribution: Attribution{"Codex Resets", "https://codex-resets.com"}}
	items := Build(p, []Evidence{e})
	if len(items) != 2 {
		t.Fatalf("events=%+v", items)
	}
	byID := map[string]Event{}
	for _, v := range items {
		byID[v.ID] = v
		if v.AccountRecoveryVerified || v.Independence != "unverified" {
			t.Fatal("fabricated recovery or independent votes")
		}
	}
	done := byID["reset:20"]
	observed := byID["reset:21"]
	if done.Phase != "author_completed" || done.ConfirmedAt == nil || *done.ConfirmedAt != 120 || len(done.PostIDs) != 3 {
		t.Fatalf("chain lost: %+v", done)
	}
	if observed.Phase != "externally_observed" || observed.ConfirmedAt != nil || observed.ObservedAt == nil || *observed.ObservedAt != 220 {
		t.Fatalf("promoted observation %+v", observed)
	}
	before := done.NotificationID
	delete(p, "20")
	delete(p, "time")
	// The monitor's copy of the same completion keeps its stage id if context arrives later.
	x := Evidence{ID: "other", SourceID: "other", SourceEventID: "done", PostID: "done", Type: "source_recorded_completion", PublishedAt: stamp(120), Status: "active"}
	for _, item := range Build(p, []Evidence{x}) {
		if item.ID == "reset:done" && item.NotificationID != before {
			t.Fatal("root change altered stage receipt id")
		}
	}
	delete(p, "done")
	delete(p, "time")
	delete(p, "20")
	if Build(p, nil)[0].Phase != "signal" {
		t.Fatal("likely_completed became completion")
	}
}
func TestN54ConservativeClaimsWithdrawalAndBanked(t *testing.T) {
	for _, text := range []string{"Codex limits have not been reset.", "Codex limits have been reset?", "\"Codex limits have been reset.\"", "It is all propagated. Not true.", "Daily Codex limits have been reset.", "It is all propagated. Joke."} {
		p := Post{ID: "1", Author: "thsottiaux", Text: text, Category: "announced", Status: "active", PublishedAt: stamp(100)}
		if len(Build(map[string]Post{"1": p}, nil)) != 0 {
			t.Fatalf("trusted bad category: %s", text)
		}
		if CompletionClaim(text) {
			t.Fatalf("unsafe context completion: %s", text)
		}
	}
	e := Evidence{ID: "e", SourceID: "m", SourceEventID: "1", PostID: "1", Type: "source_recorded_completion", EventKind: "banked_credit", PublishedAt: stamp(100), Status: "active"}
	v := Build(nil, []Evidence{e})[0]
	if v.Kind != "banked_credit" || v.Label != "作者称已发重置卡" {
		t.Fatal(v)
	}
	v = Build(map[string]Post{"1": {ID: "1", Status: "withdrawn"}}, []Evidence{e})[0]
	if v.Phase != "withdrawn" || v.Evidence[0].Text != "" {
		t.Fatal("withdrawal not propagated")
	}
	e.Type = "unclassified"
	if len(Build(nil, []Evidence{e})) != 0 {
		t.Fatal("ordinary text created reset")
	}
}
