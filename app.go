package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	diagnoseTimeout = 12 * time.Second
	maxResponseBody = 1 << 20
	proxyModeSystem = "system"
	proxyModeDirect = "direct"
	proxyModeManual = "manual"
)

// App is the backend exposed to the Wails frontend.
type App struct {
	ctx           context.Context
	traceGeoCache sync.Map
	tasksMu       sync.Mutex
	tasks         map[string]context.CancelFunc
	historyMu     sync.Mutex
	historyStore  *historyStore
	acceleration  *cfAccelerationManager
	updater       *updateManager
}

// DiagOptions controls the target and how its connection reaches the network.
type DiagOptions struct {
	TargetURL string `json:"target_url"`
	ProxyMode string `json:"proxy_mode"`
	ProxyURL  string `json:"proxy_url,omitempty"`
}

// DiagResult contains network phase durations in milliseconds. A phase can be
// zero when it was skipped, for example DNS on an IP or TLS over plain HTTP.
type DiagResult struct {
	Success              bool     `json:"success"`
	Message              string   `json:"message"`
	FailedStage          string   `json:"failed_stage,omitempty"`
	Error                string   `json:"error,omitempty"`
	TargetURL            string   `json:"target_url"`
	FinalURL             string   `json:"final_url,omitempty"`
	DNSLookupMS          int64    `json:"dns_lookup_ms"`
	DNSHosts             []string `json:"dns_hosts,omitempty"`
	TCPConnectMS         int64    `json:"tcp_connect_ms"`
	TLSHandshakeMS       int64    `json:"tls_handshake_ms"`
	TTFBMS               int64    `json:"ttfb_ms"`
	TotalMS              int64    `json:"total_ms"`
	HTTPProtocol         string   `json:"http_protocol,omitempty"`
	StatusCode           int      `json:"status_code"`
	RedirectCount        int      `json:"redirect_count"`
	RedirectChain        []string `json:"redirect_chain,omitempty"`
	ServerHeader         string   `json:"server_header,omitempty"`
	ContentType          string   `json:"content_type,omitempty"`
	ContentLength        int64    `json:"content_length"`
	ResponseBytes        int64    `json:"response_bytes"`
	ResponseTruncated    bool     `json:"response_truncated"`
	CFRay                string   `json:"cf_ray,omitempty"`
	CFColo               string   `json:"cf_colo,omitempty"`
	CFCacheStatus        string   `json:"cf_cache_status,omitempty"`
	ClientIP             string   `json:"client_ip,omitempty"`
	RemoteIP             string   `json:"remote_ip,omitempty"`
	LocalAddress         string   `json:"local_address,omitempty"`
	RemoteAddress        string   `json:"remote_address,omitempty"`
	ResolvedIPs          []string `json:"resolved_ips,omitempty"`
	TLSVersion           string   `json:"tls_version,omitempty"`
	TLSCipherSuite       string   `json:"tls_cipher_suite,omitempty"`
	TLSALPN              string   `json:"tls_alpn,omitempty"`
	TLSCertSubject       string   `json:"tls_cert_subject,omitempty"`
	TLSCertIssuer        string   `json:"tls_cert_issuer,omitempty"`
	TLSCertNotBefore     string   `json:"tls_cert_not_before,omitempty"`
	TLSCertExpiresAt     string   `json:"tls_cert_expires_at,omitempty"`
	TLSCertDaysRemaining int64    `json:"tls_cert_days_remaining"`
	TLSCertValid         bool     `json:"tls_cert_valid"`
	CheckedAt            string   `json:"checked_at"`
	ProxyMode            string   `json:"proxy_mode"`
	ProxyStatus          string   `json:"proxy_status"`
	ProxyDetected        bool     `json:"proxy_detected"`
	ProxyURL             string   `json:"proxy_url,omitempty"`
	ProxyMessage         string   `json:"proxy_message"`
}

func NewApp() *App {
	return &App{
		tasks:        make(map[string]context.CancelFunc),
		acceleration: newCFAccelerationManager(),
		updater:      newUpdateManager(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startUpdater(ctx)
	if a.acceleration != nil {
		recoveryContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		a.acceleration.RecoverStale(recoveryContext)
	}
}

func (a *App) beforeClose(context.Context) bool {
	if a.acceleration == nil {
		return false
	}
	status := a.acceleration.Status()
	if !status.Active && !status.CanRestore {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := a.acceleration.Stop(ctx)
	if err != nil {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, "cf:acceleration-status", status)
		}
		return true
	}
	return false
}

func (a *App) shutdown(context.Context) {
	if a.updater != nil {
		a.updater.Cancel()
	}
	if a.acceleration != nil {
		status := a.acceleration.Status()
		if status.Active || status.CanRestore {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _ = a.acceleration.Stop(ctx)
			cancel()
		}
	}
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()
	for id, cancel := range a.tasks {
		cancel()
		delete(a.tasks, id)
	}
}

func (a *App) baseContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// RunDiagnose uses an isolated transport so each run measures DNS, TCP and TLS
// instead of accidentally reusing a connection from an earlier diagnosis.
func (a *App) RunDiagnose(targetURL string) DiagResult {
	return a.RunDiagnoseWithOptions(DiagOptions{
		TargetURL: targetURL,
		ProxyMode: proxyModeSystem,
	})
}

// RunDiagnoseWithOptions performs a diagnosis with automatic, direct, or
// manually configured proxy routing.
func (a *App) RunDiagnoseWithOptions(options DiagOptions) DiagResult {
	return a.runDiagnose(a.baseContext(), options)
}

func (a *App) runDiagnose(parent context.Context, options DiagOptions) DiagResult {
	return a.runDiagnoseLimited(parent, options, maxResponseBody, false)
}

func (a *App) runDiagnoseLimited(parent context.Context, options DiagOptions, responseLimit int64, rangeRequest bool) DiagResult {
	startedAt := time.Now()
	result := DiagResult{
		Message:   "检测未完成",
		TargetURL: strings.TrimSpace(options.TargetURL),
		CheckedAt: startedAt.Format(time.RFC3339),
		ProxyMode: normaliseProxyMode(options.ProxyMode),
	}

	parsedURL, err := validateHTTPURL(result.TargetURL)
	if err != nil {
		result.FailedStage = "request"
		result.Message = "检测地址配置有误"
		result.Error = err.Error()
		return result
	}
	result.TargetURL = parsedURL.String()

	ctx, cancel := context.WithTimeout(parent, diagnoseTimeout)
	defer cancel()

	var (
		mu             sync.Mutex
		dnsStarted     time.Time
		connectStarted time.Time
		tlsStarted     time.Time
		requestWritten time.Time
		dnsErr         error
		connectErr     error
		connectOK      bool
		tlsErr         error
	)

	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			mu.Lock()
			dnsStarted = time.Now()
			result.DNSHosts = appendUnique(result.DNSHosts, info.Host)
			mu.Unlock()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			mu.Lock()
			if !dnsStarted.IsZero() {
				result.DNSLookupMS += durationMS(time.Since(dnsStarted))
			}
			for _, address := range info.Addrs {
				result.ResolvedIPs = appendUnique(result.ResolvedIPs, address.IP.String())
			}
			dnsErr = info.Err
			mu.Unlock()
		},
		ConnectStart: func(_, _ string) {
			mu.Lock()
			connectStarted = time.Now()
			mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			if err == nil {
				connectOK = true
				connectErr = nil
				if !connectStarted.IsZero() {
					result.TCPConnectMS += durationMS(time.Since(connectStarted))
				}
			} else if !connectOK {
				connectErr = err
			}
			mu.Unlock()
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			tlsStarted = time.Now()
			mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			mu.Lock()
			if !tlsStarted.IsZero() {
				result.TLSHandshakeMS = durationMS(time.Since(tlsStarted))
			}
			tlsErr = err
			mu.Unlock()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			mu.Lock()
			result.ClientIP = addressIP(info.Conn.LocalAddr())
			result.RemoteIP = addressIP(info.Conn.RemoteAddr())
			result.LocalAddress = addressString(info.Conn.LocalAddr())
			result.RemoteAddress = addressString(info.Conn.RemoteAddr())
			mu.Unlock()
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			mu.Lock()
			requestWritten = time.Now()
			mu.Unlock()
		},
		GotFirstResponseByte: func() {
			mu.Lock()
			base := requestWritten
			if base.IsZero() {
				base = startedAt
			}
			result.TTFBMS = durationMS(time.Since(base))
			mu.Unlock()
		},
	}

	req, err := http.NewRequestWithContext(
		httptrace.WithClientTrace(ctx, trace),
		http.MethodGet,
		result.TargetURL,
		nil,
	)
	if err != nil {
		result.FailedStage = "request"
		result.Message = "无法创建检测请求"
		result.Error = err.Error()
		return result
	}
	req.Header.Set("User-Agent", "ATNetool/1.0")
	req.Header.Set("Cache-Control", "no-cache")
	if rangeRequest {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", responseLimit-1))
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	proxyFunc, selectedProxy, err := proxyForRequest(result.ProxyMode, options.ProxyURL, req)
	if err != nil {
		result.FailedStage = "proxy"
		result.Message = "代理配置有误"
		result.Error = err.Error()
		result.ProxyStatus = "invalid"
		result.ProxyMessage = "代理地址不可用"
		return result
	}
	transport.Proxy = proxyFunc
	if selectedProxy != nil {
		result.ProxyDetected = true
		result.ProxyStatus = "proxy"
		result.ProxyURL = safeProxyURL(selectedProxy)
		if result.ProxyMode == proxyModeManual {
			result.ProxyMessage = "已通过手动配置的代理连接"
		} else {
			result.ProxyMessage = "已通过系统或环境代理连接"
		}
	} else {
		result.ProxyStatus = "direct"
		result.ProxyMessage = "未检测到显式代理"
	}
	result.RedirectChain = []string{result.TargetURL}
	client := &http.Client{
		Transport: transport,
		Timeout:   diagnoseTimeout,
		CheckRedirect: func(redirectRequest *http.Request, via []*http.Request) error {
			mu.Lock()
			result.RedirectCount = len(via)
			result.RedirectChain = append(result.RedirectChain, redirectRequest.URL.String())
			mu.Unlock()
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		result.TotalMS = durationMS(time.Since(startedAt))
		if selectedProxy != nil && connectErr != nil {
			result.FailedStage = "proxy"
			result.Message = "无法连接代理服务器，请检查代理设置"
		} else {
			result.FailedStage, result.Message = diagnoseFailure(dnsErr, connectErr, tlsErr)
		}
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.FinalURL = resp.Request.URL.String()
	result.HTTPProtocol = resp.Proto
	result.CFRay = resp.Header.Get("CF-Ray")
	result.CFColo = cloudflareColo(result.CFRay)
	result.CFCacheStatus = resp.Header.Get("CF-Cache-Status")
	result.ServerHeader = resp.Header.Get("Server")
	result.ContentType = resp.Header.Get("Content-Type")
	if resp.ContentLength >= 0 {
		result.ContentLength = resp.ContentLength
	}
	if resp.TLS != nil {
		populateTLSDetails(&result, *resp.TLS, resp.Request.URL.Hostname(), time.Now())
	}
	// Drain a bounded amount so small responses complete cleanly without
	// downloading an unexpectedly large target.
	responseBytes, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, responseLimit+1))
	if responseBytes > responseLimit {
		result.ResponseBytes = responseLimit
		result.ResponseTruncated = true
	} else {
		result.ResponseBytes = responseBytes
	}
	result.TotalMS = durationMS(time.Since(startedAt))
	if readErr != nil {
		result.FailedStage = "response"
		result.Message = "服务器响应传输中断"
		result.Error = readErr.Error()
		return result
	}
	if selectedProxy == nil && remoteDiffersFromResolved(result.RemoteIP, result.ResolvedIPs) {
		result.ProxyStatus = "suspected"
		result.ProxyMessage = "连接地址与域名解析结果不同，可能经过透明代理或 VPN"
	}

	result.Success = true
	if resp.StatusCode >= http.StatusInternalServerError {
		result.Message = "已连接，但服务器暂时异常"
	} else if resp.StatusCode >= http.StatusBadRequest {
		result.Message = "已连接，但目标返回访问错误"
	} else {
		result.Message = "检测完成"
	}
	return result
}

func normaliseProxyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case proxyModeDirect:
		return proxyModeDirect
	case proxyModeManual:
		return proxyModeManual
	default:
		return proxyModeSystem
	}
}

func proxyForRequest(mode, rawProxyURL string, request *http.Request) (func(*http.Request) (*url.URL, error), *url.URL, error) {
	switch mode {
	case proxyModeDirect:
		return nil, nil, nil
	case proxyModeManual:
		proxyURL, err := validateProxyURL(rawProxyURL)
		if err != nil {
			return nil, nil, err
		}
		return http.ProxyURL(proxyURL), proxyURL, nil
	default:
		proxyURL, err := systemProxyForRequest(request)
		if err != nil {
			return nil, nil, err
		}
		return systemProxyForRequest, proxyURL, nil
	}
}

func validateProxyURL(rawProxyURL string) (*url.URL, error) {
	proxyAddress := strings.TrimSpace(rawProxyURL)
	if proxyAddress == "" {
		return nil, fmt.Errorf("proxy URL is empty")
	}
	if !strings.Contains(proxyAddress, "://") {
		proxyAddress = "http://" + proxyAddress
	}
	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("invalid proxy URL")
	}
	if (proxyURL.Path != "" && proxyURL.Path != "/") || proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return nil, fmt.Errorf("proxy URL must not contain a path, query, or fragment")
	}
	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	switch proxyURL.Scheme {
	case "http", "https", "socks5", "socks5h":
		return proxyURL, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", proxyURL.Scheme)
	}
}

func safeProxyURL(proxyURL *url.URL) string {
	if proxyURL == nil {
		return ""
	}
	return (&url.URL{Scheme: proxyURL.Scheme, Host: proxyURL.Host}).String()
}

func remoteDiffersFromResolved(remoteIP string, resolvedIPs []string) bool {
	remote := net.ParseIP(remoteIP)
	if remote == nil || len(resolvedIPs) == 0 {
		return false
	}
	for _, resolvedIP := range resolvedIPs {
		if candidate := net.ParseIP(resolvedIP); candidate != nil && remote.Equal(candidate) {
			return false
		}
	}
	return true
}

func validateHTTPURL(rawURL string) (*url.URL, error) {
	targetURL := strings.TrimSpace(rawURL)
	if targetURL == "" {
		return nil, fmt.Errorf("URL is empty")
	}
	if !strings.Contains(targetURL, "://") {
		targetURL = "https://" + targetURL
	}

	parsedURL, err := url.ParseRequestURI(targetURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	parsedURL.Scheme = strings.ToLower(parsedURL.Scheme)
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", parsedURL.Scheme)
	}
	if parsedURL.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	return parsedURL, nil
}

func durationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	milliseconds := duration.Milliseconds()
	if milliseconds == 0 {
		return 1
	}
	return milliseconds
}

func addressIP(address net.Addr) string {
	if address == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return address.String()
	}
	return host
}

func addressString(address net.Addr) string {
	if address == nil {
		return ""
	}
	return address.String()
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloudflareColo(cfRay string) string {
	separator := strings.LastIndex(cfRay, "-")
	if separator < 0 || separator == len(cfRay)-1 {
		return ""
	}
	return cfRay[separator+1:]
}

func populateTLSDetails(result *DiagResult, state tls.ConnectionState, hostname string, now time.Time) {
	result.TLSVersion = tlsVersionName(state.Version)
	result.TLSCipherSuite = tls.CipherSuiteName(state.CipherSuite)
	result.TLSALPN = state.NegotiatedProtocol
	if len(state.PeerCertificates) == 0 {
		return
	}

	certificate := state.PeerCertificates[0]
	result.TLSCertSubject = certificate.Subject.CommonName
	if result.TLSCertSubject == "" {
		result.TLSCertSubject = certificate.Subject.String()
	}
	result.TLSCertIssuer = certificate.Issuer.CommonName
	if result.TLSCertIssuer == "" {
		result.TLSCertIssuer = certificate.Issuer.String()
	}
	result.TLSCertNotBefore = certificate.NotBefore.Format(time.RFC3339)
	result.TLSCertExpiresAt = certificate.NotAfter.Format(time.RFC3339)
	result.TLSCertDaysRemaining = int64(certificate.NotAfter.Sub(now).Hours() / 24)
	result.TLSCertValid = !now.Before(certificate.NotBefore) && !now.After(certificate.NotAfter) &&
		certificate.VerifyHostname(hostname) == nil
}

func tlsVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04X", version)
	}
}

func diagnoseFailure(dnsErr, connectErr, tlsErr error) (stage, message string) {
	switch {
	case dnsErr != nil:
		return "dns", "域名解析失败，请检查本地网络或 DNS 设置"
	case tlsErr != nil:
		return "tls", "安全连接失败，请检查系统时间或网络拦截设置"
	case connectErr != nil:
		return "tcp", "无法连接加速节点，请检查网络或防火墙"
	default:
		return "response", "服务器未及时响应，请稍后重试"
	}
}
