package xactivity

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

var ambiguousResetClaims = []struct{ name, text string }{
	{"question", "Have we just reset Codex usage limits?"},
	{"chinese-question", "Codex 额度已重置？"},
	{"routine", "Codex limits are reset every week."},
	{"test-environment", "We just reset Codex usage limits in the test environment only."},
	{"test-account", "We just reset Codex usage limits for one test account."},
	{"history", "Last month we just reset Codex usage limits."},
	{"false", "It is false that we just reset Codex limits."},
	{"late-negation", "We just reset Codex limits — actually, we haven't."},
	{"scope-before-comma", "In staging, we just reset Codex limits."},
	{"scope-after-sentence", "We just reset Codex limits. Only one account was affected."},
	{"history-after-sentence", "We just reset Codex limits. That happened last month."},
	{"chinese-test", "测试：Codex 额度已重置。"},
	{"chinese-local", "Codex 额度已重置。仅针对部分账号。"},
	{"chinese-false", "Codex 额度已重置。这是假消息。"},
	{"unknown-scope", "We just reset Codex limits for a subset of users."},
	{"conditional", "Unless approved, we just reset Codex limits."},
	{"reported", "According to someone, we just reset Codex limits."},
	{"unknown-prefix", "In a hypothetical world we just reset Codex limits."},
	{"unknown-suffix", "We just reset Codex limits in a private experiment."},
	{"comma-scope", "We just reset Codex limits, for one account."},
	{"semicolon-scope", "We just reset Codex limits; for one account."},
	{"separate-scope", "We just reset Codex limits. For one account."},
	{"chinese-conditional", "假如 Codex 额度已重置。"},
}

func TestAmbiguousResetClaimsDoNotProduceNotifications(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	for i, test := range ambiguousResetClaims {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.text); got != "" {
				t.Fatalf("ambiguous assertion classified as %s", got)
			}
			p, err := NewProcessor(c, func() time.Time { return now }, "")
			if err != nil {
				t.Fatal(err)
			}
			raw := SyntheticFrame(c, fmt.Sprintf("r6-negative-%d", i), fmt.Sprintf("92000%d", i), test.text, now, now)
			decision := p.Process(raw)
			if !decision.Accepted || decision.Notification != nil || decision.Reason != "ambiguous_or_unrelated" {
				t.Fatalf("content should be accepted without notification: %+v", decision)
			}
		})
	}
}

func TestExplicitGlobalAssertionsRetainClassification(t *testing.T) {
	for _, test := range []struct{ text, want string }{
		{"We just reset all Codex usage limits.", "announced"},
		{"Codex usage limits have just been reset.", "announced"},
		{"We just reset Codex limits for everyone.", "announced"},
		{"We just reset Codex limits for all users.", "announced"},
		{"Reset complete for Codex limits.", "announced"},
		{"Codex limits are now reset.", "announced"},
		{"Good news! We just reset Codex usage limits.", "announced"},
		{"Codex 额度已重置。感谢支持！", "announced"},
		{"Codex usage limits will be reset in 2 hours.", "notice"},
		{"Codex limits reset is scheduled tomorrow.", "notice"},
		{"We'll reset all Codex limits later today.", "notice"},
	} {
		t.Run(test.text, func(t *testing.T) {
			if got := Classify(test.text); got != test.want {
				t.Fatalf("classification = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAmbiguousClaimsThroughOfflineHTTPStream(t *testing.T) {
	c := ReplayConfig()
	now := time.Now()
	runner, closeServer := fixtureRunner(t, func(w http.ResponseWriter, r *http.Request) {
		for i, test := range ambiguousResetClaims {
			_, _ = w.Write(SyntheticFrame(c, fmt.Sprintf("r6-stream-%d", i), fmt.Sprintf("93000%d", i), test.text, now, now))
			_, _ = w.Write([]byte("\r\n"))
		}
		_, _ = w.Write(SyntheticFrame(c, "r6-stream-positive", "940001", "We just reset all Codex limits.", now, now))
	})
	defer closeServer()
	runner.Config.MaxDeliveredFrames = len(ambiguousResetClaims) + 1
	status := runner.Run(t.Context())
	if status.Frames != len(ambiguousResetClaims)+1 || status.Accepted != len(ambiguousResetClaims)+1 || status.Notifications != 1 || status.RealConnection {
		t.Fatalf("ambiguous content leaked to notifications: %+v", status)
	}
}

func TestLongObservationKeepsRollingFreshness(t *testing.T) {
	c := ReplayConfig()
	clock := time.Now()
	p, err := NewProcessor(c, func() time.Time { return clock }, "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(8 * time.Hour)
	for i, age := range []time.Duration{6 * time.Minute, time.Hour} {
		raw := SyntheticFrame(c, fmt.Sprintf("long-old-%d", i), fmt.Sprintf("95000%d", i), "We just reset Codex limits.", clock.Add(-age), clock)
		if d := p.Process(raw); d.Notification != nil || d.Reason != "old_or_future" {
			t.Fatal("long-running session accepted old publication")
		}
	}
	raw := SyntheticFrame(c, "long-fresh", "950009", "We just reset Codex limits.", clock, clock)
	if d := p.Process(raw); d.Notification == nil || d.Reason != "announced" {
		t.Fatal("long-running session dropped fresh publication")
	}
}
