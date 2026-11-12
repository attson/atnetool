package main

import (
	"math"
	"testing"
)

func TestBuildIntegratedDomainComparisonReportsImprovement(t *testing.T) {
	primary := DiagResult{
		Success: true, StatusCode: 200,
		DNSLookupMS: 40, TCPConnectMS: 80, TLSHandshakeMS: 60, TTFBMS: 200, TotalMS: 500,
	}
	compare := DiagResult{
		Success: true, StatusCode: 204,
		DNSLookupMS: 10, TCPConnectMS: 50, TLSHandshakeMS: 40, TTFBMS: 100, TotalMS: 300,
	}

	result := buildIntegratedDomainComparison("https://a.example/", primary, "https://b.example/", compare)

	if !result.Enabled || !result.Comparable {
		t.Fatalf("comparison flags = enabled %v, comparable %v", result.Enabled, result.Comparable)
	}
	if result.DNSDeltaMS != 30 || result.TCPDeltaMS != 30 || result.TLSDeltaMS != 20 || result.TTFBDeltaMS != 100 || result.TotalDeltaMS != 200 {
		t.Fatalf("comparison deltas = %+v", result)
	}
	if math.Abs(result.TTFBImprovementPercent-50) > 0.001 || math.Abs(result.TotalImprovementPercent-40) > 0.001 {
		t.Fatalf("comparison improvements = TTFB %.3f%%, total %.3f%%", result.TTFBImprovementPercent, result.TotalImprovementPercent)
	}
	if result.Primary.StatusCode != 200 || result.Compare.StatusCode != 204 {
		t.Fatalf("comparison evidence was not retained: %+v", result)
	}
}

func TestBuildIntegratedDomainComparisonDoesNotClaimImprovementAfterFailure(t *testing.T) {
	primary := DiagResult{Success: true, StatusCode: 200, TTFBMS: 200, TotalMS: 500}
	compare := DiagResult{Success: false, FailedStage: "tls", TLSHandshakeMS: 35, TotalMS: 120, Message: "TLS 握手失败"}

	result := buildIntegratedDomainComparison("https://a.example/", primary, "https://b.example/", compare)

	if result.Comparable {
		t.Fatal("a failed request must not be comparable")
	}
	if result.TTFBDeltaMS != 0 || result.TotalDeltaMS != 0 || result.TTFBImprovementPercent != 0 || result.TotalImprovementPercent != 0 {
		t.Fatalf("failed comparison claimed an improvement: %+v", result)
	}
	if result.Primary.StatusCode != 200 || result.Compare.FailedStage != "tls" || result.Compare.TLSHandshakeMS != 35 {
		t.Fatalf("failed comparison evidence was not retained: %+v", result)
	}
}

func TestIntegratedDiagnosisResultWithoutComparisonKeepsComparisonDisabled(t *testing.T) {
	result := IntegratedDiagnosisResult{}
	if result.Comparison.Enabled || result.Comparison.Comparable {
		t.Fatalf("comparison should be opt-in: %+v", result.Comparison)
	}
}

func TestRunIntegratedDiagnosisRejectsInvalidComparisonTarget(t *testing.T) {
	tests := []struct {
		name    string
		compare string
	}{
		{name: "invalid URL", compare: "://bad"},
		{name: "same hostname", compare: "http://EXAMPLE.com/other"},
		{name: "same fully qualified hostname", compare: "https://example.com./other"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := NewApp().RunIntegratedDiagnosis(IntegratedDiagnosisOptions{
				TargetURL:        "https://example.com/path",
				CompareTargetURL: test.compare,
			})
			if result.Status != "failed" || result.Completed != 0 || result.Message != "对比域名配置有误或与主域名相同" {
				t.Fatalf("invalid comparison result = %+v", result)
			}
		})
	}
}

func TestBuildIntegratedAssessmentHealthy(t *testing.T) {
	result := IntegratedDiagnosisResult{
		Baseline: NetworkBaselineResult{Findings: []BaselineFinding{{Layer: "local", Status: "healthy", Title: "本地网络正常"}}},
		DNS:      DNSDiagnoseResult{Findings: []DNSFinding{{Layer: "dns", Status: "healthy", Title: "DNS 正常"}}},
		Ports:    PortTestResult{Findings: []PortTestFinding{{Status: "healthy", Title: "端口可达"}}},
		HTTP: DiagnosisTaskResult{
			Diagnosis:  DiagResult{Success: true, StatusCode: 200, TTFBMS: 80, TotalMS: 120},
			Traceroute: TraceResult{Success: true, Reached: true, Hops: []TraceHop{{Hop: 1}}},
		},
		Paths: PathCompareResult{Finding: PathCompareFinding{Level: "good", Title: "当前路径正常"}},
	}
	findings, remediations, overall := buildIntegratedAssessment(result)
	if overall != "healthy" || len(findings) < 5 || len(remediations) != 0 {
		t.Fatalf("assessment = %q, findings %+v, remediations %+v", overall, findings, remediations)
	}
}

func TestBuildIntegratedAssessmentPrioritisesFailureLayer(t *testing.T) {
	result := IntegratedDiagnosisResult{
		DNS: DNSDiagnoseResult{
			Findings:     []DNSFinding{{Layer: "dns", Status: "critical", Title: "DNS 不可用", Detail: "所有解析器失败"}},
			Remediations: []DNSRemediation{{Action: "dns", Title: "检查 DNS", Detail: "更换解析器后复测"}},
		},
		HTTP: DiagnosisTaskResult{Diagnosis: DiagResult{FailedStage: "dns", Message: "域名解析失败"}},
	}
	findings, remediations, overall := buildIntegratedAssessment(result)
	if overall != "critical" || findings[0].Status != "critical" {
		t.Fatalf("assessment = %q, findings %+v", overall, findings)
	}
	if len(remediations) != 1 || remediations[0].Priority != 1 || remediations[0].Module != "dns" || remediations[0].Title != "恢复目标域名解析" {
		t.Fatalf("remediations = %+v", remediations)
	}
}

func TestDedupeIntegratedRemediations(t *testing.T) {
	items := []IntegratedRemediation{
		{Priority: 1, Action: "isolate", Module: "tls", Title: "处理 TLS 握手失败"},
		{Priority: 2, Action: "compare_proxy", Module: "baseline", Title: "分别验证代理与直连"},
		{Priority: 3, Action: "inspect_tls", Module: "ports", Title: "检查 TLS 配置"},
		{Priority: 3, Action: "route", Module: "paths", Title: "采用路径对照结论验证"},
	}

	got := dedupeIntegratedRemediations(items)
	if len(got) != 2 || got[0].Title != "处理 TLS 握手失败" || got[1].Title != "分别验证代理与直连" {
		t.Fatalf("deduped remediations = %+v", got)
	}
}

func TestBuildIntegratedAssessmentDoesNotRecommendCompletedIntegratedModules(t *testing.T) {
	result := IntegratedDiagnosisResult{
		Baseline: NetworkBaselineResult{Remediations: []BaselineRemediation{
			{Action: "diagnose_dns", Title: "运行 DNS 深度诊断"},
			{Action: "compare_proxy", Title: "分别验证系统代理与强制直连"},
		}},
		HTTP: DiagnosisTaskResult{Diagnosis: DiagResult{Success: true, StatusCode: 200}},
		Paths: PathCompareResult{Finding: PathCompareFinding{
			Level: "warning", Title: "强制直连响应更快", Detail: "直连快约 30%",
		}},
	}

	_, remediations, _ := buildIntegratedAssessment(result)
	if len(remediations) != 0 {
		t.Fatalf("completed modules should not be recommended again: %+v", remediations)
	}
}

func TestBuildIntegratedAssessmentExcludesGenericEgressProbe(t *testing.T) {
	result := IntegratedDiagnosisResult{
		Baseline: NetworkBaselineResult{Findings: []BaselineFinding{
			{Layer: "local", Status: "healthy", Title: "本地网络正常"},
			{Layer: "egress", Status: "warning", Title: "公网访问依赖代理"},
		}},
		HTTP: DiagnosisTaskResult{Diagnosis: DiagResult{Success: true, StatusCode: 200}},
	}

	findings, _, _ := buildIntegratedAssessment(result)
	for _, finding := range findings {
		if finding.Layer == "egress" {
			t.Fatalf("generic egress probe leaked into site findings: %+v", findings)
		}
	}
}

func TestBuildIntegratedAssessmentMergesDNSWarnings(t *testing.T) {
	result := IntegratedDiagnosisResult{
		Baseline: NetworkBaselineResult{Findings: []BaselineFinding{
			{Layer: "dns", Status: "warning", Title: "DNS 结果存在差异"},
		}},
		DNS: DNSDiagnoseResult{
			Total: 5,
			Resolvers: []DNSResolverResult{
				{Name: "系统 DNS", Transport: "system", Reachable: true},
				{Name: "Cloudflare DNS", Transport: "udp", Reachable: false},
				{Name: "AliDNS", Transport: "udp", Reachable: true},
				{Name: "Cloudflare DoH", Transport: "doh", Reachable: true},
				{Name: "Google DoH", Transport: "doh", Reachable: true},
			},
			Comparison: DNSDeepComparison{ReachableResolvers: 4, AddressDifference: true},
			Findings: []DNSFinding{
				{Status: "warning", Title: "部分公共解析器不可达"},
				{Status: "warning", Title: "系统与公共解析结果不同"},
			},
		},
		HTTP: DiagnosisTaskResult{Diagnosis: DiagResult{Success: true, StatusCode: 200}},
	}

	findings, _, _ := buildIntegratedAssessment(result)
	dnsWarnings := make([]IntegratedFinding, 0)
	for _, finding := range findings {
		if finding.Layer == "dns" && finding.Status == "warning" {
			dnsWarnings = append(dnsWarnings, finding)
		}
	}
	if len(dnsWarnings) != 1 || dnsWarnings[0].Title != "DNS 解析存在差异，1 个公共解析器未响应" {
		t.Fatalf("DNS warnings were not merged: %+v", dnsWarnings)
	}
}

func TestNormaliseIntegratedStatus(t *testing.T) {
	for input, want := range map[string]string{"good": "healthy", "warning": "warning", "failed": "critical", "unknown": "neutral"} {
		if got := normaliseIntegratedStatus(input); got != want {
			t.Fatalf("normaliseIntegratedStatus(%q) = %q, want %q", input, got, want)
		}
	}
}
