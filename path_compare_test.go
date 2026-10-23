package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunPathCompareEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
		_, _ = response.Write([]byte("path compare"))
	}))
	defer server.Close()

	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "*")
	result := NewApp().RunPathCompare(PathCompareOptions{
		TaskID:    "path-compare-e2e",
		TargetURL: server.URL,
		Samples:   2,
	})
	if result.Status != "completed" || result.Completed != 4 || result.Total != 4 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Paths) != 2 || result.Paths[0].SuccessCount != 2 || result.Paths[1].SuccessCount != 2 {
		t.Fatalf("paths = %+v", result.Paths)
	}
	if result.Finding.Code != "paths_same" {
		t.Fatalf("finding = %+v", result.Finding)
	}
}

func TestFinalisePathCompareRoute(t *testing.T) {
	route := PathCompareRoute{ID: "system", SampleCount: 3, Samples: []PathCompareSample{
		{Success: true, TCPConnectMS: 20, TLSHandshakeMS: 40, TTFBMS: 60, TotalMS: 120, ProxyURL: "http://127.0.0.1:7890", RemoteIP: "127.0.0.1"},
		{Success: false, TotalMS: 900, ProxyURL: "http://127.0.0.1:7890"},
		{Success: true, TCPConnectMS: 10, TLSHandshakeMS: 30, TTFBMS: 50, TotalMS: 80, ProxyURL: "http://127.0.0.1:7890"},
	}}
	finalisePathCompareRoute(&route)
	if route.SuccessCount != 2 || route.P50TotalMS != 80 || route.P50TCPMS != 10 {
		t.Fatalf("route = %+v", route)
	}
	if route.EffectivePath != "proxy:http://127.0.0.1:7890" || math.Abs(route.JitterMS-40) > 0.001 {
		t.Fatalf("effective path or jitter = %+v", route)
	}
}

func TestMarkEquivalentPaths(t *testing.T) {
	paths := []PathCompareRoute{
		{ID: "system", EffectivePath: "direct"},
		{ID: "direct", EffectivePath: "direct"},
		{ID: "manual", EffectivePath: "proxy:http://127.0.0.1:7890"},
	}
	markEquivalentPaths(paths)
	if paths[1].SameAs != "system" || paths[2].SameAs != "" {
		t.Fatalf("paths = %+v", paths)
	}
}

func TestFinalisePathCompareRouteDoesNotTreatInvalidProxyAsDirect(t *testing.T) {
	route := PathCompareRoute{
		ID: "system", ProxyMode: proxyModeSystem, SampleCount: 1,
		Samples: []PathCompareSample{{Success: false, ProxyStatus: "invalid"}},
	}
	finalisePathCompareRoute(&route)
	if route.EffectivePath != "unresolved:system" {
		t.Fatalf("effective path = %q", route.EffectivePath)
	}
}

func TestBuildPathCompareFindingRecognisesSamePath(t *testing.T) {
	finding := buildPathCompareFinding([]PathCompareRoute{
		{ID: "system", Label: "系统代理", EffectivePath: "direct", SuccessCount: 3, SuccessRate: 1, P50TotalMS: 100},
		{ID: "direct", Label: "强制直连", EffectivePath: "direct", SameAs: "system", SuccessCount: 3, SuccessRate: 1, P50TotalMS: 80},
	})
	if finding.Code != "paths_same" || finding.RecommendedPathID != "" {
		t.Fatalf("finding = %+v", finding)
	}
}

func TestBuildPathCompareFindingRecommendsMateriallyFasterPath(t *testing.T) {
	finding := buildPathCompareFinding([]PathCompareRoute{
		{ID: "system", Label: "系统代理", EffectivePath: "proxy:http://127.0.0.1:7890", SuccessCount: 3, SuccessRate: 1, P50TotalMS: 90},
		{ID: "direct", Label: "强制直连", EffectivePath: "direct", SuccessCount: 3, SuccessRate: 1, P50TotalMS: 150},
	})
	if finding.Code != "faster_path" || finding.RecommendedPathID != "system" {
		t.Fatalf("finding = %+v", finding)
	}
	if math.Abs(finding.ImprovementPercent-40) > 0.001 {
		t.Fatalf("improvement = %f", finding.ImprovementPercent)
	}
}

func TestBuildPathCompareFindingPrioritisesReliability(t *testing.T) {
	finding := buildPathCompareFinding([]PathCompareRoute{
		{ID: "system", Label: "系统代理", EffectivePath: "proxy:http://127.0.0.1:7890", SuccessCount: 3, SuccessRate: 1, P50TotalMS: 200},
		{ID: "direct", Label: "强制直连", EffectivePath: "direct", SuccessCount: 1, SuccessRate: 0.33, P50TotalMS: 40},
	})
	if finding.Code != "more_reliable" || finding.RecommendedPathID != "system" {
		t.Fatalf("finding = %+v", finding)
	}
}

func TestBoundedPathCompareSamples(t *testing.T) {
	if boundedPathCompareSamples(0) != 3 || boundedPathCompareSamples(4) != 4 || boundedPathCompareSamples(20) != 5 {
		t.Fatal("sample bounds are incorrect")
	}
}
