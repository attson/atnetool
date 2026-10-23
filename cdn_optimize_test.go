package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNormaliseCDNTarget(t *testing.T) {
	domain, path, targetURL, err := normaliseCDNTarget("https://Example.COM/check?mode=small", "")
	if err != nil {
		t.Fatal(err)
	}
	if domain != "example.com" || path != "/check?mode=small" || targetURL != "https://example.com/check?mode=small" {
		t.Fatalf("target = %q %q %q", domain, path, targetURL)
	}
	if _, _, _, err := normaliseCDNTarget("127.0.0.1", "/"); err == nil {
		t.Fatal("IP target should be rejected")
	}
	if _, _, _, err := normaliseCDNTarget("example.com", "/check#fragment"); err == nil {
		t.Fatal("fragment should be rejected")
	}
}

func TestVerifyCDNTargetAcceptsOfficialEdgeIP(t *testing.T) {
	verification := verifyCDNTarget(context.Background(), "http://127.0.0.1:1/", []string{"104.16.1.10"})
	if !verification.Verified || !verification.DNSMatched || verification.Status != "verified" {
		t.Fatalf("verification=%+v", verification)
	}
}

func TestVerifyCDNTargetAcceptsCloudflareResponseHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("CF-Ray", "test-HKG")
		writer.Header().Set("Server", "cloudflare")
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	verification := verifyCDNTarget(context.Background(), server.URL, []string{"203.0.113.10"})
	if !verification.Verified || !verification.HeaderMatched || verification.CFRay != "test-HKG" {
		t.Fatalf("verification=%+v", verification)
	}
}

func TestVerifyCDNTargetRejectsDomainWithoutCloudflareEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Server", "example-origin")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	verification := verifyCDNTarget(context.Background(), server.URL, []string{"203.0.113.10"})
	if verification.Verified || verification.Status != "not_cloudflare" {
		t.Fatalf("verification=%+v", verification)
	}
}

func TestNormaliseCDNCandidatesFiltersAndDeduplicates(t *testing.T) {
	result := normaliseCDNCandidates(
		[]string{"1.1.1.1", "2606:4700:4700::1111"},
		[]string{"1.1.1.1, 8.8.8.8", "127.0.0.1", "192.168.1.1", "invalid"},
	)
	want := []CDNCandidate{
		{IP: "1.1.1.1", Source: "dns"},
		{IP: "2606:4700:4700::1111", Source: "dns"},
		{IP: "8.8.8.8", Source: "manual"},
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
}

func TestFinaliseCDNProbeCalculatesMediansAndJitter(t *testing.T) {
	result := CDNProbeResult{
		SampleCount: 3,
		Samples: []CDNProbeSample{
			{Success: true, TCPConnectMS: 30, TLSHandshakeMS: 50, TTFBMS: 90, TotalMS: 200, StatusCode: 200, CFRay: "one-SIN", CFColo: "SIN"},
			{Success: true, TCPConnectMS: 10, TLSHandshakeMS: 30, TTFBMS: 70, TotalMS: 100, StatusCode: 200, CFRay: "two-SIN", CFColo: "SIN"},
			{Success: false, TotalMS: 800},
		},
	}
	finaliseCDNProbe(&result)
	if result.SuccessCount != 2 || result.P50TCPMS != 10 || result.P50TotalMS != 100 || result.P95TotalMS != 200 || !result.VerifiedCloudflare {
		t.Fatalf("result = %+v", result)
	}
	if math.Abs(result.JitterMS-100) > 0.001 {
		t.Fatalf("jitter = %f, want 100", result.JitterMS)
	}
}

func TestFinaliseCDNRankingPrefersVerifiedFasterCandidate(t *testing.T) {
	result := CDNOptimizeResult{Domain: "example.com", Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "1.1.1.1", Source: "dns"}, SampleCount: 3, SuccessCount: 3, SuccessRate: 1, P50TotalMS: 200, P95TotalMS: 240, VerifiedCloudflare: true},
		{Candidate: CDNCandidate{IP: "203.0.113.10", Source: "manual"}, SampleCount: 3, SuccessCount: 3, SuccessRate: 1, P50TotalMS: 100, P95TotalMS: 130, VerifiedCloudflare: true},
		{Candidate: CDNCandidate{IP: "8.8.8.8", Source: "manual"}, SampleCount: 3, SuccessCount: 3, SuccessRate: 1, P50TotalMS: 50, P95TotalMS: 60},
	}}
	finaliseCDNRanking(&result)
	if result.BestIP != "203.0.113.10" || result.BaselineIP != "1.1.1.1" || result.Finding.Code != "better_candidate" {
		t.Fatalf("result = %+v", result)
	}
	if math.Abs(result.ImprovementPercent-50) > 0.001 || !result.Results[1].Recommended || result.Results[1].Rank != 1 {
		t.Fatalf("ranking = %+v", result.Results)
	}
	if result.Confidence != "medium" || len(result.Governance) != 3 {
		t.Fatalf("governance = confidence %q, advice %+v", result.Confidence, result.Governance)
	}
}

func TestCDNProbeRankingUsesTailLatencyBeforeMedian(t *testing.T) {
	left := CDNProbeResult{Candidate: CDNCandidate{IP: "1.1.1.1"}, SuccessRate: 1, P50TotalMS: 80, P95TotalMS: 400}
	right := CDNProbeResult{Candidate: CDNCandidate{IP: "1.0.0.1"}, SuccessRate: 1, P50TotalMS: 100, P95TotalMS: 150}
	if cdnProbeLess(left, right) {
		t.Fatal("candidate with much worse P95 should not outrank a stable candidate")
	}
}

func TestCDNRecommendationConfidenceRequiresFiveStableSamples(t *testing.T) {
	baseline := CDNProbeResult{SampleCount: 5, SuccessRate: 1}
	best := CDNProbeResult{SampleCount: 5, SuccessRate: 1}
	if got := cdnRecommendationConfidence(best, &baseline); got != "high" {
		t.Fatalf("confidence = %q, want high", got)
	}
	best.SampleCount = 3
	if got := cdnRecommendationConfidence(best, &baseline); got != "medium" {
		t.Fatalf("confidence = %q, want medium", got)
	}
}

func TestFinaliseCDNRankingFlagsHighTailLatency(t *testing.T) {
	result := CDNOptimizeResult{Domain: "example.com", Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "1.1.1.1", Source: "dns"}, SampleCount: 3, SuccessCount: 3, SuccessRate: 1, P50TotalMS: 400, P95TotalMS: 1400, JitterMS: 500, VerifiedCloudflare: true},
	}}
	finaliseCDNRanking(&result)
	if result.Finding.Code != "current_route_unstable" || len(result.Governance) != 3 {
		t.Fatalf("result = %+v", result)
	}
}

func TestBuildCDNCandidatesExpandsCustomIPv4CIDR(t *testing.T) {
	settings, err := normaliseCDNOptimizeOptions(CDNOptimizeOptions{MaxLossRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	candidates, ranges, warnings, truncated := buildCDNCandidates(nil, CDNOptimizeOptions{
		IPRanges:    []string{"8.8.8.0/30"},
		TestAllIPv4: true,
	}, settings)
	if truncated || len(warnings) != 0 {
		t.Fatalf("truncated=%v warnings=%v", truncated, warnings)
	}
	if len(candidates) != 4 || len(ranges) != 1 {
		t.Fatalf("candidates=%v ranges=%v", candidates, ranges)
	}
	if candidates[0].IP != "8.8.8.0" || candidates[3].IP != "8.8.8.3" || candidates[0].Source != "custom" {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestPresetCandidateDiscoveryStaysInSampledScale(t *testing.T) {
	settings, err := normaliseCDNOptimizeOptions(CDNOptimizeOptions{MaxLossRate: 1, IPv6Samples: 4})
	if err != nil {
		t.Fatal(err)
	}
	candidates, ranges, warnings, truncated := buildCDNCandidates(nil, CDNOptimizeOptions{
		UsePresetIPv4: true,
		UsePresetIPv6: true,
	}, settings)
	if truncated || len(warnings) != 0 {
		t.Fatalf("truncated=%v warnings=%v", truncated, warnings)
	}
	if len(ranges) != len(cloudflareIPv4Ranges)+len(cloudflareIPv6Ranges) {
		t.Fatalf("ranges=%d", len(ranges))
	}
	if len(candidates) < 5000 || len(candidates) > 10000 {
		t.Fatalf("unexpected sampled candidate count: %d", len(candidates))
	}
}

func TestFilterCDNLatencyResultsAppliesDelayLossAndColo(t *testing.T) {
	result := CDNOptimizeResult{Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "1.1.1.1"}, LossRate: 0, AverageLatencyMS: 80, CFColo: "HKG"},
		{Candidate: CDNCandidate{IP: "1.0.0.1"}, LossRate: 0.25, AverageLatencyMS: 70, CFColo: "HKG"},
		{Candidate: CDNCandidate{IP: "8.8.8.8"}, LossRate: 0, AverageLatencyMS: 250, CFColo: "LAX"},
	}}
	filterCDNLatencyResults(&result, cdnOptimizeSettings{
		MinDelayMS: 20, MaxDelayMS: 200, MaxLossRate: 0.2, Colos: map[string]struct{}{"HKG": {}},
	})
	if result.EligibleCount != 1 || result.FilteredCount != 2 || !result.Results[0].Eligible {
		t.Fatalf("result=%+v", result)
	}
}

func TestModernCDNRankingPrefersDownloadSpeed(t *testing.T) {
	result := CDNOptimizeResult{Mode: "tcp", DownloadEnabled: true, Domain: "example.com", Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "1.1.1.1", Source: "dns"}, Eligible: true, SampleCount: 4, SuccessCount: 4, SuccessRate: 1, Sent: 4, Received: 4, AverageLatencyMS: 30, DownloadSpeedMBps: 8, DownloadTested: true},
		{Candidate: CDNCandidate{IP: "1.0.0.1", Source: "cloudflare_ipv4"}, Eligible: true, SampleCount: 4, SuccessCount: 4, SuccessRate: 1, Sent: 4, Received: 4, AverageLatencyMS: 45, DownloadSpeedMBps: 20, DownloadTested: true},
	}}
	finaliseCDNRanking(&result)
	if result.BestIP != "1.0.0.1" || result.Results[1].Rank != 1 || !result.Results[1].Recommended {
		t.Fatalf("result=%+v", result)
	}
	if !result.Comparison.Comparable || !result.Comparison.SpeedComparable || math.Abs(result.Comparison.SpeedImprovementPercent-150) > 0.001 {
		t.Fatalf("comparison=%+v", result.Comparison)
	}
	if result.Comparison.LatencyImprovementPercent >= 0 {
		t.Fatalf("latency comparison should expose the speed/latency tradeoff: %+v", result.Comparison)
	}
}

func TestModernCDNRankingKeepsFilteredDNSAsComparisonBaseline(t *testing.T) {
	result := CDNOptimizeResult{Mode: "tcp", Domain: "example.com", Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "1.1.1.1", Source: "dns"}, Eligible: false, SampleCount: 4, SuccessCount: 4, SuccessRate: 1, Sent: 4, Received: 4, LossRate: 0.25, AverageLatencyMS: 200},
		{Candidate: CDNCandidate{IP: "1.0.0.1", Source: "cloudflare_ipv4"}, Eligible: true, SampleCount: 4, SuccessCount: 4, SuccessRate: 1, Sent: 4, Received: 4, AverageLatencyMS: 80},
	}}
	finaliseCDNRanking(&result)
	if result.BaselineIP != "1.1.1.1" || !result.Comparison.Comparable {
		t.Fatalf("comparison baseline=%+v", result)
	}
	if math.Abs(result.Comparison.LatencyImprovementPercent-60) > 0.001 || math.Abs(result.Comparison.LossReductionPoints-25) > 0.001 {
		t.Fatalf("comparison=%+v", result.Comparison)
	}
}
