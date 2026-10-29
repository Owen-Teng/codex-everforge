package xactivity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Runner struct {
	Config      Config
	Connector   Connector
	Processor   *Processor
	OnDecision  func(Decision)
	OnStatus    func(Status)
	Sleep       func(context.Context, time.Duration) bool
	Observation bool
}

func sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (r Runner) Run(parent context.Context) Status {
	status := Status{State: "disabled", Reason: "disabled", Authentication: "not_checked"}
	defer func() {
		if r.OnStatus != nil {
			r.OnStatus(status)
		}
	}()
	if !r.Config.Enabled {
		return status
	}
	if r.Connector == nil || r.Processor == nil {
		status.State = "stopped"
		status.Reason = "not_configured"
		return status
	}
	if r.Connector.IsLive() && (!r.Config.BillingApproved || !r.Config.BillingCycleLimitConfirmed) {
		status.State = "stopped"
		status.Reason = "billing_not_authorized"
		return status
	}
	validate := r.Config.Validate
	if r.Observation {
		validate = r.Config.ValidateObservation
	}
	if err := validate(); err != nil {
		status.State = "stopped"
		status.Reason = err.Error()
		return status
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(r.Config.MaxDurationSeconds)*time.Second)
	defer cancel()
	if closer, ok := r.Connector.(interface{ Close() }); ok {
		defer closer.Close()
	}
	if r.Sleep == nil {
		r.Sleep = sleep
	}
	status.State = "connecting"
	status.Reason = ""
	if r.OnStatus != nil {
		r.OnStatus(status)
	}
	for status.Attempts < r.Config.MaxAttempts && ctx.Err() == nil {
		status.Attempts++
		res, err := r.Connector.Open(ctx)
		delay := Backoff(status.Attempts)
		if err != nil {
			if retry, ok := err.(interface{ RetryDelay() time.Duration }); ok && retry.RetryDelay() > delay {
				delay = retry.RetryDelay()
			}
			var safe safeError
			if errors.As(err, &safe) {
				status.Reason = safe.code
				if safe.code == "authentication_or_permission_denied" {
					status.Authentication = "rejected_or_unverified"
				}
				if safe.code == "rate_limited" && delay < rateBackoff(status.Attempts) {
					delay = rateBackoff(status.Attempts)
				}
				if safe.code == "server_unavailable" && delay < httpBackoff(status.Attempts) {
					delay = httpBackoff(status.Attempts)
				}
				if safe.permanent {
					status.StopCondition = "permanent_error"
					break
				}
			} else {
				status.Reason = "connection_failed"
			}
		} else {
			if res.StatusCode != http.StatusOK {
				failure := statusFailure(res.StatusCode).(safeError)
				status.Reason = failure.code
				delay = httpBackoff(status.Attempts)
				if res.StatusCode == http.StatusTooManyRequests && delay < rateBackoff(status.Attempts) {
					delay = rateBackoff(status.Attempts)
				}
				if retry := retryDelay(res.Header.Get("Retry-After")); retry > delay {
					delay = retry
				}
				res.Body.Close()
				if failure.permanent {
					status.Authentication = "rejected_or_unverified"
					status.StopCondition = "permanent_error"
					break
				}
			} else {
				status.RealConnection = r.Connector.IsLive()
				status.State = "connected"
				status.ConnectionActive = true
				connectedAt := time.Now().Unix()
				status.LastConnectedAt = &connectedAt
				status.NextRetryAt = nil
				status.Reason = "waiting_for_event"
				status.Authentication = "http_accepted"
				if !status.RealConnection {
					status.Authentication = "synthetic_http"
				}
				if r.OnStatus != nil {
					r.OnStatus(status)
				}
				reason := r.read(ctx, res, &status)
				status.ConnectionActive = false
				status.Reason = reason
				if reason == "frame_limit" || reason == "wire_limit" || reason == "invalid_stream_json" || reason == "invalid_frame" || reason == "authentication_or_permission_denied" {
					status.StopCondition = reason
					break
				}
			}
		}
		if status.Attempts >= r.Config.MaxAttempts {
			break
		}
		status.State = "reconnecting"
		retryAt := time.Now().Add(delay).Unix()
		status.NextRetryAt = &retryAt
		if r.OnStatus != nil {
			r.OnStatus(status)
		}
		if !r.Sleep(ctx, delay) {
			break
		}
	}
	if ctx.Err() != nil {
		if parent.Err() != nil && !errors.Is(parent.Err(), context.DeadlineExceeded) {
			status.Reason = "cancelled"
		} else {
			status.Reason = "time_limit"
		}
		status.StopCondition = status.Reason
	}
	if status.StopCondition == "" && status.Attempts >= r.Config.MaxAttempts {
		status.StopCondition = "attempt_limit"
	}
	status.State = "stopped"
	status.ConnectionActive = false
	status.NextRetryAt = nil
	return status
}

// X's general streaming guidance starts retryable HTTP errors at 5 seconds;
// rate limiting uses at least one minute (or the longer Retry-After value).
func httpBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 7 {
		attempt = 7
	}
	return min(5*time.Second*time.Duration(1<<(attempt-1)), 320*time.Second)
}
func rateBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Minute * time.Duration(1<<(attempt-1))
}

func retryDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
		if seconds > 86400 {
			seconds = 86400
		}
		return time.Duration(seconds) * time.Second
	}
	if stamp, err := http.ParseTime(value); err == nil {
		delay := time.Until(stamp)
		if delay > 24*time.Hour {
			delay = 24 * time.Hour
		}
		if delay > 0 {
			return delay
		}
	}
	return 0
}

type monitoredReader struct {
	io.Reader
	touch func()
	total *int64
}

func (r monitoredReader) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	if n > 0 {
		*r.total += int64(n)
		r.touch()
	}
	return n, err
}
func (r Runner) read(parent context.Context, res *http.Response, status *Status) string {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer res.Body.Close()
	// Cancel both header/request context and body on silence; whitespace
	// heartbeats count as network activity, not events or notifications.
	idle := time.AfterFunc(time.Duration(r.Config.IdleTimeoutSeconds)*time.Second, func() { cancel(); _ = res.Body.Close() })
	defer idle.Stop()
	reader := monitoredReader{Reader: io.LimitReader(res.Body, r.Config.MaxWireBytes-status.WireBytes+1), total: &status.WireBytes, touch: func() {
		idle.Reset(time.Duration(r.Config.IdleTimeoutSeconds) * time.Second)
		stamp := time.Now().Unix()
		status.LastWireAt = &stamp
		if r.OnStatus != nil {
			r.OnStatus(*status)
		}
	}}
	decoder := json.NewDecoder(reader)
	for ctx.Err() == nil {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if parent.Err() != nil {
			return "cancelled"
		}
		if status.WireBytes > r.Config.MaxWireBytes {
			return "wire_limit"
		}
		if err != nil {
			if ctx.Err() != nil {
				return "idle_timeout"
			}
			var syntax *json.SyntaxError
			if err == io.EOF || err == io.ErrUnexpectedEOF || !errors.As(err, &syntax) {
				return "disconnected"
			}
			if r.OnDecision != nil {
				r.OnDecision(Decision{Reason: "invalid_stream_json"})
			}
			return "invalid_stream_json"
		}
		status.Frames++
		stamp := time.Now().Unix()
		status.LastActivityAt = &stamp
		if len(raw) > 65536 {
			return "invalid_frame"
		}
		if authErrorFrame(raw) {
			status.Authentication = "rejected_or_unverified"
			if r.OnDecision != nil {
				r.OnDecision(Decision{Reason: "authentication_or_permission_denied"})
			}
			return "authentication_or_permission_denied"
		}
		if operationalDisconnect(raw) {
			return "operational_disconnect"
		}
		decision := r.Processor.Process(raw)
		if decision.Accepted {
			status.Accepted++
		}
		if decision.Notification != nil {
			status.Notifications++
			status.LastNotificationAt = &stamp
		}
		if ctx.Err() != nil {
			return "cancelled"
		}
		if r.OnDecision != nil {
			r.OnDecision(decision)
		}
		if r.OnStatus != nil {
			r.OnStatus(*status)
		}
		if status.Frames >= r.Config.MaxDeliveredFrames {
			return "frame_limit"
		}
	}
	return "idle_timeout"
}

func operationalDisconnect(raw []byte) bool {
	var frame struct {
		Errors []struct {
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &frame) != nil {
		return false
	}
	for _, entry := range frame.Errors {
		if entry.Title == "operational-disconnect" || entry.Type == "https://api.x.com/2/problems/operational-disconnect" {
			return true
		}
	}
	return false
}

func authErrorFrame(raw []byte) bool {
	var value struct {
		Code   int `json:"code"`
		Errors []struct {
			Status int `json:"status"`
		} `json:"errors"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	if value.Code == 401 || value.Code == 403 || value.Code == 402 {
		return true
	}
	for _, entry := range value.Errors {
		if entry.Status == 401 || entry.Status == 403 || entry.Status == 402 {
			return true
		}
	}
	return false
}
