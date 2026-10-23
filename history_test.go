package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestHistoryStoreSaveDeduplicatesAndPersists(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "diagnosis",
		HistoryID: "test-record",
		Result: DiagResult{
			TargetURL: "https://example.com",
			CheckedAt: "2026-10-21T12:00:00Z",
			TotalMS:   120,
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	record.Result.TotalMS = 90
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Result.TotalMS != 90 {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsStabilityResult(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "stability",
		HistoryID: "stability-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		Result: DiagResult{
			TargetURL: "https://example.com",
			CheckedAt: "2026-10-21T12:00:00Z",
		},
		Stability: &StabilityResult{P95MS: 80, SampleCount: 10, SuccessCount: 10},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "stability" || records[0].Stability.P95MS != 80 {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsSitePatrolResult(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "site_patrol",
		HistoryID: "patrol-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		Result: DiagResult{
			CheckedAt: "2026-10-21T12:00:00Z",
		},
		SitePatrol: &SitePatrolResult{TotalCount: 6, SuccessCount: 5, AverageMS: 120},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "site_patrol" || records[0].SitePatrol.SuccessCount != 5 {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsCDNOptimizeResult(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "cdn_optimize",
		HistoryID: "cdn-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		Result:    DiagResult{CheckedAt: "2026-10-21T12:00:00Z"},
		CDNOptimize: &CDNOptimizeResult{
			Domain:         "example.com",
			CandidateCount: 3,
			VerifiedCount:  2,
			BestIP:         "203.0.113.10",
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "cdn_optimize" || records[0].CDNOptimize.BestIP != "203.0.113.10" {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsNetworkBaselineResult(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "network_baseline",
		HistoryID: "baseline-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		Result:    DiagResult{CheckedAt: "2026-10-21T12:00:00Z"},
		NetworkBaseline: &NetworkBaselineResult{
			TargetHost:    "example.com",
			Status:        "completed",
			DNSComparison: DNSComparison{SuccessfulResolvers: 4, MatchingResolvers: 3},
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "network_baseline" || records[0].NetworkBaseline.DNSComparison.SuccessfulResolvers != 4 {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsPathCompareResult(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "path_compare",
		HistoryID: "path-compare-1",
		CreatedAt: "2026-10-21T10:00:00+08:00",
		PathCompare: &PathCompareResult{
			Status:    "completed",
			TargetURL: "https://example.com/",
			Finding: PathCompareFinding{
				Code:              "faster_path",
				RecommendedPathID: "direct",
			},
			Paths: []PathCompareRoute{{ID: "direct", SuccessCount: 3, P50TotalMS: 80}},
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "path_compare" || records[0].PathCompare == nil {
		t.Fatalf("records = %+v", records)
	}
	if records[0].PathCompare.Finding.RecommendedPathID != "direct" {
		t.Fatalf("path compare = %+v", records[0].PathCompare)
	}
}

func TestHistoryStorePersistsDNSDiagnosis(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "dns_diagnosis",
		HistoryID: "dns-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		DNSDiagnosis: &DNSDiagnoseResult{
			Status: "completed",
			Domain: "example.com",
			Comparison: DNSDeepComparison{
				ReachableResolvers:  5,
				SystemMatchesPublic: true,
			},
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "dns_diagnosis" || records[0].DNSDiagnosis == nil {
		t.Fatalf("records = %+v", records)
	}
	if records[0].DNSDiagnosis.Comparison.ReachableResolvers != 5 {
		t.Fatalf("DNS diagnosis = %+v", records[0].DNSDiagnosis)
	}
}

func TestHistoryStorePersistsPortTest(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "port_test",
		HistoryID: "ports-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		PortTest: &PortTestResult{
			Status:     "completed",
			Target:     "example.com",
			TotalCount: 3,
			OpenCount:  2,
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "port_test" || records[0].PortTest == nil || records[0].PortTest.OpenCount != 2 {
		t.Fatalf("records = %+v", records)
	}
}

func TestHistoryStorePersistsIntegratedDiagnosis(t *testing.T) {
	store := &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	record := HistoryRecord{
		Kind:      "integrated",
		HistoryID: "integrated-1",
		CreatedAt: "2026-10-21T12:00:00Z",
		Integrated: &IntegratedDiagnosisResult{
			Status:        "completed",
			OverallStatus: "warning",
			TargetURL:     "https://example.com/",
			Completed:     5,
			Total:         5,
		},
	}
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "integrated" || records[0].Integrated == nil || records[0].Integrated.OverallStatus != "warning" {
		t.Fatalf("records = %+v", records)
	}
}

func TestMigrateLegacyHistory(t *testing.T) {
	app := NewApp()
	app.historyStore = &historyStore{path: filepath.Join(t.TempDir(), "history.json")}
	payload, err := json.Marshal([]map[string]any{{
		"history_id":   "legacy-1",
		"target_url":   "https://example.com",
		"checked_at":   "2026-10-21T12:00:00Z",
		"success":      true,
		"total_ms":     42,
		"proxy_mode":   "direct",
		"proxy_status": "direct",
	}})
	if err != nil {
		t.Fatal(err)
	}
	added, err := app.MigrateLegacyHistory(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	records, err := app.GetHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Result.TotalMS != 42 || records[0].Environment.ProxyMode != "direct" {
		t.Fatalf("records = %+v", records)
	}
}
