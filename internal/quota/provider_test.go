package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// A subprocess fixture exercises the actual stdio handshake, error handling, and cleanup.
func TestRPCFixture(t *testing.T) {
	mode := os.Getenv("QUOTA_TEST_RPC")
	if mode == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	initialized := false
	accountReads := 0
	for scan.Scan() {
		var q struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.Unmarshal(scan.Bytes(), &q)
		var result any = map[string]any{}
		switch q.Method {
		case "initialize":
			initialized = true
		case "initialized":
			continue
		case "account/read":
			if !initialized {
				os.Exit(9)
			}
			accountReads++
			email := "test@example.invalid"
			if mode == "changed" && accountReads > 1 {
				email = "changed@example.invalid"
			}
			result = map[string]any{"account": map[string]any{"type": "chatgpt", "email": email}}
		case "account/rateLimits/read":
			if mode == "hang" {
				time.Sleep(10 * time.Second)
				os.Exit(0)
			}
			if mode == "rate" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": q.ID, "error": map[string]any{"code": 429, "message": "too many requests secret-test-token", "data": map[string]int{"retryAfterSeconds": 600}}})
				continue
			}
			result = map[string]any{"rateLimits": map[string]any{"limitId": "codex", "primary": map[string]any{"usedPercent": 25, "windowDurationMins": 10080, "resetsAt": 2000000000}}}
		case "account/usage/read":
			if mode == "usage_unsupported" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": q.ID, "error": map[string]any{"code": -32601, "message": "secret-test-token method unavailable"}})
				continue
			}
			result = map[string]any{"summary": map[string]any{"lifetimeTokens": 987654321}, "dailyUsageBuckets": []map[string]any{{"startDate": "2026-10-12", "tokens": 1234567}, {"startDate": "2026-10-13", "tokens": 7654321}}}
		default:
			os.Exit(8)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": q.ID, "result": result})
	}
	os.Exit(0)
}
func runFixture(t *testing.T, mode string, timeout time.Duration) (Snapshot, error) {
	t.Helper()
	t.Setenv("QUOTA_TEST_RPC", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return readProcess(ctx, exe, []string{"-test.run=^TestRPCFixture$"}, "")
}
func TestActualProtocolRoundtrip(t *testing.T) {
	s, e := runFixture(t, "ok", 4*time.Second)
	if e != nil || s.Remaining == nil || *s.Remaining != 75 {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestAccountChangeDiscardsSnapshot(t *testing.T) {
	s, e := runFixture(t, "changed", 4*time.Second)
	if e == nil || s.Remaining != nil || e.Error() != "account_changed" {
		t.Fatalf("%+v %v", s, e)
	}
}
func TestRateLimitAndSanitizedError(t *testing.T) {
	_, e := runFixture(t, "rate", 4*time.Second)
	f, ok := e.(*Failure)
	if !ok || f.Kind != "rate_limited" || f.RetryAfter != 10*time.Minute {
		t.Fatal(e)
	}
	if strings.Contains(Reason(e), "secret") || strings.Contains(e.Error(), "secret") {
		t.Fatal("raw error leaked")
	}
}
func TestProcessCancellation(t *testing.T) {
	start := time.Now()
	_, e := runFixture(t, "hang", 200*time.Millisecond)
	if e == nil || time.Since(start) > 3*time.Second {
		t.Fatal("child did not terminate promptly", e)
	}
}
