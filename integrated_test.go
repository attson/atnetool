package main

import "testing"

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
