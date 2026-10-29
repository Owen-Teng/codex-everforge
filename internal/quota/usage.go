package quota

// UsageStatus contains only structural/coverage conclusions. No account, email,
// daily token values, lifetime totals, or raw response can be serialized here.
import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

type UsageStatus struct {
	State           string   `json:"state"`
	RankingEligible bool     `json:"rankingEligible"`
	Reason          string   `json:"reason"`
	CheckedAt       *int64   `json:"checkedAt"`
	BucketCount     int      `json:"bucketCount"`
	CoverageStart   *string  `json:"coverageStart"`
	CoverageEnd     *string  `json:"coverageEnd"`
	Continuous      bool     `json:"continuous"`
	FieldsPresent   []string `json:"fieldsPresent"`
}

func UnavailableUsage(reason string) UsageStatus {
	return UsageStatus{State: "unavailable", Reason: reason, FieldsPresent: []string{}}
}

func SummarizeUsage(raw json.RawMessage, now time.Time) UsageStatus {
	stamp := now.Unix()
	out := UsageStatus{State: "available", Reason: "coverage_not_verified", CheckedAt: &stamp, FieldsPresent: []string{}}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return UnavailableUsage("invalid_data")
	}
	for _, name := range []string{"summary", "dailyUsageBuckets"} {
		if field, ok := root[name]; ok && string(field) != "null" {
			out.FieldsPresent = append(out.FieldsPresent, name)
		}
	}
	buckets, ok := root["dailyUsageBuckets"]
	if !ok || string(buckets) == "null" {
		out.Reason = "daily_buckets_unavailable"
		return out
	}
	var days []struct {
		StartDate string `json:"startDate"`
		Tokens    *int64 `json:"tokens"`
	}
	if json.Unmarshal(buckets, &days) != nil {
		return UnavailableUsage("invalid_data")
	}
	out.BucketCount = len(days)
	if len(days) == 0 {
		out.Reason = "empty_coverage"
		return out
	}
	dates := make([]string, 0, len(days))
	seen := map[string]bool{}
	for _, day := range days {
		parsed, err := time.Parse("2006-01-02", day.StartDate)
		if err != nil || parsed.Format("2006-01-02") != day.StartDate || day.Tokens == nil || *day.Tokens < 0 || seen[day.StartDate] {
			return UnavailableUsage("invalid_data")
		}
		seen[day.StartDate] = true
		dates = append(dates, day.StartDate)
	}
	sort.Strings(dates)
	out.CoverageStart = &dates[0]
	out.CoverageEnd = &dates[len(dates)-1]
	first, _ := time.Parse("2006-01-02", dates[0])
	last, _ := time.Parse("2006-01-02", dates[len(dates)-1])
	out.Continuous = int(last.Sub(first)/(24*time.Hour))+1 == len(dates)
	// Continuous buckets still do not prove service timezone, finality or scope.
	return out
}

func (Provider) ReadUsage(ctx context.Context, settings Settings) UsageStatus {
	exe, err := FindCodex(settings.Executable)
	if err != nil {
		return UnavailableUsage(failureKind(err))
	}
	return readUsageProcess(ctx, exe, []string{"app-server"})
}

func failureKind(err error) string {
	if failure, ok := err.(*Failure); ok {
		return failure.Kind
	}
	return "unknown"
}

func readUsageProcess(ctx context.Context, exe string, args []string) UsageStatus {
	out := UnavailableUsage("not_checked")
	err := withRPC(ctx, exe, args, func(c *rpcClient) error {
		var before, after accountResponse
		if err := c.call("account/read", map[string]bool{"refreshToken": false}, &before); err != nil {
			return err
		}
		if _, err := accountKey(before.Account); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := c.call("account/usage/read", nil, &raw); err != nil {
			return err
		}
		if err := c.call("account/read", map[string]bool{"refreshToken": false}, &after); err != nil {
			return err
		}
		if string(before.Account) != string(after.Account) {
			return Fail("account_changed")
		}
		out = SummarizeUsage(raw, time.Now())
		return nil
	})
	if err != nil {
		out = UnavailableUsage(failureKind(err))
	}
	stamp := time.Now().Unix()
	out.CheckedAt = &stamp
	return out
}
