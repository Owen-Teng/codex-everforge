package resettext

import (
	"testing"
)

func TestN51DaySeriesAndConservativeBoundaries(t *testing.T) {
	for _, text := range []string{"Day 21/ Wonder what happens the remaining seven days. But today is a reset.", "Day 20/ Rest and reset", "Day 22/ Today is a reset."} {
		if Classify(text) != "signal" {
			t.Fatalf("series missed: %q", text)
		}
	}
	for _, text := range []string{"Day 22/ Today is not a reset.", "Day 22/ Is today a reset?", "Reset your password.", "We have not reset Codex limits.", "If we reset Codex limits, we will announce it.", "Codex limits reset every week.", "Someone said we reset Codex limits.", "Day 22/ Today is a reset password.", "We reset one test account only.", "It is all propagated."} {
		if Classify(text) != "" {
			t.Fatalf("unsafe completion: %q", text)
		}
	}
	if Classify("We will reset Codex usage limits tomorrow.") != "notice" || Classify("We have reset Codex usage limits.") != "announced" {
		t.Fatal("explicit claims missed")
	}
}
