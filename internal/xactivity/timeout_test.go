package xactivity

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestOfficialHeaderWaitAndSanitizedRequestTimeout(t *testing.T) {
	c := NewOfficialConnector(DefaultConfig(), nil)
	defer c.Close()
	if c.client.Transport.(*http.Transport).ResponseHeaderTimeout != 30*time.Second || PreflightTimeout != 30*time.Second {
		t.Fatal("header or preflight timeout changed beyond requested scope")
	}
	for _, timeout := range []bool{true, false} {
		c.client.Transport = preflightTransport(func(*http.Request) (*http.Response, error) {
			if timeout {
				return nil, &net.DNSError{Err: "fixture-secret-marker", IsTimeout: true}
			}
			return nil, errors.New("fixture-secret-marker")
		})
		_, err := c.request(context.Background(), StreamURL)
		want := "connection_failed"
		if timeout {
			want = "request_timeout"
		}
		if err == nil || err.Error() != want {
			t.Fatal("request error not classified safely")
		}
	}
}
