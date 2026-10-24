package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBuildCFGovernanceBundleGeneratesReadyToUseConfigs(t *testing.T) {
	result := CDNOptimizeResult{
		Domain:             "example.com",
		BestIP:             "104.16.1.20",
		TargetValidated:    true,
		TargetVerification: CDNTargetVerification{Verified: true},
		Results: []CDNProbeResult{
			{Candidate: CDNCandidate{IP: "104.16.1.30"}, Eligible: true, VerifiedCloudflare: true, Rank: 2, AverageLatencyMS: 40},
			{Candidate: CDNCandidate{IP: "2606:4700::20"}, Eligible: true, VerifiedCloudflare: true, Rank: 3, AverageLatencyMS: 45},
			{Candidate: CDNCandidate{IP: "104.16.1.20"}, Eligible: true, VerifiedCloudflare: true, Recommended: true, Rank: 1, AverageLatencyMS: 30},
		},
	}

	bundle, err := buildCFGovernanceBundle(result)
	if err != nil {
		t.Fatal(err)
	}
	files := readGovernanceZIP(t, bundle)
	for _, name := range []string{"README.txt", "mosdns/config.yaml", "3proxy/3proxy.cfg", "hosts/current-domain", "results/selection.json"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("missing %s in bundle", name)
		}
	}

	mosdns := files["mosdns/config.yaml"]
	for _, value := range []string{"104.16.0.0/13", "2606:4700::/32", "black_hole 104.16.1.20 2606:4700::20"} {
		if !strings.Contains(mosdns, value) {
			t.Fatalf("mosdns config does not contain %q:\n%s", value, mosdns)
		}
	}
	proxy := files["3proxy/3proxy.cfg"]
	for _, value := range []string{"104.16.1.20", "2606:4700::/32", "-i127.0.0.1"} {
		if !strings.Contains(proxy, value) {
			t.Fatalf("3proxy config does not contain %q:\n%s", value, proxy)
		}
	}
	hosts := files["hosts/current-domain"]
	if !strings.Contains(hosts, "104.16.1.20 example.com") || !strings.Contains(hosts, "# Alternative: 2606:4700::20 example.com") {
		t.Fatalf("unexpected hosts config:\n%s", hosts)
	}
	var decoded CDNOptimizeResult
	if err := json.Unmarshal([]byte(files["results/selection.json"]), &decoded); err != nil || decoded.BestIP != result.BestIP {
		t.Fatalf("invalid selection json: err=%v result=%+v", err, decoded)
	}
}

func TestBuildCFGovernanceBundleRequiresVerifiedTargetAndRecommendation(t *testing.T) {
	base := CDNOptimizeResult{
		Domain:             "example.com",
		BestIP:             "104.16.1.20",
		TargetVerification: CDNTargetVerification{Verified: true},
		Results: []CDNProbeResult{{
			Candidate: CDNCandidate{IP: "104.16.1.20"}, Eligible: true, VerifiedCloudflare: true, Recommended: true,
		}},
	}
	if _, err := buildCFGovernanceBundle(base); err == nil || !strings.Contains(err.Error(), "推荐 IP") {
		t.Fatalf("expected recommendation validation error, got %v", err)
	}
	base.TargetValidated = true
	base.TargetVerification.Verified = false
	if _, err := buildCFGovernanceBundle(base); err == nil || !strings.Contains(err.Error(), "Cloudflare") {
		t.Fatalf("expected Cloudflare verification error, got %v", err)
	}
}

func TestBuildCFGovernanceBundleRejectsProxyRouteResult(t *testing.T) {
	result := CDNOptimizeResult{
		Domain:             "example.com",
		BestIP:             "104.16.1.20",
		EffectiveRoute:     cdnRouteProxy,
		TargetValidated:    true,
		TargetVerification: CDNTargetVerification{Verified: true},
		Results: []CDNProbeResult{{
			Candidate: CDNCandidate{IP: "104.16.1.20"}, Eligible: true, VerifiedCloudflare: true, Recommended: true,
		}},
	}
	if _, err := buildCFGovernanceBundle(result); err == nil || !strings.Contains(err.Error(), "代理出口") {
		t.Fatalf("expected proxy-route error, got %v", err)
	}
}

func TestBestGovernanceIPsUsesRankedVerifiedNodes(t *testing.T) {
	result := CDNOptimizeResult{Results: []CDNProbeResult{
		{Candidate: CDNCandidate{IP: "104.16.1.30"}, Eligible: true, VerifiedCloudflare: true, Rank: 3},
		{Candidate: CDNCandidate{IP: "104.16.1.20"}, Eligible: true, VerifiedCloudflare: true, Rank: 1},
		{Candidate: CDNCandidate{IP: "104.16.1.10"}, Eligible: true, Rank: 0},
		{Candidate: CDNCandidate{IP: "2606:4700::30"}, Eligible: true, VerifiedCloudflare: true, Rank: 2},
	}}
	v4, v6 := bestGovernanceIPs(result)
	if v4 != "104.16.1.20" || v6 != "2606:4700::30" {
		t.Fatalf("v4=%s v6=%s", v4, v6)
	}
}

func readGovernanceZIP(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		files[file.Name] = string(content)
	}
	return files
}
