package main

import "testing"

func TestBuildProxyCompatibilityRecognisesClashHybridMode(t *testing.T) {
	result := buildProxyCompatibility(
		"http://127.0.0.1:7890",
		NetworkEnvironment{Interfaces: []NetworkInterfaceSnapshot{{Name: "Mihomo", Tunnel: true, Active: true}}},
		[]ProxyListenerResult{{Endpoint: "127.0.0.1:7890", Protocol: "HTTP", Listening: true}},
		[]string{"198.18.1.10"},
	)
	if !result.Detected || result.Provider != "Clash / Mihomo" || result.Mode != "hybrid" {
		t.Fatalf("result=%+v", result)
	}
	if result.SuggestedProxyURL != "http://127.0.0.1:7890" || result.RecommendedRoute != "proxy" || !result.FakeIPDetected {
		t.Fatalf("result=%+v", result)
	}
}

func TestBuildProxyCompatibilityKeepsTunOnlyEnvironmentExplicit(t *testing.T) {
	result := buildProxyCompatibility(
		"",
		NetworkEnvironment{Interfaces: []NetworkInterfaceSnapshot{{Name: "tun0", Tunnel: true, Active: true}}},
		nil,
		nil,
	)
	if result.Mode != "tun" || result.RecommendedRoute != "direct" || result.SuggestedProxyURL != "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestBuildProxyCompatibilityUsesListeningProxyWhenSystemProxyIsEmpty(t *testing.T) {
	result := buildProxyCompatibility("", NetworkEnvironment{}, []ProxyListenerResult{
		{Endpoint: "127.0.0.1:7891", Protocol: "SOCKS5", Listening: true},
	}, nil)
	if result.SuggestedProxyURL != "socks5://127.0.0.1:7891" || result.Provider != "Clash / Mihomo" {
		t.Fatalf("result=%+v", result)
	}
}

func TestBuildProxyCompatibilityIgnoresClosedListener(t *testing.T) {
	result := buildProxyCompatibility("", NetworkEnvironment{}, []ProxyListenerResult{
		{Endpoint: "127.0.0.1:7890", Protocol: "HTTP"},
	}, nil)
	if result.Detected || result.Mode != "direct" || result.SuggestedProxyURL != "" {
		t.Fatalf("result=%+v", result)
	}
}
