package xactivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type preflightTransport func(*http.Request) (*http.Response, error)

func (f preflightTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func preflightFixture(t *testing.T) (*OfficialConnector, *credentialFixture, *int) {
	t.Helper()
	c := ReplayConfig()
	c.AuthorID = ""
	c.BillingApproved, c.BillingCycleLimitConfirmed = true, true
	reader := &credentialFixture{}
	connector := NewOfficialConnector(c, reader)
	calls := new(int)
	connector.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
		*calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-secret-marker" {
			t.Fatal("request contract changed")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > PreflightTimeout {
			t.Fatal("request deadline unbounded")
		}
		body := ""
		switch r.URL.String() {
		case SubscriptionsURL:
			body = fmt.Sprintf(`{"data":[{"subscription_id":%q,"event_type":"post.create","tag":%q,"filter":{"user_id":"999000111"}}]}`, c.SubscriptionID, c.Tag)
		case UserLookupURL:
			body = `{"data":{"id":"999000111","username":"thsottiaux"}}`
		default:
			t.Fatal("unexpected endpoint")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(connector.Close)
	return connector, reader, calls
}

func TestPreflightResolvesAuthorAndReusesVerifiedConnector(t *testing.T) {
	c, reader, calls := preflightFixture(t)
	resolved, err := c.Preflight(context.Background())
	if err != nil || resolved.AuthorID != "999000111" || *calls != 2 || reader.calls != 1 {
		t.Fatalf("preflight did not resolve in two fixed reads: %v", err)
	}
	c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != StreamURL {
			t.Fatal("preflight repeated before stream")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	res, err := c.Open(context.Background())
	if err != nil || reader.calls != 1 {
		t.Fatal("verified connector not reused")
	}
	res.Body.Close()
}

func TestPreflightGatesBeforeAnyCredentialOrRequest(t *testing.T) {
	for _, mode := range []string{"disabled", "billing", "cycle", "bad-id", "too-long", "too-many-frames", "too-many-attempts"} {
		t.Run(mode, func(t *testing.T) {
			c, reader, calls := preflightFixture(t)
			switch mode {
			case "disabled":
				c.config.Enabled = false
			case "billing":
				c.config.BillingApproved = false
			case "cycle":
				c.config.BillingCycleLimitConfirmed = false
			case "bad-id":
				c.config.SubscriptionID = "unknown"
			case "too-long":
				c.config.MaxDurationSeconds = 121
			case "too-many-frames":
				c.config.MaxDeliveredFrames = 21
			case "too-many-attempts":
				c.config.MaxAttempts = 4
			}
			if _, err := c.Preflight(context.Background()); err == nil || reader.calls != 0 || *calls != 0 {
				t.Fatal("gate accessed credential or network")
			}
		})
	}
}

func TestPreflightFailsClosedAtExactStageWithoutRetryOrLeak(t *testing.T) {
	for _, stage := range []string{"subscriptions", "username"} {
		for _, code := range []int{301, 401, 403, 402, 429, 500} {
			t.Run(fmt.Sprintf("%s-%d", stage, code), func(t *testing.T) {
				c, _, calls := preflightFixture(t)
				original := c.client.Transport
				c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
					if stage == "username" && r.URL.String() == SubscriptionsURL {
						return original.RoundTrip(r)
					}
					*calls++
					return &http.Response{StatusCode: code, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader("fixture-secret-marker"))}, nil
				})
				_, err := c.Preflight(context.Background())
				wantCalls := 1
				if stage == "username" {
					wantCalls = 2
				}
				if err == nil || err.Error() != fmt.Sprintf("%s_http_%d", stage, code) || *calls != wantCalls || len(c.secret.value) != 0 || c.checked {
					t.Fatal("failure retried, leaked, or lost stage")
				}
			})
		}
	}
}

func TestPreflightRejectsWrongSubscriptionAndUsername(t *testing.T) {
	for _, mode := range []string{"subscription-id", "tag", "event", "webhook", "multiple", "known-author", "username", "lookup-id", "lookup-errors", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			c, _, calls := preflightFixture(t)
			original := c.client.Transport
			if mode == "known-author" {
				c.config.AuthorID = "123"
			}
			c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
				res, err := original.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				raw, _ := io.ReadAll(res.Body)
				res.Body.Close()
				text := string(raw)
				if r.URL.String() == SubscriptionsURL {
					switch mode {
					case "subscription-id":
						text = strings.ReplaceAll(text, c.config.SubscriptionID, "123")
					case "tag":
						text = strings.ReplaceAll(text, ExpectedTag, "wrong")
					case "event":
						text = strings.ReplaceAll(text, "post.create", "post.reply.create")
					case "webhook":
						text = strings.Replace(text, `"event_type"`, `"webhook_id":"123","event_type"`, 1)
					case "multiple":
						text = `{"data":[],"meta":{"total_subscriptions":2}}`
					}
				} else {
					switch mode {
					case "username":
						text = strings.ReplaceAll(text, TargetUsername, "someoneelse")
					case "lookup-id":
						text = strings.ReplaceAll(text, "999000111", "123")
					case "lookup-errors":
						text = strings.TrimSuffix(text, "}") + `,"errors":[{"detail":"fixture-secret-marker"}]}`
					case "duplicate":
						text = strings.Replace(text, `"id":`, `"id":"123","id":`, 1)
					}
				}
				res.Body = io.NopCloser(strings.NewReader(text))
				return res, nil
			})
			_, err := c.Preflight(context.Background())
			if err == nil || strings.Contains(err.Error(), "fixture-secret-marker") || *calls > 2 || c.checked {
				t.Fatal("unverified identity accepted")
			}
			if !strings.HasPrefix(mode, "lookup") && mode != "username" && mode != "duplicate" && *calls != 1 {
				t.Fatal("lookup followed failed subscriptions")
			}
		})
	}
}

type failingCredential struct{}

func (failingCredential) Read(context.Context) (Secret, error) {
	return Secret{}, errors.New("fixture-secret-marker")
}

func TestPreflightCredentialNetworkAndByteFailuresAreSafe(t *testing.T) {
	c, _, calls := preflightFixture(t)
	c.reader = failingCredential{}
	if _, err := c.Preflight(context.Background()); err == nil || err.Error() != "credential_read_failed" || *calls != 0 {
		t.Fatal("credential failure not isolated")
	}
	for _, mode := range []string{"network", "bytes"} {
		c, _, _ := preflightFixture(t)
		c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
			if mode == "network" {
				return nil, errors.New("fixture-secret-marker")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", int(PreflightMaxBytes)+1)))}, nil
		})
		_, err := c.Preflight(context.Background())
		if err == nil || strings.Contains(err.Error(), "fixture-secret-marker") || c.checked {
			t.Fatal("unsafe failure")
		}
	}
}

func TestPreflightHonorsParentDeadlineAndDoesNotOpenStream(t *testing.T) {
	c, _, calls := preflightFixture(t)
	c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
		*calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > time.Second {
			t.Fatal("parent deadline extended")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Preflight(ctx)
	if err == nil || *calls != 1 || c.checked {
		t.Fatal("preflight ignored deadline")
	}
	encoded, _ := json.Marshal(err.Error())
	if strings.Contains(string(encoded), "fixture-secret-marker") {
		t.Fatal("error leaked")
	}
}

func TestPreflightByteBudgetIsSharedAcrossBothReads(t *testing.T) {
	c, _, calls := preflightFixture(t)
	original := c.client.Transport
	c.client.Transport = preflightTransport(func(r *http.Request) (*http.Response, error) {
		res, err := original.RoundTrip(r)
		if err != nil {
			return nil, err
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		// Each response fits on its own; together they exceed the total cap.
		res.Body = io.NopCloser(strings.NewReader(string(raw) + strings.Repeat(" ", 40000)))
		return res, nil
	})
	_, err := c.Preflight(context.Background())
	if err == nil || err.Error() != "username_wire_limit" || *calls != 2 || c.checked {
		t.Fatal("byte budget reset between requests")
	}
}
