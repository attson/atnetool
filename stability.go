package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultStabilityDuration = 30 * time.Second
	minStabilityDuration     = 5 * time.Second
	maxStabilityDuration     = 30 * time.Minute
	defaultProbeInterval     = time.Second
	minProbeInterval         = 250 * time.Millisecond
	maxProbeInterval         = 10 * time.Second
	stabilityProbeTimeout    = 5 * time.Second
)

type StabilityOptions struct {
	TaskID                  string  `json:"task_id"`
	TargetURL               string  `json:"target_url"`
	ProxyMode               string  `json:"proxy_mode"`
	ProxyURL                string  `json:"proxy_url,omitempty"`
	DurationSecond          int     `json:"duration_seconds"`
	IntervalMS              int     `json:"interval_ms"`
	LatencyThresholdMS      int64   `json:"latency_threshold_ms"`
	FailureThresholdPercent float64 `json:"failure_threshold_percent"`
}

type StabilitySample struct {
	Sequence   int    `json:"sequence"`
	CheckedAt  string `json:"checked_at"`
	LatencyMS  int64  `json:"latency_ms"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"status_code"`
	RemoteIP   string `json:"remote_ip,omitempty"`
	CFRay      string `json:"cf_ray,omitempty"`
	CFColo     string `json:"cf_colo,omitempty"`
	Error      string `json:"error,omitempty"`
}

type StabilityIncident struct {
	Type          string `json:"type"`
	StartedAt     string `json:"started_at"`
	EndedAt       string `json:"ended_at"`
	StartSequence int    `json:"start_sequence"`
	EndSequence   int    `json:"end_sequence"`
	SampleCount   int    `json:"sample_count"`
	PeakLatencyMS int64  `json:"peak_latency_ms"`
	Resolved      bool   `json:"resolved"`
	Detail        string `json:"detail"`
}

type StabilityRouteChange struct {
	Sequence  int    `json:"sequence"`
	CheckedAt string `json:"checked_at"`
	Previous  string `json:"previous"`
	Current   string `json:"current"`
}

type StabilityProgress struct {
	TaskID     string          `json:"task_id"`
	ElapsedMS  int64           `json:"elapsed_ms"`
	DurationMS int64           `json:"duration_ms"`
	Sample     StabilitySample `json:"sample"`
}

type StabilityResult struct {
	SchemaVersion           int                    `json:"schema_version"`
	TaskID                  string                 `json:"task_id"`
	Status                  string                 `json:"status"`
	Message                 string                 `json:"message"`
	TargetURL               string                 `json:"target_url"`
	ProxyMode               string                 `json:"proxy_mode"`
	ProxyURL                string                 `json:"proxy_url,omitempty"`
	StartedAt               string                 `json:"started_at"`
	FinishedAt              string                 `json:"finished_at"`
	DurationMS              int64                  `json:"duration_ms"`
	ConfiguredDurationMS    int64                  `json:"configured_duration_ms"`
	IntervalMS              int64                  `json:"interval_ms"`
	SampleCount             int                    `json:"sample_count"`
	SuccessCount            int                    `json:"success_count"`
	FailureRate             float64                `json:"failure_rate"`
	AvailabilityPercent     float64                `json:"availability_percent"`
	AverageMS               float64                `json:"average_ms"`
	MinimumMS               int64                  `json:"minimum_ms"`
	MaximumMS               int64                  `json:"maximum_ms"`
	P50MS                   int64                  `json:"p50_ms"`
	P95MS                   int64                  `json:"p95_ms"`
	JitterMS                float64                `json:"jitter_ms"`
	LatencyThresholdMS      int64                  `json:"latency_threshold_ms"`
	FailureThresholdPercent float64                `json:"failure_threshold_percent"`
	SlowCount               int                    `json:"slow_count"`
	LongestFailureStreak    int                    `json:"longest_failure_streak"`
	Incidents               []StabilityIncident    `json:"incidents"`
	RouteChanges            []StabilityRouteChange `json:"route_changes"`
	Finding                 PatrolFinding          `json:"finding"`
	Samples                 []StabilitySample      `json:"samples"`
	Environment             NetworkEnvironment     `json:"environment"`
}

// RunStabilityTest repeatedly measures response-header latency with bounded
// one-kilobyte reads. It evaluates a real URL without requiring a test server.
func (a *App) RunStabilityTest(options StabilityOptions) StabilityResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := StabilityResult{
		SchemaVersion:           taskSchemaVersion,
		TaskID:                  taskID,
		Status:                  "running",
		Message:                 "稳定性测试未完成",
		TargetURL:               strings.TrimSpace(options.TargetURL),
		ProxyMode:               normaliseProxyMode(options.ProxyMode),
		LatencyThresholdMS:      boundedLatencyThreshold(options.LatencyThresholdMS),
		FailureThresholdPercent: boundedFailureThreshold(options.FailureThresholdPercent),
		StartedAt:               started.Format(time.RFC3339),
	}
	parsedURL, err := validateHTTPURL(result.TargetURL)
	if err != nil {
		result.Status = "failed"
		result.Message = "检测地址配置有误"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	result.TargetURL = parsedURL.String()

	duration := boundedStabilityDuration(options.DurationSecond)
	interval := boundedProbeInterval(options.IntervalMS)
	result.ConfiguredDurationMS = duration.Milliseconds()
	result.IntervalMS = interval.Milliseconds()
	ctx, cancel := context.WithCancel(a.baseContext())
	if !a.registerTask(taskID, cancel) {
		result.Status = "rejected"
		result.Message = "任务标识已在运行"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	defer func() {
		cancel()
		a.unregisterTask(taskID)
	}()

	request, err := http.NewRequest(http.MethodGet, result.TargetURL, nil)
	if err != nil {
		result.Status = "failed"
		result.Message = "无法创建稳定性探测请求"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	proxyFunc, selectedProxy, err := proxyForRequest(result.ProxyMode, options.ProxyURL, request)
	if err != nil {
		result.Status = "failed"
		result.Message = "代理配置有误"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	if selectedProxy != nil {
		result.ProxyURL = safeProxyURL(selectedProxy)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFunc
	transport.MaxIdleConns = 4
	transport.MaxIdleConnsPerHost = 2
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	runContext, stopAtDuration := context.WithTimeout(ctx, duration)
	defer stopAtDuration()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for sequence := 1; ; sequence++ {
		sample := runStabilityProbe(runContext, client, result.TargetURL, sequence)
		result.Samples = append(result.Samples, sample)
		a.emitStabilityProgress(StabilityProgress{
			TaskID:     taskID,
			ElapsedMS:  durationMS(time.Since(started)),
			DurationMS: duration.Milliseconds(),
			Sample:     sample,
		})
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Message = "稳定性测试已停止"
			finaliseStabilityResult(&result, started)
			return result
		}
		select {
		case <-ctx.Done():
			result.Status = "cancelled"
			result.Message = "稳定性测试已停止"
			finaliseStabilityResult(&result, started)
			return result
		case <-runContext.Done():
			if ctx.Err() != nil {
				result.Status = "cancelled"
				result.Message = "稳定性测试已停止"
			} else {
				result.Status = "completed"
				result.Message = "稳定性测试完成"
			}
			finaliseStabilityResult(&result, started)
			return result
		case <-ticker.C:
		}
	}
}

func runStabilityProbe(parent context.Context, client *http.Client, targetURL string, sequence int) StabilitySample {
	sample := StabilitySample{Sequence: sequence, CheckedAt: time.Now().Format(time.RFC3339Nano)}
	ctx, cancel := context.WithTimeout(parent, stabilityProbeTimeout)
	defer cancel()
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		host, _, err := net.SplitHostPort(info.Conn.RemoteAddr().String())
		if err == nil {
			sample.RemoteIP = host
		} else {
			sample.RemoteIP = info.Conn.RemoteAddr().String()
		}
	}}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, targetURL, nil)
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Range", "bytes=0-1023")
	started := time.Now()
	response, err := client.Do(request)
	sample.LatencyMS = durationMS(time.Since(started))
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	defer response.Body.Close()
	sample.StatusCode = response.StatusCode
	sample.CFRay = response.Header.Get("CF-Ray")
	sample.CFColo = cloudflareColo(sample.CFRay)
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if readErr != nil {
		sample.Error = readErr.Error()
		return sample
	}
	if response.StatusCode >= http.StatusInternalServerError {
		sample.Error = response.Status
		return sample
	}
	sample.Success = true
	return sample
}

func finaliseStabilityResult(result *StabilityResult, started time.Time) {
	result.FinishedAt = time.Now().Format(time.RFC3339)
	result.DurationMS = durationMS(time.Since(started))
	result.SampleCount = len(result.Samples)
	latencies := make([]int64, 0, len(result.Samples))
	var total int64
	var previous int64
	var jitterTotal float64
	for _, sample := range result.Samples {
		if !sample.Success {
			continue
		}
		result.SuccessCount++
		latencies = append(latencies, sample.LatencyMS)
		total += sample.LatencyMS
		if previous > 0 {
			jitterTotal += math.Abs(float64(sample.LatencyMS - previous))
		}
		previous = sample.LatencyMS
	}
	if result.SampleCount > 0 {
		result.FailureRate = float64(result.SampleCount-result.SuccessCount) / float64(result.SampleCount)
		result.AvailabilityPercent = float64(result.SuccessCount) / float64(result.SampleCount) * 100
	}
	result.Incidents = buildStabilityIncidents(result.Samples, result.LatencyThresholdMS)
	result.RouteChanges = buildStabilityRouteChanges(result.Samples)
	result.LongestFailureStreak = longestStabilityFailureStreak(result.Samples)
	for _, sample := range result.Samples {
		if sample.Success && sample.LatencyMS > result.LatencyThresholdMS {
			result.SlowCount++
		}
	}
	if len(latencies) == 0 {
		result.Finding = PatrolFinding{Code: "monitor_unavailable", Level: "critical", Title: "监控期间目标持续不可用", Message: "所有采样均失败，请结合 DNS、端口和路径诊断定位故障层级。"}
		result.Environment = captureNetworkEnvironment(DiagResult{
			ProxyMode: result.ProxyMode,
			ProxyURL:  result.ProxyURL,
		})
		return
	}
	sort.Slice(latencies, func(left, right int) bool { return latencies[left] < latencies[right] })
	result.MinimumMS = latencies[0]
	result.MaximumMS = latencies[len(latencies)-1]
	result.AverageMS = float64(total) / float64(len(latencies))
	result.P50MS = percentileLatency(latencies, 0.50)
	result.P95MS = percentileLatency(latencies, 0.95)
	if len(latencies) > 1 {
		result.JitterMS = jitterTotal / float64(len(latencies)-1)
	}
	result.Finding = buildStabilityFinding(*result)
	result.Environment = captureNetworkEnvironment(DiagResult{
		ProxyMode: result.ProxyMode,
		ProxyURL:  result.ProxyURL,
	})
}

func buildStabilityIncidents(samples []StabilitySample, latencyThreshold int64) []StabilityIncident {
	var result []StabilityIncident
	var active *StabilityIncident
	closeIncident := func(resolved bool) {
		if active == nil {
			return
		}
		active.Resolved = resolved
		if active.Type == "failure" {
			active.Detail = fmt.Sprintf("连续 %d 次请求失败", active.SampleCount)
		} else {
			active.Detail = fmt.Sprintf("连续 %d 次超过 %d ms，峰值 %d ms", active.SampleCount, latencyThreshold, active.PeakLatencyMS)
		}
		result = append(result, *active)
		active = nil
	}
	for _, sample := range samples {
		incidentType := ""
		if !sample.Success {
			incidentType = "failure"
		} else if sample.LatencyMS > latencyThreshold {
			incidentType = "latency"
		}
		if incidentType == "" {
			closeIncident(true)
			continue
		}
		if active != nil && active.Type != incidentType {
			closeIncident(true)
		}
		if active == nil {
			active = &StabilityIncident{Type: incidentType, StartedAt: sample.CheckedAt, StartSequence: sample.Sequence}
		}
		active.EndedAt = sample.CheckedAt
		active.EndSequence = sample.Sequence
		active.SampleCount++
		if sample.LatencyMS > active.PeakLatencyMS {
			active.PeakLatencyMS = sample.LatencyMS
		}
	}
	closeIncident(false)
	return result
}

func buildStabilityRouteChanges(samples []StabilitySample) []StabilityRouteChange {
	var result []StabilityRouteChange
	previous := ""
	for _, sample := range samples {
		if !sample.Success {
			continue
		}
		current := stabilityRouteIdentity(sample)
		if current == "" {
			continue
		}
		if previous != "" && current != previous {
			result = append(result, StabilityRouteChange{Sequence: sample.Sequence, CheckedAt: sample.CheckedAt, Previous: previous, Current: current})
		}
		previous = current
	}
	return result
}

func stabilityRouteIdentity(sample StabilitySample) string {
	if sample.CFColo != "" {
		return sample.RemoteIP + " / " + sample.CFColo
	}
	return sample.RemoteIP
}

func longestStabilityFailureStreak(samples []StabilitySample) int {
	longest, current := 0, 0
	for _, sample := range samples {
		if sample.Success {
			current = 0
			continue
		}
		current++
		if current > longest {
			longest = current
		}
	}
	return longest
}

func buildStabilityFinding(result StabilityResult) PatrolFinding {
	if result.SuccessCount == 0 || result.FailureRate*100 > result.FailureThresholdPercent {
		return PatrolFinding{Code: "monitor_failure_rate", Level: "critical", Title: "可用率低于监控阈值", Message: fmt.Sprintf("可用率 %.1f%%，最长连续失败 %d 次。", result.AvailabilityPercent, result.LongestFailureStreak)}
	}
	if result.SlowCount > 0 || result.P95MS > result.LatencyThresholdMS || result.JitterMS > 100 {
		return PatrolFinding{Code: "monitor_latency", Level: "warning", Title: "监控期间存在延迟波动", Message: fmt.Sprintf("%d 个样本超过 %d ms，P95 %d ms，形成 %d 个异常事件。", result.SlowCount, result.LatencyThresholdMS, result.P95MS, len(result.Incidents))}
	}
	if len(result.RouteChanges) > 0 {
		return PatrolFinding{Code: "monitor_route_change", Level: "warning", Title: "监控期间连接路径发生变化", Message: fmt.Sprintf("检测到 %d 次远端 IP 或 Cloudflare 节点变化。", len(result.RouteChanges))}
	}
	return PatrolFinding{Code: "monitor_stable", Level: "good", Title: "监控期间连接稳定", Message: fmt.Sprintf("可用率 %.1f%%，P95 %d ms，未形成异常事件。", result.AvailabilityPercent, result.P95MS)}
}

func percentileLatency(sorted []int64, percentile float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func boundedStabilityDuration(seconds int) time.Duration {
	duration := time.Duration(seconds) * time.Second
	if duration == 0 {
		return defaultStabilityDuration
	}
	if duration < minStabilityDuration {
		return minStabilityDuration
	}
	if duration > maxStabilityDuration {
		return maxStabilityDuration
	}
	return duration
}

func boundedProbeInterval(milliseconds int) time.Duration {
	interval := time.Duration(milliseconds) * time.Millisecond
	if interval == 0 {
		return defaultProbeInterval
	}
	if interval < minProbeInterval {
		return minProbeInterval
	}
	if interval > maxProbeInterval {
		return maxProbeInterval
	}
	return interval
}

func boundedLatencyThreshold(milliseconds int64) int64 {
	if milliseconds <= 0 {
		return 500
	}
	if milliseconds < 50 {
		return 50
	}
	if milliseconds > 10000 {
		return 10000
	}
	return milliseconds
}

func boundedFailureThreshold(percent float64) float64 {
	if percent <= 0 {
		return 10
	}
	if percent < 1 {
		return 1
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func (a *App) emitStabilityProgress(progress StabilityProgress) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "stability:sample", progress)
}
