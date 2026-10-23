package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunDiagnosisTaskCanBeCancelled(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	app := NewApp()
	resultChannel := make(chan DiagnosisTaskResult, 1)
	go func() {
		resultChannel <- app.RunDiagnosisTask(DiagnosisTaskOptions{
			TaskID:    "cancel-test",
			TargetURL: server.URL,
			ProxyMode: proxyModeDirect,
		})
	}()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("diagnostic request did not start")
	}
	if !app.CancelTask("cancel-test") {
		t.Fatal("CancelTask() did not find the running task")
	}
	select {
	case result := <-resultChannel:
		if result.Status != "cancelled" {
			t.Fatalf("status = %q, want cancelled", result.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled task did not return promptly")
	}
	if app.CancelTask("cancel-test") {
		t.Fatal("completed task was still registered")
	}
}
