package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunSitePatrolCollectsAndOrdersTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") != "bytes=0-4095" {
			t.Errorf("Range = %q", request.Header.Get("Range"))
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	result := NewApp().RunSitePatrol(SitePatrolOptions{
		TaskID:    "patrol-test",
		ProxyMode: proxyModeDirect,
		Targets: []SiteTarget{
			{ID: "b", Name: "B", URL: server.URL + "/b"},
			{ID: "a", Name: "A", URL: server.URL + "/a"},
		},
		Concurrency: 2,
	})
	if result.Status != "completed" || result.SuccessCount != 2 || len(result.Results) != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Results[0].Target.ID != "b" || result.Results[1].Target.ID != "a" {
		t.Fatalf("result order = %+v", result.Results)
	}
	if len(result.Findings) != 1 || result.Findings[0].Code != "healthy" {
		t.Fatalf("findings = %+v", result.Findings)
	}
}

func TestAnalysePatrolFindingsDetectsCommonDNSFailure(t *testing.T) {
	results := []SiteProbeResult{
		{Result: DiagResult{FailedStage: "dns"}},
		{Result: DiagResult{FailedStage: "dns"}},
		{Result: DiagResult{Success: true}},
	}
	findings := analysePatrolFindings(results)
	if len(findings) != 1 || findings[0].Code != "dns_common" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestAnalysePatrolFindingsRequiresStrictMajority(t *testing.T) {
	results := []SiteProbeResult{
		{Result: DiagResult{FailedStage: "dns"}},
		{Result: DiagResult{FailedStage: "dns"}},
		{Result: DiagResult{Success: true}},
		{Result: DiagResult{Success: true}},
	}
	findings := analysePatrolFindings(results)
	if len(findings) != 1 || findings[0].Code != "partial_failure" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestNormalisePatrolTargetsBoundsAndDeduplicates(t *testing.T) {
	input := make([]SiteTarget, 0, maxPatrolTargets+4)
	for index := 0; index < maxPatrolTargets+3; index++ {
		input = append(input, SiteTarget{URL: "https://example.com/" + string(rune('a'+index))})
	}
	input = append(input, input[0])
	result := normalisePatrolTargets(input)
	if len(result) != maxPatrolTargets {
		t.Fatalf("len = %d, want %d", len(result), maxPatrolTargets)
	}
}

func TestRunSitePatrolCanBeCancelled(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestStarted <- struct{}{}
		<-request.Context().Done()
	}))
	defer server.Close()

	app := NewApp()
	resultChannel := make(chan SitePatrolResult, 1)
	go func() {
		resultChannel <- app.RunSitePatrol(SitePatrolOptions{
			TaskID:      "patrol-cancel-test",
			ProxyMode:   proxyModeDirect,
			Targets:     []SiteTarget{{Name: "blocked", URL: server.URL}},
			Concurrency: 1,
		})
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("patrol request did not start")
	}
	if !app.CancelTask("patrol-cancel-test") {
		t.Fatal("CancelTask returned false")
	}
	select {
	case result := <-resultChannel:
		if result.Status != "cancelled" {
			t.Fatalf("status = %q, want cancelled", result.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("patrol did not stop after cancellation")
	}
}
