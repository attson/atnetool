package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestBuildHistoryReportBundleIncludesReadableAndMachineEvidence(t *testing.T) {
	record := diagnosisReportFixture()
	bundle, err := buildHistoryReportBundle(record)
	if err != nil {
		t.Fatalf("buildHistoryReportBundle: %v", err)
	}
	files := readReportArchive(t, bundle)
	for _, name := range []string{
		"README.md",
		"summary.json",
		"evidence/raw-result.json",
		"evidence/environment.json",
		"evidence/traceroute.json",
		"evidence/diagnosis.json",
	} {
		if _, ok := files[name]; !ok {
			t.Errorf("report archive is missing %s", name)
		}
	}
	readme := string(files["README.md"])
	for _, expected := range []string{"ATNetool 诊断报告", "example.com", "DNS 解析", "HTTP、TLS 与 Cloudflare", "Traceroute 链路证据", "代理与连接路径", "本机网络环境"} {
		if !strings.Contains(readme, expected) {
			t.Errorf("README does not contain %q", expected)
		}
	}
	var summary historyReportSummary
	if err := json.Unmarshal(files["summary.json"], &summary); err != nil {
		t.Fatalf("decode summary.json: %v", err)
	}
	if summary.Target != "https://example.com/path" || summary.Kind != "diagnosis" || summary.Metrics["total_ms"] != float64(120) {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}

func TestBuildHistoryReportBundleRemovesProxyCredentialsAndURLSecrets(t *testing.T) {
	record := diagnosisReportFixture()
	record.Result.TargetURL = "https://example.com/path?access_token=target-secret#section"
	record.Result.FinalURL = "https://example.com/final?session=final-secret"
	record.Result.ProxyURL = "http://alice:proxy-secret@127.0.0.1:7890?token=proxy-token#fragment"
	record.Environment.ProxyURL = "socks5://bob:environment-secret@127.0.0.1:1080"
	record.Environment.Hostname = "private-workstation"
	record.Environment.Interfaces = []NetworkInterfaceSnapshot{{Name: "eth0", Hardware: "00:11:22:33:44:55"}}
	bundle, err := buildHistoryReportBundle(record)
	if err != nil {
		t.Fatalf("buildHistoryReportBundle: %v", err)
	}
	contents := readReportArchive(t, bundle)
	for name, body := range contents {
		text := string(body)
		for _, secret := range []string{"target-secret", "final-secret", "proxy-secret", "proxy-token", "environment-secret", "alice", "bob", "private-workstation", "00:11:22:33:44:55"} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaked %q", name, secret)
			}
		}
	}
	raw := string(contents["evidence/raw-result.json"])
	for _, expected := range []string{"https://example.com/path", "https://example.com/final", "http://127.0.0.1:7890", "socks5://127.0.0.1:1080"} {
		if !strings.Contains(raw, expected) {
			t.Errorf("raw evidence does not retain sanitised value %q", expected)
		}
	}
}

func TestSanitiseReportValueRedactsNestedSensitiveFields(t *testing.T) {
	input := map[string]interface{}{
		"authorization": "Bearer private",
		"nested": map[string]interface{}{
			"access_token": "secret-token",
			"cookie_value": "session-cookie",
			"request_url":  "https://example.com/check?signature=secret#fragment",
		},
	}
	filtered := sanitiseReportValue("", input).(map[string]interface{})
	if filtered["authorization"] != "[REDACTED]" {
		t.Fatalf("authorization was not redacted: %#v", filtered)
	}
	nested := filtered["nested"].(map[string]interface{})
	if nested["access_token"] != "[REDACTED]" || nested["cookie_value"] != "[REDACTED]" {
		t.Fatalf("nested secrets were not redacted: %#v", nested)
	}
	if nested["request_url"] != "https://example.com/check" {
		t.Fatalf("URL was not sanitised: %#v", nested["request_url"])
	}
}

func TestHistoryReportKindEvidence(t *testing.T) {
	tests := []struct {
		kind   string
		name   string
		record HistoryRecord
	}{
		{kind: "integrated", name: "integrated", record: HistoryRecord{Integrated: &IntegratedDiagnosisResult{TargetURL: "https://example.com"}}},
		{kind: "stability", name: "stability", record: HistoryRecord{Stability: &StabilityResult{TargetURL: "https://example.com"}}},
		{kind: "site_patrol", name: "site-patrol", record: HistoryRecord{SitePatrol: &SitePatrolResult{TotalCount: 2}}},
		{kind: "cdn_optimize", name: "cf-ip-selection", record: HistoryRecord{CDNOptimize: &CDNOptimizeResult{Domain: "example.com"}}},
		{kind: "network_baseline", name: "network-baseline", record: HistoryRecord{NetworkBaseline: &NetworkBaselineResult{TargetHost: "example.com"}}},
		{kind: "path_compare", name: "path-compare", record: HistoryRecord{PathCompare: &PathCompareResult{TargetURL: "https://example.com"}}},
		{kind: "dns_diagnosis", name: "dns-diagnosis", record: HistoryRecord{DNSDiagnosis: &DNSDiagnoseResult{Domain: "example.com"}}},
		{kind: "port_test", name: "port-test", record: HistoryRecord{PortTest: &PortTestResult{Target: "example.com"}}},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			test.record.Kind = test.kind
			test.record.HistoryID = "history-" + test.kind
			test.record.CreatedAt = "2026-10-26T09:30:00+08:00"
			test.record.Result.Success = true
			name, evidence := historyReportKindEvidence(test.record)
			if name != test.name || evidence == nil {
				t.Fatalf("historyReportKindEvidence()=(%q, %#v), want %q and evidence", name, evidence, test.name)
			}
			bundle, err := buildHistoryReportBundle(test.record)
			if err != nil {
				t.Fatalf("buildHistoryReportBundle: %v", err)
			}
			files := readReportArchive(t, bundle)
			if _, ok := files["evidence/"+test.name+".json"]; !ok {
				t.Errorf("report archive is missing evidence/%s.json", test.name)
			}
		})
	}
}

func TestBuildHistoryReportBundleRejectsIncompleteRecords(t *testing.T) {
	tests := []HistoryRecord{
		{},
		{Kind: "unknown", Result: DiagResult{TargetURL: "https://example.com"}},
		{Kind: "diagnosis"},
		{Kind: "integrated", Result: DiagResult{TargetURL: "https://example.com"}},
	}
	for _, record := range tests {
		if _, err := buildHistoryReportBundle(record); err == nil {
			t.Errorf("buildHistoryReportBundle(%#v) succeeded, want error", record)
		}
	}
}

func TestHistoryReportFilenameIsPortable(t *testing.T) {
	record := diagnosisReportFixture()
	record.Result.TargetURL = "https://example.com/a:b?token=secret"
	name := historyReportFilename(record)
	if !strings.HasPrefix(name, "ATNetool-example.com-") || !strings.HasSuffix(name, ".zip") {
		t.Fatalf("unexpected report filename %q", name)
	}
	if strings.ContainsAny(name, `<>:"/\\|?*`) {
		t.Fatalf("report filename contains a reserved character: %q", name)
	}
}

func diagnosisReportFixture() HistoryRecord {
	return HistoryRecord{
		SchemaVersion: historySchemaVersion,
		Kind:          "diagnosis",
		HistoryID:     "history-report-test",
		CreatedAt:     "2026-10-26T09:30:00+08:00",
		Result: DiagResult{
			Success:        true,
			Message:        "网络连接正常",
			TargetURL:      "https://example.com/path",
			DNSLookupMS:    12,
			TCPConnectMS:   20,
			TLSHandshakeMS: 25,
			TTFBMS:         60,
			TotalMS:        120,
			StatusCode:     200,
			RemoteIP:       "203.0.113.10",
			CheckedAt:      "2026-10-26T09:30:00+08:00",
			ProxyMode:      "system",
			ProxyStatus:    "direct",
		},
		Traceroute: &TraceResult{
			Success:     true,
			TargetHost:  "example.com",
			Reached:     true,
			DurationMS:  45,
			GeoResolved: 1,
			Hops: []TraceHop{
				{Hop: 1, Addresses: []string{"203.0.113.1"}, Reached: true},
			},
		},
		Environment: NetworkEnvironment{OS: "linux", Arch: "amd64", ProxyMode: "system"},
	}
}

func readReportArchive(t *testing.T, bundle []byte) map[string][]byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatalf("open report archive: %v", err)
	}
	files := make(map[string][]byte, len(archive.File))
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s: %v", file.Name, err)
		}
		contents, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read %s: %v", file.Name, err)
		}
		files[file.Name] = contents
	}
	return files
}
