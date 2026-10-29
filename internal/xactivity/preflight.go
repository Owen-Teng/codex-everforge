package xactivity

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
)

const PreflightMaxBytes int64 = 65536
const PreflightTimeout = 30 * time.Second

// Preflight only reads existing subscriptions and the fixed public username.
// It makes at most two requests, never retries, and never opens a stream.
// On success this connector and the returned config share the verified author.
func (c *OfficialConnector) Preflight(parent context.Context) (Config, error) {
	c.checked = false
	if !c.config.Enabled || !c.config.BillingApproved || !c.config.BillingCycleLimitConfirmed {
		return Config{}, safeError{"billing_not_authorized", true}
	}
	if err := c.validateLiveBounds(); err != nil {
		return Config{}, safeError{err.Error(), true}
	}
	limit := min(PreflightTimeout, time.Duration(c.config.MaxDurationSeconds)*time.Second)
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	stage := "credential"
	fail := func(code string) (Config, error) {
		c.secret.clear()
		if ctx.Err() != nil {
			code = stage + "_time_limit"
			if parent.Err() != nil {
				code = stage + "_cancelled_or_deadline"
			}
		}
		return Config{}, safeError{code, true}
	}
	if ctx.Err() != nil {
		return fail("cancelled")
	}
	if c.reader == nil {
		return fail("credential_unavailable")
	}
	secret, err := c.reader.Read(ctx)
	if err != nil || len(secret.value) == 0 {
		return fail("credential_read_failed")
	}
	c.secret.clear()
	c.secret = secret
	remaining := min(PreflightMaxBytes, c.config.MaxWireBytes)
	read := func(endpoint, stage string) ([]byte, error) {
		res, err := c.request(ctx, endpoint)
		if err != nil {
			return nil, safeError{stage + "_connection_failed", true}
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, safeError{stage + "_http_" + strconv.Itoa(res.StatusCode), true}
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, remaining+1))
		remaining -= int64(len(raw))
		if remaining < 0 {
			return nil, safeError{stage + "_wire_limit", true}
		}
		if err != nil {
			return nil, safeError{stage + "_read_failed", true}
		}
		return raw, nil
	}
	stage = "subscriptions"
	raw, err := read(SubscriptionsURL, stage)
	if err != nil {
		return fail(err.Error())
	}
	author, err := subscriptionAuthor(raw, c.config, true)
	if err != nil {
		return fail(err.Error())
	}
	stage = "username"
	raw, err = read(UserLookupURL, stage)
	if err != nil {
		return fail(err.Error())
	}
	var user struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if !uniqueJSONKeys(raw) || json.Unmarshal(raw, &user) != nil || !noErrors(user.Errors) || !numericID.MatchString(user.Data.ID) {
		return fail("username_response_unverified")
	}
	if !strings.EqualFold(user.Data.Username, TargetUsername) {
		return fail("username_mismatch")
	}
	if user.Data.ID != author {
		return fail("author_id_mismatch")
	}
	if ctx.Err() != nil {
		return fail("preflight_time_limit")
	}
	c.config.AuthorID = author
	c.checked = true
	return c.config, nil
}
