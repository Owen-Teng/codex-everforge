package live

import (
	"errors"
	"golang.org/x/sys/windows/registry"
)

// Read only the current user's explicitly enabled manual Internet Settings.
// PAC/WPAD evaluation and registry writes are deliberately outside this path.
func readManualSystemProxy() (manualProxySettings, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return manualProxySettings{}, nil
	}
	if err != nil {
		return manualProxySettings{}, err
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if errors.Is(err, registry.ErrNotExist) {
		return manualProxySettings{}, nil
	}
	if err != nil {
		return manualProxySettings{}, err
	}
	if enabled != 1 {
		return manualProxySettings{}, nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return manualProxySettings{}, err
	}
	bypass, _, err := key.GetStringValue("ProxyOverride")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return manualProxySettings{}, err
	}
	if len(server) > 16384 || len(bypass) > 16384 {
		return manualProxySettings{}, errors.New("system_proxy_settings_too_large")
	}
	return manualProxySettings{Enabled: true, Server: server, Bypass: bypass}, nil
}
