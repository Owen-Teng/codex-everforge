package live

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type manualProxySettings struct {
	Enabled        bool
	Server, Bypass string
}
type publicProxyState struct {
	mu   sync.Mutex
	mode string
}

func (s *publicProxyState) Mode() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode == "" {
		return "not_checked"
	}
	return s.mode
}
func (s *publicProxyState) set(mode string) { s.mu.Lock(); s.mode = mode; s.mu.Unlock() }

func environmentProxyConfigured(r *http.Request) bool {
	if r.URL.Scheme == "https" {
		return os.Getenv("HTTPS_PROXY") != "" || os.Getenv("https_proxy") != ""
	}
	return os.Getenv("HTTP_PROXY") != "" || os.Getenv("http_proxy") != ""
}

func newPublicHTTPClient(timeout time.Duration) (*http.Client, *publicProxyState) {
	state := &publicProxyState{}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		proxy, mode, err := selectPublicProxy(r, http.ProxyFromEnvironment, environmentProxyConfigured(r), os.Getenv("NO_PROXY")+","+os.Getenv("no_proxy"), readManualSystemProxy)
		state.set(mode)
		return proxy, err
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, state
}

func selectPublicProxy(r *http.Request, environment func(*http.Request) (*url.URL, error), configured bool, noProxy string, system func() (manualProxySettings, error)) (*url.URL, string, error) {
	proxy, err := environment(r)
	if err != nil {
		return nil, "environment_error", errors.New("public_environment_proxy_invalid")
	}
	if proxy != nil {
		return proxy, "environment", nil
	}
	// An explicit environment bypass must never fall through to a system proxy.
	if configured {
		return nil, "environment_bypass", nil
	}
	if publicLoopback(r.URL.Hostname()) || noProxyBypass(r.URL, noProxy) {
		return nil, "direct", nil
	}
	settings, err := system()
	if err != nil {
		return nil, "system_unavailable", errors.New("public_system_proxy_unavailable")
	}
	if !settings.Enabled {
		return nil, "direct", nil
	}
	if manualProxyBypass(r.URL, settings.Bypass) {
		return nil, "system_bypass", nil
	}
	server := strings.TrimSpace(settings.Server)
	if strings.Contains(server, "=") {
		server = ""
		for _, entry := range strings.Split(settings.Server, ";") {
			bits := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(bits) == 2 && strings.EqualFold(strings.TrimSpace(bits[0]), r.URL.Scheme) {
				server = strings.TrimSpace(bits[1])
				break
			}
		}
	}
	if server == "" {
		return nil, "direct", nil
	}
	if !strings.Contains(server, "://") {
		server = "http://" + server
	}
	proxy, err = url.Parse(server)
	if err != nil || (proxy.Scheme != "http" && proxy.Scheme != "https") || proxy.Hostname() == "" || proxy.User != nil || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.Path != "" && proxy.Path != "/" {
		return nil, "system_unsupported", errors.New("public_system_proxy_invalid")
	}
	return proxy, "system", nil
}

func publicLoopback(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func requestPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
func noProxyBypass(u *url.URL, list string) bool {
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	for _, entry := range strings.Split(list, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			if ip != nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if h, p, err := net.SplitHostPort(entry); err == nil {
			if p != requestPort(u) {
				continue
			}
			entry = h
		}
		entry = strings.Trim(entry, "[]")
		if candidate := net.ParseIP(entry); candidate != nil {
			if ip != nil && candidate.Equal(ip) {
				return true
			}
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			entry = entry[1:]
		}
		if strings.HasPrefix(entry, ".") {
			if strings.HasSuffix(host, entry) {
				return true
			}
		} else if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}
func manualProxyBypass(u *url.URL, list string) bool {
	host := strings.ToLower(u.Hostname())
	if publicLoopback(host) {
		return true
	}
	for _, entry := range strings.Split(list, ";") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		if entry == "<local>" {
			if net.ParseIP(host) == nil && !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if strings.Contains(entry, "://") {
			parsed, err := url.Parse(entry)
			if err != nil || parsed.Scheme != u.Scheme {
				continue
			}
			entry = parsed.Host
		}
		if matched, _ := path.Match(entry, host); matched {
			return true
		}
		if matched, _ := path.Match(entry, strings.ToLower(u.Host)); matched {
			return true
		}
	}
	return false
}
