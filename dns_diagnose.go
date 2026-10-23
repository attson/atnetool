package main

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	dnsDiagnoseTimeout = 15 * time.Second
	dnsQueryTimeout    = 5 * time.Second
	maxDNSMessageSize  = 64 << 10
)

type DNSDiagnoseOptions struct {
	TaskID string `json:"task_id"`
	Domain string `json:"domain"`
}

type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	TTL   uint32 `json:"ttl"`
}

type DNSQueryResult struct {
	Type          string      `json:"type"`
	Responded     bool        `json:"responded"`
	Success       bool        `json:"success"`
	RCode         string      `json:"rcode,omitempty"`
	DurationMS    int64       `json:"duration_ms"`
	AuthenticData bool        `json:"authentic_data"`
	Truncated     bool        `json:"truncated"`
	Records       []DNSRecord `json:"records,omitempty"`
	Error         string      `json:"error,omitempty"`
}

type DNSResolverResult struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Transport  string           `json:"transport"`
	Endpoint   string           `json:"endpoint,omitempty"`
	Reachable  bool             `json:"reachable"`
	DurationMS int64            `json:"duration_ms"`
	Queries    []DNSQueryResult `json:"queries"`
}

type DNSDeepComparison struct {
	ReachableResolvers  int  `json:"reachable_resolvers"`
	PublicResolvers     int  `json:"public_resolvers"`
	SystemAvailable     bool `json:"system_available"`
	UDPAvailable        bool `json:"udp_available"`
	DoHAvailable        bool `json:"doh_available"`
	SystemMatchesPublic bool `json:"system_matches_public"`
	PublicAddressSets   int  `json:"public_address_sets"`
	AddressDifference   bool `json:"address_difference"`
	PrivateRewriteRisk  bool `json:"private_rewrite_risk"`
	DNSSECObserved      bool `json:"dnssec_observed"`
}

type DNSFinding struct {
	Code   string `json:"code"`
	Layer  string `json:"layer"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type DNSRemediation struct {
	Action string `json:"action"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

type DNSDiagnoseProgress struct {
	TaskID   string            `json:"task_id"`
	Complete int               `json:"complete"`
	Total    int               `json:"total"`
	Resolver DNSResolverResult `json:"resolver"`
}

type DNSDiagnoseResult struct {
	SchemaVersion int                 `json:"schema_version"`
	TaskID        string              `json:"task_id"`
	Status        string              `json:"status"`
	Message       string              `json:"message"`
	Domain        string              `json:"domain"`
	StartedAt     string              `json:"started_at"`
	FinishedAt    string              `json:"finished_at"`
	DurationMS    int64               `json:"duration_ms"`
	Completed     int                 `json:"completed"`
	Total         int                 `json:"total"`
	Resolvers     []DNSResolverResult `json:"resolvers"`
	Comparison    DNSDeepComparison   `json:"comparison"`
	Findings      []DNSFinding        `json:"findings"`
	Remediations  []DNSRemediation    `json:"remediations"`
	Environment   NetworkEnvironment  `json:"environment"`
}

type dnsResolverDefinition struct {
	id        string
	name      string
	transport string
	endpoint  string
}

var dnsDeepResolvers = []dnsResolverDefinition{
	{id: "system", name: "系统 DNS", transport: "system"},
	{id: "cloudflare-udp", name: "Cloudflare DNS", transport: "udp", endpoint: "1.1.1.1:53"},
	{id: "alidns-udp", name: "AliDNS", transport: "udp", endpoint: "223.5.5.5:53"},
	{id: "cloudflare-doh", name: "Cloudflare DoH", transport: "doh", endpoint: "https://cloudflare-dns.com/dns-query"},
	{id: "google-doh", name: "Google DoH", transport: "doh", endpoint: "https://dns.google/dns-query"},
}

var dnsDeepQueryTypes = []dnsmessage.Type{
	dnsmessage.TypeA,
	dnsmessage.TypeAAAA,
	dnsmessage.TypeCNAME,
	dnsmessage.TypeNS,
}

func (a *App) RunDNSDiagnose(options DNSDiagnoseOptions) DNSDiagnoseResult {
	started := time.Now()
	taskID := strings.TrimSpace(options.TaskID)
	if taskID == "" {
		taskID = newTaskID()
	}
	result := DNSDiagnoseResult{
		SchemaVersion: 1,
		TaskID:        taskID,
		Status:        "running",
		Message:       "DNS 深度诊断未完成",
		Domain:        normaliseBaselineHost(options.Domain),
		StartedAt:     started.Format(time.RFC3339),
		Total:         len(dnsDeepResolvers),
	}
	if result.Domain == "" {
		result.Status = "failed"
		result.Message = "请输入有效的域名"
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
	ctx, stopAtTimeout := context.WithTimeout(baseContext, dnsDiagnoseTimeout)
	defer stopAtTimeout()

	type completedResolver struct {
		index    int
		resolver DNSResolverResult
	}
	completed := make(chan completedResolver, len(dnsDeepResolvers))
	for index, definition := range dnsDeepResolvers {
		index, definition := index, definition
		go func() {
			completed <- completedResolver{index: index, resolver: runDNSResolver(ctx, result.Domain, definition)}
		}()
	}
	result.Resolvers = make([]DNSResolverResult, len(dnsDeepResolvers))
	for range dnsDeepResolvers {
		item := <-completed
		result.Resolvers[item.index] = item.resolver
		result.Completed++
		a.emitDNSDiagnoseProgress(DNSDiagnoseProgress{
			TaskID: taskID, Complete: result.Completed, Total: result.Total, Resolver: item.resolver,
		})
	}

	result.Comparison = compareDNSDeepResults(result.Resolvers)
	result.Findings, result.Remediations = buildDNSDeepFindings(result.Comparison, result.Resolvers)
	if baseContext.Err() != nil {
		result.Status = "cancelled"
		result.Message = "DNS 深度诊断已停止"
	} else if ctx.Err() == context.DeadlineExceeded {
		result.Status = "timeout"
		result.Message = "DNS 深度诊断超时，已保留完成结果"
	} else {
		result.Status = "completed"
		result.Message = "DNS 深度诊断完成"
	}
	result.DurationMS = durationMS(time.Since(started))
	result.FinishedAt = time.Now().Format(time.RFC3339)
	result.Environment = captureNetworkEnvironment(DiagResult{ProxyMode: proxyModeSystem})
	return result
}

func runDNSResolver(parent context.Context, domain string, definition dnsResolverDefinition) DNSResolverResult {
	started := time.Now()
	result := DNSResolverResult{
		ID: definition.id, Name: definition.name, Transport: definition.transport, Endpoint: definition.endpoint,
		Queries: make([]DNSQueryResult, len(dnsDeepQueryTypes)),
	}
	var wait sync.WaitGroup
	for index, queryType := range dnsDeepQueryTypes {
		index, queryType := index, queryType
		wait.Add(1)
		go func() {
			defer wait.Done()
			ctx, cancel := context.WithTimeout(parent, dnsQueryTimeout)
			defer cancel()
			switch definition.transport {
			case "system":
				result.Queries[index] = runSystemDNSQuery(ctx, domain, queryType)
			case "doh":
				result.Queries[index] = runWireDNSQuery(ctx, domain, queryType, func(ctx context.Context, message []byte) ([]byte, error) {
					return exchangeDoH(ctx, definition.endpoint, message)
				})
			default:
				result.Queries[index] = runWireDNSQuery(ctx, domain, queryType, func(ctx context.Context, message []byte) ([]byte, error) {
					return exchangeDNS(ctx, definition.endpoint, message)
				})
			}
		}()
	}
	wait.Wait()
	result.DurationMS = durationMS(time.Since(started))
	for _, query := range result.Queries {
		if query.Responded {
			result.Reachable = true
			break
		}
	}
	return result
}

func runSystemDNSQuery(ctx context.Context, domain string, queryType dnsmessage.Type) DNSQueryResult {
	started := time.Now()
	result := DNSQueryResult{Type: dnsTypeLabel(queryType)}
	appendNameRecord := func(recordType, value string) {
		value = strings.TrimSuffix(strings.ToLower(value), ".")
		if value != "" {
			result.Records = append(result.Records, DNSRecord{Type: recordType, Name: domain, Value: value})
		}
	}
	var err error
	switch queryType {
	case dnsmessage.TypeA, dnsmessage.TypeAAAA:
		network := "ip4"
		if queryType == dnsmessage.TypeAAAA {
			network = "ip6"
		}
		var addresses []net.IP
		addresses, err = net.DefaultResolver.LookupIP(ctx, network, domain)
		for _, address := range addresses {
			appendNameRecord(result.Type, address.String())
		}
	case dnsmessage.TypeCNAME:
		var canonical string
		canonical, err = net.DefaultResolver.LookupCNAME(ctx, domain)
		canonical = strings.TrimSuffix(strings.ToLower(canonical), ".")
		if canonical != "" && canonical != domain {
			appendNameRecord("CNAME", canonical)
		}
	case dnsmessage.TypeNS:
		var servers []*net.NS
		servers, err = net.DefaultResolver.LookupNS(ctx, domain)
		for _, server := range servers {
			appendNameRecord("NS", server.Host)
		}
	}
	result.DurationMS = durationMS(time.Since(started))
	if err != nil {
		var dnsError *net.DNSError
		if errors.As(err, &dnsError) && dnsError.IsNotFound {
			result.Responded = true
			result.RCode = "NO DATA"
		} else {
			result.Error = compactError(err)
		}
		return result
	}
	result.Responded = true
	result.Success = true
	result.RCode = "NOERROR"
	sortDNSRecords(result.Records)
	return result
}

func runWireDNSQuery(ctx context.Context, domain string, queryType dnsmessage.Type, exchange func(context.Context, []byte) ([]byte, error)) DNSQueryResult {
	started := time.Now()
	result := DNSQueryResult{Type: dnsTypeLabel(queryType)}
	request, requestID, err := buildDNSQuery(domain, queryType)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	response, err := exchange(ctx, request)
	result.DurationMS = durationMS(time.Since(started))
	if err != nil {
		result.Error = compactError(err)
		return result
	}
	var message dnsmessage.Message
	if err := message.Unpack(response); err != nil {
		result.Error = "DNS 响应无法解析: " + compactError(err)
		return result
	}
	if message.ID != requestID || !message.Response {
		result.Error = "DNS 响应标识不匹配"
		return result
	}
	result.Responded = true
	result.RCode = dnsRCodeLabel(message.RCode)
	result.AuthenticData = message.AuthenticData
	result.Truncated = message.Truncated
	result.Success = message.RCode == dnsmessage.RCodeSuccess
	if !result.Success {
		result.Error = "DNS 返回 " + result.RCode
	}
	for _, resource := range append(message.Answers, message.Authorities...) {
		if record, ok := dnsRecordFromResource(resource); ok {
			result.Records = appendUniqueDNSRecord(result.Records, record)
		}
	}
	sortDNSRecords(result.Records)
	return result
}

func buildDNSQuery(domain string, queryType dnsmessage.Type) ([]byte, uint16, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(domain, ".") + ".")
	if err != nil {
		return nil, 0, err
	}
	var idBytes [2]byte
	if _, err := cryptorand.Read(idBytes[:]); err != nil {
		return nil, 0, err
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	message := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: name, Type: queryType, Class: dnsmessage.ClassINET}},
	}
	packed, err := message.Pack()
	return packed, id, err
}

func exchangeDNS(ctx context.Context, endpoint string, message []byte) ([]byte, error) {
	response, truncated, err := exchangeDNSNetwork(ctx, "udp", endpoint, message)
	if err != nil || !truncated {
		return response, err
	}
	response, _, err = exchangeDNSNetwork(ctx, "tcp", endpoint, message)
	return response, err
}

func exchangeDNSNetwork(ctx context.Context, network, endpoint string, message []byte) ([]byte, bool, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, endpoint)
	if err != nil {
		return nil, false, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	if network == "tcp" {
		framed := make([]byte, len(message)+2)
		binary.BigEndian.PutUint16(framed[:2], uint16(len(message)))
		copy(framed[2:], message)
		if _, err := connection.Write(framed); err != nil {
			return nil, false, err
		}
		var length [2]byte
		if _, err := io.ReadFull(connection, length[:]); err != nil {
			return nil, false, err
		}
		size := int(binary.BigEndian.Uint16(length[:]))
		if size == 0 || size > maxDNSMessageSize {
			return nil, false, fmt.Errorf("invalid DNS response size %d", size)
		}
		response := make([]byte, size)
		_, err := io.ReadFull(connection, response)
		return response, false, err
	}
	if _, err := connection.Write(message); err != nil {
		return nil, false, err
	}
	response := make([]byte, maxDNSMessageSize)
	count, err := connection.Read(response)
	if err != nil {
		return nil, false, err
	}
	response = response[:count]
	var parsed dnsmessage.Message
	if err := parsed.Unpack(response); err != nil {
		return response, false, nil
	}
	return response, parsed.Truncated, nil
}

func exchangeDoH(ctx context.Context, endpoint string, message []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(message))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-message")
	request.Header.Set("Content-Type", "application/dns-message")
	request.Header.Set("User-Agent", "ATNetool/1.0")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.Proxy = systemProxyForRequest
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: dnsQueryTimeout}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH HTTP status %d", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); !strings.Contains(contentType, "application/dns-message") {
		return nil, fmt.Errorf("unexpected DoH content type %q", contentType)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDNSMessageSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDNSMessageSize {
		return nil, fmt.Errorf("DoH response is too large")
	}
	return body, nil
}

func dnsRecordFromResource(resource dnsmessage.Resource) (DNSRecord, bool) {
	record := DNSRecord{
		Type: dnsTypeLabel(resource.Header.Type),
		Name: strings.TrimSuffix(strings.ToLower(resource.Header.Name.String()), "."),
		TTL:  resource.Header.TTL,
	}
	switch body := resource.Body.(type) {
	case *dnsmessage.AResource:
		record.Value = net.IP(body.A[:]).String()
	case *dnsmessage.AAAAResource:
		record.Value = net.IP(body.AAAA[:]).String()
	case *dnsmessage.CNAMEResource:
		record.Value = strings.TrimSuffix(strings.ToLower(body.CNAME.String()), ".")
	case *dnsmessage.NSResource:
		record.Value = strings.TrimSuffix(strings.ToLower(body.NS.String()), ".")
	default:
		return DNSRecord{}, false
	}
	return record, record.Value != ""
}

func compareDNSDeepResults(resolvers []DNSResolverResult) DNSDeepComparison {
	comparison := DNSDeepComparison{}
	publicSets := make(map[string]struct{})
	publicAddressValues := make(map[string]struct{})
	var systemAddresses []string
	for _, resolver := range resolvers {
		if resolver.Reachable {
			comparison.ReachableResolvers++
		}
		switch resolver.Transport {
		case "system":
			comparison.SystemAvailable = resolver.Reachable
			systemAddresses = resolverAddressValues(resolver)
		case "udp":
			comparison.UDPAvailable = comparison.UDPAvailable || resolver.Reachable
		case "doh":
			comparison.DoHAvailable = comparison.DoHAvailable || resolver.Reachable
		}
		if resolver.Transport == "system" || !resolver.Reachable {
			continue
		}
		comparison.PublicResolvers++
		addresses := resolverAddressValues(resolver)
		if len(addresses) > 0 {
			publicSets[strings.Join(addresses, "|")] = struct{}{}
			for _, address := range addresses {
				publicAddressValues[address] = struct{}{}
			}
		}
		for _, query := range resolver.Queries {
			if query.AuthenticData {
				comparison.DNSSECObserved = true
			}
		}
	}
	comparison.PublicAddressSets = len(publicSets)
	comparison.AddressDifference = comparison.PublicAddressSets > 1
	for _, address := range systemAddresses {
		if _, found := publicAddressValues[address]; found {
			comparison.SystemMatchesPublic = true
		}
		parsed := net.ParseIP(address)
		if parsed != nil && (parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast()) && !publicContainsPrivate(publicAddressValues) {
			comparison.PrivateRewriteRisk = true
		}
	}
	return comparison
}

func buildDNSDeepFindings(comparison DNSDeepComparison, resolvers []DNSResolverResult) ([]DNSFinding, []DNSRemediation) {
	findings := make([]DNSFinding, 0, 4)
	remediations := make([]DNSRemediation, 0, 3)
	if !comparison.SystemAvailable && comparison.PublicResolvers > 0 {
		findings = append(findings, DNSFinding{Code: "system_dns_failed", Layer: "availability", Status: "critical", Title: "系统 DNS 解析异常", Detail: "公共解析器可以响应，但当前系统解析器未返回有效响应。"})
		remediations = append(remediations, DNSRemediation{Action: "change_dns", Title: "检查或更换系统 DNS", Detail: "检查网卡 DNS、VPN 与安全软件配置，切换后清理本机 DNS 缓存并复测。"})
	} else if comparison.SystemAvailable {
		findings = append(findings, DNSFinding{Code: "system_dns_ok", Layer: "availability", Status: "healthy", Title: "系统 DNS 可用", Detail: fmt.Sprintf("共 %d/%d 个解析来源返回响应。", comparison.ReachableResolvers, len(resolvers))})
	} else {
		findings = append(findings, DNSFinding{Code: "all_dns_failed", Layer: "availability", Status: "critical", Title: "所有 DNS 路径均不可用", Detail: "系统 DNS、公共 UDP DNS 与 DoH 均未返回有效响应。"})
		remediations = append(remediations, DNSRemediation{Action: "check_network", Title: "先恢复基础网络连接", Detail: "检查默认路由、代理和防火墙，再运行网络基线确认公网出口。"})
	}

	if comparison.DoHAvailable && !comparison.UDPAvailable {
		findings = append(findings, DNSFinding{Code: "udp_dns_blocked", Layer: "transport", Status: "warning", Title: "传统 DNS 通道不可用", Detail: "DoH 可用但公共 UDP/53 无响应，可能受到网络策略或防火墙限制。"})
		remediations = append(remediations, DNSRemediation{Action: "use_doh", Title: "验证加密 DNS 策略", Detail: "可在浏览器或系统中临时启用可信 DoH，复测前确认组织网络策略允许。"})
	} else if comparison.UDPAvailable && !comparison.DoHAvailable {
		findings = append(findings, DNSFinding{Code: "doh_unavailable", Layer: "transport", Status: "warning", Title: "DoH 通道不可用", Detail: "传统 DNS 可响应，但 HTTPS 加密 DNS 请求失败，可能受代理或访问策略影响。"})
	} else if comparison.UDPAvailable && comparison.DoHAvailable {
		findings = append(findings, DNSFinding{Code: "dns_transports_ok", Layer: "transport", Status: "healthy", Title: "DNS 传输通道正常", Detail: "公共 UDP DNS 与 DoH 均可访问。"})
	}
	unreachable := make([]string, 0)
	for _, resolver := range resolvers {
		if resolver.Transport != "system" && !resolver.Reachable {
			unreachable = append(unreachable, resolver.Name)
		}
	}
	if len(unreachable) > 0 && comparison.PublicResolvers > 0 {
		findings = append(findings, DNSFinding{Code: "partial_resolver_failure", Layer: "transport", Status: "warning", Title: "部分公共解析器不可达", Detail: strings.Join(unreachable, "、") + " 未响应，可能受到路由、代理或网络策略限制。"})
		remediations = append(remediations, DNSRemediation{Action: "verify_policy", Title: "核对 DNS 出口策略", Detail: "确认 UDP/53 与目标 DoH 域名的访问策略；保留至少两个可用解析来源后再判断业务影响。"})
	}

	if comparison.PrivateRewriteRisk {
		findings = append(findings, DNSFinding{Code: "private_rewrite_risk", Layer: "integrity", Status: "warning", Title: "系统解析可能被本地重写", Detail: "系统 DNS 返回私网或回环地址，而公共解析器未返回同类地址；企业内网分流也可能产生该结果。"})
		remediations = append(remediations, DNSRemediation{Action: "inspect_override", Title: "检查本地解析接管", Detail: "检查 hosts、代理 DNS、VPN、广告过滤与安全软件规则，确认该私网映射是否符合预期。"})
	} else if comparison.SystemAvailable && comparison.PublicResolvers > 0 && !comparison.SystemMatchesPublic {
		findings = append(findings, DNSFinding{Code: "system_public_difference", Layer: "integrity", Status: "warning", Title: "系统与公共解析结果不同", Detail: "结果可能来自 CDN 调度、运营商策略或本地解析接管，需要结合 CNAME 与业务地域判断。"})
		remediations = append(remediations, DNSRemediation{Action: "compare_routes", Title: "结合路径与地域继续验证", Detail: "对不同解析地址执行域名诊断或 CF IP 优选，不要仅凭 IP 不同判定污染。"})
	} else if comparison.SystemMatchesPublic {
		findings = append(findings, DNSFinding{Code: "system_public_match", Layer: "integrity", Status: "healthy", Title: "系统解析与公共结果存在交集", Detail: "未发现明确的本地解析重写证据。"})
	}

	if comparison.AddressDifference {
		findings = append(findings, DNSFinding{Code: "public_variants", Layer: "scheduling", Status: "neutral", Title: "公共解析器返回不同地址组", Detail: "这通常由 CDN 地域调度、Anycast 或缓存时点差异造成，不直接视为异常。"})
	} else if comparison.PublicResolvers > 0 {
		findings = append(findings, DNSFinding{Code: "public_consensus", Layer: "scheduling", Status: "healthy", Title: "公共解析结果一致", Detail: "可用公共解析器返回相同的地址集合。"})
	}
	return findings, remediations
}

func resolverAddressValues(resolver DNSResolverResult) []string {
	values := make([]string, 0)
	for _, query := range resolver.Queries {
		for _, record := range query.Records {
			if record.Type == "A" || record.Type == "AAAA" {
				values = appendUnique(values, record.Value)
			}
		}
	}
	sort.Strings(values)
	return values
}

func publicContainsPrivate(values map[string]struct{}) bool {
	for value := range values {
		address := net.ParseIP(value)
		if address != nil && (address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast()) {
			return true
		}
	}
	return false
}

func appendUniqueDNSRecord(records []DNSRecord, candidate DNSRecord) []DNSRecord {
	for index := range records {
		if records[index].Type == candidate.Type && records[index].Name == candidate.Name && records[index].Value == candidate.Value {
			if candidate.TTL < records[index].TTL {
				records[index].TTL = candidate.TTL
			}
			return records
		}
	}
	return append(records, candidate)
}

func sortDNSRecords(records []DNSRecord) {
	sort.Slice(records, func(left, right int) bool {
		if records[left].Type != records[right].Type {
			return records[left].Type < records[right].Type
		}
		if records[left].Name != records[right].Name {
			return records[left].Name < records[right].Name
		}
		return records[left].Value < records[right].Value
	})
}

func dnsTypeLabel(queryType dnsmessage.Type) string {
	switch queryType {
	case dnsmessage.TypeA:
		return "A"
	case dnsmessage.TypeAAAA:
		return "AAAA"
	case dnsmessage.TypeCNAME:
		return "CNAME"
	case dnsmessage.TypeNS:
		return "NS"
	default:
		return queryType.String()
	}
}

func dnsRCodeLabel(code dnsmessage.RCode) string {
	switch code {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeRefused:
		return "REFUSED"
	default:
		return code.String()
	}
}

func (a *App) emitDNSDiagnoseProgress(progress DNSDiagnoseProgress) {
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "dns-diagnose:resolver", progress)
	}
}
