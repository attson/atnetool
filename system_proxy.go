package main

import (
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// systemProxyForRequest prefers Go's standard proxy environment variables,
// then falls back to the operating system proxy where supported.
func systemProxyForRequest(request *http.Request) (*url.URL, error) {
	proxyURL, err := http.ProxyFromEnvironment(request)
	if proxyURL != nil || err != nil {
		return proxyURL, err
	}
	return platformProxyForRequest(request)
}

func proxyBypassHost(host string, rules []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	address := net.ParseIP(host)
	for _, rawRule := range rules {
		rule := strings.ToLower(strings.TrimSpace(rawRule))
		if rule == "" {
			continue
		}
		if rule == "<local>" && !strings.Contains(host, ".") {
			return true
		}
		if _, network, err := net.ParseCIDR(rule); err == nil && address != nil && network.Contains(address) {
			return true
		}
		if matched, err := path.Match(rule, host); err == nil && matched {
			return true
		}
		if strings.HasPrefix(rule, ".") && strings.HasSuffix(host, rule) {
			return true
		}
	}
	return false
}
