package xactivity

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// SetLoopbackProxy opts this connector into an existing local HTTP proxy.
// No system settings, environment, proxy credentials or TLS settings are read.
func (c *OfficialConnector) SetLoopbackProxy(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(endpoint, "?#%") {
		return errors.New("invalid_loopback_proxy")
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 {
		return errors.New("invalid_loopback_proxy")
	}
	transport, ok := c.client.Transport.(*http.Transport)
	if !ok || c.checked || len(c.secret.value) != 0 {
		return errors.New("proxy_configuration_locked")
	}
	transport.Proxy = http.ProxyURL(u)
	return nil
}
