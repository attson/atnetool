package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxPatrolTargets     = 48
	defaultPatrolWorkers = 4
	maxPatrolWorkers     = 6
	patrolResponseLimit  = 4 << 10
	patrolTimeout        = 45 * time.Second
)

type SiteTarget struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
}

type SitePatrolOptions struct {
	TaskID      string       `json:"task_id"`
	Targets     []SiteTarget `json:"targets"`
	ProxyMode   string       `json:"proxy_mode"`
	ProxyURL    string       `json:"proxy_url,omitempty"`
	Concurrency int          `json:"concurrency"`
}

type SiteProbeResult struct {
	Index  int        `json:"index"`
	Target SiteTarget `json:"target"`
	Result DiagResult `json:"result"`
}

type PatrolFinding struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

type SitePatrolProgress struct {
	TaskID   string          `json:"task_id"`
	Complete int             `json:"complete"`
	Total    int             `json:"total"`
	Probe    SiteProbeResult `json:"probe"`
}

type SitePatrolResult struct {
	SchemaVersion int                `json:"schema_version"`
	TaskID        string             `json:"task_id"`
	Status        string             `json:"status"`
	Message       string             `json:"message"`
	ProxyMode     string             `json:"proxy_mode"`
	ProxyURL      string             `json:"proxy_url,omitempty"`
	StartedAt     string             `json:"started_at"`
	FinishedAt    string             `json:"finished_at"`
	DurationMS    int64              `json:"duration_ms"`
	TotalCount    int                `json:"total_count"`
	SuccessCount  int                `json:"success_count"`
	AverageMS     float64            `json:"average_ms"`
	Results       []SiteProbeResult  `json:"results"`
	Findings      []PatrolFinding    `json:"findings"`
	Environment   NetworkEnvironment `json:"environment"`
}

// RunSitePatrol performs bounded, lightweight HTTP diagnostics against a set
// of existing sites. It never requires or assumes a dedicated test endpoint.
func (a *App) RunSitePatrol(options SitePatrolOptions) SitePatrolResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := SitePatrolResult{
		SchemaVersion: taskSchemaVersion,
		TaskID:        taskID,
		Status:        "running",
		Message:       "站点巡检未完成",
		ProxyMode:     normaliseProxyMode(options.ProxyMode),
		StartedAt:     started.Format(time.RFC3339),
	}
	targets := normalisePatrolTargets(options.Targets)
	result.TotalCount = len(targets)
	if len(targets) == 0 {
		result.Status = "failed"
		result.Message = "请至少选择一个巡检目标"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}

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
	ctx, stopAtTimeout := context.WithTimeout(baseContext, patrolTimeout)
	defer stopAtTimeout()

	type patrolJob struct {
		index  int
		target SiteTarget
	}
	jobs := make(chan patrolJob)
	completed := make(chan SiteProbeResult, len(targets))
	workers := boundedPatrolWorkers(options.Concurrency, len(targets))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				diagnosis := a.runDiagnoseLimited(ctx, DiagOptions{
					TargetURL: job.target.URL,
					ProxyMode: result.ProxyMode,
					ProxyURL:  options.ProxyURL,
				}, patrolResponseLimit, true)
				completed <- SiteProbeResult{Index: job.index, Target: job.target, Result: diagnosis}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index, target := range targets {
			select {
			case jobs <- patrolJob{index: index, target: target}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		group.Wait()
		close(completed)
	}()

	for probe := range completed {
		result.Results = append(result.Results, probe)
		if probe.Result.Success {
			result.SuccessCount++
			result.AverageMS += float64(probe.Result.TotalMS)
		}
		if result.ProxyURL == "" && probe.Result.ProxyURL != "" {
			result.ProxyURL = probe.Result.ProxyURL
		}
		a.emitSitePatrolProgress(SitePatrolProgress{
			TaskID:   taskID,
			Complete: len(result.Results),
			Total:    len(targets),
			Probe:    probe,
		})
	}
	sort.Slice(result.Results, func(left, right int) bool { return result.Results[left].Index < result.Results[right].Index })
	if result.SuccessCount > 0 {
		result.AverageMS /= float64(result.SuccessCount)
	}
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "站点巡检已停止"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Message = "站点巡检超时，已保留完成的检测结果"
	} else {
		result.Status = "completed"
		result.Message = "站点巡检完成"
	}
	result.Findings = analysePatrolFindings(result.Results)
	result.Environment = captureNetworkEnvironment(firstSuccessfulDiagnosis(result.Results, result.ProxyMode, result.ProxyURL))
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	return result
}

func normalisePatrolTargets(input []SiteTarget) []SiteTarget {
	result := make([]SiteTarget, 0, len(input))
	seen := make(map[string]struct{})
	for _, target := range input {
		target.Name = strings.TrimSpace(target.Name)
		target.URL = strings.TrimSpace(target.URL)
		if target.URL == "" {
			continue
		}
		if target.ID == "" {
			target.ID = target.URL
		}
		if target.Name == "" {
			target.Name = target.URL
		}
		key := strings.ToLower(target.URL)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
		if len(result) == maxPatrolTargets {
			break
		}
	}
	return result
}

func boundedPatrolWorkers(requested, targets int) int {
	if requested <= 0 {
		requested = defaultPatrolWorkers
	}
	if requested > maxPatrolWorkers {
		requested = maxPatrolWorkers
	}
	if requested > targets {
		requested = targets
	}
	return requested
}

func analysePatrolFindings(results []SiteProbeResult) []PatrolFinding {
	if len(results) == 0 {
		return nil
	}
	failed := 0
	stageCounts := make(map[string]int)
	slow := 0
	for _, probe := range results {
		if !probe.Result.Success {
			failed++
			stageCounts[probe.Result.FailedStage]++
		}
		if probe.Result.Success && (probe.Result.TTFBMS > 800 || probe.Result.TotalMS > 3000) {
			slow++
		}
	}
	majority := len(results)/2 + 1
	switch {
	case failed == len(results):
		return []PatrolFinding{{Code: "all_unreachable", Level: "critical", Title: "全部站点不可达", Message: "优先检查本地网络、默认路由和代理出口。"}}
	case stageCounts["dns"] >= majority:
		return []PatrolFinding{{Code: "dns_common", Level: "critical", Title: "DNS 存在共性异常", Message: "多个站点解析失败，问题更可能位于本地或上游 DNS。"}}
	case stageCounts["proxy"]+stageCounts["tcp"] >= majority:
		return []PatrolFinding{{Code: "connect_common", Level: "critical", Title: "网络出口存在共性异常", Message: "多个站点无法建立连接，建议对比直连和代理路径。"}}
	case stageCounts["tls"] >= majority:
		return []PatrolFinding{{Code: "tls_common", Level: "critical", Title: "TLS 握手存在共性异常", Message: "检查系统时间、证书环境以及 HTTPS 代理干预。"}}
	case failed > 0:
		return []PatrolFinding{{Code: "partial_failure", Level: "warning", Title: "部分站点异常", Message: "整体网络可用，优先检查失败站点、区域路由或访问策略。"}}
	case slow >= majority:
		return []PatrolFinding{{Code: "latency_common", Level: "warning", Title: "多数站点响应偏慢", Message: "可能存在接入网络、代理出口或跨网链路性能问题。"}}
	default:
		return []PatrolFinding{{Code: "healthy", Level: "good", Title: "常用站点整体正常", Message: "当前未发现跨站点的共性网络异常。"}}
	}
}

func firstSuccessfulDiagnosis(results []SiteProbeResult, proxyMode, proxyURL string) DiagResult {
	for _, probe := range results {
		if probe.Result.Success {
			return probe.Result
		}
	}
	return DiagResult{ProxyMode: proxyMode, ProxyURL: proxyURL}
}

func (a *App) emitSitePatrolProgress(progress SitePatrolProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "patrol:result", progress)
	}
}
