package live

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestN56ProxySelectionWithoutNetworkOrRegistry(t *testing.T) {
	for _, tc := range []struct {
		name, target, server, bypass, noProxy string
		enabled, environment, bypassed        bool
		want                                  string
		invalid                               bool
	}{
		{name: "environment-first", target: "https://public.example/feed", server: "127.0.0.1:10808", enabled: true, environment: true, want: "environment"},
		{name: "environment-no-proxy", target: "https://public.example/feed", server: "127.0.0.1:10808", enabled: true, environment: true, bypassed: true, want: "environment_bypass"},
		{name: "no-env-system", target: "https://public.example/feed", server: "127.0.0.1:10808", enabled: true, want: "system"},
		{name: "per-scheme-system", target: "https://public.example/feed", server: "http=127.0.0.1:10807;https=127.0.0.1:10808", enabled: true, want: "system"},
		{name: "disabled", target: "https://public.example/feed", server: "127.0.0.1:10808", want: "direct"},
		{name: "localhost", target: "https://localhost/feed", server: "127.0.0.1:10808", enabled: true, want: "direct"},
		{name: "loopback", target: "http://127.0.0.1:43188/feed", server: "127.0.0.1:10808", enabled: true, want: "direct"},
		{name: "ipv6-loopback", target: "http://[::1]:43188/feed", server: "127.0.0.1:10808", enabled: true, want: "direct"},
		{name: "manual-wildcard", target: "https://public.example/feed", server: "127.0.0.1:10808", bypass: "*.example", enabled: true, want: "system_bypass"},
		{name: "manual-local", target: "https://intranet/feed", server: "127.0.0.1:10808", bypass: "<local>", enabled: true, want: "system_bypass"},
		{name: "manual-port", target: "https://public.example:8443/feed", server: "127.0.0.1:10808", bypass: "public.example:8443", enabled: true, want: "system_bypass"},
		{name: "no-proxy-only-domain", target: "https://sub.example.com/feed", server: "127.0.0.1:10808", noProxy: "example.com", enabled: true, want: "direct"},
		{name: "no-proxy-only-cidr", target: "https://10.2.3.4/feed", server: "127.0.0.1:10808", noProxy: "10.0.0.0/8", enabled: true, want: "direct"},
		{name: "no-proxy-port", target: "https://public.example:8443/feed", server: "127.0.0.1:10808", noProxy: "public.example:8443", enabled: true, want: "direct"},
		{name: "no-proxy-port-miss", target: "https://public.example:443/feed", server: "127.0.0.1:10808", noProxy: "public.example:8443", enabled: true, want: "system"},
		{name: "credentials-rejected", target: "https://public.example/feed", server: "http://synthetic:example@127.0.0.1:10808", enabled: true, want: "system_unsupported", invalid: true},
		{name: "unsupported-socks", target: "https://public.example/feed", server: "socks5://127.0.0.1:10808", enabled: true, want: "system_unsupported", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", tc.target, nil)
			reads := 0
			env := func(*http.Request) (*url.URL, error) {
				if tc.environment && !tc.bypassed {
					return url.Parse("http://127.0.0.1:10809")
				}
				return nil, nil
			}
			selected, mode, err := selectPublicProxy(r, env, tc.environment, tc.noProxy, func() (manualProxySettings, error) {
				reads++
				return manualProxySettings{Enabled: tc.enabled, Server: tc.server, Bypass: tc.bypass}, nil
			})
			if mode != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("mode=%s error=%v", mode, err)
			}
			if tc.environment && reads != 0 {
				t.Fatal("environment bypass or priority fell through to system")
			}
			if mode == "system" && (selected == nil || selected.Host != "127.0.0.1:10808") {
				t.Fatal("wrong system proxy")
			}
		})
	}
	r, _ := http.NewRequest("GET", "https://public.example/feed", nil)
	_, mode, err := selectPublicProxy(r, func(*http.Request) (*url.URL, error) { return nil, nil }, false, "", func() (manualProxySettings, error) {
		return manualProxySettings{}, errors.New("synthetic read failure")
	})
	if err == nil || mode != "system_unavailable" {
		t.Fatal("system read error silently bypassed configured proxy")
	}
}
