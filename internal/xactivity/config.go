// Package xactivity is an opt-in finite Activity receiver. Nothing starts on import.
package xactivity

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

const CredentialTarget = "CodexQuotaMonitor/XActivityBearer"
const StreamURL = "https://api.x.com/2/activity/stream"
const SubscriptionsURL = "https://api.x.com/2/activity/subscriptions"
const ExpectedTag = "tibo-codex-reset"
const TargetUsername = "thsottiaux"
const UserLookupURL = "https://api.x.com/2/users/by/username/thsottiaux"

type Config struct {
	Enabled                    bool   `json:"enabled"`
	BillingApproved            bool   `json:"billingApproved"`
	BillingCycleLimitConfirmed bool   `json:"billingCycleLimitConfirmed"`
	AuthorID                   string `json:"authorId"`
	SubscriptionID             string `json:"subscriptionId"`
	Tag                        string `json:"tag"`
	MaxDurationSeconds         int    `json:"maxDurationSeconds"`
	MaxDeliveredFrames         int    `json:"maxDeliveredFrames"`
	MaxAttempts                int    `json:"maxAttempts"`
	MaxWireBytes               int64  `json:"maxWireBytes"`
	IdleTimeoutSeconds         int    `json:"idleTimeoutSeconds"`
}

func DefaultConfig() Config {
	return Config{Tag: ExpectedTag, MaxDurationSeconds: 120, MaxDeliveredFrames: 20, MaxAttempts: 3, MaxWireBytes: 262144, IdleTimeoutSeconds: 45}
}

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,24}$`)

func (c Config) Validate() error {
	if !numericID.MatchString(c.AuthorID) {
		return errors.New("target_not_configured")
	}
	return c.ValidatePreflight()
}

// ValidatePreflight permits only the author ID to be unresolved.
func (c Config) ValidatePreflight() error {
	return c.validatePreflightBounds(900, 100, 5, 1<<20)
}

func (c Config) validatePreflightBounds(seconds, frames, attempts int, wire int64) error {
	if c.Tag != ExpectedTag || c.AuthorID != "" && !numericID.MatchString(c.AuthorID) || !numericID.MatchString(c.SubscriptionID) {
		return errors.New("target_not_configured")
	}
	if c.MaxDurationSeconds < 1 || c.MaxDurationSeconds > seconds || c.MaxDeliveredFrames < 1 || c.MaxDeliveredFrames > frames || c.MaxAttempts < 1 || c.MaxAttempts > attempts || c.MaxWireBytes < 1024 || c.MaxWireBytes > wire || c.IdleTimeoutSeconds < 1 || c.IdleTimeoutSeconds > 120 {
		return errors.New("invalid_limits")
	}
	return nil
}

// Observation is a separate opt-in policy; the old 120-second test cannot
// select it. Live startup needs the dedicated observation authorization flag.
func (c Config) ValidateObservationPreflight() error {
	return c.validatePreflightBounds(24*60*60, 20, 3, 262144)
}
func (c Config) ValidateObservation() error {
	if !numericID.MatchString(c.AuthorID) {
		return errors.New("target_not_configured")
	}
	return c.ValidateObservationPreflight()
}

// ValidateLiveTest keeps this candidate within the one authorized test budget.
func (c Config) ValidateLiveTest() error {
	if err := c.ValidatePreflight(); err != nil {
		return err
	}
	if c.MaxDurationSeconds > 120 || c.MaxDeliveredFrames > 20 || c.MaxAttempts > 3 {
		return errors.New("test_budget_exceeded")
	}
	return nil
}
func ReadConfig(reader io.Reader) (Config, error) {
	c := DefaultConfig()
	raw, err := io.ReadAll(io.LimitReader(reader, 8193))
	if err != nil || len(raw) > 8192 || !uniqueJSONKeys(raw) {
		return Config{}, errors.New("invalid_config")
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || keys == nil {
		return Config{}, errors.New("invalid_config")
	}
	allowed := map[string]bool{"enabled": true, "billingApproved": true, "billingCycleLimitConfirmed": true, "authorId": true, "subscriptionId": true, "tag": true, "maxDurationSeconds": true, "maxDeliveredFrames": true, "maxAttempts": true, "maxWireBytes": true, "idleTimeoutSeconds": true}
	for key := range keys {
		if !allowed[key] {
			return Config{}, errors.New("invalid_config")
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF {
		return Config{}, errors.New("invalid_config")
	}
	return c, nil
}

type Status struct {
	State              string `json:"state"`
	Reason             string `json:"reason"`
	Attempts           int    `json:"attempts"`
	Frames             int    `json:"frames"`
	Accepted           int    `json:"accepted"`
	Notifications      int    `json:"notifications"`
	WireBytes          int64  `json:"wireBytes"`
	LastActivityAt     *int64 `json:"lastActivityAt"`
	Authentication     string `json:"authentication"`
	RealConnection     bool   `json:"realConnection"`
	ConnectionActive   bool   `json:"connectionActive"`
	LastConnectedAt    *int64 `json:"lastConnectedAt"`
	LastWireAt         *int64 `json:"lastWireAt"`
	LastNotificationAt *int64 `json:"lastNotificationAt"`
	NextRetryAt        *int64 `json:"nextRetryAt"`
	StopCondition      string `json:"stopCondition,omitempty"`
}

func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	delay := time.Second * time.Duration(1<<attempt)
	if delay > time.Minute {
		delay = time.Minute
	}
	return delay
}
