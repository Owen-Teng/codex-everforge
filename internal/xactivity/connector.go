package xactivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Secret struct{ value []byte }

func (s Secret) Format(state fmt.State, verb rune) { _, _ = io.WriteString(state, "[redacted]") }
func (s Secret) MarshalJSON() ([]byte, error)      { return []byte(`"[redacted]"`), nil }
func newSecret(data []byte) (Secret, error) {
	if len(data) == 0 || len(data) > 2560 {
		return Secret{}, errors.New("credential_invalid")
	}
	for _, b := range data {
		if b < 33 || b > 126 {
			return Secret{}, errors.New("credential_invalid")
		}
	}
	return Secret{value: append([]byte(nil), data...)}, nil
}
func (s *Secret) clear() {
	for i := range s.value {
		s.value[i] = 0
	}
	s.value = nil
}

type CredentialReader interface {
	Read(context.Context) (Secret, error)
}
type CredentialManager struct{}
type Connector interface {
	Open(context.Context) (*http.Response, error)
	IsLive() bool
}
type safeError struct {
	code      string
	permanent bool
}
type retryFailure struct{ wait time.Duration }

func (e retryFailure) Error() string             { return "rate_limited" }
func (e retryFailure) Unwrap() error             { return safeError{"rate_limited", false} }
func (e retryFailure) RetryDelay() time.Duration { return e.wait }

func (e safeError) Error() string { return e.code }

type OfficialConnector struct {
	config      Config
	reader      CredentialReader
	client      *http.Client
	secret      Secret
	checked     bool
	observation bool
}

func NewOfficialConnector(c Config, reader CredentialReader) *OfficialConnector {
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	return &OfficialConnector{config: c, reader: reader, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func NewObservationConnector(c Config, reader CredentialReader) (*OfficialConnector, error) {
	if err := c.ValidateObservationPreflight(); err != nil {
		return nil, err
	}
	connector := NewOfficialConnector(c, reader)
	connector.observation = true
	return connector, nil
}
func (c *OfficialConnector) validateLiveBounds() error {
	if c.observation {
		return c.config.ValidateObservationPreflight()
	}
	return c.config.ValidateLiveTest()
}
func (*OfficialConnector) IsLive() bool { return true }
func (c *OfficialConnector) Close()     { c.secret.clear(); c.client.CloseIdleConnections() }
func (c *OfficialConnector) request(ctx context.Context, endpoint string) (*http.Response, error) {
	if endpoint != SubscriptionsURL && endpoint != UserLookupURL && endpoint != StreamURL {
		return nil, safeError{"request_endpoint_rejected", true}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, safeError{"request_failed", true}
	}
	req.Header.Set("Authorization", "Bearer "+string(c.secret.value))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "CodexQuotaMonitor-XCandidate/0.1")
	res, err := c.client.Do(req)
	if err != nil {
		var networkError net.Error
		if errors.As(err, &networkError) && networkError.Timeout() {
			return nil, safeError{"request_timeout", false}
		}
		return nil, safeError{"connection_failed", false}
	}
	return res, nil
}
func ValidateSubscription(raw []byte, c Config) error {
	_, err := subscriptionAuthor(raw, c, false)
	return err
}

func subscriptionAuthor(raw []byte, c Config, allowUnknownAuthor bool) (string, error) {
	var root struct {
		Data []struct {
			ID        string          `json:"subscription_id"`
			EventType string          `json:"event_type"`
			Tag       string          `json:"tag"`
			WebhookID json.RawMessage `json:"webhook_id"`
			Filter    struct {
				UserID string `json:"user_id"`
			} `json:"filter"`
		} `json:"data"`
		Meta struct {
			Total      *int            `json:"total_subscriptions"`
			NextToken  string          `json:"next_token"`
			NextCursor json.RawMessage `json:"next_cursor"`
		} `json:"meta"`
		NextToken  string          `json:"next_token"`
		NextCursor json.RawMessage `json:"nextCursor"`
		Errors     json.RawMessage `json:"errors"`
	}
	if len(raw) > 65536 || !uniqueJSONKeys(raw) || json.Unmarshal(raw, &root) != nil || len(root.Data) != 1 || root.Meta.Total != nil && *root.Meta.Total != 1 || !noErrors(root.Errors) || root.Meta.NextToken != "" || root.NextToken != "" || cursorPresent(root.Meta.NextCursor) || cursorPresent(root.NextCursor) {
		return "", errors.New("subscription_set_unverified")
	}
	s := root.Data[0]
	if s.ID != c.SubscriptionID || s.EventType != "post.create" || s.Tag != c.Tag || !numericID.MatchString(s.Filter.UserID) || (!allowUnknownAuthor || c.AuthorID != "") && s.Filter.UserID != c.AuthorID || len(s.WebhookID) > 0 && string(s.WebhookID) != "null" && string(s.WebhookID) != `""` {
		return "", errors.New("subscription_mismatch")
	}
	return s.Filter.UserID, nil
}

func noErrors(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var entries []json.RawMessage
	return json.Unmarshal(raw, &entries) == nil && len(entries) == 0
}
func cursorPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != `""` && string(raw) != "0"
}
func (c *OfficialConnector) Open(ctx context.Context) (*http.Response, error) {
	// This gate is checked before either a secret read or any network request.
	if !c.config.Enabled || !c.config.BillingApproved || !c.config.BillingCycleLimitConfirmed {
		return nil, safeError{"billing_not_authorized", true}
	}
	if err := c.validateLiveBounds(); err != nil {
		return nil, safeError{err.Error(), true}
	}
	if !numericID.MatchString(c.config.AuthorID) {
		return nil, safeError{"target_not_configured", true}
	}
	if len(c.secret.value) == 0 {
		if c.reader == nil {
			return nil, safeError{"not_configured", true}
		}
		secret, err := c.reader.Read(ctx)
		if err != nil || len(secret.value) == 0 {
			return nil, safeError{"not_configured", true}
		}
		c.secret = secret
	}
	if !c.checked {
		res, err := c.request(ctx, SubscriptionsURL)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			if res.StatusCode == 429 {
				return nil, retryFailure{retryDelay(res.Header.Get("Retry-After"))}
			}
			return nil, statusFailure(res.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
		res.Body.Close()
		if err != nil || ValidateSubscription(raw, c.config) != nil {
			return nil, safeError{"subscription_set_unverified", true}
		}
		c.checked = true
	}
	return c.request(ctx, StreamURL)
}
func statusFailure(status int) error {
	switch status {
	case 401, 403:
		return safeError{"authentication_or_permission_denied", true}
	case 402:
		return safeError{"payment_required", true}
	case 429:
		return safeError{"rate_limited", false}
	}
	if status >= 400 && status < 500 || status >= 300 && status < 400 {
		return safeError{"http_rejected", true}
	}
	return safeError{"server_unavailable", false}
}

// The only alternative URL is a literal loopback HTTP fixture, never an
// arbitrary remote endpoint. No Authorization header is used for replay.
type LoopbackConnector struct {
	endpoint string
	client   *http.Client
}

func NewLoopbackConnector(endpoint string) (*LoopbackConnector, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "%") {
		return nil, errors.New("loopback_fixture_required")
	}
	return &LoopbackConnector{endpoint: endpoint, client: &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 3 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (*LoopbackConnector) IsLive() bool { return false }
func (c *LoopbackConnector) Open(ctx context.Context) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.endpoint, nil)
	res, err := c.client.Do(req)
	if err != nil {
		return nil, safeError{"fixture_connection_failed", false}
	}
	return res, nil
}
func (c *LoopbackConnector) Close() { c.client.CloseIdleConnections() }
