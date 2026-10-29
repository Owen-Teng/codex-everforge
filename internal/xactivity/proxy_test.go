package xactivity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoopbackProxyAcceptsOnlyExplicitLocalHTTPAddress(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:10808", "http://[::1]:10808"} {
		c := NewOfficialConnector(DefaultConfig(), nil)
		if err := c.SetLoopbackProxy(endpoint); err != nil {
			t.Fatal(err)
		}
		transport := c.client.Transport.(*http.Transport)
		u, err := transport.Proxy(&http.Request{})
		if err != nil || u.String() != endpoint || transport.TLSClientConfig != nil {
			t.Fatal("proxy changed TLS validation")
		}
		c.Close()
	}
	for _, endpoint := range []string{"", "http://localhost:10808", "http://example.com:10808", "http://192.168.1.1:10808", "https://127.0.0.1:10808", "socks5://127.0.0.1:10808", "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:abc", "http://a:b@127.0.0.1:10808", "http://127.0.0.1:10808/", "http://127.0.0.1:10808/path", "http://127.0.0.1:10808?", "http://127.0.0.1:10808?q=1", "http://127.0.0.1:10808#", "http://127.0.0.1:10808#x", "http://127.0.0.1:10808/%2f", "http://[::]:10808"} {
		c := NewOfficialConnector(DefaultConfig(), nil)
		if err := c.SetLoopbackProxy(endpoint); err == nil || c.client.Transport.(*http.Transport).Proxy != nil {
			t.Fatalf("unsafe proxy accepted: %s", endpoint)
		}
		c.Close()
	}
}

func TestProxyConfigurationDoesNotBypassBillingGatesOrTargetAllowlist(t *testing.T) {
	for _, mode := range []string{"disabled", "billing", "cycle", "budget"} {
		c := ReplayConfig()
		c.BillingApproved, c.BillingCycleLimitConfirmed = true, true
		switch mode {
		case "disabled":
			c.Enabled = false
		case "billing":
			c.BillingApproved = false
		case "cycle":
			c.BillingCycleLimitConfirmed = false
		case "budget":
			c.MaxDurationSeconds = 121
		}
		reader := &credentialFixture{}
		connector := NewOfficialConnector(c, reader)
		if err := connector.SetLoopbackProxy("http://127.0.0.1:1"); err != nil {
			t.Fatal(err)
		}
		if _, err := connector.Preflight(context.Background()); err == nil || reader.calls != 0 {
			t.Fatal("proxy bypassed gate")
		}
		if _, err := connector.request(context.Background(), "https://example.com/"); err == nil || err.Error() != "request_endpoint_rejected" {
			t.Fatal("authorization target unrestricted")
		}
		connector.Close()
	}
	c := NewOfficialConnector(DefaultConfig(), nil)
	if c.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("default stopped being direct")
	}
	c.Close()
}

func TestProxyCONNECTCarriesNoBearerAndPreservesTLSValidation(t *testing.T) {
	for _, mode := range []string{"valid", "untrusted", "wrong-host", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			dns := "api.x.com"
			if mode == "wrong-host" {
				dns = "other.example"
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dns}, DNSNames: []string{dns}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			cert, _ := x509.ParseCertificate(der)
			var deliveries, connectCalls, badConnect atomic.Int32
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deliveries.Add(1)
				if r.Host != "api.x.com" || r.Header.Get("Authorization") != "Bearer fixture-secret-marker" {
					t.Error("wrong auth target")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://example.com/", 302)
					return
				}
				switch r.URL.Path {
				case "/2/activity/subscriptions":
					io.WriteString(w, `{"data":[{"subscription_id":"999000222","event_type":"post.create","tag":"tibo-codex-reset","filter":{"user_id":"999000111"}}]}`)
				case "/2/users/by/username/thsottiaux":
					io.WriteString(w, `{"data":{"id":"999000111","username":"thsottiaux"}}`)
				case "/2/activity/stream":
					io.WriteString(w, "\n")
				default:
					t.Error("unexpected path")
				}
			}))
			target.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
			target.Config.ErrorLog = log.New(io.Discard, "", 0)
			target.StartTLS()
			defer target.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connectCalls.Add(1)
				if r.Method != "CONNECT" || r.Host != "api.x.com:443" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
					badConnect.Add(1)
					w.WriteHeader(400)
					return
				}
				upstream, err := net.DialTimeout("tcp", target.Listener.Addr().String(), time.Second)
				if err != nil {
					w.WriteHeader(502)
					return
				}
				client, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					upstream.Close()
					return
				}
				io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { defer upstream.Close(); defer client.Close(); io.Copy(upstream, client) }()
				go func() { defer upstream.Close(); defer client.Close(); io.Copy(client, upstream) }()
			}))
			defer proxy.Close()
			cfg := ReplayConfig()
			cfg.AuthorID = ""
			cfg.BillingApproved, cfg.BillingCycleLimitConfirmed = true, true
			connector := NewOfficialConnector(cfg, &credentialFixture{})
			defer connector.Close()
			if err := connector.SetLoopbackProxy(proxy.URL); err != nil {
				t.Fatal(err)
			}
			roots := x509.NewCertPool()
			if mode != "untrusted" {
				roots.AddCert(cert)
			}
			// Test-only trust root; production leaves default certificate validation intact.
			connector.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			resolved, err := connector.Preflight(ctx)
			if mode == "untrusted" || mode == "wrong-host" {
				if err == nil || deliveries.Load() != 0 || !strings.HasPrefix(err.Error(), "subscriptions_") {
					t.Fatal("TLS validation bypassed")
				}
			} else if mode == "redirect" {
				if err == nil || err.Error() != "subscriptions_http_302" || deliveries.Load() != 1 {
					t.Fatal("redirect followed")
				}
			} else {
				if err != nil || resolved.AuthorID != "999000111" {
					t.Fatalf("proxy preflight failed: %v", err)
				}
				res, err := connector.Open(ctx)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				if deliveries.Load() != 3 || connectCalls.Load() != 1 {
					t.Fatal("preflight and stream did not share tunnel")
				}
			}
			if badConnect.Load() != 0 || connectCalls.Load() < 1 {
				t.Fatal("CONNECT leaked bearer or wrong endpoint")
			}
		})
	}
}
