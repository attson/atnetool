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
	if len(remediations) < 2 || remediations[0].Priority != 1 || remediations[0].Module != "dns" {
		t.Fatalf("remediations = %+v", remediations)
	}
}

func TestNormaliseIntegratedStatus(t *testing.T) {
	for input, want := range map[string]string{"good": "healthy", "warning": "warning", "failed": "critical", "unknown": "neutral"} {
		if got := normaliseIntegratedStatus(input); got != want {
			t.Fatalf("normaliseIntegratedStatus(%q) = %q, want %q", input, got, want)
		}
	}
}
