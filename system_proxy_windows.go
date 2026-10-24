//go:build windows

package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

func platformProxyForRequest(request *http.Request) (*url.URL, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()

	if autoConfigURL, _, readErr := key.GetStringValue("AutoConfigURL"); readErr == nil && strings.TrimSpace(autoConfigURL) != "" {
		return nil, fmt.Errorf("Windows 自动代理配置（PAC）暂不支持作为上游，请改用手动代理出口")
	}
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil
	}
	if bypass, _, err := key.GetStringValue("ProxyOverride"); err == nil && windowsProxyBypass(request.URL.Hostname(), bypass) {
		return nil, nil
	}
	proxyServer, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(proxyServer) == "" {
		return nil, nil
	}
	return validateProxyURL(selectWindowsProxy(proxyServer, request.URL.Scheme))
}

func selectWindowsProxy(proxyServer, scheme string) string {
	if !strings.Contains(proxyServer, "=") {
		return proxyServer
	}
	proxies := make(map[string]string)
	for _, entry := range strings.Split(proxyServer, ";") {
		name, address, found := strings.Cut(entry, "=")
		if found {
			proxies[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(address)
		}
	}
	if address := proxies[strings.ToLower(scheme)]; address != "" {
		return address
	}
	if address := proxies["http"]; address != "" {
		return address
	}
	if address := proxies["socks"]; address != "" {
		if strings.Contains(address, "://") {
			return address
		}
		return "socks5://" + address
	}
	return ""
}

func windowsProxyBypass(host, bypassList string) bool {
	return proxyBypassHost(host, strings.Split(bypassList, ";"))
}
