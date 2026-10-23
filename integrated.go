package main

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const integratedComponentCount = 5

type IntegratedDiagnosisOptions struct {
	TaskID    string `json:"task_id"`
	TargetURL string `json:"target_url"`
	ProxyMode string `json:"proxy_mode"`
	ProxyURL  string `json:"proxy_url,omitempty"`
}

type IntegratedFinding struct {
	Layer  string `json:"layer"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type IntegratedRemediation struct {
	Priority int    `json:"priority"`
	Action   string `json:"action"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Module   string `json:"module"`
}

type IntegratedProgress struct {
	TaskID   string `json:"task_id"`
	Module   string `json:"module"`
	Complete int    `json:"complete"`
	Total    int    `json:"total"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

type IntegratedDiagnosisResult struct {
	SchemaVersion int                     `json:"schema_version"`
	TaskID        string                  `json:"task_id"`
	Status        string                  `json:"status"`
	OverallStatus string                  `json:"overall_status"`
	Message       string                  `json:"message"`
	TargetURL     string                  `json:"target_url"`
	Domain        string                  `json:"domain"`
	StartedAt     string                  `json:"started_at"`
	FinishedAt    string                  `json:"finished_at"`
	DurationMS    int64                   `json:"duration_ms"`
	Completed     int                     `json:"completed"`
	Total         int                     `json:"total"`
	Findings      []IntegratedFinding     `json:"findings"`
	Remediations  []IntegratedRemediation `json:"remediations"`
	Baseline      NetworkBaselineResult   `json:"baseline"`
	DNS           DNSDiagnoseResult       `json:"dns"`
	Ports         PortTestResult          `json:"ports"`
	HTTP          DiagnosisTaskResult     `json:"http"`
	Paths         PathCompareResult       `json:"paths"`
	Environment   NetworkEnvironment      `json:"environment"`
}

func (a *App) RunIntegratedDiagnosis(options IntegratedDiagnosisOptions) IntegratedDiagnosisResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := IntegratedDiagnosisResult{
		SchemaVersion: 1,
		TaskID:        taskID,
		Status:        "running",
		Message:       "综合诊断未完成",
		StartedAt:     started.Format(time.RFC3339),
		Total:         integratedComponentCount,
	}
	parsed, err := validateHTTPURL(strings.TrimSpace(options.TargetURL))
	if err != nil {
		result.Status = "failed"
		result.OverallStatus = "critical"
		result.Message = "检测地址配置有误"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	result.TargetURL = parsed.String()
	result.Domain = parsed.Hostname()

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

	childIDs := map[string]string{
		"baseline": taskID + "-baseline",
		"dns":      taskID + "-dns",
		"ports":    taskID + "-ports",
		"http":     taskID + "-http",
		"paths":    taskID + "-paths",
	}
	finished := make(chan struct{})
	defer close(finished)
	go a.cancelIntegratedChildren(ctx, finished, childIDs)

	type componentDone struct {
		module  string
		status  string
		message string
	}
	completed := make(chan componentDone, integratedComponentCount)
	go func() {
		result.Baseline = a.RunNetworkBaseline(NetworkBaselineOptions{TaskID: childIDs["baseline"], TargetHost: result.Domain})
		completed <- componentDone{module: "baseline", status: result.Baseline.Status, message: result.Baseline.Message}
	}()
	go func() {
		result.DNS = a.RunDNSDiagnose(DNSDiagnoseOptions{TaskID: childIDs["dns"], Domain: result.Domain})
		completed <- componentDone{module: "dns", status: result.DNS.Status, message: result.DNS.Message}
	}()
	go func() {
		result.Ports = a.RunPortTest(PortTestOptions{TaskID: childIDs["ports"], Target: result.Domain, Ports: []int{80, 443}, Concurrency: 2})
		completed <- componentDone{module: "ports", status: result.Ports.Status, message: result.Ports.Message}
	}()
	go func() {
		result.HTTP = a.RunDiagnosisTask(DiagnosisTaskOptions{TaskID: childIDs["http"], TargetURL: result.TargetURL, ProxyMode: options.ProxyMode, ProxyURL: options.ProxyURL})
		completed <- componentDone{module: "http", status: result.HTTP.Status, message: result.HTTP.Diagnosis.Message}
	}()
	go func() {
		manualProxy := ""
		if normaliseProxyMode(options.ProxyMode) == proxyModeManual {
			manualProxy = options.ProxyURL
		}
		result.Paths = a.RunPathCompare(PathCompareOptions{TaskID: childIDs["paths"], TargetURL: result.TargetURL, ManualProxyURL: manualProxy, Samples: 3})
		completed <- componentDone{module: "paths", status: result.Paths.Status, message: result.Paths.Message}
	}()

	for result.Completed < result.Total {
		component := <-completed
		result.Completed++
		a.emitIntegratedProgress(IntegratedProgress{
			TaskID: taskID, Module: component.module, Complete: result.Completed, Total: result.Total,
			Status: component.status, Message: component.message,
		})
	}
	result.Environment = result.HTTP.Environment
	result.Findings, result.Remediations, result.OverallStatus = buildIntegratedAssessment(result)
	if ctx.Err() != nil {
		result.Status = "cancelled"
		result.Message = "综合诊断已停止，已保留完成结果"
	} else {
		result.Status = "completed"
		result.Message = "综合诊断完成"
	}
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	return result
}

func (a *App) cancelIntegratedChildren(ctx context.Context, finished <-chan struct{}, childIDs map[string]string) {
	select {
	case <-finished:
		return
	case <-ctx.Done():
	}
	for attempt := 0; attempt < 10; attempt++ {
		for _, childID := range childIDs {
			a.CancelTask(childID)
		}
		select {
		case <-finished:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func buildIntegratedAssessment(result IntegratedDiagnosisResult) ([]IntegratedFinding, []IntegratedRemediation, string) {
	findings := make([]IntegratedFinding, 0, 16)
	remediations := make([]IntegratedRemediation, 0, 12)
	addFinding := func(layer, status, title, detail string) {
		findings = append(findings, IntegratedFinding{Layer: layer, Status: normaliseIntegratedStatus(status), Title: title, Detail: detail})
	}
	addRemediation := func(priority int, action, module, title, detail string) {
		remediations = append(remediations, IntegratedRemediation{Priority: priority, Action: action, Module: module, Title: title, Detail: detail})
	}

	for _, item := range result.Baseline.Findings {
		addFinding(item.Layer, item.Status, item.Title, item.Detail)
	}
	for _, item := range result.DNS.Findings {
		addFinding("dns", item.Status, item.Title, item.Detail)
	}
	for _, item := range result.Ports.Findings {
		addFinding("ports", item.Status, item.Title, item.Detail)
	}
	for _, item := range result.DNS.Remediations {
		addRemediation(2, item.Action, "dns", item.Title, item.Detail)
	}
	for _, item := range result.Ports.Remediations {
		addRemediation(2, item.Action, "ports", item.Title, item.Detail)
	}
	for _, item := range result.Baseline.Remediations {
		addRemediation(1, item.Action, "baseline", item.Title, item.Detail)
	}

	diagnosis := result.HTTP.Diagnosis
	if !diagnosis.Success {
		layer := diagnosis.FailedStage
		if layer == "" {
			layer = "http"
		}
		addFinding(layer, "critical", "目标请求未完成", diagnosis.Message)
		addRemediation(1, "isolate", layer, "先处理失败阶段", integratedFailureRemediation(diagnosis))
	} else {
		addFinding("http", "healthy", "HTTP/TLS 请求完成", fmt.Sprintf("HTTP %d，TTFB %d ms，总耗时 %d ms。", diagnosis.StatusCode, diagnosis.TTFBMS, diagnosis.TotalMS))
		if diagnosis.TTFBMS > 1000 || diagnosis.TotalMS > 2000 {
			addFinding("http", "warning", "服务器响应偏慢", fmt.Sprintf("TTFB %d ms，总耗时 %d ms。", diagnosis.TTFBMS, diagnosis.TotalMS))
			addRemediation(2, "server", "http", "区分网络与源站耗时", "结合路径对照结果；若不同路径均慢且 TCP/TLS 正常，优先排查源站处理和回源链路。")
		}
	}
	if result.HTTP.Traceroute.Success {
		if result.HTTP.Traceroute.Reached {
			addFinding("route", "healthy", "路由追踪已到达目标", fmt.Sprintf("记录 %d 个跳点。", len(result.HTTP.Traceroute.Hops)))
		} else {
			addFinding("route", "neutral", "路由未显示完整终点", "部分网络设备不响应 ICMP，不单独判定为链路中断。")
		}
	} else if result.HTTP.Traceroute.Error != "" {
		addFinding("route", "neutral", "路由追踪证据不可用", result.HTTP.Traceroute.Message)
	}
	if result.Paths.Finding.Title != "" {
		addFinding("paths", result.Paths.Finding.Level, result.Paths.Finding.Title, result.Paths.Finding.Detail)
		if result.Paths.Finding.Level == "warning" || result.Paths.Finding.Level == "critical" {
			addRemediation(2, "route", "paths", "采用路径对照结论验证", result.Paths.Finding.Detail)
		}
	}

	if len(findings) == 0 {
		addFinding("workflow", "critical", "未获得有效诊断证据", "重新运行综合诊断，或使用专项工具逐层确认。")
	}
	sort.SliceStable(findings, func(left, right int) bool {
		return integratedSeverity(findings[left].Status) > integratedSeverity(findings[right].Status)
	})
	sort.SliceStable(remediations, func(left, right int) bool { return remediations[left].Priority < remediations[right].Priority })
	overall := "healthy"
	for _, item := range findings {
		if item.Status == "critical" {
			overall = "critical"
			break
		}
		if item.Status == "warning" {
			overall = "warning"
		}
	}
	return limitIntegratedFindings(findings, 16), limitIntegratedRemediations(remediations, 12), overall
}

func normaliseIntegratedStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "critical", "failed", "error":
		return "critical"
	case "warning", "warn":
		return "warning"
	case "healthy", "good", "ok", "completed":
		return "healthy"
	default:
		return "neutral"
	}
}

func integratedSeverity(status string) int {
	return map[string]int{"critical": 3, "warning": 2, "healthy": 1, "neutral": 0}[status]
}

func integratedFailureRemediation(result DiagResult) string {
	switch result.FailedStage {
	case "dns":
		return "先使用 DNS 诊断确认系统解析与公共解析器差异，再调整本机解析配置。"
	case "connect":
		return "核对目标端口、防火墙与代理出口，并使用端口检测确认拒绝、超时或无路由。"
	case "tls":
		return "检查系统时间、证书链、SNI 与中间人代理，再分别使用直连和系统代理复测。"
	default:
		return "保留本次报告，按本地基线、DNS、端口、TLS、HTTP 的顺序逐层复测。"
	}
}

func limitIntegratedFindings(items []IntegratedFinding, limit int) []IntegratedFinding {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func limitIntegratedRemediations(items []IntegratedRemediation, limit int) []IntegratedRemediation {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func (a *App) emitIntegratedProgress(progress IntegratedProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "integrated:progress", progress)
	}
}

func integratedDomain(targetURL string) string {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
