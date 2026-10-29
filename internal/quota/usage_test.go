package quota

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUsageProtocolAndRedaction(t *testing.T) {
	for _, mode := range []string{"usage_ok", "usage_unsupported", "changed"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("QUOTA_TEST_RPC", mode)
			exe, _ := os.Executable()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			value := readUsageProcess(ctx, exe, []string{"-test.run=^TestRPCFixture$"})
			if value.RankingEligible {
				t.Fatal("unverified usage ranked")
			}
			switch mode {
			case "usage_ok":
				if value.State != "available" || value.BucketCount != 2 || !value.Continuous {
					t.Fatal("usage structure not available")
				}
			case "usage_unsupported":
				if value.Reason != "unsupported_version" {
					t.Fatal(value.Reason)
				}
			case "changed":
				if value.Reason != "account_changed" {
					t.Fatal(value.Reason)
				}
			}
			data, _ := json.Marshal(value)
			for _, secret := range []string{"987654321", "1234567", "7654321", "secret-test-token", "test@example"} {
				if strings.Contains(string(data), secret) {
					t.Fatal("usage privacy boundary failed")
				}
			}
		})
	}
}
func TestUsageMissingInvalidAndIncompleteCoverage(t *testing.T) {
	for _, test := range []struct {
		raw    string
		state  string
		reason string
	}{
		{`null`, "unavailable", "invalid_data"},
		{`{"summary":null,"dailyUsageBuckets":null}`, "available", "daily_buckets_unavailable"},
		{`{"dailyUsageBuckets":[]}`, "available", "empty_coverage"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-02-30","tokens":1}]}`, "unavailable", "invalid_data"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":-1}]}`, "unavailable", "invalid_data"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":null}]}`, "unavailable", "invalid_data"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":true}]}`, "unavailable", "invalid_data"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":1.2}]}`, "unavailable", "invalid_data"},
		{`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":1},{"startDate":"2026-10-12","tokens":2}]}`, "unavailable", "invalid_data"},
	} {
		out := SummarizeUsage(json.RawMessage(test.raw), time.Now())
		if out.State != test.state || out.Reason != test.reason || out.RankingEligible {
			t.Fatalf("wrong conclusion for structure: %s", test.reason)
		}
	}
	value := SummarizeUsage(json.RawMessage(`{"dailyUsageBuckets":[{"startDate":"2026-10-12","tokens":0},{"startDate":"2026-10-14","tokens":0}]}`), time.Now())
	if value.Continuous || value.BucketCount != 2 || value.RankingEligible {
		t.Fatal("missing day was treated as zero/complete")
	}
}
func TestOptionalResetCreditsPreservesNullAndZero(t *testing.T) {
	for _, suffix := range []string{``, `,"rateLimitResetCredits":null`, `,"rateLimitResetCredits":{"availableCount":0}`} {
		var response Response
		if json.Unmarshal([]byte(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":100}}`+suffix+`}`), &response) != nil {
			t.Fatal("fixture invalid")
		}
		s, err := Normalize(response, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(suffix, "availableCount") {
			if s.AvailableResetCount == nil || *s.AvailableResetCount != 0 {
				t.Fatal("valid zero lost")
			}
		} else if s.AvailableResetCount != nil {
			t.Fatal("missing reset count invented")
		}
	}
}
