package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	defaultCDNWorkers  = 200
	maxCDNWorkers      = 1000
	defaultCDNSamples  = 4
	maxCDNSamples      = 10
	maxCDNCandidates   = 2000000
	cdnResponseLimit   = 4 << 10
	cdnProbeTimeout    = 1500 * time.Millisecond
	defaultDownloadURL = "https://speed.cloudflare.com/__down?bytes=200000000"
)

var cloudflareIPv4Ranges = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

var cloudflareIPv6Ranges = []string{
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

type CDNOptimizeOptions struct {
	TaskID            string   `json:"task_id"`
	Domain            string   `json:"domain"`
	Path              string   `json:"path"`
	CandidateIPs      []string `json:"candidate_ips"`
	IPRanges          []string `json:"ip_ranges"`
	UsePresetIPv4     bool     `json:"use_preset_ipv4"`
	UsePresetIPv6     bool     `json:"use_preset_ipv6"`
	ExcludeCurrentDNS bool     `json:"exclude_current_dns"`
	TestAllIPv4       bool     `json:"test_all_ipv4"`
	IPv6Samples       int      `json:"ipv6_samples"`
	Mode              string   `json:"mode"`
	Port              int      `json:"port"`
	Concurrency       int      `json:"concurrency"`
	Samples           int      `json:"samples"`
	HTTPStatusCodes   []int    `json:"http_status_codes"`
	Colos             []string `json:"colos"`
	MinDelayMS        int64    `json:"min_delay_ms"`
	MaxDelayMS        int64    `json:"max_delay_ms"`
	MaxLossRate       float64  `json:"max_loss_rate"`
	DownloadEnabled   bool     `json:"download_enabled"`
	DownloadURL       string   `json:"download_url"`
	DownloadCount     int      `json:"download_count"`
	DownloadSeconds   int      `json:"download_seconds"`
	MinSpeedMBps      float64  `json:"min_speed_mbps"`
	ResultLimit       int      `json:"result_limit"`
	Debug             bool     `json:"debug"`
}

type CDNCandidate struct {
	IP     string `json:"ip"`
	Source string `json:"source"`
	Range  string `json:"range,omitempty"`
}

type CDNProbeSample struct {
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
	HTTPProtocol   string `json:"http_protocol,omitempty"`
	CFRay          string `json:"cf_ray,omitempty"`
	CFColo         string `json:"cf_colo,omitempty"`
	CDNProvider    string `json:"cdn_provider,omitempty"`
	LocalAddress   string `json:"local_address,omitempty"`
	RemoteAddress  string `json:"remote_address,omitempty"`
}

type CDNProbeResult struct {
	Index              int              `json:"index"`
	Candidate          CDNCandidate     `json:"candidate"`
	Samples            []CDNProbeSample `json:"samples"`
	SampleCount        int              `json:"sample_count"`
	SuccessCount       int              `json:"success_count"`
	SuccessRate        float64          `json:"success_rate"`
	P50TCPMS           int64            `json:"p50_tcp_ms"`
	P50TLSMS           int64            `json:"p50_tls_ms"`
	P50TTFBMS          int64            `json:"p50_ttfb_ms"`
	P50TotalMS         int64            `json:"p50_total_ms"`
	P95TotalMS         int64            `json:"p95_total_ms"`
	JitterMS           float64          `json:"jitter_ms"`
	StatusCode         int              `json:"status_code"`
	CFRay              string           `json:"cf_ray,omitempty"`
	CFColo             string           `json:"cf_colo,omitempty"`
	VerifiedCloudflare bool             `json:"verified_cloudflare"`
	Rank               int              `json:"rank,omitempty"`
	Recommended        bool             `json:"recommended"`
	Sent               int              `json:"sent"`
	Received           int              `json:"received"`
	LossRate           float64          `json:"loss_rate"`
	AverageLatencyMS   float64          `json:"average_latency_ms"`
	DownloadSpeedMBps  float64          `json:"download_speed_mbps"`
	DownloadBytes      int64            `json:"download_bytes"`
	DownloadDurationMS int64            `json:"download_duration_ms"`
	DownloadTested     bool             `json:"download_tested"`
	CDNProvider        string           `json:"cdn_provider,omitempty"`
	Eligible           bool             `json:"eligible"`
	FilterReason       string           `json:"filter_reason,omitempty"`
	DebugErrors        []string         `json:"debug_errors,omitempty"`
	LocalAddress       string           `json:"local_address,omitempty"`
	RemoteAddress      string           `json:"remote_address,omitempty"`
}

type CDNNodeComparison struct {
	IP                string  `json:"ip,omitempty"`
	Source            string  `json:"source,omitempty"`
	AverageLatencyMS  float64 `json:"average_latency_ms"`
	LossRate          float64 `json:"loss_rate"`
	DownloadSpeedMBps float64 `json:"download_speed_mbps"`
	DownloadTested    bool    `json:"download_tested"`
	CFColo            string  `json:"cf_colo,omitempty"`
	CDNProvider       string  `json:"cdn_provider,omitempty"`
}

type CDNOptimizeComparison struct {
	Comparable                bool              `json:"comparable"`
	SameIP                    bool              `json:"same_ip"`
	Current                   CDNNodeComparison `json:"current"`
	Recommended               CDNNodeComparison `json:"recommended"`
	LatencyDeltaMS            float64           `json:"latency_delta_ms"`
	LatencyImprovementPercent float64           `json:"latency_improvement_percent"`
	LossReductionPoints       float64           `json:"loss_reduction_points"`
	SpeedComparable           bool              `json:"speed_comparable"`
	SpeedDeltaMBps            float64           `json:"speed_delta_mbps"`
	SpeedImprovementPercent   float64           `json:"speed_improvement_percent"`
	SpeedMultiplier           float64           `json:"speed_multiplier"`
}

type CDNPathEvidence struct {
	Mode                   string   `json:"mode"`
	SystemProxyConfigured  bool     `json:"system_proxy_configured"`
	SystemProxyURL         string   `json:"system_proxy_url,omitempty"`
	TunnelDetected         bool     `json:"tunnel_detected"`
	ActiveTunnelInterfaces []string `json:"active_tunnel_interfaces,omitempty"`
	HostsApplicable        bool     `json:"hosts_applicable"`
	Message                string   `json:"message"`
}

type CDNTargetVerification struct {
	Status            string   `json:"status"`
	Verified          bool     `json:"verified"`
	DNSMatched        bool     `json:"dns_matched"`
	HeaderMatched     bool     `json:"header_matched"`
	MatchedIPs        []string `json:"matched_ips,omitempty"`
	HTTPStatusCode    int      `json:"http_status_code,omitempty"`
	CFRay             string   `json:"cf_ray,omitempty"`
	CDNProvider       string   `json:"cdn_provider,omitempty"`
	Message           string   `json:"message"`
	VerificationError string   `json:"verification_error,omitempty"`
}

type CDNGovernanceAdvice struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type CDNOptimizeProgress struct {
	TaskID   string         `json:"task_id"`
	Stage    string         `json:"stage"`
	Message  string         `json:"message,omitempty"`
	Complete int            `json:"complete"`
	Total    int            `json:"total"`
	Probe    CDNProbeResult `json:"probe"`
}

type CDNOptimizeResult struct {
	SchemaVersion       int                   `json:"schema_version"`
	TaskID              string                `json:"task_id"`
	Status              string                `json:"status"`
	Message             string                `json:"message"`
	Domain              string                `json:"domain"`
	Path                string                `json:"path"`
	TargetURL           string                `json:"target_url"`
	StartedAt           string                `json:"started_at"`
	FinishedAt          string                `json:"finished_at"`
	DurationMS          int64                 `json:"duration_ms"`
	DNSIPs              []string              `json:"dns_ips,omitempty"`
	DNSError            string                `json:"dns_error,omitempty"`
	CandidateCount      int                   `json:"candidate_count"`
	CompletedCount      int                   `json:"completed_count"`
	VerifiedCount       int                   `json:"verified_count"`
	ResponsiveCount     int                   `json:"responsive_count"`
	EligibleCount       int                   `json:"eligible_count"`
	FilteredCount       int                   `json:"filtered_count"`
	DownloadTestedCount int                   `json:"download_tested_count"`
	FailedCount         int                   `json:"failed_count"`
	Mode                string                `json:"mode"`
	Port                int                   `json:"port"`
	DownloadEnabled     bool                  `json:"download_enabled"`
	DownloadURL         string                `json:"download_url,omitempty"`
	CandidateRanges     []string              `json:"candidate_ranges,omitempty"`
	Truncated           bool                  `json:"truncated"`
	Warnings            []string              `json:"warnings,omitempty"`
	BestIP              string                `json:"best_ip,omitempty"`
	BaselineIP          string                `json:"baseline_ip,omitempty"`
	ImprovementPercent  float64               `json:"improvement_percent"`
	P95Improvement      float64               `json:"p95_improvement_percent"`
	Comparison          CDNOptimizeComparison `json:"comparison"`
	PathEvidence        CDNPathEvidence       `json:"path_evidence"`
	ValidationScope     string                `json:"validation_scope"`
	TargetValidated     bool                  `json:"target_validated"`
	TargetValidation    string                `json:"target_validation"`
	TargetVerification  CDNTargetVerification `json:"target_verification"`
	Confidence          string                `json:"confidence"`
	Finding             PatrolFinding         `json:"finding"`
	Governance          []CDNGovernanceAdvice `json:"governance"`
	Results             []CDNProbeResult      `json:"results"`
	Environment         NetworkEnvironment    `json:"environment"`
}

// RunCDNOptimize implements the CloudflareSpeedTest-style two-stage pipeline:
// generate candidates from IP/CIDR ranges, filter them by repeated TCP/HTTP
// latency probes, then optionally measure download throughput for the best
// candidates. All sockets are direct and never inherit the system proxy.
func (a *App) RunCDNOptimize(options CDNOptimizeOptions) CDNOptimizeResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := CDNOptimizeResult{
		SchemaVersion: taskSchemaVersion,
		TaskID:        taskID,
		Status:        "running",
		Message:       "Cloudflare IP 优选未完成",
		StartedAt:     started.Format(time.RFC3339),
	}

	domain, testPath, targetURL, err := normaliseCDNTarget(options.Domain, options.Path)
	if err != nil {
		result.Status = "failed"
		result.Message = "域名或测试路径配置有误"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	result.Domain = domain
	result.Path = testPath
	result.TargetURL = targetURL
	settings, err := normaliseCDNOptimizeOptions(options)
	if err != nil {
		result.Status = "failed"
		result.Message = err.Error()
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	result.Mode = settings.Mode
	result.Port = settings.Port
	result.DownloadEnabled = settings.DownloadEnabled
	result.DownloadURL = settings.DownloadURL
	if settings.Mode == "http" {
		result.ValidationScope = "target_domain"
		result.TargetValidation = "HTTPing 使用目标域名的 Host、SNI、路径与状态码验证候选节点"
	} else {
		result.ValidationScope = "network_layer"
		result.TargetValidation = "TCPing 仅评价网络层可达性与质量，未验证目标域名的业务配置"
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

	a.emitCDNOptimizeProgress(CDNOptimizeProgress{TaskID: taskID, Stage: "discovery", Message: "正在生成候选地址"})
	lookupContext, stopLookup := context.WithTimeout(baseContext, 5*time.Second)
	dnsIPs, lookupErr := lookupPublicIPs(lookupContext, domain)
	stopLookup()
	result.DNSIPs = dnsIPs
	if lookupErr != nil {
		result.DNSError = lookupErr.Error()
	}
	a.emitCDNOptimizeProgress(CDNOptimizeProgress{TaskID: taskID, Stage: "verification", Message: "正在验证目标域名的 Cloudflare 代理状态"})
	verificationContext, stopVerification := context.WithTimeout(baseContext, 6*time.Second)
	result.TargetVerification = verifyCDNTarget(verificationContext, targetURL, dnsIPs)
	stopVerification()
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "Cloudflare IP 优选已停止"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		result.DurationMS = durationMS(time.Since(started))
		return result
	}
	if !result.TargetVerification.Verified {
		result.Status = "failed"
		result.Message = result.TargetVerification.Message
		result.FinishedAt = time.Now().Format(time.RFC3339)
		result.DurationMS = durationMS(time.Since(started))
		level := "critical"
		title := "目标域名未经过 Cloudflare 代理"
		if result.TargetVerification.Status == "inconclusive" {
			level = "warning"
			title = "无法确认 Cloudflare 代理状态"
		}
		result.Finding = PatrolFinding{Code: "cloudflare_verification_failed", Level: level, Title: title, Message: result.TargetVerification.Message}
		result.Governance = []CDNGovernanceAdvice{{Action: "verify_cloudflare", Title: "检查 Cloudflare 代理状态", Detail: "确认域名 DNS 记录已开启代理，并检查 A/AAAA、CNAME、证书和访问规则后重新测试。"}}
		result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeDirect})
		result.PathEvidence = buildCDNPathEvidence(domain, result.Environment)
		return result
	}
	candidates, ranges, warnings, truncated := buildCDNCandidates(dnsIPs, options, settings)
	result.CandidateRanges = ranges
	result.Warnings = warnings
	result.Truncated = truncated
	result.CandidateCount = len(candidates)
	if len(candidates) == 0 {
		result.Status = "failed"
		result.Message = "没有生成可测试的公网候选 IP，请启用预置网段或填写 IP / CIDR"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeDirect})
		result.PathEvidence = buildCDNPathEvidence(domain, result.Environment)
		return result
	}
	a.emitCDNOptimizeProgress(CDNOptimizeProgress{
		TaskID: taskID, Stage: "latency", Message: "正在测试延迟与丢包", Total: len(candidates),
	})

	type candidateJob struct {
		index     int
		candidate CDNCandidate
	}
	jobs := make(chan candidateJob)
	workers := boundedCDNWorkers(options.Concurrency, len(candidates))
	resultBuffer := workers * 2
	if resultBuffer > 1024 {
		resultBuffer = 1024
	}
	completed := make(chan CDNProbeResult, resultBuffer)
	samples := boundedCDNSamples(options.Samples)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for job := range jobs {
				if baseContext.Err() != nil {
					return
				}
				probe := runCDNLatencyCandidate(baseContext, job.index, job.candidate, targetURL, domain, samples, settings)
				completed <- probe
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index, candidate := range candidates {
			select {
			case jobs <- candidateJob{index: index, candidate: candidate}:
			case <-baseContext.Done():
				return
			}
		}
	}()
	go func() {
		group.Wait()
		close(completed)
	}()

	for probe := range completed {
		result.CompletedCount++
		if probe.Received > 0 {
			result.Results = append(result.Results, probe)
			result.ResponsiveCount++
		} else {
			result.FailedCount++
		}
		if probe.VerifiedCloudflare {
			result.VerifiedCount++
		}
		if probe.Received > 0 || result.CompletedCount%25 == 0 || result.CompletedCount == len(candidates) {
			a.emitCDNOptimizeProgress(CDNOptimizeProgress{
				TaskID: taskID, Stage: "latency", Complete: result.CompletedCount, Total: len(candidates), Probe: probe,
			})
		}
	}
	filterCDNLatencyResults(&result, settings)
	if baseContext.Err() == nil && settings.DownloadEnabled && result.EligibleCount > 0 {
		runCDNDownloadStage(baseContext, a, &result, settings)
	}
	finaliseCDNRanking(&result)
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "Cloudflare IP 优选已停止"
	} else {
		result.Status = "completed"
		result.Message = "Cloudflare IP 优选完成"
	}
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	result.Environment = captureNetworkEnvironment(DiagResult{
		ProxyMode: proxyModeDirect,
		ClientIP:  firstCDNLocalIP(result.Results),
	})
	result.PathEvidence = buildCDNPathEvidence(domain, result.Environment)
	return result
}

type cdnOptimizeSettings struct {
	Mode             string
	Port             int
	HTTPStatusCodes  map[int]struct{}
	Colos            map[string]struct{}
	MinDelayMS       int64
	MaxDelayMS       int64
	MaxLossRate      float64
	DownloadEnabled  bool
	DownloadURL      string
	DownloadCount    int
	DownloadDuration time.Duration
	MinSpeedMBps     float64
	ResultLimit      int
	IPv6Samples      int
	Debug            bool
}

func normaliseCDNOptimizeOptions(options CDNOptimizeOptions) (cdnOptimizeSettings, error) {
	settings := cdnOptimizeSettings{
		Mode: options.Mode, Port: options.Port, MinDelayMS: options.MinDelayMS,
		MaxDelayMS: options.MaxDelayMS, MaxLossRate: options.MaxLossRate,
		DownloadEnabled: options.DownloadEnabled, DownloadURL: strings.TrimSpace(options.DownloadURL),
		DownloadCount: options.DownloadCount, MinSpeedMBps: options.MinSpeedMBps,
		ResultLimit: options.ResultLimit, IPv6Samples: options.IPv6Samples, Debug: options.Debug,
	}
	if settings.Mode != "http" {
		settings.Mode = "tcp"
	}
	if settings.Port <= 0 || settings.Port > 65535 {
		settings.Port = 443
	}
	if settings.MaxDelayMS <= 0 {
		settings.MaxDelayMS = 9999
	}
	if settings.MinDelayMS < 0 || settings.MinDelayMS > settings.MaxDelayMS {
		return settings, fmt.Errorf("延迟筛选范围无效")
	}
	if settings.MaxLossRate < 0 || settings.MaxLossRate > 1 {
		settings.MaxLossRate = 1
	}
	if settings.DownloadCount <= 0 {
		settings.DownloadCount = 10
	}
	if settings.DownloadCount > 100 {
		settings.DownloadCount = 100
	}
	seconds := options.DownloadSeconds
	if seconds <= 0 {
		seconds = 10
	}
	if seconds > 60 {
		seconds = 60
	}
	settings.DownloadDuration = time.Duration(seconds) * time.Second
	if settings.DownloadURL == "" {
		settings.DownloadURL = defaultDownloadURL
	}
	if settings.DownloadEnabled {
		parsed, err := url.Parse(settings.DownloadURL)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return settings, fmt.Errorf("下载测速地址必须是有效的 HTTP 或 HTTPS URL")
		}
	}
	if settings.ResultLimit <= 0 {
		settings.ResultLimit = 10
	}
	if settings.ResultLimit > 500 {
		settings.ResultLimit = 500
	}
	if settings.IPv6Samples <= 0 {
		settings.IPv6Samples = 16
	}
	if settings.IPv6Samples > 256 {
		settings.IPv6Samples = 256
	}
	settings.HTTPStatusCodes = make(map[int]struct{})
	for _, code := range options.HTTPStatusCodes {
		if code >= 100 && code <= 599 {
			settings.HTTPStatusCodes[code] = struct{}{}
		}
	}
	if len(settings.HTTPStatusCodes) == 0 {
		settings.HTTPStatusCodes[200] = struct{}{}
		settings.HTTPStatusCodes[301] = struct{}{}
		settings.HTTPStatusCodes[302] = struct{}{}
	}
	settings.Colos = make(map[string]struct{})
	if settings.Mode == "http" {
		for _, colo := range options.Colos {
			colo = strings.ToUpper(strings.TrimSpace(colo))
			if colo != "" {
				settings.Colos[colo] = struct{}{}
			}
		}
	}
	return settings, nil
}

func verifyCDNTarget(ctx context.Context, targetURL string, dnsIPs []string) CDNTargetVerification {
	verification := CDNTargetVerification{Status: "checking", Message: "正在验证 Cloudflare 代理状态"}
	for _, rawIP := range dnsIPs {
		if isCloudflareEdgeIP(net.ParseIP(rawIP)) {
			verification.DNSMatched = true
			verification.MatchedIPs = appendUnique(verification.MatchedIPs, rawIP)
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err == nil {
		request.Header.Set("User-Agent", "ATNetool/1.0")
		request.Header.Set("Cache-Control", "no-cache")
		request.Header.Set("Range", "bytes=0-0")
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		transport.DisableKeepAlives = true
		defer transport.CloseIdleConnections()
		client := &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			verification.VerificationError = requestErr.Error()
		} else {
			defer response.Body.Close()
			verification.HTTPStatusCode = response.StatusCode
			verification.CFRay = response.Header.Get("CF-Ray")
			verification.CDNProvider, _ = cdnProviderAndColo(response.Header)
			verification.HeaderMatched = verification.CFRay != "" || verification.CDNProvider == "Cloudflare"
		}
	} else {
		verification.VerificationError = err.Error()
	}

	verification.Verified = verification.DNSMatched || verification.HeaderMatched
	switch {
	case verification.DNSMatched && verification.HeaderMatched:
		verification.Status = "verified"
		verification.Message = "DNS 地址与响应头均确认目标域名已通过 Cloudflare 代理"
	case verification.DNSMatched:
		verification.Status = "verified"
		verification.Message = "目标解析地址位于 Cloudflare 官方网段，已确认代理状态"
	case verification.HeaderMatched:
		verification.Status = "verified"
		verification.Message = "目标响应包含 Cloudflare 边缘标识，已确认代理状态"
	case verification.VerificationError != "":
		verification.Status = "inconclusive"
		verification.Message = "目标请求失败且解析地址不在 Cloudflare 官方网段，暂时无法确认代理状态"
	default:
		verification.Status = "not_cloudflare"
		verification.Message = "目标解析地址和响应头均未发现 Cloudflare 代理证据"
	}
	return verification
}

func isCloudflareEdgeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, ranges := range [][]string{cloudflareIPv4Ranges, cloudflareIPv6Ranges} {
		for _, rawRange := range ranges {
			_, network, err := net.ParseCIDR(rawRange)
			if err == nil && network.Contains(ip) {
				return true
			}
		}
	}
	return false
}

func buildCDNCandidates(dnsIPs []string, options CDNOptimizeOptions, settings cdnOptimizeSettings) ([]CDNCandidate, []string, []string, bool) {
	candidates := make([]CDNCandidate, 0, 8192)
	seen := make(map[string]struct{})
	warnings := make([]string, 0)
	ranges := make([]string, 0)
	truncated := false
	appendCandidate := func(ip net.IP, source, sourceRange string) bool {
		if !isPublicCandidateIP(ip) {
			return true
		}
		value := ip.String()
		if _, exists := seen[value]; exists {
			return true
		}
		if len(candidates) >= maxCDNCandidates {
			truncated = true
			return false
		}
		seen[value] = struct{}{}
		candidates = append(candidates, CDNCandidate{IP: value, Source: source, Range: sourceRange})
		return true
	}
	if !options.ExcludeCurrentDNS {
		for _, value := range dnsIPs {
			appendCandidate(net.ParseIP(value), "dns", "")
		}
	}
	specs := make([]struct{ value, source string }, 0)
	if options.UsePresetIPv4 {
		for _, value := range cloudflareIPv4Ranges {
			specs = append(specs, struct{ value, source string }{value, "cloudflare_ipv4"})
		}
	}
	if options.UsePresetIPv6 {
		for _, value := range cloudflareIPv6Ranges {
			specs = append(specs, struct{ value, source string }{value, "cloudflare_ipv6"})
		}
	}
	for _, raw := range append(append([]string(nil), options.IPRanges...), options.CandidateIPs...) {
		for _, value := range splitCDNCandidateSpecs(raw) {
			specs = append(specs, struct{ value, source string }{value, "custom"})
		}
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for _, spec := range specs {
		ip, network, err := parseCDNNetwork(spec.value)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("已忽略无效地址或网段：%s", spec.value))
			continue
		}
		if !isPublicCandidateIP(ip) {
			warnings = append(warnings, fmt.Sprintf("已忽略非公网地址：%s", spec.value))
			continue
		}
		ranges = appendUnique(ranges, spec.value)
		var values []net.IP
		if ip.To4() != nil {
			values, truncated = expandCDNIPv4Range(network, options.TestAllIPv4, maxCDNCandidates-len(candidates), rng)
		} else {
			values = sampleCDNIPv6Range(network, settings.IPv6Samples, rng)
		}
		for _, value := range values {
			if !appendCandidate(value, spec.source, spec.value) {
				break
			}
		}
		if len(candidates) >= maxCDNCandidates {
			truncated = true
			break
		}
	}
	if truncated {
		warnings = append(warnings, fmt.Sprintf("候选数量已达到桌面端安全上限 %d", maxCDNCandidates))
	}
	return candidates, ranges, warnings, truncated
}

func splitCDNCandidateSpecs(raw string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		if index := strings.IndexByte(line, '#'); index >= 0 {
			line = line[:index]
		}
		for _, field := range strings.FieldsFunc(line, func(character rune) bool {
			return character == ',' || character == ';' || character == '，' || character == '；' || character == '\t' || character == ' '
		}) {
			if value := strings.TrimSpace(field); value != "" {
				result = append(result, value)
			}
		}
	}
	return result
}

func parseCDNNetwork(raw string) (net.IP, *net.IPNet, error) {
	value := strings.TrimSpace(strings.Trim(raw, "[]"))
	if !strings.Contains(value, "/") {
		ip := net.ParseIP(value)
		if ip == nil {
			return nil, nil, fmt.Errorf("invalid IP")
		}
		if ip.To4() != nil {
			value = ip.String() + "/32"
		} else {
			value = ip.String() + "/128"
		}
	}
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		return nil, nil, err
	}
	return ip, network, nil
}

func expandCDNIPv4Range(network *net.IPNet, all bool, remaining int, rng *rand.Rand) ([]net.IP, bool) {
	if remaining <= 0 {
		return nil, true
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil, false
	}
	base := ipv4ToUint32(network.IP)
	hostBits := uint(32 - ones)
	count := uint64(1) << hostBits
	if all {
		limit := count
		truncated := false
		if limit > uint64(remaining) {
			limit = uint64(remaining)
			truncated = true
		}
		result := make([]net.IP, 0, int(limit))
		for offset := uint64(0); offset < limit; offset++ {
			result = append(result, uint32ToIPv4(base+uint32(offset)))
		}
		return result, truncated
	}
	blocks := uint64(1)
	if ones < 24 {
		blocks = uint64(1) << uint(24-ones)
	}
	if blocks > uint64(remaining) {
		blocks = uint64(remaining)
	}
	result := make([]net.IP, 0, int(blocks))
	for block := uint64(0); block < blocks; block++ {
		blockBase := base
		blockSize := count
		if ones < 24 {
			blockBase += uint32(block << 8)
			blockSize = 256
		}
		offset := uint64(0)
		if blockSize > 1 {
			offset = uint64(rng.Int63n(int64(blockSize)))
		}
		result = append(result, uint32ToIPv4(blockBase+uint32(offset)))
	}
	return result, uint64(1)<<uint(maxInt(0, 24-ones)) > uint64(remaining)
}

func sampleCDNIPv6Range(network *net.IPNet, count int, rng *rand.Rand) []net.IP {
	ones, bits := network.Mask.Size()
	if bits != 128 {
		return nil
	}
	if ones == 128 {
		return []net.IP{append(net.IP(nil), network.IP...)}
	}
	result := make([]net.IP, 0, count)
	seen := make(map[string]struct{})
	for len(result) < count {
		value := append(net.IP(nil), network.IP.To16()...)
		for bit := ones; bit < 128; bit++ {
			if rng.Intn(2) == 1 {
				value[bit/8] |= 1 << uint(7-bit%8)
			}
		}
		key := value.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func ipv4ToUint32(ip net.IP) uint32 {
	value := ip.To4()
	return uint32(value[0])<<24 | uint32(value[1])<<16 | uint32(value[2])<<8 | uint32(value[3])
}

func uint32ToIPv4(value uint32) net.IP {
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func runCDNLatencyCandidate(parent context.Context, index int, candidate CDNCandidate, targetURL, domain string, samples int, settings cdnOptimizeSettings) CDNProbeResult {
	result := CDNProbeResult{Index: index, Candidate: candidate, SampleCount: samples, Sent: samples}
	for sequence := 1; sequence <= samples; sequence++ {
		if parent.Err() != nil {
			break
		}
		var sample CDNProbeSample
		if settings.Mode == "http" {
			sample = runCDNHTTPing(parent, targetURL, domain, candidate.IP, sequence, settings)
		} else {
			sample = runCDNTCPing(parent, candidate.IP, sequence, settings.Port)
		}
		result.Samples = append(result.Samples, sample)
		if settings.Debug && !sample.Success && sample.Error != "" {
			result.DebugErrors = append(result.DebugErrors, sample.Error)
		}
	}
	finaliseCDNProbe(&result)
	result.Sent = result.SampleCount
	result.Received = result.SuccessCount
	if result.Sent > 0 {
		result.LossRate = float64(result.Sent-result.Received) / float64(result.Sent)
	}
	var total int64
	for _, sample := range result.Samples {
		if sample.Success {
			total += sample.TotalMS
		}
	}
	if result.Received > 0 {
		result.AverageLatencyMS = float64(total) / float64(result.Received)
	}
	for _, sample := range result.Samples {
		if sample.Success {
			result.LocalAddress = sample.LocalAddress
			result.RemoteAddress = sample.RemoteAddress
			break
		}
	}
	if !settings.Debug {
		result.Samples = nil
	}
	return result
}

func runCDNTCPing(parent context.Context, candidateIP string, sequence, port int) CDNProbeSample {
	sample := CDNProbeSample{Sequence: sequence, Message: "TCP 连接失败", FailedStage: "tcp"}
	ctx, cancel := context.WithTimeout(parent, cdnProbeTimeout)
	defer cancel()
	started := time.Now()
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(candidateIP, strconv.Itoa(port)))
	sample.TotalMS = durationMS(time.Since(started))
	sample.TCPConnectMS = sample.TotalMS
	if err != nil {
		sample.Error = err.Error()
		return sample
	}
	defer connection.Close()
	sample.Success = true
	sample.Message = "TCP 连接正常"
	sample.FailedStage = ""
	sample.LocalAddress = addressString(connection.LocalAddr())
	sample.RemoteAddress = addressString(connection.RemoteAddr())
	return sample
}

func runCDNHTTPing(parent context.Context, targetURL, domain, candidateIP string, sequence int, settings cdnOptimizeSettings) CDNProbeSample {
	sample := runCDNProbeWithSettings(parent, targetURL, domain, candidateIP, sequence, settings.Port, cdnProbeTimeout, false)
	if sample.Success {
		if _, allowed := settings.HTTPStatusCodes[sample.StatusCode]; !allowed {
			sample.Success = false
			sample.FailedStage = "status"
			sample.Message = fmt.Sprintf("HTTP 状态码 %d 不符合筛选条件", sample.StatusCode)
		}
	}
	return sample
}

func filterCDNLatencyResults(result *CDNOptimizeResult, settings cdnOptimizeSettings) {
	sort.SliceStable(result.Results, func(left, right int) bool {
		if result.Results[left].LossRate != result.Results[right].LossRate {
			return result.Results[left].LossRate < result.Results[right].LossRate
		}
		if result.Results[left].AverageLatencyMS != result.Results[right].AverageLatencyMS {
			return result.Results[left].AverageLatencyMS < result.Results[right].AverageLatencyMS
		}
		return result.Results[left].Candidate.IP < result.Results[right].Candidate.IP
	})
	for index := range result.Results {
		probe := &result.Results[index]
		reason := ""
		switch {
		case probe.LossRate > settings.MaxLossRate:
			reason = "丢包率超过上限"
		case probe.AverageLatencyMS < float64(settings.MinDelayMS):
			reason = "平均延迟低于筛选下限"
		case probe.AverageLatencyMS > float64(settings.MaxDelayMS):
			reason = "平均延迟超过筛选上限"
		case len(settings.Colos) > 0:
			if _, ok := settings.Colos[strings.ToUpper(probe.CFColo)]; !ok {
				reason = "节点地区不符合筛选条件"
			}
		}
		probe.FilterReason = reason
		probe.Eligible = reason == ""
		if probe.Eligible {
			result.EligibleCount++
		} else {
			result.FilteredCount++
		}
	}
}

func runCDNDownloadStage(ctx context.Context, app *App, result *CDNOptimizeResult, settings cdnOptimizeSettings) {
	eligible := make([]*CDNProbeResult, 0, result.EligibleCount)
	for index := range result.Results {
		if result.Results[index].Eligible {
			eligible = append(eligible, &result.Results[index])
		}
	}
	queue := make([]*CDNProbeResult, 0, len(eligible)+1)
	baseline := bestCDNDNSBaseline(result.Results)
	if baseline != nil {
		queue = append(queue, baseline)
	}
	for _, probe := range eligible {
		if baseline == nil || probe != baseline {
			queue = append(queue, probe)
		}
	}
	total := settings.DownloadCount
	if settings.MinSpeedMBps > 0 || len(queue) < total {
		total = len(queue)
	}
	app.emitCDNOptimizeProgress(CDNOptimizeProgress{TaskID: result.TaskID, Stage: "download", Message: "正在测试下载速度", Total: total})
	accepted := 0
	for index, probe := range queue {
		if ctx.Err() != nil || (settings.MinSpeedMBps <= 0 && index >= settings.DownloadCount) || accepted >= settings.DownloadCount {
			break
		}
		speed, bytesRead, elapsed, provider, colo, status, err := runCDNDownloadProbe(ctx, probe.Candidate.IP, settings)
		probe.DownloadTested = true
		probe.DownloadSpeedMBps = speed
		probe.DownloadBytes = bytesRead
		probe.DownloadDurationMS = durationMS(elapsed)
		if provider != "" {
			probe.CDNProvider = provider
		}
		if colo != "" {
			probe.CFColo = colo
		}
		if status > 0 {
			probe.StatusCode = status
		}
		if err != nil && settings.Debug {
			probe.DebugErrors = append(probe.DebugErrors, err.Error())
		}
		result.DownloadTestedCount++
		if speed >= settings.MinSpeedMBps {
			accepted++
		} else {
			if probe.Eligible {
				probe.Eligible = false
				probe.FilterReason = "下载速度低于筛选下限"
				result.EligibleCount--
				result.FilteredCount++
			}
		}
		app.emitCDNOptimizeProgress(CDNOptimizeProgress{
			TaskID: result.TaskID, Stage: "download", Complete: result.DownloadTestedCount, Total: total, Probe: *probe,
		})
	}
}

func bestCDNDNSBaseline(results []CDNProbeResult) *CDNProbeResult {
	var baseline *CDNProbeResult
	for index := range results {
		probe := &results[index]
		if probe.Candidate.Source != "dns" || (probe.Received == 0 && probe.SuccessCount == 0) {
			continue
		}
		if baseline == nil || cdnBaselineLess(*probe, *baseline) {
			baseline = probe
		}
	}
	return baseline
}

func cdnBaselineLess(left, right CDNProbeResult) bool {
	if left.AverageLatencyMS == 0 && right.AverageLatencyMS == 0 {
		return cdnProbeLess(left, right)
	}
	if left.LossRate != right.LossRate {
		return left.LossRate < right.LossRate
	}
	if left.AverageLatencyMS != right.AverageLatencyMS {
		return left.AverageLatencyMS < right.AverageLatencyMS
	}
	return left.Candidate.IP < right.Candidate.IP
}

func runCDNDownloadProbe(parent context.Context, candidateIP string, settings cdnOptimizeSettings) (float64, int64, time.Duration, string, string, int, error) {
	ctx, cancel := context.WithTimeout(parent, settings.DownloadDuration)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, settings.DownloadURL, nil)
	if err != nil {
		return 0, 0, 0, "", "", 0, err
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = true
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		_, port, splitErr := net.SplitHostPort(address)
		if splitErr != nil || settings.Port != 443 {
			port = strconv.Itoa(settings.Port)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(candidateIP, port))
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, time.Since(started), "", "", 0, err
	}
	defer response.Body.Close()
	provider, colo := cdnProviderAndColo(response.Header)
	if response.StatusCode != http.StatusOK {
		return 0, 0, time.Since(started), provider, colo, response.StatusCode, fmt.Errorf("下载测速返回 HTTP %d", response.StatusCode)
	}
	readStarted := time.Now()
	bytesRead, readErr := io.Copy(io.Discard, response.Body)
	elapsed := time.Since(readStarted)
	if elapsed <= 0 {
		elapsed = time.Millisecond
	}
	speed := float64(bytesRead) / 1024 / 1024 / elapsed.Seconds()
	if readErr != nil && ctx.Err() == nil {
		return speed, bytesRead, elapsed, provider, colo, response.StatusCode, readErr
	}
	return speed, bytesRead, elapsed, provider, colo, response.StatusCode, nil
}

func cdnProviderAndColo(header http.Header) (string, string) {
	server := header.Get("Server")
	if strings.EqualFold(server, "cloudflare") || header.Get("CF-Ray") != "" {
		return "Cloudflare", cloudflareColo(header.Get("CF-Ray"))
	}
	if value := header.Get("X-Amz-Cf-Pop"); value != "" {
		return "AWS CloudFront", firstUpperCode(value, 3)
	}
	if value := header.Get("X-Served-By"); value != "" {
		parts := strings.Split(value, "-")
		return "Fastly", strings.ToUpper(parts[len(parts)-1])
	}
	if value := header.Get("X-ID-FE"); value != "" {
		return "Gcore", firstUpperCode(value, 2)
	}
	if value := header.Get("X-77-Pop"); value != "" {
		return "CDN77", firstUpperCode(value, 2)
	}
	if strings.Contains(strings.ToLower(server), "bunnycdn") {
		return "Bunny CDN", firstUpperCode(server, 2)
	}
	return "", ""
}

func firstUpperCode(value string, length int) string {
	upper := strings.ToUpper(value)
	for start := 0; start+length <= len(upper); start++ {
		part := upper[start : start+length]
		valid := true
		for _, character := range part {
			if character < 'A' || character > 'Z' {
				valid = false
				break
			}
		}
		if valid {
			return part
		}
	}
	return ""
}

func runCDNCandidate(parent context.Context, index int, candidate CDNCandidate, targetURL, domain string, sampleCount int) CDNProbeResult {
	result := CDNProbeResult{Index: index, Candidate: candidate, SampleCount: sampleCount}
	for sequence := 1; sequence <= sampleCount; sequence++ {
		if parent.Err() != nil {
			break
		}
		sample := runCDNProbe(parent, targetURL, domain, candidate.IP, sequence)
		result.Samples = append(result.Samples, sample)
	}
	finaliseCDNProbe(&result)
	return result
}

func runCDNProbe(parent context.Context, targetURL, domain, candidateIP string, sequence int) CDNProbeSample {
	return runCDNProbeWithSettings(parent, targetURL, domain, candidateIP, sequence, 0, cdnProbeTimeout, true)
}

func runCDNProbeWithSettings(parent context.Context, targetURL, domain, candidateIP string, sequence, port int, timeout time.Duration, readBody bool) CDNProbeSample {
	sample := CDNProbeSample{Sequence: sequence, Message: "候选节点未响应"}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	var mu sync.Mutex
	var connectStarted, tlsStarted, requestWritten time.Time
	var dnsErr, connectErr, tlsErr error
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			mu.Lock()
			connectStarted = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			defer mu.Unlock()
			connectErr = err
			if err == nil && !connectStarted.IsZero() {
				sample.TCPConnectMS = durationMS(time.Since(connectStarted))
			}
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			tlsStarted = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mu.Lock()
			defer mu.Unlock()
			tlsErr = err
			if !tlsStarted.IsZero() {
				sample.TLSHandshakeMS = durationMS(time.Since(tlsStarted))
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			defer mu.Unlock()
			sample.LocalAddress = addressString(info.Conn.LocalAddr())
			sample.RemoteAddress = addressString(info.Conn.RemoteAddr())
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			mu.Lock()
			requestWritten = time.Now()
			mu.Unlock()
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			defer mu.Unlock()
			if !requestWritten.IsZero() {
				sample.TTFBMS = durationMS(time.Since(requestWritten))
			}
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mu.Lock()
			dnsErr = info.Err
			mu.Unlock()
		},
	}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, targetURL, nil)
	if err != nil {
		sample.FailedStage = "request"
		sample.Error = err.Error()
		return sample
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", cdnResponseLimit-1))

	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = true
	transport.TLSClientConfig = &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		_, targetPort, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			targetPort = "443"
		}
		if port > 0 {
			targetPort = strconv.Itoa(port)
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(candidateIP, targetPort))
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	started := time.Now()
	response, err := client.Do(request)
	sample.TotalMS = durationMS(time.Since(started))
	if err != nil {
		mu.Lock()
		traceDNSError, traceConnectError, traceTLSError := dnsErr, connectErr, tlsErr
		mu.Unlock()
		sample.FailedStage, sample.Message = diagnoseFailure(traceDNSError, traceConnectError, traceTLSError)
		sample.Error = err.Error()
		return sample
	}
	defer response.Body.Close()
	sample.StatusCode = response.StatusCode
	sample.HTTPProtocol = response.Proto
	sample.CFRay = response.Header.Get("CF-Ray")
	sample.CDNProvider, sample.CFColo = cdnProviderAndColo(response.Header)
	var readErr error
	if readBody {
		_, readErr = io.Copy(io.Discard, io.LimitReader(response.Body, cdnResponseLimit))
	}
	sample.TotalMS = durationMS(time.Since(started))
	if readErr != nil {
		sample.FailedStage = "response"
		sample.Message = "候选节点响应传输中断"
		sample.Error = readErr.Error()
		return sample
	}
	if response.StatusCode >= http.StatusInternalServerError {
		sample.FailedStage = "response"
		sample.Message = "候选节点返回服务错误"
		return sample
	}
	sample.Success = true
	sample.Message = "候选节点响应正常"
	return sample
}

func finaliseCDNProbe(result *CDNProbeResult) {
	var tcpValues, tlsValues, ttfbValues, totalValues []int64
	var jitterTotal float64
	var previous int64
	for _, sample := range result.Samples {
		if sample.CFRay != "" {
			result.VerifiedCloudflare = true
			result.CFRay = sample.CFRay
			result.CFColo = sample.CFColo
		}
		if sample.CDNProvider != "" {
			result.CDNProvider = sample.CDNProvider
			if result.CFColo == "" {
				result.CFColo = sample.CFColo
			}
		}
		if !sample.Success {
			continue
		}
		result.SuccessCount++
		result.StatusCode = sample.StatusCode
		tcpValues = append(tcpValues, sample.TCPConnectMS)
		tlsValues = append(tlsValues, sample.TLSHandshakeMS)
		ttfbValues = append(ttfbValues, sample.TTFBMS)
		totalValues = append(totalValues, sample.TotalMS)
		if previous > 0 {
			jitterTotal += math.Abs(float64(sample.TotalMS - previous))
		}
		previous = sample.TotalMS
	}
	if result.SampleCount > 0 {
		result.SuccessRate = float64(result.SuccessCount) / float64(result.SampleCount)
	}
	result.P50TCPMS = medianLatency(tcpValues)
	result.P50TLSMS = medianLatency(tlsValues)
	result.P50TTFBMS = medianLatency(ttfbValues)
	result.P50TotalMS = medianLatency(totalValues)
	sort.Slice(totalValues, func(left, right int) bool { return totalValues[left] < totalValues[right] })
	result.P95TotalMS = percentileLatency(totalValues, 0.95)
	if len(totalValues) > 1 {
		result.JitterMS = jitterTotal / float64(len(totalValues)-1)
	}
}

func finaliseCDNRanking(result *CDNOptimizeResult) {
	eligible := make([]*CDNProbeResult, 0, len(result.Results))
	baseline := bestCDNDNSBaseline(result.Results)
	for index := range result.Results {
		probe := &result.Results[index]
		isEligible := cdnProbeEligible(*probe)
		if result.Mode != "" {
			isEligible = probe.Eligible
		}
		if !isEligible {
			continue
		}
		eligible = append(eligible, probe)
	}
	sort.SliceStable(eligible, func(left, right int) bool {
		return cdnResultLess(result.DownloadEnabled, *eligible[left], *eligible[right])
	})
	for index, probe := range eligible {
		probe.Rank = index + 1
	}
	if len(eligible) == 0 {
		if result.Mode != "" {
			result.Finding = PatrolFinding{Code: "no_eligible_edge", Level: "critical", Title: "没有节点通过当前筛选条件", Message: "可适当放宽延迟、丢包、地区或下载速度条件后重新测试。"}
			result.Confidence = "low"
			result.Governance = []CDNGovernanceAdvice{
				{Action: "filters", Title: "检查筛选阈值", Detail: "先关闭地区和速度下限，仅保留默认延迟、丢包条件，确认基础可达性。"},
				{Action: "mode", Title: "切换延迟测试模式", Detail: "TCPing 适合快速发现可达节点；HTTPing 可进一步验证状态码、SNI 与 CDN 地区。"},
			}
			return
		}
		result.Finding = PatrolFinding{Code: "no_verified_edge", Level: "critical", Title: "未找到可用的 Cloudflare 候选", Message: "检查域名是否已开启 Cloudflare 代理，并确认候选 IP 可直连。"}
		result.Confidence = "low"
		result.Governance = []CDNGovernanceAdvice{
			{Action: "verify", Title: "确认 Cloudflare 代理状态", Detail: "检查目标域名是否返回 CF-Ray，并确认候选 IP、TLS SNI 与 Host 配置一致。"},
			{Action: "retry", Title: "分时段重新采样", Detail: "在当前网络与业务高峰各测试一次，排除临时出口抖动后再判断节点质量。"},
		}
		return
	}
	best := eligible[0]
	best.Recommended = true
	result.BestIP = best.Candidate.IP
	if result.Mode != "" {
		finaliseModernCDNRecommendation(result, best, baseline)
		return
	}
	if baseline != nil {
		result.BaselineIP = baseline.Candidate.IP
		if baseline.P50TotalMS > 0 {
			result.ImprovementPercent = (float64(baseline.P50TotalMS-best.P50TotalMS) / float64(baseline.P50TotalMS)) * 100
		}
		if baseline.P95TotalMS > 0 {
			result.P95Improvement = (float64(baseline.P95TotalMS-best.P95TotalMS) / float64(baseline.P95TotalMS)) * 100
		}
	}
	result.Confidence = cdnRecommendationConfidence(*best, baseline)
	if baseline == nil {
		result.Finding = PatrolFinding{Code: "candidate_without_baseline", Level: "warning", Title: "候选节点可用，但缺少 DNS 基线", Message: "当前域名未解析到可用公网地址，暂时无法计算候选节点的提升比例。"}
		result.Governance = []CDNGovernanceAdvice{
			{Action: "dns", Title: "先恢复 DNS 基线", Detail: "确认域名存在有效 A/AAAA 记录后重新测试，避免在缺少当前路径对照时直接切换节点。"},
		}
		return
	}
	if best.SuccessRate < 1 || best.P95TotalMS > 1000 || best.JitterMS > 250 {
		result.Finding = PatrolFinding{Code: "current_route_unstable", Level: "warning", Title: "当前 CDN 路径存在尾延迟波动", Message: fmt.Sprintf("最佳节点 P95 为 %d ms、抖动 %.0f ms；当前候选中没有形成稳定的替代路径。", best.P95TotalMS, best.JitterMS)}
		result.Governance = []CDNGovernanceAdvice{
			{Action: "retest", Title: "在问题时段重复采样", Detail: "将单 IP 采样调整为 5 次，并在相同网络环境复测，确认尾延迟是否持续出现。"},
			{Action: "expand", Title: "补充明确候选节点", Detail: "导入可信来源的候选 IP 文件进行对照；客户端只测试文件中的明确地址，不扫描网段。"},
			{Action: "path", Title: "结合路径对照定位出口", Detail: "若所有候选均高波动，使用路径对照比较直连与代理，判断问题是否位于本地出口。"},
		}
		return
	}
	if best.Candidate.Source == "dns" || result.ImprovementPercent < 10 {
		result.Finding = PatrolFinding{Code: "current_route_good", Level: "good", Title: "当前 CDN 路径已较优", Message: "当前 DNS 节点与候选节点之间没有明显的可重复性能差异。"}
		result.Governance = []CDNGovernanceAdvice{
			{Action: "keep", Title: "保持当前解析策略", Detail: "候选节点未形成至少 10% 的中位耗时优势，暂不建议修改 hosts 或解析策略。"},
			{Action: "monitor", Title: "保留峰值时段复测", Detail: "Anycast 路径可能随运营商与时段变化，可在问题时段使用同一候选集再次对照。"},
		}
		return
	}
	result.Finding = PatrolFinding{Code: "better_candidate", Level: "warning", Title: "发现更优的 Cloudflare 候选", Message: fmt.Sprintf("候选 %s 的中位总耗时较当前 DNS 基线降低约 %.0f%%。", best.Candidate.IP, result.ImprovementPercent)}
	result.Governance = []CDNGovernanceAdvice{
		{Action: "canary", Title: "在单台设备灰度验证", Detail: fmt.Sprintf("临时将 %s 指向 %s，分别验证登录、静态资源与核心接口，不要直接全量修改 DNS。", result.Domain, best.Candidate.IP)},
		{Action: "verify", Title: "验证高峰期稳定性", Detail: fmt.Sprintf("关注成功率、P95 %d ms 与抖动 %.0f ms；若失败率上升或尾延迟恶化，立即恢复原解析。", best.P95TotalMS, best.JitterMS)},
		{Action: "retest", Title: "变更后重复对照", Detail: "使用相同路径和采样次数复测，并通过本地历史对比确认改善可重复。"},
	}
}

func cdnResultLess(downloadEnabled bool, left, right CDNProbeResult) bool {
	if downloadEnabled && left.DownloadSpeedMBps != right.DownloadSpeedMBps {
		return left.DownloadSpeedMBps > right.DownloadSpeedMBps
	}
	if left.LossRate != right.LossRate {
		return left.LossRate < right.LossRate
	}
	if left.AverageLatencyMS != right.AverageLatencyMS && (left.AverageLatencyMS > 0 || right.AverageLatencyMS > 0) {
		return left.AverageLatencyMS < right.AverageLatencyMS
	}
	return cdnProbeLess(left, right)
}

func finaliseModernCDNRecommendation(result *CDNOptimizeResult, best, baseline *CDNProbeResult) {
	if baseline != nil {
		result.BaselineIP = baseline.Candidate.IP
		if baseline.AverageLatencyMS > 0 {
			result.ImprovementPercent = (baseline.AverageLatencyMS - best.AverageLatencyMS) / baseline.AverageLatencyMS * 100
		}
		if baseline.P95TotalMS > 0 {
			result.P95Improvement = float64(baseline.P95TotalMS-best.P95TotalMS) / float64(baseline.P95TotalMS) * 100
		}
	}
	result.Comparison = buildCDNOptimizeComparison(baseline, best)
	result.TargetValidated = result.ValidationScope == "target_domain" && best.SuccessCount > 0
	result.Confidence = cdnRecommendationConfidence(*best, baseline)
	if result.DownloadEnabled && best.DownloadSpeedMBps > 0 {
		result.Finding = PatrolFinding{
			Code: "optimized_edge_found", Level: "good", Title: "已找到综合表现最佳的 CDN 节点",
			Message: fmt.Sprintf("%s 平均延迟 %.1f ms、丢包率 %.0f%%、下载速度 %.2f MB/s。", best.Candidate.IP, best.AverageLatencyMS, best.LossRate*100, best.DownloadSpeedMBps),
		}
	} else {
		result.Finding = PatrolFinding{
			Code: "lowest_latency_edge", Level: "good", Title: "已找到网络质量最佳的 CDN 节点",
			Message: fmt.Sprintf("%s 平均延迟 %.1f ms、丢包率 %.0f%%。", best.Candidate.IP, best.AverageLatencyMS, best.LossRate*100),
		}
	}
	result.Governance = []CDNGovernanceAdvice{
		{Action: "verify", Title: "先验证业务完整性", Detail: fmt.Sprintf("临时将 %s 指向 %s，验证登录、API、静态资源与大文件传输。", result.Domain, best.Candidate.IP)},
		{Action: "canary", Title: "小范围灰度使用", Detail: "Anycast 路径会随运营商和时段变化，先在单台设备或小流量范围观察，不自动修改系统 hosts。"},
		{Action: "retest", Title: "在高峰时段复测", Detail: "保存本次结果并在网络较慢时使用同一参数复测，优先选择多次排名稳定的节点。"},
	}
}

func buildCDNOptimizeComparison(baseline, best *CDNProbeResult) CDNOptimizeComparison {
	comparison := CDNOptimizeComparison{}
	if baseline == nil || best == nil {
		return comparison
	}
	comparison.Comparable = true
	comparison.SameIP = baseline.Candidate.IP == best.Candidate.IP
	comparison.Current = cdnNodeComparison(*baseline)
	comparison.Recommended = cdnNodeComparison(*best)
	comparison.LatencyDeltaMS = baseline.AverageLatencyMS - best.AverageLatencyMS
	if baseline.AverageLatencyMS > 0 {
		comparison.LatencyImprovementPercent = comparison.LatencyDeltaMS / baseline.AverageLatencyMS * 100
	}
	comparison.LossReductionPoints = (baseline.LossRate - best.LossRate) * 100
	comparison.SpeedComparable = baseline.DownloadTested && best.DownloadTested && baseline.DownloadSpeedMBps > 0 && best.DownloadSpeedMBps > 0
	if comparison.SpeedComparable {
		comparison.SpeedDeltaMBps = best.DownloadSpeedMBps - baseline.DownloadSpeedMBps
		comparison.SpeedImprovementPercent = comparison.SpeedDeltaMBps / baseline.DownloadSpeedMBps * 100
		comparison.SpeedMultiplier = best.DownloadSpeedMBps / baseline.DownloadSpeedMBps
	}
	return comparison
}

func cdnNodeComparison(probe CDNProbeResult) CDNNodeComparison {
	return CDNNodeComparison{
		IP: probe.Candidate.IP, Source: probe.Candidate.Source,
		AverageLatencyMS: probe.AverageLatencyMS, LossRate: probe.LossRate,
		DownloadSpeedMBps: probe.DownloadSpeedMBps, DownloadTested: probe.DownloadTested,
		CFColo: probe.CFColo, CDNProvider: probe.CDNProvider,
	}
}

func cdnRecommendationConfidence(best CDNProbeResult, baseline *CDNProbeResult) string {
	if baseline == nil || best.SampleCount < 3 || baseline.SampleCount < 3 {
		return "low"
	}
	if best.SampleCount >= 5 && baseline.SampleCount >= 5 && best.SuccessRate == 1 && baseline.SuccessRate == 1 {
		return "high"
	}
	if best.SuccessRate >= 0.8 && baseline.SuccessRate >= 0.8 {
		return "medium"
	}
	return "low"
}

func buildCDNPathEvidence(domain string, environment NetworkEnvironment) CDNPathEvidence {
	evidence := CDNPathEvidence{
		Mode:            proxyModeDirect,
		HostsApplicable: true,
		Message:         "候选连接已绕过显式 HTTP/SOCKS 代理，结果可用于 hosts 或 DNS 治理",
	}
	evidence.SystemProxyURL = configuredSystemProxyEndpoint(domain)
	evidence.SystemProxyConfigured = evidence.SystemProxyURL != ""
	for _, item := range environment.Interfaces {
		if !item.Tunnel {
			continue
		}
		evidence.TunnelDetected = true
		if item.Active {
			evidence.ActiveTunnelInterfaces = appendUnique(evidence.ActiveTunnelInterfaces, item.Name)
		}
	}
	if len(evidence.ActiveTunnelInterfaces) > 0 {
		evidence.HostsApplicable = false
		evidence.Message = fmt.Sprintf("显式代理已绕过，但活动隧道 %s 仍可能接管流量；建议关闭隧道后复测", strings.Join(evidence.ActiveTunnelInterfaces, ", "))
	} else if evidence.SystemProxyConfigured {
		evidence.Message = fmt.Sprintf("检测到系统代理 %s，但本次候选连接已强制直连；结果仅适用于直连治理", evidence.SystemProxyURL)
	}
	return evidence
}

func cdnProbeEligible(result CDNProbeResult) bool {
	requiredSuccesses := int(math.Ceil(float64(result.SampleCount) * 0.66))
	return result.VerifiedCloudflare && result.SuccessCount >= requiredSuccesses && result.P50TotalMS > 0
}

func cdnProbeLess(left, right CDNProbeResult) bool {
	if left.SuccessRate != right.SuccessRate {
		return left.SuccessRate > right.SuccessRate
	}
	if left.P95TotalMS != right.P95TotalMS {
		return left.P95TotalMS < right.P95TotalMS
	}
	if left.P50TotalMS != right.P50TotalMS {
		return left.P50TotalMS < right.P50TotalMS
	}
	if left.JitterMS != right.JitterMS {
		return left.JitterMS < right.JitterMS
	}
	return left.Candidate.IP < right.Candidate.IP
}

func normaliseCDNTarget(rawDomain, rawPath string) (domain, path, targetURL string, err error) {
	input := strings.TrimSpace(rawDomain)
	if input == "" {
		return "", "", "", fmt.Errorf("domain is empty")
	}
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", "", "", fmt.Errorf("a valid HTTP or HTTPS domain is required")
	}
	if parsed.Port() != "" || net.ParseIP(parsed.Hostname()) != nil {
		return "", "", "", fmt.Errorf("a hostname without a custom port is required")
	}
	domain = strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	path = strings.TrimSpace(rawPath)
	if path == "" {
		path = parsed.EscapedPath()
		if path == "" {
			path = "/"
		}
		if parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", "", "", fmt.Errorf("path must start with one slash")
	}
	target, err := url.Parse(parsed.Scheme + "://" + domain + path)
	if err != nil || target.Hostname() != domain || target.Fragment != "" {
		return "", "", "", fmt.Errorf("invalid test path")
	}
	return domain, path, target.String(), nil
}

func lookupPublicIPs(ctx context.Context, domain string) ([]string, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if isPublicCandidateIP(address.IP) {
			result = appendUnique(result, address.IP.String())
		}
	}
	return result, nil
}

func normaliseCDNCandidates(dnsIPs, manualIPs []string) []CDNCandidate {
	result := make([]CDNCandidate, 0, len(dnsIPs)+len(manualIPs))
	seen := make(map[string]struct{})
	appendCandidate := func(rawIP, source string) {
		ip := net.ParseIP(strings.TrimSpace(rawIP))
		if !isPublicCandidateIP(ip) {
			return
		}
		value := ip.String()
		if _, exists := seen[value]; exists || len(result) >= maxCDNCandidates {
			return
		}
		seen[value] = struct{}{}
		result = append(result, CDNCandidate{IP: value, Source: source})
	}
	for _, address := range dnsIPs {
		appendCandidate(address, "dns")
	}
	for _, address := range manualIPs {
		for _, field := range strings.FieldsFunc(address, func(character rune) bool {
			return character == ',' || character == ';' || character == '\n' || character == '\r' || character == '\t' || character == ' '
		}) {
			appendCandidate(field, "manual")
		}
	}
	return result
}

func isPublicCandidateIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func boundedCDNWorkers(requested, candidates int) int {
	if requested <= 0 {
		requested = defaultCDNWorkers
	}
	if requested > maxCDNWorkers {
		requested = maxCDNWorkers
	}
	if requested > candidates {
		requested = candidates
	}
	return requested
}

func boundedCDNSamples(requested int) int {
	if requested <= 0 {
		return defaultCDNSamples
	}
	if requested > maxCDNSamples {
		return maxCDNSamples
	}
	return requested
}

func medianLatency(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	return percentileLatency(sorted, 0.50)
}

func firstCDNLocalIP(results []CDNProbeResult) string {
	for _, result := range results {
		if result.LocalAddress != "" {
			host, _, err := net.SplitHostPort(result.LocalAddress)
			if err == nil {
				return host
			}
		}
		for _, sample := range result.Samples {
			if sample.LocalAddress == "" {
				continue
			}
			host, _, err := net.SplitHostPort(sample.LocalAddress)
			if err == nil {
				return host
			}
		}
	}
	return ""
}

// ExportCDNResultsCSV opens a native save dialog and writes the complete
// responsive result set. A UTF-8 BOM keeps Chinese headers readable in Excel.
func (a *App) ExportCDNResultsCSV(result CDNOptimizeResult) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("应用窗口尚未就绪")
	}
	if len(result.Results) == 0 {
		return "", fmt.Errorf("没有可导出的优选结果")
	}
	name := "cdn-results.csv"
	if result.Domain != "" {
		name = strings.ReplaceAll(result.Domain, ":", "-") + "-cdn-results.csv"
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出 Cloudflare IP 优选结果",
		DefaultFilename: name,
		Filters:         []wailsruntime.FileFilter{{DisplayName: "CSV 文件 (*.csv)", Pattern: "*.csv"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	var buffer bytes.Buffer
	buffer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"IP 地址", "已发送", "已接收", "丢包率", "平均延迟(ms)", "下载速度(MB/s)", "地区码", "CDN", "HTTP 状态", "来源", "筛选状态"})
	for _, probe := range result.Results {
		state := "通过"
		if !probe.Eligible {
			state = probe.FilterReason
		}
		_ = writer.Write([]string{
			probe.Candidate.IP,
			strconv.Itoa(probe.Sent), strconv.Itoa(probe.Received),
			strconv.FormatFloat(probe.LossRate, 'f', 2, 64),
			strconv.FormatFloat(probe.AverageLatencyMS, 'f', 2, 64),
			strconv.FormatFloat(probe.DownloadSpeedMBps, 'f', 2, 64),
			probe.CFColo, probe.CDNProvider, strconv.Itoa(probe.StatusCode), probe.Candidate.Source, state,
		})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) emitCDNOptimizeProgress(progress CDNOptimizeProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "cdn:result", progress)
	}
}
