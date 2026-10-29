package xactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConservativeClassification(t *testing.T) {
	for _, test := range []struct{ text, kind string }{
		{"Codex usage limits will be reset tomorrow.", "notice"},
		{"We’re resetting Codex usage limits tomorrow", "notice"},
		{"We're resetting Codex usage limits tomorrow", "notice"},
		{"We are going to reset Codex limits", "notice"},
		{"We will be resetting something else.", ""},
		{"We'll reset Codex limits tomorrow", "notice"},
		{"We just reset Codex usage limits.", "announced"},
		{"Codex usage limits have now been reset.", "announced"},
		{"Codex 额度已重置。", "announced"},
		{"Codex 额度将重置。", "notice"},
		{"I reset my password. Codex limits are unchanged", ""},
		{"I reset my password, Codex limits are unchanged", ""},
		{"I reset my password and Codex limits are unchanged", ""},
		{"Codex limits are reset tomorrow", ""},
		{"Codex usage limits have not been reset.", ""},
		{"Maybe we just reset Codex limits", ""},
		{"If we just reset Codex limits, would it help?", ""},
		{"Someone said we just reset Codex usage limits", ""},
		{"\"We just reset Codex usage limits\"", ""},
		{"New Codex model is available.", ""},
		{"We just reset API usage limits.", ""},
		{"<b>We just reset Codex usage limits</b>", ""},
		{"Codex usage limits have been reset. Codex limits will be reset tomorrow.", ""},
	} {
		t.Run(test.text, func(t *testing.T) {
			if actual := Classify(test.text); actual != test.kind {
				t.Fatalf("classification %q, want %q", actual, test.kind)
			}
		})
	}
}
func mutatedFrame(raw []byte, mutate func(map[string]any)) []byte {
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	mutate(data["data"].(map[string]any))
	out, _ := json.Marshal(data)
	return out
}
func TestEnvelopeTargetAndUntrustedObjectValidation(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	raw := SyntheticFrame(c, "demo-event", "900000", "We just reset Codex limits.", now, now)
	if _, err := Parse(raw, c); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"event", func(d map[string]any) { d["event_type"] = "post.reply.create" }},
		{"tag", func(d map[string]any) { d["tag"] = "another-subscription" }},
		{"filter", func(d map[string]any) { d["filter"].(map[string]any)["user_id"] = "8888" }},
		{"author", func(d map[string]any) { d["payload"].(map[string]any)["author_id"] = "8888" }},
		{"subscription", func(d map[string]any) { d["subscription_id"] = "8888" }},
		{"missing-id", func(d map[string]any) { delete(d, "event_uuid") }},
		{"invalid-date", func(d map[string]any) { d["payload"].(map[string]any)["created_at"] = "today" }},
		{"quote", func(d map[string]any) {
			d["payload"].(map[string]any)["referenced_tweets"] = []map[string]string{{"type": "quoted", "id": "100"}}
		}},
		{"repost", func(d map[string]any) {
			d["payload"].(map[string]any)["referenced_tweets"] = []map[string]string{{"type": "retweeted", "id": "100"}}
		}},
		{"new-schema-repost", func(d map[string]any) {
			d["payload"].(map[string]any)["referenced_posts"] = []map[string]string{{"type": "retweeted", "id": "100"}}
		}},
		{"html", func(d map[string]any) { d["payload"].(map[string]any)["text"] = "<script>reset Codex limits</script>" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(mutatedFrame(raw, test.mutate), c); err == nil {
				t.Fatal("invalid event accepted")
			}
		})
	}
	reply := mutatedFrame(raw, func(d map[string]any) {
		d["payload"].(map[string]any)["referenced_tweets"] = []map[string]string{{"type": "replied_to", "id": "100"}}
	})
	if _, err := Parse(reply, c); err != nil {
		t.Fatal("own clear reply should remain eligible")
	}
}
func TestProcessorDedupAcrossDeliveriesAndRestart(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	path := filepath.Join(t.TempDir(), "seen.json")
	p, err := NewProcessor(c, func() time.Time { return now }, path)
	if err != nil {
		t.Fatal(err)
	}
	raw := SyntheticFrame(c, "demo-1", "900001", "We just reset Codex usage limits.", now, now)
	first := p.Process(raw)
	if first.Notification == nil || first.Notification.Kind != "announced" {
		t.Fatal("valid author announcement missing")
	}
	if first.Notification.Kind == "recovered" || strings.Contains(first.Notification.Body, "本机额度已回升。") {
		t.Fatal("author claim turned into local recovery")
	}
	if second := p.Process(raw); second.Notification != nil || second.Reason != "duplicate" {
		t.Fatal("same delivery repeated")
	}
	redelivered := mutatedFrame(raw, func(d map[string]any) { d["event_uuid"] = "demo-2" })
	if p.Process(redelivered).Notification != nil {
		t.Fatal("same post with new delivery repeated")
	}
	restarted, _ := NewProcessor(c, func() time.Time { return now }, path)
	if restarted.Process(raw).Notification != nil {
		t.Fatal("restart replayed post")
	}
	data, _ := os.ReadFile(path)
	var state map[string]int64
	if json.Unmarshal(data, &state) != nil {
		t.Fatal("dedup state invalid")
	}
	for key := range state {
		if len(key) != 64 {
			t.Fatal("raw identity/content persisted")
		}
	}
}
func TestProcessorOldFutureAndSaveFailureFailClosed(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	p, _ := NewProcessor(c, func() time.Time { return now }, "")
	for _, stamp := range []time.Time{now.Add(-24 * time.Hour), now.Add(-6 * time.Minute), now.Add(6 * time.Minute)} {
		raw := SyntheticFrame(c, "demo-old", "900001", "We just reset Codex limits.", stamp, now)
		if p.Process(raw).Notification != nil {
			t.Fatal("old/future notification")
		}
	}
	blocked := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocked, []byte("fixture"), 0600)
	p, _ = NewProcessor(c, func() time.Time { return now }, "")
	p.stateFile = filepath.Join(blocked, "seen.json")
	decision := p.Process(SyntheticFrame(c, "demo-save", "900002", "We just reset Codex limits.", now, now))
	if decision.Notification != nil || decision.Reason != "dedup_save_failed" {
		t.Fatal("unsaved dedup emitted notice")
	}
}
func TestSubscriptionMustBeExactSingleStreamOnlyTarget(t *testing.T) {
	c := ReplayConfig()
	valid := fmt.Sprintf(`{"data":[{"subscription_id":%q,"event_type":"post.create","filter":{"user_id":%q},"tag":%q}],"meta":{"total_subscriptions":1}}`, c.SubscriptionID, c.AuthorID, c.Tag)
	if ValidateSubscription([]byte(valid), c) != nil {
		t.Fatal("valid subscription rejected")
	}
	var object map[string]any
	_ = json.Unmarshal([]byte(valid), &object)
	delete(object, "meta")
	for _, emptyErrors := range []any{nil, []any{}} {
		object["errors"] = emptyErrors
		raw, _ := json.Marshal(object)
		if ValidateSubscription(raw, c) != nil {
			t.Fatal("official data-only/empty-errors shape rejected")
		}
	}
	delete(object, "errors")
	raw, _ := json.Marshal(object)
	if ValidateSubscription(raw, c) != nil {
		t.Fatal("missing optional errors/meta rejected")
	}
	object["meta"] = map[string]any{"next_token": "another-page"}
	raw, _ = json.Marshal(object)
	if ValidateSubscription(raw, c) == nil {
		t.Fatal("unknown paginated remainder accepted")
	}
	for _, bad := range []string{strings.Replace(valid, "post.create", "post.reply.create", 1), strings.Replace(valid, c.Tag, "other", 1), strings.Replace(valid, `"total_subscriptions":1`, `"total_subscriptions":2`, 1), strings.Replace(valid, `"event_type"`, `"webhook_id":"100","event_type"`, 1), `{"data":[]}`} {
		if ValidateSubscription([]byte(bad), c) == nil {
			t.Fatal("unsafe subscription set accepted")
		}
	}
}

type credentialFixture struct{ calls int }

func (f *credentialFixture) Read(context.Context) (Secret, error) {
	f.calls++
	return newSecret([]byte("fixture-secret-marker"))
}
func TestDisabledAndBudgetGatesDoNotReadCredentials(t *testing.T) {
	for _, mode := range []string{"disabled", "not-approved", "cycle-not-confirmed", "invalid-target"} {
		c := ReplayConfig()
		c.BillingApproved = true
		c.BillingCycleLimitConfirmed = true
		switch mode {
		case "disabled":
			c.Enabled = false
		case "not-approved":
			c.BillingApproved = false
		case "cycle-not-confirmed":
			c.BillingCycleLimitConfirmed = false
		case "invalid-target":
			c.AuthorID = ""
		}
		reader := &credentialFixture{}
		connector := NewOfficialConnector(c, reader)
		_, _ = connector.Open(context.Background())
		connector.Close()
		if reader.calls != 0 {
			t.Fatal("gate read a credential")
		}
	}
}
func TestSecretsAndConfigurationAreNeverPrintableOrAcceptedFromConfig(t *testing.T) {
	secret, _ := newSecret([]byte("fixture-secret-marker"))
	data, _ := json.Marshal(secret)
	for _, text := range []string{fmt.Sprintf("%v", secret), fmt.Sprintf("%#v", secret), fmt.Sprintf("%+v", &secret), string(data)} {
		if strings.Contains(text, "fixture-secret-marker") {
			t.Fatal("secret serialization leak")
		}
	}
	if _, err := ReadConfig(strings.NewReader(`{"bearerToken":"fixture-secret-marker"}`)); err == nil {
		t.Fatal("secret accepted in JSON config")
	}
	if _, err := newSecret([]byte("bad\r\nheader")); err == nil {
		t.Fatal("header injection accepted")
	}
}

func TestDuplicateKeysAndInvalidUTF8CannotSpoofTarget(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	raw := SyntheticFrame(c, "demo-event", "900001", "We just reset Codex limits.", now, now)
	duplicated := strings.Replace(string(raw), `"event_type":"post.create"`, `"event_type":"other","event_type":"post.create"`, 1)
	if _, err := Parse([]byte(duplicated), c); err == nil {
		t.Fatal("duplicate target keys accepted")
	}
	if _, err := Parse([]byte{'{', '"', 'x', '"', ':', '"', 255, '"', '}'}, c); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
