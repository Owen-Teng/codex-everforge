package live

import (
	"context"
	"errors"
	"net"
	"time"
)

func sourceFetchBudget(source Source) time.Duration {
	if timed, ok := source.(interface{ FetchTimeout() time.Duration }); ok {
		if budget := timed.FetchTimeout(); budget > 0 && budget <= 45*time.Second {
			return budget
		}
	}
	return 15 * time.Second
}

func networkFailureKind(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "network"
}

func httpFailureKind(status int) string {
	if status == 429 {
		return "http_429"
	}
	if status >= 500 && status <= 599 {
		return "http_5xx"
	}
	return "http_other"
}

// Transient recovery uses the normal minimum polling cadence. Explicit server
// retry promises are stored separately and always take precedence. Legacy
// reservations are never shortened or reclassified by this function.
func publicFailureDelay(minimum int64, failures int, kind string) int64 {
	switch kind {
	case "network", "timeout", "canceled":
		return minimum
	case "http_5xx", "body", "schema":
		return min(minimum<<min(max(failures-1, 0), 5), 900)
	}
	return minimum << min(max(failures-1, 0), 5)
}

func publicFailureReason(prefix, kind string, retryAfterUntil, now int64, fallback string) string {
	switch kind {
	case "network", "timeout", "canceled", "http_429", "http_5xx", "http_other", "schema", "body":
		suffix := "_retry"
		if retryAfterUntil > now {
			suffix = "_retry_after"
		}
		return prefix + "_" + kind + suffix
	}
	return fallback
}
