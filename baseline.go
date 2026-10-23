package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const baselineTimeout = 10 * time.Second

type NetworkBaselineOptions struct {
	TaskID     string `json:"task_id"`
	TargetHost string `json:"target_host"`
}

type DefaultRoute struct {
	Family    string `json:"family"`
	Interface string `json:"interface,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
	LocalIP   string `json:"local_ip,omitempty"`
	Metric    int    `json:"metric,omitempty"`
	Selected  bool   `json:"selected"`
}

type IPStackProbeResult struct {
	Family    string `json:"family"`
	Available bool   `json:"available"`
	LatencyMS int64  `json:"latency_ms"`
	LocalIP   string `json:"local_ip,omitempty"`
	RemoteIP  string `json:"remote_ip,omitempty"`
	Interface string `json:"interface,omitempty"`
	Message   string `json:"message"`
}

type DNSProbeResult struct {
	Name       string   `json:"name"`
	Server     string   `json:"server,omitempty"`
	Success    bool     `json:"success"`
	DurationMS int64    `json:"duration_ms"`
	IPv4       []string `json:"ipv4,omitempty"`
	IPv6       []string `json:"ipv6,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type DNSComparison struct {
	SuccessfulResolvers int  `json:"successful_resolvers"`
	MatchingResolvers   int  `json:"matching_resolvers"`
	Different           bool `json:"different"`
}

type ProxyListenerResult struct {
	Endpoint  string `json:"endpoint"`
	Source    string `json:"source"`
	Listening bool   `json:"listening"`
	Protocol  string `json:"protocol,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type BaselineFinding struct {
	Layer  string `json:"layer"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type BaselineRemediation struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type LocalNetworkAssessment struct {
	Status                 string   `json:"status"`
	Summary                string   `json:"summary"`
	ActiveInterfaces       []string `json:"active_interfaces,omitempty"`
	VirtualInterfaces      []string `json:"virtual_interfaces,omitempty"`
	TunnelInterfaces       []string `json:"tunnel_interfaces,omitempty"`
	MTURisks               []string `json:"mtu_risks,omitempty"`
	DefaultRouteCount      int      `json:"default_route_count"`
	IPv4DefaultRoutes      int      `json:"ipv4_default_routes"`
	IPv6DefaultRoutes      int      `json:"ipv6_default_routes"`
	MultipleDefaultRoutes  bool     `json:"multiple_default_routes"`
	GatewayBindingComplete bool     `json:"gateway_binding_complete"`
	RouteTakeover          bool     `json:"route_takeover"`
	SystemProxyConfigured  bool     `json:"system_proxy_configured"`
	SystemProxyEndpoint    string   `json:"system_proxy_endpoint,omitempty"`
	DNSMode                string   `json:"dns_mode"`
	Correlation            string   `json:"correlation"`
}

type NetworkBaselineResult struct {
	SchemaVersion  int                    `json:"schema_version"`
	TaskID         string                 `json:"task_id"`
	Status         string                 `json:"status"`
	Message        string                 `json:"message"`
	TargetHost     string                 `json:"target_host"`
	StartedAt      string                 `json:"started_at"`
	FinishedAt     string                 `json:"finished_at"`
	DurationMS     int64                  `json:"duration_ms"`
	Routes         []DefaultRoute         `json:"routes,omitempty"`
	IPStacks       []IPStackProbeResult   `json:"ip_stacks"`
	DNSProbes      []DNSProbeResult       `json:"dns_probes"`
	DNSComparison  DNSComparison          `json:"dns_comparison"`
	ProxyListeners []ProxyListenerResult  `json:"proxy_listeners,omitempty"`
	Findings       []BaselineFinding      `json:"findings"`
	Remediations   []BaselineRemediation  `json:"remediations,omitempty"`
	LocalNetwork   LocalNetworkAssessment `json:"local_network"`
	Environment    NetworkEnvironment     `json:"environment"`
}

type dnsProbeDefinition struct {
	name   string
	server string
}

var baselineDNSResolvers = []dnsProbeDefinition{
	{name: "系统 DNS"},
	{name: "Cloudflare", server: "1.1.1.1:53"},
	{name: "AliDNS", server: "223.5.5.5:53"},
	{name: "Google", server: "8.8.8.8:53"},
}

var baselineStackEndpoints = map[string][]string{
	"IPv4": {"1.1.1.1:443", "8.8.8.8:443"},
	"IPv6": {"[2606:4700:4700::1111]:443", "[2001:4860:4860::8888]:443"},
}

func (a *App) RunNetworkBaseline(options NetworkBaselineOptions) NetworkBaselineResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := NetworkBaselineResult{
		SchemaVersion: 1,
		TaskID:        taskID,
		Status:        "running",
		TargetHost:    normaliseBaselineHost(options.TargetHost),
		StartedAt:     started.Format(time.RFC3339),
	}
	if result.TargetHost == "" {
		result.Status = "failed"
		result.Message = "请输入有效的参考域名"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}

	ctx, cancel := context.WithTimeout(a.baseContext(), baselineTimeout)
	if !a.registerTask(taskID, cancel) {
		cancel()
		result.Status = "failed"
		result.Message = "任务标识已在运行"
		result.FinishedAt = time.Now().Format(time.RFC3339)
		return result
	}
	defer func() {
		cancel()
		a.unregisterTask(taskID)
	}()

	var wait sync.WaitGroup
	wait.Add(4)
	go func() {
		defer wait.Done()
		result.Routes = detectDefaultRoutes()
	}()
	go func() {
		defer wait.Done()
		result.IPStacks = probeIPStacks(ctx)
	}()
	go func() {
		defer wait.Done()
		result.DNSProbes = probeDNSResolvers(ctx, result.TargetHost)
		result.DNSComparison = compareDNSAnswers(result.DNSProbes)
	}()
	go func() {
		defer wait.Done()
		result.ProxyListeners = detectProxyListeners(ctx, result.TargetHost)
	}()
	wait.Wait()

	result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeSystem})
	result.LocalNetwork = buildLocalNetworkAssessment(&result)
	result.Findings = buildBaselineFindings(result)
	result.Remediations = buildBaselineRemediations(result)
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	if errors.Is(ctx.Err(), context.Canceled) {
		result.Status = "cancelled"
		result.Message = "网络基线检测已取消"
	} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Status = "timeout"
		result.Message = "网络基线检测超时"
	} else {
		result.Status = "completed"
		result.Message = "网络基线检测完成"
	}
	return result
}

func normaliseBaselineHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || strings.ContainsAny(parsed.Hostname(), " \t\r\n/") {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if net.ParseIP(host) != nil || len(host) > 253 || !strings.Contains(host, ".") {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ""
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return ""
			}
		}
	}
	return host
}

func probeIPStacks(ctx context.Context) []IPStackProbeResult {
	families := []string{"IPv4", "IPv6"}
	results := make([]IPStackProbeResult, len(families))
	var wait sync.WaitGroup
	for index, family := range families {
		index, family := index, family
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index] = probeIPStack(ctx, family, baselineStackEndpoints[family])
		}()
	}
	wait.Wait()
	return results
}

func probeIPStack(ctx context.Context, family string, endpoints []string) IPStackProbeResult {
	result := IPStackProbeResult{Family: family, Message: "未找到可用路径"}
	network := "tcp4"
	if family == "IPv6" {
		network = "tcp6"
	}
	for _, endpoint := range endpoints {
		started := time.Now()
		connection, err := (&net.Dialer{Timeout: 2500 * time.Millisecond}).DialContext(ctx, network, endpoint)
		if err != nil {
			continue
		}
		result.Available = true
		result.LatencyMS = durationMS(time.Since(started))
		result.LocalIP = addressIP(connection.LocalAddr())
		result.RemoteIP = addressIP(connection.RemoteAddr())
		result.Interface = interfaceForIP(result.LocalIP)
		result.Message = "公网 TCP 路径可用"
		_ = connection.Close()
		return result
	}
	return result
}

func probeDNSResolvers(ctx context.Context, host string) []DNSProbeResult {
	results := make([]DNSProbeResult, len(baselineDNSResolvers))
	var wait sync.WaitGroup
	for index, definition := range baselineDNSResolvers {
		index, definition := index, definition
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[index] = probeDNSResolver(ctx, host, definition)
		}()
	}
	wait.Wait()
	return results
}

func probeDNSResolver(parent context.Context, host string, definition dnsProbeDefinition) DNSProbeResult {
	result := DNSProbeResult{Name: definition.name, Server: definition.server}
	ctx, cancel := context.WithTimeout(parent, 3500*time.Millisecond)
	defer cancel()
	resolver := net.DefaultResolver
	if definition.server != "" {
		resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, definition.server)
		}}
	}
	started := time.Now()
	addresses, err := resolver.LookupIPAddr(ctx, host)
	result.DurationMS = durationMS(time.Since(started))
	if err != nil {
		result.Error = compactError(err)
		return result
	}
	for _, address := range addresses {
		if address.IP.To4() != nil {
			result.IPv4 = appendUnique(result.IPv4, address.IP.String())
		} else if address.IP.To16() != nil {
			result.IPv6 = appendUnique(result.IPv6, address.IP.String())
		}
	}
	sort.Strings(result.IPv4)
	sort.Strings(result.IPv6)
	result.Success = len(result.IPv4)+len(result.IPv6) > 0
	return result
}

func compareDNSAnswers(probes []DNSProbeResult) DNSComparison {
	comparison := DNSComparison{}
	var system map[string]struct{}
	for index, probe := range probes {
		if !probe.Success {
			continue
		}
		comparison.SuccessfulResolvers++
		answers := make(map[string]struct{}, len(probe.IPv4)+len(probe.IPv6))
		for _, address := range append(append([]string{}, probe.IPv4...), probe.IPv6...) {
			answers[address] = struct{}{}
		}
		if index == 0 {
			system = answers
			comparison.MatchingResolvers++
			continue
		}
		if system == nil {
			continue
		}
		matched := false
		for address := range answers {
			if _, exists := system[address]; exists {
				matched = true
				break
			}
		}
		if matched {
			comparison.MatchingResolvers++
		} else {
			comparison.Different = true
		}
	}
	return comparison
}

func detectProxyListeners(ctx context.Context, targetHost string) []ProxyListenerResult {
	endpoints := make(map[string]string)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+targetHost+"/", nil)
	if request != nil {
		if configured, err := systemProxyForRequest(request); err == nil && configured != nil {
			if endpoint := configured.Host; endpoint != "" {
				endpoints[endpoint] = "系统配置"
			}
		}
	}
	for _, endpoint := range []string{"127.0.0.1:7890", "127.0.0.1:7891", "127.0.0.1:1080", "127.0.0.1:8080", "127.0.0.1:10809"} {
		if _, exists := endpoints[endpoint]; !exists {
			endpoints[endpoint] = "常见端口"
		}
	}

	results := make([]ProxyListenerResult, 0, len(endpoints))
	var mu sync.Mutex
	var wait sync.WaitGroup
	for endpoint, source := range endpoints {
		endpoint, source := endpoint, source
		wait.Add(1)
		go func() {
			defer wait.Done()
			if result := probeProxyEndpoint(endpoint, source); result.Listening {
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}
		}()
	}
	wait.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Endpoint < results[j].Endpoint })
	return results
}

func probeProxyEndpoint(endpoint, source string) ProxyListenerResult {
	result := ProxyListenerResult{Endpoint: endpoint, Source: source}
	started := time.Now()
	connection, err := net.DialTimeout("tcp", endpoint, 450*time.Millisecond)
	if err != nil {
		return result
	}
	result.Listening = true
	result.LatencyMS = durationMS(time.Since(started))
	_ = connection.Close()
	if probeSOCKS5(endpoint) {
		result.Protocol = "SOCKS5"
	} else if probeHTTPProxy(endpoint) {
		result.Protocol = "HTTP"
	} else {
		result.Protocol = "未知 TCP 服务"
	}
	return result
}

func probeSOCKS5(endpoint string) bool {
	connection, err := net.DialTimeout("tcp", endpoint, 450*time.Millisecond)
	if err != nil {
		return false
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(650 * time.Millisecond))
	if _, err = connection.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return false
	}
	reply := make([]byte, 2)
	_, err = io.ReadFull(connection, reply)
	return err == nil && reply[0] == 0x05
}

func probeHTTPProxy(endpoint string) bool {
	connection, err := net.DialTimeout("tcp", endpoint, 450*time.Millisecond)
	if err != nil {
		return false
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(650 * time.Millisecond))
	_, _ = fmt.Fprintf(connection, "CONNECT proxy-detect.invalid:443 HTTP/1.1\r\nHost: proxy-detect.invalid:443\r\n\r\n")
	line, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "HTTP/") {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	code, _ := strconv.Atoi(fields[1])
	return code == http.StatusOK || code == http.StatusProxyAuthRequired ||
		code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

func buildBaselineFindings(result NetworkBaselineResult) []BaselineFinding {
	interfaceCount := len(result.Environment.Interfaces)
	interfaceStatus := "healthy"
	interfaceTitle := "活动接口正常"
	interfaceDetail := fmt.Sprintf("检测到 %d 个启用中的非回环接口", interfaceCount)
	if interfaceCount == 0 {
		interfaceStatus, interfaceTitle, interfaceDetail = "critical", "未发现活动接口", "系统未返回可用的非回环网络接口"
	} else if result.LocalNetwork.Status == "warning" || result.LocalNetwork.Status == "critical" {
		interfaceStatus = result.LocalNetwork.Status
		interfaceTitle = "本地网络存在关注项"
		interfaceDetail = result.LocalNetwork.Summary
	} else if result.LocalNetwork.Summary != "" {
		interfaceDetail = result.LocalNetwork.Summary
	}

	dnsStatus, dnsTitle := "healthy", "DNS 解析可用"
	dnsDetail := fmt.Sprintf("%d/%d 个解析器返回结果", result.DNSComparison.SuccessfulResolvers, len(result.DNSProbes))
	if result.DNSComparison.SuccessfulResolvers == 0 {
		dnsStatus, dnsTitle = "critical", "DNS 解析失败"
	} else if result.DNSComparison.SuccessfulResolvers < len(result.DNSProbes) || result.DNSComparison.Different {
		dnsStatus, dnsTitle = "warning", "DNS 结果存在差异"
		dnsDetail += "；可能来自 CDN 调度或解析器策略差异"
	}

	availableStacks := 0
	for _, stack := range result.IPStacks {
		if stack.Available {
			availableStacks++
		}
	}
	exitStatus, exitTitle := "healthy", "公网直连可用"
	exitDetail := fmt.Sprintf("IPv4 / IPv6 中有 %d 条协议栈通过公网直连 TCP 验证", availableStacks)
	if availableStacks == 0 {
		if len(result.ProxyListeners) > 0 {
			exitStatus, exitTitle = "warning", "公网访问依赖代理"
			exitDetail += "；已发现本地监听端点"
		} else {
			exitStatus, exitTitle = "critical", "公网直连不可用"
		}
	} else if availableStacks == 1 {
		exitStatus, exitTitle = "warning", "公网直连部分可用"
	}

	proxyStatus, proxyTitle := "healthy", "未识别到本地代理监听"
	proxyDetail := "常见本地代理端口未响应"
	if len(result.ProxyListeners) > 0 {
		proxyStatus, proxyTitle = "warning", "识别到本地代理或 TCP 服务"
		proxyDetail = fmt.Sprintf("%d 个端点正在监听，请结合连接策略判断是否接管流量", len(result.ProxyListeners))
	}
	if result.LocalNetwork.SystemProxyConfigured {
		proxyStatus, proxyTitle = "warning", "系统代理已配置"
		proxyDetail = fmt.Sprintf("系统连接策略指向 %s；%s", result.LocalNetwork.SystemProxyEndpoint, result.LocalNetwork.Correlation)
	} else if result.LocalNetwork.RouteTakeover {
		proxyStatus, proxyTitle = "warning", "默认路由由隧道接口接管"
		proxyDetail = result.LocalNetwork.Correlation
	}
	return []BaselineFinding{
		{Layer: "local", Status: interfaceStatus, Title: interfaceTitle, Detail: interfaceDetail},
		{Layer: "dns", Status: dnsStatus, Title: dnsTitle, Detail: dnsDetail},
		{Layer: "egress", Status: exitStatus, Title: exitTitle, Detail: exitDetail},
		{Layer: "proxy", Status: proxyStatus, Title: proxyTitle, Detail: proxyDetail},
	}
}

func buildLocalNetworkAssessment(result *NetworkBaselineResult) LocalNetworkAssessment {
	assessment := LocalNetworkAssessment{Status: "healthy", GatewayBindingComplete: len(result.Routes) > 0}
	interfaces := result.Environment.Interfaces
	interfaceIndex := make(map[string]int, len(interfaces))
	for index := range interfaces {
		interfaceIndex[interfaces[index].Name] = index
		if interfaces[index].Virtual {
			assessment.VirtualInterfaces = appendUnique(assessment.VirtualInterfaces, interfaces[index].Name)
		}
		if interfaces[index].Tunnel {
			assessment.TunnelInterfaces = appendUnique(assessment.TunnelInterfaces, interfaces[index].Name)
		}
	}

	routesByFamily := map[string]int{}
	primaryByFamily := make(map[string]DefaultRoute)
	for _, route := range result.Routes {
		assessment.DefaultRouteCount++
		routesByFamily[route.Family]++
		if route.Interface == "" {
			assessment.GatewayBindingComplete = false
			continue
		}
		index, found := interfaceIndex[route.Interface]
		if !found {
			assessment.GatewayBindingComplete = false
			continue
		}
		interfaces[index].DefaultRoute = true
		interfaces[index].Active = true
		assessment.ActiveInterfaces = appendUnique(assessment.ActiveInterfaces, route.Interface)
		current, exists := primaryByFamily[route.Family]
		if !exists || (route.Selected && !current.Selected) || (route.Selected == current.Selected && routeMetric(route) < routeMetric(current)) {
			primaryByFamily[route.Family] = route
		}
	}
	for _, stack := range result.IPStacks {
		if stack.Interface == "" {
			continue
		}
		assessment.ActiveInterfaces = appendUnique(assessment.ActiveInterfaces, stack.Interface)
		if index, found := interfaceIndex[stack.Interface]; found {
			interfaces[index].Active = true
		}
	}
	result.Environment.Interfaces = interfaces
	for _, item := range interfaces {
		if item.Active && item.MTURisk {
			assessment.MTURisks = appendUnique(assessment.MTURisks, fmt.Sprintf("%s (%d)", item.Name, item.MTU))
		}
	}
	assessment.IPv4DefaultRoutes = routesByFamily["IPv4"]
	assessment.IPv6DefaultRoutes = routesByFamily["IPv6"]
	assessment.MultipleDefaultRoutes = assessment.IPv4DefaultRoutes > 1 || assessment.IPv6DefaultRoutes > 1
	for _, route := range primaryByFamily {
		if index, found := interfaceIndex[route.Interface]; found && interfaces[index].Tunnel {
			assessment.RouteTakeover = true
		}
	}

	assessment.SystemProxyEndpoint = configuredSystemProxyEndpoint(result.TargetHost)
	assessment.SystemProxyConfigured = assessment.SystemProxyEndpoint != ""
	assessment.DNSMode = classifyDNSMode(result.Environment.DNSServers)
	switch {
	case assessment.RouteTakeover && assessment.SystemProxyConfigured:
		assessment.Correlation = "默认路由和系统代理同时接管流量，排障时应分别验证隧道出口与应用代理"
	case assessment.RouteTakeover:
		assessment.Correlation = "默认路由经 VPN/TUN 接口转发，强制直连仍可能经过该隧道"
	case assessment.SystemProxyConfigured:
		assessment.Correlation = "应用请求可能使用系统代理；路由追踪只反映本机网络栈路径"
	case len(result.ProxyListeners) > 0:
		assessment.Correlation = "检测到本地代理服务，但未证明系统流量已经使用该端点"
	default:
		assessment.Correlation = "未发现显式代理或默认路由接管证据"
	}

	problems := make([]string, 0, 4)
	if len(assessment.ActiveInterfaces) == 0 {
		assessment.Status = "critical"
		problems = append(problems, "未能绑定活动出口接口")
	}
	if !assessment.GatewayBindingComplete {
		assessment.Status = maxBaselineStatus(assessment.Status, "warning")
		problems = append(problems, "部分默认路由未匹配到活动接口")
	}
	if assessment.MultipleDefaultRoutes {
		assessment.Status = maxBaselineStatus(assessment.Status, "warning")
		problems = append(problems, "同一协议族存在多条默认路由")
	}
	if assessment.RouteTakeover {
		assessment.Status = maxBaselineStatus(assessment.Status, "warning")
		problems = append(problems, "默认出口由 VPN/TUN 接管")
	}
	if len(assessment.MTURisks) > 0 {
		assessment.Status = maxBaselineStatus(assessment.Status, "warning")
		problems = append(problems, "活动接口 MTU 超出常用安全范围")
	}
	if len(problems) == 0 {
		assessment.Summary = fmt.Sprintf("默认路由已绑定到 %s，%s", strings.Join(assessment.ActiveInterfaces, ", "), assessment.Correlation)
	} else {
		assessment.Summary = strings.Join(problems, "；")
	}
	return assessment
}

func routeMetric(route DefaultRoute) int {
	return route.Metric
}

func configuredSystemProxyEndpoint(targetHost string) string {
	request, err := http.NewRequest(http.MethodGet, "https://"+targetHost+"/", nil)
	if err != nil {
		return ""
	}
	configured, err := systemProxyForRequest(request)
	if err != nil || configured == nil || configured.Host == "" {
		return ""
	}
	return configured.Scheme + "://" + configured.Host
}

func classifyDNSMode(servers []string) string {
	if len(servers) == 0 {
		return "未获取"
	}
	hasLoopback, hasPrivate, hasPublic := false, false, false
	for _, raw := range servers {
		ip := net.ParseIP(raw)
		if ip == nil {
			continue
		}
		switch {
		case ip.IsLoopback():
			hasLoopback = true
		case ip.IsPrivate() || ip.IsLinkLocalUnicast():
			hasPrivate = true
		default:
			hasPublic = true
		}
	}
	parts := make([]string, 0, 3)
	if hasLoopback {
		parts = append(parts, "本地转发")
	}
	if hasPrivate {
		parts = append(parts, "局域网/VPN")
	}
	if hasPublic {
		parts = append(parts, "公共 DNS")
	}
	if len(parts) == 0 {
		return "未分类"
	}
	return strings.Join(parts, " + ")
}

func maxBaselineStatus(left, right string) string {
	severity := map[string]int{"healthy": 0, "warning": 1, "critical": 2}
	if severity[right] > severity[left] {
		return right
	}
	return left
}

func buildBaselineRemediations(result NetworkBaselineResult) []BaselineRemediation {
	assessment := result.LocalNetwork
	remediations := make([]BaselineRemediation, 0, 6)
	if !assessment.GatewayBindingComplete || assessment.DefaultRouteCount == 0 {
		remediations = append(remediations, BaselineRemediation{Action: "inspect_routes", Title: "核对默认路由与网关绑定", Detail: "检查活动接口是否存在对应的 IPv4/IPv6 默认路由，并确认网关、接口与路由优先级符合预期。"})
	}
	if assessment.MultipleDefaultRoutes {
		remediations = append(remediations, BaselineRemediation{Action: "compare_route_metrics", Title: "核对多默认路由优先级", Detail: "确认多个出口的 metric 和策略路由，避免流量在 Wi-Fi、有线网络或虚拟接口之间漂移。"})
	}
	if assessment.RouteTakeover {
		remediations = append(remediations, BaselineRemediation{Action: "verify_tunnel", Title: "关闭 VPN/TUN 后建立对照基线", Detail: "先记录当前结果，再暂时关闭隧道复测；强制直连只能绕过显式代理，不能绕过系统级路由接管。"})
	}
	if len(assessment.MTURisks) > 0 {
		remediations = append(remediations, BaselineRemediation{Action: "verify_mtu", Title: "核验活动接口 MTU", Detail: fmt.Sprintf("关注 %s；结合隧道封装和上游链路核对 MTU，避免分片或 PMTU 黑洞。", strings.Join(assessment.MTURisks, ", "))})
	}
	if assessment.SystemProxyConfigured {
		remediations = append(remediations, BaselineRemediation{Action: "compare_proxy", Title: "分别验证系统代理与强制直连", Detail: "对同一目标运行路径对照，确认故障来自代理端点、隧道路由还是目标服务。"})
	}
	if result.DNSComparison.Different || result.DNSComparison.SuccessfulResolvers < len(result.DNSProbes) {
		remediations = append(remediations, BaselineRemediation{Action: "diagnose_dns", Title: "运行 DNS 深度诊断", Detail: "比较系统 DNS、公共 UDP DNS 与 DoH，区分 CDN 调度差异、通道阻断和本地重写。"})
	}
	return remediations
}

func interfaceForIP(raw string) string {
	wanted := net.ParseIP(raw)
	if wanted == nil {
		return ""
	}
	interfaces, _ := net.Interfaces()
	for _, item := range interfaces {
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			if addressContainsIP(address.String(), wanted.String()) {
				return item.Name
			}
		}
	}
	return ""
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(message) > 180 {
		return message[:180]
	}
	return message
}
