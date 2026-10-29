//go:build !windows

package live

func readManualSystemProxy() (manualProxySettings, error) { return manualProxySettings{}, nil }
