package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunStabilityProbeMeasuresResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Range") != "bytes=0-1023" {
			t.Errorf("Range = %q", request.Header.Get("Range"))
		}
		time.Sleep(5 * time.Millisecond)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sample := runStabilityProbe(context.Background(), server.Client(), server.URL, 3)
	if !sample.Success || sample.StatusCode != http.StatusNoContent || sample.Sequence != 3 {
		t.Fatalf("sample = %+v", sample)
	}
	if sample.LatencyMS < 5 {
		t.Fatalf("latency = %d, want at least 5 ms", sample.LatencyMS)
	}
}

func TestFinaliseStabilityResult(t *testing.T) {
	result := StabilityResult{LatencyThresholdMS: 25, FailureThresholdPercent: 30, Samples: []StabilitySample{
		{Sequence: 1, Success: true, LatencyMS: 10, RemoteIP: "1.1.1.1"},
		{Sequence: 2, Success: true, LatencyMS: 20, RemoteIP: "1.1.1.1"},
		{Sequence: 3, Success: false},
		{Sequence: 4, Success: true, LatencyMS: 40, RemoteIP: "1.0.0.1"},
	}}
	finaliseStabilityResult(&result, time.Now())
	if result.SuccessCount != 3 || result.SampleCount != 4 || result.FailureRate != 0.25 {
		t.Fatalf("counts = %+v", result)
	}
	if result.MinimumMS != 10 || result.MaximumMS != 40 || result.P50MS != 20 || result.P95MS != 40 {
		t.Fatalf("latency summary = %+v", result)
	}
	if result.JitterMS != 15 {
		t.Fatalf("jitter = %f, want 15", result.JitterMS)
	}
	if result.AvailabilityPercent != 75 || result.SlowCount != 1 || result.LongestFailureStreak != 1 {
		t.Fatalf("monitor summary = %+v", result)
	}
	if len(result.Incidents) != 2 || !result.Incidents[0].Resolved || result.Incidents[1].Resolved {
		t.Fatalf("incidents = %+v", result.Incidents)
	}
	if len(result.RouteChanges) != 1 || result.Finding.Code != "monitor_latency" {
		t.Fatalf("route/finding = %+v / %+v", result.RouteChanges, result.Finding)
	}
}

func TestStabilityBounds(t *testing.T) {
	if got := boundedStabilityDuration(1); got != minStabilityDuration {
		t.Fatalf("duration = %s", got)
	}
	if got := boundedProbeInterval(20); got != minProbeInterval {
		t.Fatalf("interval = %s", got)
	}
	if got := boundedLatencyThreshold(20); got != 50 {
		t.Fatalf("latency threshold = %d", got)
	}
	if got := boundedFailureThreshold(120); got != 100 {
		t.Fatalf("failure threshold = %f", got)
	}
}

func TestRunStabilityTestCanBeCancelled(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		select {
		case requestSeen <- struct{}{}:
		default:
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	app := NewApp()
	resultChannel := make(chan StabilityResult, 1)
	go func() {
		resultChannel <- app.RunStabilityTest(StabilityOptions{
			TaskID:         "stability-cancel",
			TargetURL:      server.URL,
			ProxyMode:      proxyModeDirect,
			DurationSecond: 60,
			IntervalMS:     250,
		})
	}()
	select {
	case <-requestSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("stability probe did not start")
	}
	if !app.CancelTask("stability-cancel") {
		t.Fatal("CancelTask() did not find stability task")
	}
	select {
	case result := <-resultChannel:
		if result.Status != "cancelled" || result.SampleCount == 0 {
			t.Fatalf("result = %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled stability task did not return promptly")
	}
}
