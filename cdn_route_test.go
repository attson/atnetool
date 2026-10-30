package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConfigureCDNRouteUsesDetectedProxyInAutoMode(t *testing.T) {
	settings := cdnOptimizeSettings{}
	result := CDNOptimizeResult{ProxyCompatibility: ProxyCompatibility{
		RecommendedRoute:  cdnRouteProxy,
		SuggestedProxyURL: "http://127.0.0.1:7890",
	}}
	if err := configureCDNRoute(CDNOptimizeOptions{}, &settings, &result); err != nil {
		t.Fatal(err)
	}
	if settings.UpstreamProxy == nil || result.EffectiveRoute != cdnRouteProxy || result.UpstreamProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("settings=%+v result=%+v", settings, result)
	}
}

func TestConfigureCDNRouteDoesNotCallTunDirect(t *testing.T) {
	settings := cdnOptimizeSettings{}
	result := CDNOptimizeResult{ProxyCompatibility: ProxyCompatibility{TunnelDetected: true}}
	if err := configureCDNRoute(CDNOptimizeOptions{RouteMode: cdnRouteDirect}, &settings, &result); err != nil {
		t.Fatal(err)
	}
	if settings.UpstreamProxy != nil || result.EffectiveRoute != cdnRouteTunnel {
		t.Fatalf("settings=%+v result=%+v", settings, result)
	}
}

func TestConfigureCDNRouteDoesNotExposeProxyCredentials(t *testing.T) {
	settings := cdnOptimizeSettings{}
	result := CDNOptimizeResult{}
	if err := configureCDNRoute(CDNOptimizeOptions{
		RouteMode:        cdnRouteProxy,
		UpstreamProxyURL: "http://user:secret@127.0.0.1:7890",
	}, &settings, &result); err != nil {
		t.Fatal(err)
	}
	if settings.UpstreamProxy == nil || settings.UpstreamProxy.User == nil {
		t.Fatalf("proxy credentials were not retained for the connection: %+v", settings.UpstreamProxy)
	}
	if result.UpstreamProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("credentials leaked into result: %q", result.UpstreamProxyURL)
	}
}

func TestBuildCDNPathEvidenceKeepsTunRouteExplicit(t *testing.T) {
	evidence := buildCDNPathEvidence("example.com", NetworkEnvironment{Interfaces: []NetworkInterfaceSnapshot{{
		Name: "Mihomo", Tunnel: true,
	}}}, cdnRouteTunnel, "")
	if !evidence.TunnelDetected || evidence.HostsApplicable || !strings.Contains(evidence.Message, "Mihomo") {
		t.Fatalf("evidence=%+v", evidence)
	}
}

func TestApplyCDNRouteGovernanceDoesNotSuggestDirectConfigForProxyResult(t *testing.T) {
	result := CDNOptimizeResult{
		BestIP:             "104.16.1.20",
		EffectiveRoute:     cdnRouteProxy,
		TargetValidated:    true,
		TargetVerification: CDNTargetVerification{Verified: true},
		Governance:         []CDNGovernanceAdvice{{Action: "verify", Title: "先验证业务完整性"}},
	}
	applyCDNRouteGovernance(&result)
	if len(result.Governance) == 0 || result.Governance[0].Action != "gateway_apply" {
		t.Fatalf("governance=%+v", result.Governance)
	}
	for _, item := range result.Governance {
		if item.Action == "verify" {
			t.Fatalf("direct governance leaked into proxy result: %+v", result.Governance)
		}
	}
}

func TestApplyCDNRouteGovernanceDoesNotAdvertiseBlockedGatewayAction(t *testing.T) {
	result := CDNOptimizeResult{
		BestIP:             "104.16.1.20",
		EffectiveRoute:     cdnRouteProxy,
		TargetVerification: CDNTargetVerification{Verified: true},
		Governance: []CDNGovernanceAdvice{{
			Action: "retest_http", Title: "切换 HTTPing 后重新优选",
		}},
	}
	applyCDNRouteGovernance(&result)
	if len(result.Governance) != 1 || result.Governance[0].Action != "retest_http" {
		t.Fatalf("governance=%+v", result.Governance)
	}
}

func TestRunCDNProbeWithRouteTargetsCandidateThroughHTTPProxy(t *testing.T) {
	type observation struct{ connectTarget, host string }
	observed := make(chan observation, 1)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connectTarget := request.Host
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Error("proxy response writer does not support hijacking")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack proxy connection: %v", err)
			return
		}
		defer connection.Close()
		if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Errorf("write CONNECT response: %v", err)
			return
		}
		if err := buffered.Flush(); err != nil {
			t.Errorf("flush CONNECT response: %v", err)
			return
		}
		tunneledRequest, err := http.ReadRequest(bufio.NewReader(connection))
		if err != nil {
			t.Errorf("read tunneled request: %v", err)
			return
		}
		observed <- observation{connectTarget: connectTarget, host: tunneledRequest.Host}
		_, _ = fmt.Fprint(connection, "HTTP/1.1 200 OK\r\nCF-Ray: route-test-HKG\r\nServer: cloudflare\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	}))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}

	sample := runCDNProbeWithRoute(
		context.Background(), "http://example.com/", "example.com", "104.16.1.20", 1, 80, 2*time.Second, false, proxyURL,
	)
	if !sample.Success || sample.CFRay != "route-test-HKG" {
		t.Fatalf("sample=%+v", sample)
	}
	got := <-observed
	if got.connectTarget != "104.16.1.20:80" || got.host != "example.com" {
		t.Fatalf("proxy observed CONNECT %q and tunneled Host %q", got.connectTarget, got.host)
	}
}
