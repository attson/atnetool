package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxPortTestTargets = 24
	defaultPortWorkers = 6
	maxPortWorkers     = 8
	portTestTimeout    = 30 * time.Second
	portProbeTimeout   = 4 * time.Second
)

type PortTestOptions struct {
	TaskID      string `json:"task_id"`
	Target      string `json:"target"`
	Ports       []int  `json:"ports"`
	Concurrency int    `json:"concurrency"`
}

type PortProbeResult struct {
	Index            int    `json:"index"`
	Port             int    `json:"port"`
	Service          string `json:"service"`
	Status           string `json:"status"`
	Reachable        bool   `json:"reachable"`
	LatencyMS        int64  `json:"latency_ms"`
	Address          string `json:"address,omitempty"`
	Protocol         string `json:"protocol"`
	HTTPStatus       int    `json:"http_status,omitempty"`
	TLSVersion       string `json:"tls_version,omitempty"`
	TLSALPN          string `json:"tls_alpn,omitempty"`
	TLSCertSubject   string `json:"tls_cert_subject,omitempty"`
	TLSCertIssuer    string `json:"tls_cert_issuer,omitempty"`
	TLSCertExpiresAt string `json:"tls_cert_expires_at,omitempty"`
	TLSDaysRemaining int64  `json:"tls_days_remaining"`
	TLSValid         bool   `json:"tls_valid"`
	Message          string `json:"message"`
	Error            string `json:"error,omitempty"`
}

type PortTestFinding struct {
	Code   string `json:"code"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type PortTestRemediation struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type PortTestProgress struct {
	TaskID   string          `json:"task_id"`
	Complete int             `json:"complete"`
	Total    int             `json:"total"`
	Probe    PortProbeResult `json:"probe"`
}

type PortTestResult struct {
	SchemaVersion  int                   `json:"schema_version"`
	TaskID         string                `json:"task_id"`
	Status         string                `json:"status"`
	Message        string                `json:"message"`
	Target         string                `json:"target"`
	ResolvedIPs    []string              `json:"resolved_ips,omitempty"`
	DNSError       string                `json:"dns_error,omitempty"`
	StartedAt      string                `json:"started_at"`
	FinishedAt     string                `json:"finished_at"`
	DurationMS     int64                 `json:"duration_ms"`
	TotalCount     int                   `json:"total_count"`
	CompletedCount int                   `json:"completed_count"`
	OpenCount      int                   `json:"open_count"`
	RefusedCount   int                   `json:"refused_count"`
	TimeoutCount   int                   `json:"timeout_count"`
	Results        []PortProbeResult     `json:"results"`
	Findings       []PortTestFinding     `json:"findings"`
	Remediations   []PortTestRemediation `json:"remediations"`
	Environment    NetworkEnvironment    `json:"environment"`
}

var commonPortServices = map[int]string{
	21: "FTP", 22: "SSH", 25: "SMTP", 53: "DNS", 80: "HTTP", 110: "POP3",
	143: "IMAP", 443: "HTTPS", 465: "SMTPS", 587: "SMTP", 993: "IMAPS",
	995: "POP3S", 1433: "MSSQL", 3306: "MySQL", 5432: "PostgreSQL",
	6379: "Redis", 8000: "HTTP", 8080: "HTTP", 8443: "HTTPS", 9200: "Elasticsearch",
}

func (a *App) RunPortTest(options PortTestOptions) PortTestResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := PortTestResult{
		SchemaVersion: 1,
		TaskID:        taskID,
		Status:        "running",
		Message:       "端口检测未完成",
		Target:        normalisePortTarget(options.Target),
		StartedAt:     started.Format(time.RFC3339),
	}
	ports := normalisePorts(options.Ports)
	result.TotalCount = len(ports)
	if result.Target == "" || len(ports) == 0 {
		result.Status = "failed"
		result.Message = "目标或端口配置有误"
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
	ctx, stopAtTimeout := context.WithTimeout(baseContext, portTestTimeout)
	defer stopAtTimeout()

	result.ResolvedIPs, result.DNSError = resolvePortTarget(ctx, result.Target)
	if len(result.ResolvedIPs) == 0 {
		result.Status = "completed"
		result.Message = "目标域名无法解析"
		result.Findings, result.Remediations = buildPortTestFindings(result)
		result.DurationMS = durationMS(time.Since(started))
		result.FinishedAt = time.Now().Format(time.RFC3339)
		result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeDirect})
		return result
	}

	type portJob struct {
		index int
		port  int
	}
	jobs := make(chan portJob)
	completed := make(chan PortProbeResult, len(ports))
	workers := boundedPortWorkers(options.Concurrency, len(ports))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				completed <- probePort(ctx, job.index, result.Target, result.ResolvedIPs, job.port)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index, port := range ports {
			select {
			case jobs <- portJob{index: index, port: port}:
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
		result.CompletedCount++
		switch probe.Status {
		case "open":
			result.OpenCount++
		case "refused":
			result.RefusedCount++
		case "timeout":
			result.TimeoutCount++
		}
		a.emitPortTestProgress(PortTestProgress{TaskID: taskID, Complete: result.CompletedCount, Total: len(ports), Probe: probe})
	}
	sort.Slice(result.Results, func(left, right int) bool { return result.Results[left].Index < result.Results[right].Index })
	result.Findings, result.Remediations = buildPortTestFindings(result)
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "端口检测已停止"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Message = "端口检测超时，已保留完成结果"
	} else {
		result.Status = "completed"
		result.Message = "端口检测完成"
	}
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeDirect})
	return result
}

func normalisePortTarget(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" {
			return ""
		}
		value = parsed.Hostname()
	} else if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(strings.ToLower(value), "[]")
	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}
	return normaliseBaselineHost(value)
}

func normalisePorts(raw []int) []int {
	ports := make([]int, 0, len(raw))
	seen := make(map[int]struct{})
	for _, port := range raw {
		if port < 1 || port > 65535 {
			continue
		}
		if _, found := seen[port]; found || len(ports) >= maxPortTestTargets {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func resolvePortTarget(ctx context.Context, target string) ([]string, string) {
	if address := net.ParseIP(target); address != nil {
		return []string{address.String()}, ""
	}
	lookupContext, cancel := context.WithTimeout(ctx, dnsQueryTimeout)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupContext, target)
	if err != nil {
		return nil, compactError(err)
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = appendUnique(result, address.IP.String())
	}
	sort.Strings(result)
	return result, ""
}

func probePort(parent context.Context, index int, target string, addresses []string, port int) PortProbeResult {
	result := PortProbeResult{
		Index: index, Port: port, Service: commonPortService(port), Status: "unreachable",
		Protocol: "TCP", Message: "未建立 TCP 连接",
	}
	var lastErr error
	for _, address := range addresses {
		ctx, cancel := context.WithTimeout(parent, portProbeTimeout)
		started := time.Now()
		connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(address, strconv.Itoa(port)))
		result.LatencyMS = durationMS(time.Since(started))
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		result.Reachable = true
		result.Status = "open"
		result.Address = address
		result.Message = "TCP 端口可达"
		inspectPortProtocol(parent, connection, target, &result)
		_ = connection.Close()
		return result
	}
	result.Status, result.Message = classifyPortError(lastErr)
	if lastErr != nil {
		result.Error = compactError(lastErr)
	}
	return result
}

func inspectPortProtocol(parent context.Context, connection net.Conn, target string, result *PortProbeResult) {
	if deadline, ok := parent.Deadline(); ok {
		_ = connection.SetDeadline(minimumTime(deadline, time.Now().Add(3*time.Second)))
	} else {
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	}
	if isTLSPort(result.Port) {
		tlsConnection := tls.Client(connection, &tls.Config{ServerName: target, MinVersion: tls.VersionTLS12})
		if err := tlsConnection.HandshakeContext(parent); err != nil {
			result.Protocol = "TCP"
			result.Message = "TCP 可达，但 TLS 握手失败"
			result.Error = compactError(err)
			return
		}
		state := tlsConnection.ConnectionState()
		result.Protocol = "TLS"
		result.TLSVersion = tlsVersionName(state.Version)
		result.TLSALPN = state.NegotiatedProtocol
		if len(state.PeerCertificates) > 0 {
			certificate := state.PeerCertificates[0]
			result.TLSCertSubject = certificate.Subject.CommonName
			result.TLSCertIssuer = certificate.Issuer.CommonName
			result.TLSCertExpiresAt = certificate.NotAfter.Format(time.RFC3339)
			result.TLSDaysRemaining = int64(time.Until(certificate.NotAfter).Hours() / 24)
			result.TLSValid = time.Now().After(certificate.NotBefore) && time.Now().Before(certificate.NotAfter)
		}
		if isHTTPPort(result.Port) {
			result.Protocol = "HTTPS"
			result.HTTPStatus = probeHTTPStatus(tlsConnection, target)
		}
		result.Message = "TLS 服务可达"
		return
	}
	if isHTTPPort(result.Port) {
		result.Protocol = "HTTP"
		result.HTTPStatus = probeHTTPStatus(connection, target)
		if result.HTTPStatus > 0 {
			result.Message = "HTTP 服务可达"
		}
	}
}

func probeHTTPStatus(connection net.Conn, host string) int {
	_, err := fmt.Fprintf(connection, "HEAD / HTTP/1.1\r\nHost: %s\r\nUser-Agent: ATNetool/1.0\r\nConnection: close\r\n\r\n", host)
	if err != nil {
		return 0
	}
	line, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return 0
	}
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0
	}
	status, _ := strconv.Atoi(fields[1])
	return status
}

func classifyPortError(err error) (string, string) {
	if err == nil {
		return "unreachable", "未建立 TCP 连接"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused", "目标明确拒绝连接"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout", "连接超时，可能被防火墙丢弃"
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return "no_route", "没有到目标的可用路由"
	}
	return "unreachable", "目标端口不可达"
}

func buildPortTestFindings(result PortTestResult) ([]PortTestFinding, []PortTestRemediation) {
	if len(result.ResolvedIPs) == 0 {
		return []PortTestFinding{{Code: "dns_failed", Status: "critical", Title: "目标域名无法解析", Detail: result.DNSError}},
			[]PortTestRemediation{{Action: "diagnose_dns", Title: "运行 DNS 深度诊断", Detail: "先恢复目标域名解析，再判断端口和防火墙状态。"}}
	}
	findings := make([]PortTestFinding, 0, 3)
	remediations := make([]PortTestRemediation, 0, 3)
	if result.OpenCount == result.TotalCount && result.TotalCount > 0 {
		findings = append(findings, PortTestFinding{Code: "all_open", Status: "healthy", Title: "所选端口全部可达", Detail: fmt.Sprintf("%d 个端口完成 TCP 建连。", result.OpenCount)})
	} else if result.OpenCount > 0 {
		findings = append(findings, PortTestFinding{Code: "partial_open", Status: "warning", Title: "部分端口不可达", Detail: fmt.Sprintf("%d/%d 个端口可达；关闭端口不一定是故障，应结合预期服务判断。", result.OpenCount, result.TotalCount)})
	} else {
		findings = append(findings, PortTestFinding{Code: "none_open", Status: "critical", Title: "所选端口均不可达", Detail: "目标已解析，但所有 TCP 连接均未建立。"})
	}
	if result.RefusedCount > 0 {
		findings = append(findings, PortTestFinding{Code: "connection_refused", Status: "warning", Title: "目标主动拒绝部分连接", Detail: fmt.Sprintf("%d 个端口返回拒绝，通常表示目标可达但服务未监听或被规则显式拒绝。", result.RefusedCount)})
		remediations = append(remediations, PortTestRemediation{Action: "check_listener", Title: "检查服务监听与安全组", Detail: "确认服务进程监听目标地址和端口，并核对主机防火墙及云安全组入站规则。"})
	}
	if result.TimeoutCount > 0 {
		findings = append(findings, PortTestFinding{Code: "connection_timeout", Status: "warning", Title: "部分连接静默超时", Detail: fmt.Sprintf("%d 个端口未收到拒绝或响应，可能在路径中被防火墙丢弃。", result.TimeoutCount)})
		remediations = append(remediations, PortTestRemediation{Action: "check_firewall", Title: "定位路径中的丢弃规则", Detail: "对照直连与代理路径，并检查出口 ACL、主机防火墙和服务端安全策略。"})
	}
	for _, probe := range result.Results {
		if probe.Reachable && isTLSPort(probe.Port) && probe.Protocol == "TCP" {
			findings = append(findings, PortTestFinding{Code: "tls_failed", Status: "warning", Title: "TCP 可达但 TLS 握手失败", Detail: fmt.Sprintf("端口 %d 已建立 TCP 连接，但未完成 TLS 协商。", probe.Port)})
			remediations = append(remediations, PortTestRemediation{Action: "inspect_tls", Title: "检查 TLS 配置", Detail: "确认端口协议、SNI、证书链和最低 TLS 版本是否与客户端兼容。"})
			break
		}
	}
	return findings, remediations
}

func commonPortService(port int) string {
	if service := commonPortServices[port]; service != "" {
		return service
	}
	return "TCP"
}

func isHTTPPort(port int) bool {
	return port == 80 || port == 443 || port == 8000 || port == 8080 || port == 8443
}

func isTLSPort(port int) bool {
	return port == 443 || port == 465 || port == 993 || port == 995 || port == 8443
}

func boundedPortWorkers(requested, total int) int {
	if requested <= 0 {
		requested = defaultPortWorkers
	}
	if requested > maxPortWorkers {
		requested = maxPortWorkers
	}
	if requested > total {
		requested = total
	}
	return requested
}

func minimumTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (a *App) emitPortTestProgress(progress PortTestProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "port-test:result", progress)
	}
}
