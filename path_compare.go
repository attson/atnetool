package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultPathCompareSamples = 3
	maxPathCompareSamples     = 5
	pathCompareResponseLimit  = 4 << 10
	pathCompareTimeout        = 70 * time.Second
)

type PathCompareOptions struct {
	TaskID         string `json:"task_id"`
	TargetURL      string `json:"target_url"`
	ManualProxyURL string `json:"manual_proxy_url,omitempty"`
	Samples        int    `json:"samples"`
}

type PathCompareSample struct {
	Sequence       int    `json:"sequence"`
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	FailedStage    string `json:"failed_stage,omitempty"`
	Error          string `json:"error,omitempty"`
	TCPConnectMS   int64  `json:"tcp_connect_ms"`
	TLSHandshakeMS int64  `json:"tls_handshake_ms"`
	TTFBMS         int64  `json:"ttfb_ms"`
	TotalMS        int64  `json:"total_ms"`
	StatusCode     int    `json:"status_code"`
	ProxyStatus    string `json:"proxy_status"`
	ProxyURL       string `json:"proxy_url,omitempty"`
	RemoteIP       string `json:"remote_ip,omitempty"`
	CFColo         string `json:"cf_colo,omitempty"`
}

type PathCompareRoute struct {
	ID             string              `json:"id"`
	Label          string              `json:"label"`
	ProxyMode      string              `json:"proxy_mode"`
	ConfiguredURL  string              `json:"configured_url,omitempty"`
	EffectivePath  string              `json:"effective_path"`
	SameAs         string              `json:"same_as,omitempty"`
	Samples        []PathCompareSample `json:"samples"`
	SampleCount    int                 `json:"sample_count"`
	CompletedCount int                 `json:"completed_count"`
	SuccessCount   int                 `json:"success_count"`
	SuccessRate    float64             `json:"success_rate"`
	P50TCPMS       int64               `json:"p50_tcp_ms"`
	P50TLSMS       int64               `json:"p50_tls_ms"`
	P50TTFBMS      int64               `json:"p50_ttfb_ms"`
	P50TotalMS     int64               `json:"p50_total_ms"`
	JitterMS       float64             `json:"jitter_ms"`
	RemoteIP       string              `json:"remote_ip,omitempty"`
	ProxyURL       string              `json:"proxy_url,omitempty"`
}

type PathCompareFinding struct {
	Code               string  `json:"code"`
	Level              string  `json:"level"`
	Title              string  `json:"title"`
	Detail             string  `json:"detail"`
	RecommendedPathID  string  `json:"recommended_path_id,omitempty"`
	ImprovementPercent float64 `json:"improvement_percent"`
}

type PathCompareProgress struct {
	TaskID    string            `json:"task_id"`
	Complete  int               `json:"complete"`
	Total     int               `json:"total"`
	PathID    string            `json:"path_id"`
	PathLabel string            `json:"path_label"`
	Sample    PathCompareSample `json:"sample"`
}

type PathCompareResult struct {
	SchemaVersion int                `json:"schema_version"`
	TaskID        string             `json:"task_id"`
	Status        string             `json:"status"`
	Message       string             `json:"message"`
	TargetURL     string             `json:"target_url"`
	StartedAt     string             `json:"started_at"`
	FinishedAt    string             `json:"finished_at"`
	DurationMS    int64              `json:"duration_ms"`
	SampleCount   int                `json:"sample_count"`
	Completed     int                `json:"completed"`
	Total         int                `json:"total"`
	Finding       PathCompareFinding `json:"finding"`
	Paths         []PathCompareRoute `json:"paths"`
	Environment   NetworkEnvironment `json:"environment"`
}

type pathCompareDefinition struct {
	id       string
	label    string
	mode     string
	proxyURL string
}

func (a *App) RunPathCompare(options PathCompareOptions) PathCompareResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := PathCompareResult{
		SchemaVersion: 1,
		TaskID:        taskID,
		Status:        "running",
		Message:       "路径对照未完成",
		StartedAt:     started.Format(time.RFC3339),
		SampleCount:   boundedPathCompareSamples(options.Samples),
	}
	parsedTarget, err := validateHTTPURL(strings.TrimSpace(options.TargetURL))
	if err != nil {
		result.Status = "failed"
		result.Message = "检测地址配置有误"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	result.TargetURL = parsedTarget.String()

	definitions := []pathCompareDefinition{
		{id: "system", label: "系统代理", mode: proxyModeSystem},
		{id: "direct", label: "强制直连", mode: proxyModeDirect},
	}
	if strings.TrimSpace(options.ManualProxyURL) != "" {
		proxyURL, proxyErr := validateProxyURL(options.ManualProxyURL)
		if proxyErr != nil {
			result.Status = "failed"
			result.Message = "手动代理地址配置有误"
			result.FinishedAt = time.Now().Format(time.RFC3339)
			return result
		}
		definitions = append(definitions, pathCompareDefinition{
			id: "manual", label: "手动代理", mode: proxyModeManual, proxyURL: safeProxyURL(proxyURL),
		})
	}
	result.Total = len(definitions) * result.SampleCount

	baseContext, cancel := context.WithCancel(a.baseContext())
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
	ctx, stopAtTimeout := context.WithTimeout(baseContext, pathCompareTimeout)
	defer stopAtTimeout()

	type completedRoute struct {
		index int
		route PathCompareRoute
	}
	completed := make(chan completedRoute, len(definitions))
	var progressMu sync.Mutex
	for index, definition := range definitions {
		index, definition := index, definition
		go func() {
			route := a.runPathCompareRoute(ctx, definition, result.TargetURL, result.SampleCount, func(sample PathCompareSample) {
				progressMu.Lock()
				result.Completed++
				complete := result.Completed
				progressMu.Unlock()
				a.emitPathCompareProgress(PathCompareProgress{
					TaskID: taskID, Complete: complete, Total: result.Total,
					PathID: definition.id, PathLabel: definition.label, Sample: sample,
				})
			})
			completed <- completedRoute{index: index, route: route}
		}()
	}

	result.Paths = make([]PathCompareRoute, len(definitions))
	for range definitions {
		item := <-completed
		result.Paths[item.index] = item.route
	}
	markEquivalentPaths(result.Paths)
	result.Finding = buildPathCompareFinding(result.Paths)
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "路径对照已停止"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Message = "路径对照超时，已保留完成结果"
	} else {
		result.Status = "completed"
		result.Message = "路径对照完成"
	}
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeSystem})
	return result
}

func (a *App) runPathCompareRoute(ctx context.Context, definition pathCompareDefinition, targetURL string, samples int, onSample func(PathCompareSample)) PathCompareRoute {
	route := PathCompareRoute{
		ID: definition.id, Label: definition.label, ProxyMode: definition.mode,
		ConfiguredURL: definition.proxyURL, SampleCount: samples,
	}
	for sequence := 1; sequence <= samples && ctx.Err() == nil; sequence++ {
		diagnosis := a.runDiagnoseLimited(ctx, DiagOptions{
			TargetURL: targetURL, ProxyMode: definition.mode, ProxyURL: definition.proxyURL,
		}, pathCompareResponseLimit, true)
		sample := pathCompareSampleFromDiagnosis(sequence, diagnosis)
		route.Samples = append(route.Samples, sample)
		if onSample != nil {
			onSample(sample)
		}
	}
	finalisePathCompareRoute(&route)
	return route
}

func pathCompareSampleFromDiagnosis(sequence int, result DiagResult) PathCompareSample {
	return PathCompareSample{
		Sequence: sequence, Success: result.Success, Message: result.Message,
		FailedStage: result.FailedStage, Error: result.Error,
		TCPConnectMS: result.TCPConnectMS, TLSHandshakeMS: result.TLSHandshakeMS,
		TTFBMS: result.TTFBMS, TotalMS: result.TotalMS, StatusCode: result.StatusCode,
		ProxyStatus: result.ProxyStatus, ProxyURL: result.ProxyURL,
		RemoteIP: result.RemoteIP, CFColo: result.CFColo,
	}
}

func finalisePathCompareRoute(route *PathCompareRoute) {
	var tcpValues, tlsValues, ttfbValues, totalValues []int64
	var previous int64
	var jitterTotal float64
	directObserved := route.ProxyMode == proxyModeDirect
	route.CompletedCount = len(route.Samples)
	for _, sample := range route.Samples {
		if route.ProxyURL == "" && sample.ProxyURL != "" {
			route.ProxyURL = sample.ProxyURL
		}
		if route.RemoteIP == "" && sample.RemoteIP != "" {
			route.RemoteIP = sample.RemoteIP
		}
		if sample.ProxyStatus == "direct" {
			directObserved = true
		}
		if !sample.Success {
			continue
		}
		route.SuccessCount++
		tcpValues = append(tcpValues, sample.TCPConnectMS)
		tlsValues = append(tlsValues, sample.TLSHandshakeMS)
		ttfbValues = append(ttfbValues, sample.TTFBMS)
		totalValues = append(totalValues, sample.TotalMS)
		if previous > 0 {
			jitterTotal += math.Abs(float64(sample.TotalMS - previous))
		}
		previous = sample.TotalMS
	}
	if route.SampleCount > 0 {
		route.SuccessRate = float64(route.SuccessCount) / float64(route.SampleCount)
	}
	route.P50TCPMS = medianLatency(tcpValues)
	route.P50TLSMS = medianLatency(tlsValues)
	route.P50TTFBMS = medianLatency(ttfbValues)
	route.P50TotalMS = medianLatency(totalValues)
	if len(totalValues) > 1 {
		route.JitterMS = jitterTotal / float64(len(totalValues)-1)
	}
	if route.ProxyURL != "" {
		route.EffectivePath = "proxy:" + route.ProxyURL
	} else if directObserved {
		route.EffectivePath = "direct"
	} else {
		route.EffectivePath = "unresolved:" + route.ID
	}
}

func markEquivalentPaths(paths []PathCompareRoute) {
	firstByPath := make(map[string]string)
	for index := range paths {
		key := paths[index].EffectivePath
		if key == "" {
			continue
		}
		if first, found := firstByPath[key]; found {
			paths[index].SameAs = first
			continue
		}
		firstByPath[key] = paths[index].ID
	}
}

func buildPathCompareFinding(paths []PathCompareRoute) PathCompareFinding {
	unique := make([]PathCompareRoute, 0, len(paths))
	seen := make(map[string]struct{})
	for _, path := range paths {
		key := path.EffectivePath
		if key == "" {
			key = path.ID
		}
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	if len(unique) == 0 {
		return PathCompareFinding{Code: "no_result", Level: "critical", Title: "没有可比较的路径", Detail: "所有路径都未返回有效采样。"}
	}
	if len(unique) == 1 {
		path := unique[0]
		if path.SuccessCount == 0 {
			return PathCompareFinding{Code: "all_failed", Level: "critical", Title: "目标在当前路径不可达", Detail: "系统代理与直连落到同一路径，且该路径的采样全部失败。"}
		}
		return PathCompareFinding{Code: "paths_same", Level: "neutral", Title: "系统代理与直连路径相同", Detail: "系统未为该目标启用显式代理，两组数据不代表不同出口。"}
	}

	sort.SliceStable(unique, func(left, right int) bool {
		if unique[left].SuccessRate != unique[right].SuccessRate {
			return unique[left].SuccessRate > unique[right].SuccessRate
		}
		if unique[left].P50TotalMS != unique[right].P50TotalMS {
			if unique[left].P50TotalMS == 0 {
				return false
			}
			if unique[right].P50TotalMS == 0 {
				return true
			}
			return unique[left].P50TotalMS < unique[right].P50TotalMS
		}
		return unique[left].JitterMS < unique[right].JitterMS
	})
	best, runnerUp := unique[0], unique[1]
	if best.SuccessCount == 0 {
		return PathCompareFinding{Code: "all_failed", Level: "critical", Title: "所有路径均不可达", Detail: "请先检查本地网络、代理监听和目标服务状态。"}
	}
	if best.SuccessRate-runnerUp.SuccessRate >= 0.2 {
		return PathCompareFinding{
			Code: "more_reliable", Level: "warning", Title: fmt.Sprintf("%s成功率更高", best.Label),
			Detail:            fmt.Sprintf("成功率 %.0f%%，对照路径为 %.0f%%；建议优先排查较低成功率路径。", best.SuccessRate*100, runnerUp.SuccessRate*100),
			RecommendedPathID: best.ID,
		}
	}
	if best.P50TotalMS > 0 && runnerUp.P50TotalMS > best.P50TotalMS {
		improvement := float64(runnerUp.P50TotalMS-best.P50TotalMS) / float64(runnerUp.P50TotalMS) * 100
		if improvement >= 10 && runnerUp.P50TotalMS-best.P50TotalMS >= 20 {
			return PathCompareFinding{
				Code: "faster_path", Level: "warning", Title: fmt.Sprintf("%s响应更快", best.Label),
				Detail:            fmt.Sprintf("中位总耗时降低约 %.0f%%（%d ms → %d ms），可作为后续连接策略治理依据。", improvement, runnerUp.P50TotalMS, best.P50TotalMS),
				RecommendedPathID: best.ID, ImprovementPercent: improvement,
			}
		}
	}
	return PathCompareFinding{Code: "no_material_difference", Level: "good", Title: "各路径表现接近", Detail: "成功率和中位耗时未形成稳定的显著差异，暂不建议调整连接策略。"}
}

func boundedPathCompareSamples(requested int) int {
	if requested <= 0 {
		return defaultPathCompareSamples
	}
	if requested > maxPathCompareSamples {
		return maxPathCompareSamples
	}
	return requested
}

func (a *App) emitPathCompareProgress(progress PathCompareProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "path-compare:sample", progress)
	}
}
