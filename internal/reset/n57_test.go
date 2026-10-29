package reset

import "testing"

func n57Chain() map[string]Post {
	return map[string]Post{
		"root":     {ID: "root", Author: "thsottiaux", Status: "active", Category: "signal", Text: "Day 42/ Calendar plugins. Just kidding, today is a reset. Enjoy!", PublishedAt: stamp(100)},
		"deadline": {ID: "deadline", Author: "thsottiaux", Status: "active", Category: "signal", Text: "Lands by midnight PST.", PublishedAt: stamp(150), Links: []Link{{"root", "thsottiaux", "reply"}}},
		"done":     {ID: "done", Author: "thsottiaux", Status: "active", Category: "signal", Text: "And it's done. See you tomorrow for some good news.", PublishedAt: stamp(200), AggregateStatus: "confirmed", AggregateAction: "confirm", ConfirmationBasis: "source_post", Links: []Link{{"deadline", "thsottiaux", "quote"}}},
	}
}
func TestN57ExplicitContextChainAndCounterexamples(t *testing.T) {
	for _, mode := range []string{"valid", "orphan", "wrong-author", "wrong-relation", "wrong-parent", "intermediate-unrelated", "expired-parent", "future-parent", "withdrawn-root", "expired-root", "cycle", "unconfirmed", "non-source-basis", "missing-basis", "question", "denial", "joke", "unrelated-quote", "future-extra-quote", "incomplete-root", "incomplete-done", "missing-root-time"} {
		t.Run(mode, func(t *testing.T) {
			p := n57Chain()
			done := p["done"]
			deadline := p["deadline"]
			root := p["root"]
			switch mode {
			case "orphan":
				done.Links = nil
			case "wrong-author":
				deadline.Author = "someoneelse"
			case "wrong-relation":
				done.Links[0].Relation = "mention"
			case "wrong-parent":
				deadline.Links = []Link{{"absent", "thsottiaux", "reply"}}
			case "intermediate-unrelated":
				deadline.Text = "The plugins are ready."
			case "expired-parent":
				done.PublishedAt = stamp(90000)
			case "future-parent":
				deadline.PublishedAt = stamp(201)
			case "withdrawn-root":
				root.Status = "withdrawn"
			case "expired-root":
				root.Status = "expired"
			case "cycle":
				deadline.Links = []Link{{"done", "thsottiaux", "reply"}}
			case "unconfirmed":
				done.AggregateStatus = "likely_completed"
			case "non-source-basis":
				done.ConfirmationBasis = "inference"
			case "missing-basis":
				done.ConfirmationBasis = ""
			case "question":
				done.Text = "And it's done?"
			case "denial":
				done.Text = "And it's done. Not true."
			case "joke":
				done.Text = "And it's done. Just kidding."
			case "unrelated-quote":
				p["a"] = Post{ID: "a", Author: "thsottiaux", Status: "active", Text: "A plugin is ready."}
				done.Links = append(done.Links, Link{"a", "thsottiaux", "quote"})
			case "future-extra-quote":
				p["a"] = Post{ID: "a", Author: "thsottiaux", Status: "active", Text: "Day 99/ Today is a reset.", PublishedAt: stamp(300)}
				done.Links = append(done.Links, Link{"a", "thsottiaux", "quote"})
			case "incomplete-root":
				root.TextCompleteness = "excerpt"
			case "incomplete-done":
				done.TextCompleteness = "excerpt"
			case "missing-root-time":
				root.TextCompleteness = "full"
				root.PublishedAt = nil
			}
			p["done"], p["deadline"], p["root"] = done, deadline, root
			completed := false
			for _, e := range Build(p, nil) {
				if e.Phase == "author_completed" {
					completed = true
					if e.ID != "reset:root" || len(e.PostIDs) != 3 || e.AccountRecoveryVerified {
						t.Fatalf("bad attribution %+v", e)
					}
				}
			}
			want := mode == "valid" || mode == "unrelated-quote" || mode == "future-extra-quote"
			if completed != want {
				t.Fatalf("completed=%v want %v", completed, want)
			}
		})
	}
}
func TestN57GenericCompletionCannotBypassContextViaMonitorContract(t *testing.T) {
	for _, text := range []string{"And it's done.", "And it's done. See you tomorrow for some good news."} {
		if !completion(text) || CompletionClaim(text) {
			t.Fatal("generic completion bypassed graph", text)
		}
	}
}
