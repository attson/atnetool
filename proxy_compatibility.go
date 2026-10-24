package main

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var clashLikePorts = map[string]struct{}{
	"7890": {}, "7891": {}, "7892": {}, "7893": {}, "7897": {}, "1080": {},
}

var fakeIPRange = mustIPNetwork("198.18.0.0/15")

// ProxyCompatibility describes the active local routing environment without
// changing proxy, DNS, route, or application settings.
type ProxyCompatibility struct {
	Detected          bool                  `json:"detected"`
	Provider          string                `json:"provider"`
	Mode              string                `json:"mode"`
	SystemProxyURL    string                `json:"system_proxy_url,omitempty"`
	SuggestedProxyURL string                `json:"suggested_proxy_url,omitempty"`
	TunnelDetected    bool                  `json:"tunnel_detected"`
	TunnelInterfaces  []string              `json:"tunnel_interfaces,omitempty"`
	FakeIPDetected    bool                  `json:"fake_ip_detected"`
	FakeIPAddresses   []string              `json:"fake_ip_addresses,omitempty"`
	Listeners         []ProxyListenerResult `json:"listeners,omitempty"`
	RecommendedRoute  string                `json:"recommended_route"`
	Coverage          string                `json:"coverage"`
	Message           string                `json:"message"`
}

func (a *App) DetectProxyCompatibility() ProxyCompatibility {
	ctx, cancel := context.WithTimeout(a.baseContext(), 3*time.Second)
	defer cancel()

	environment := captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeSystem})
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.cloudflare.com/", nil)
	var systemProxy string
	if request != nil {
		if configured, err := systemProxyForRequest(request); err == nil && configured != nil {
			systemProxy = safeProxyURL(configured)
		}
	}
	listeners := detectProxyListeners(ctx, "www.cloudflare.com")
	addresses := lookupCompatibilityAddresses(ctx, "www.cloudflare.com")
	return buildProxyCompatibility(systemProxy, environment, listeners, addresses)
}

func buildProxyCompatibility(systemProxy string, environment NetworkEnvironment, listeners []ProxyListenerResult, resolved []string) ProxyCompatibility {
	result := ProxyCompatibility{
		SystemProxyURL:   systemProxy,
		Listeners:        listeners,
		Mode:             "direct",
		RecommendedRoute: "direct",
		Coverage:         "当前未检测到显式代理，候选测试将使用系统网络出口",
		Message:          "未检测到正在接管流量的本地代理",
	}
	for _, item := range environment.Interfaces {
		if !item.Tunnel {
			continue
		}
		result.TunnelDetected = true
		result.TunnelInterfaces = appendUnique(result.TunnelInterfaces, item.Name)
		if isClashLikeName(item.Name) {
			result.Provider = "Clash / Mihomo"
		}
	}
	for _, address := range resolved {
		ip := net.ParseIP(address)
		if ip != nil && fakeIPRange != nil && fakeIPRange.Contains(ip) {
			result.FakeIPDetected = true
			result.FakeIPAddresses = appendUnique(result.FakeIPAddresses, ip.String())
		}
	}

	result.SuggestedProxyURL = normaliseCompatibilityProxy(systemProxy)
	if result.SuggestedProxyURL == "" {
		for _, listener := range listeners {
			if !listener.Listening {
				continue
			}
			if listener.Protocol != "HTTP" && listener.Protocol != "SOCKS5" {
				continue
			}
			scheme := "http"
			if listener.Protocol == "SOCKS5" {
				scheme = "socks5"
			}
			result.SuggestedProxyURL = scheme + "://" + listener.Endpoint
			break
		}
	}
	if result.Provider == "" && isClashLikeProxy(result.SuggestedProxyURL) {
		result.Provider = "Clash / Mihomo"
	}
	if result.Provider == "" && result.SuggestedProxyURL != "" {
		if systemProxy != "" {
			result.Provider = "系统代理"
		} else {
			result.Provider = "本地代理"
		}
	}

	hasProxy := result.SuggestedProxyURL != ""
	result.Detected = hasProxy || result.TunnelDetected
	switch {
	case hasProxy && result.TunnelDetected:
		result.Mode = "hybrid"
		result.RecommendedRoute = "proxy"
		result.Coverage = "候选测试将通过现有代理出口执行；TUN 仍可能接管其他应用流量"
		result.Message = "检测到本地代理与 TUN，可按现有代理出口进行兼容优选"
	case hasProxy:
		result.Mode = "system_proxy"
		result.RecommendedRoute = "proxy"
		result.Coverage = "候选测试将通过现有 HTTP / SOCKS 代理出口执行"
		result.Message = "检测到可串联的本地代理"
	case result.TunnelDetected:
		result.Mode = "tun"
		result.RecommendedRoute = "direct"
		result.Coverage = "未发现可串联端口；所谓直连仍可能被 TUN 接管"
		result.Message = "检测到 TUN，但没有发现可用的本地代理端口"
	}
	if result.FakeIPDetected {
		result.Message += "；系统 DNS 正在返回 Fake-IP"
	}
	return result
}

func lookupCompatibilityAddresses(ctx context.Context, domain string) []string {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = appendUnique(result, address.IP.String())
	}
	return result
}

func normaliseCompatibilityProxy(raw string) string {
	proxyURL, err := validateProxyURL(raw)
	if err != nil {
		return ""
	}
	return safeProxyURL(proxyURL)
}

func isClashLikeProxy(raw string) bool {
	proxyURL, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(proxyURL.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	_, ok := clashLikePorts[proxyURL.Port()]
	return ok
}

func isClashLikeName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "clash") || strings.Contains(lower, "mihomo") || strings.Contains(lower, "meta")
}

func mustIPNetwork(raw string) *net.IPNet {
	_, network, _ := net.ParseCIDR(raw)
	return network
}
