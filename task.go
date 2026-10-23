package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

const taskSchemaVersion = 2

// DiagnosisTaskOptions identifies one complete HTTP and route diagnosis.
type DiagnosisTaskOptions struct {
	TaskID    string `json:"task_id"`
	TargetURL string `json:"target_url"`
	ProxyMode string `json:"proxy_mode"`
	ProxyURL  string `json:"proxy_url,omitempty"`
}

// DiagnosisTaskResult keeps all evidence produced by one cancellable run.
type DiagnosisTaskResult struct {
	SchemaVersion int                `json:"schema_version"`
	TaskID        string             `json:"task_id"`
	Status        string             `json:"status"`
	StartedAt     string             `json:"started_at"`
	FinishedAt    string             `json:"finished_at"`
	Diagnosis     DiagResult         `json:"diagnosis"`
	Traceroute    TraceResult        `json:"traceroute"`
	Environment   NetworkEnvironment `json:"environment"`
}

// RunDiagnosisTask runs HTTP diagnosis and traceroute under one cancellation
// scope. The synchronous return keeps Wails error handling straightforward.
func (a *App) RunDiagnosisTask(options DiagnosisTaskOptions) DiagnosisTaskResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := DiagnosisTaskResult{
		SchemaVersion: taskSchemaVersion,
		TaskID:        taskID,
		Status:        "running",
		StartedAt:     started.Format(time.RFC3339),
	}

	ctx, cancel := context.WithCancel(a.baseContext())
	if !a.registerTask(taskID, cancel) {
		result.Status = "rejected"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		result.Diagnosis = DiagResult{
			Message:     "任务标识已在运行",
			FailedStage: "task",
			Error:       "duplicate task id",
			TargetURL:   strings.TrimSpace(options.TargetURL),
			CheckedAt:   started.Format(time.RFC3339),
		}
		return result
	}
	defer func() {
		cancel()
		a.unregisterTask(taskID)
	}()

	diagnosisChannel := make(chan DiagResult, 1)
	traceChannel := make(chan TraceResult, 1)
	go func() {
		diagnosisChannel <- a.runDiagnose(ctx, DiagOptions{
			TargetURL: options.TargetURL,
			ProxyMode: options.ProxyMode,
			ProxyURL:  options.ProxyURL,
		})
	}()
	go func() {
		traceChannel <- a.runTraceroute(ctx, options.TargetURL)
	}()

	result.Diagnosis = <-diagnosisChannel
	result.Traceroute = <-traceChannel
	result.Environment = captureNetworkEnvironment(result.Diagnosis)
	if ctx.Err() != nil {
		result.Status = "cancelled"
	} else {
		result.Status = "completed"
	}
	result.FinishedAt = time.Now().Format(time.RFC3339)
	return result
}

func (a *App) CancelTask(taskID string) bool {
	a.tasksMu.Lock()
	cancel, exists := a.tasks[strings.TrimSpace(taskID)]
	a.tasksMu.Unlock()
	if exists {
		cancel()
	}
	return exists
}

func (a *App) registerTask(taskID string, cancel context.CancelFunc) bool {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	if a.tasks == nil {
		a.tasks = make(map[string]context.CancelFunc)
	}
	if _, exists := a.tasks[taskID]; exists {
		return false
	}
	a.tasks[taskID] = cancel
	return true
}

func (a *App) unregisterTask(taskID string) {
	a.tasksMu.Lock()
	delete(a.tasks, taskID)
	a.tasksMu.Unlock()
}

func newTaskID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return time.Now().UTC().Format("20060102T150405.000000000")
}
