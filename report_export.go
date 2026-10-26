package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const reportFormatVersion = 1

var unsafeReportFilenameCharacters = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

type historyReportSummary struct {
	FormatVersion int                    `json:"format_version"`
	Application   string                 `json:"application"`
	AppVersion    string                 `json:"app_version"`
	GeneratedAt   string                 `json:"generated_at"`
	HistoryID     string                 `json:"history_id"`
	Kind          string                 `json:"kind"`
	KindLabel     string                 `json:"kind_label"`
	CheckedAt     string                 `json:"checked_at"`
	Target        string                 `json:"target"`
	Success       bool                   `json:"success"`
	Conclusion    string                 `json:"conclusion"`
	Metrics       map[string]interface{} `json:"metrics,omitempty"`
	Proxy         map[string]interface{} `json:"proxy,omitempty"`
	Traceroute    map[string]interface{} `json:"traceroute,omitempty"`
}

// ExportHistoryReport writes a privacy-filtered diagnosis package selected by
// the user. An empty path means the native save dialog was cancelled.
func (a *App) ExportHistoryReport(record HistoryRecord) (string, error) {
	bundle, err := buildHistoryReportBundle(record)
	if err != nil {
		return "", err
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出诊断报告包",
		DefaultFilename: historyReportFilename(record),
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "ZIP 诊断报告 (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil || path == "" {
		return path, err
	}
	if !strings.EqualFold(filepathExtension(path), ".zip") {
		path += ".zip"
	}
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		return "", fmt.Errorf("write diagnosis report: %w", err)
	}
	return path, nil
}

func buildHistoryReportBundle(record HistoryRecord) ([]byte, error) {
	if err := validateHistoryReportRecord(record); err != nil {
		return nil, err
	}
	sanitised, err := sanitiseHistoryRecord(record)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]interface{}{
		"summary.json":              buildHistoryReportSummary(sanitised),
		"evidence/raw-result.json":  sanitised,
		"evidence/environment.json": sanitised.Environment,
	}
	if sanitised.Traceroute != nil {
		files["evidence/traceroute.json"] = sanitised.Traceroute
	}
	if name, evidence := historyReportKindEvidence(sanitised); name != "" && evidence != nil {
		files["evidence/"+name+".json"] = evidence
	}
	if err := writeReportFile(archive, "README.md", []byte(buildHistoryReportMarkdown(sanitised))); err != nil {
		_ = archive.Close()
		return nil, err
	}
	for _, name := range sortedReportFileNames(files) {
		contents, marshalErr := json.MarshalIndent(files[name], "", "  ")
		if marshalErr != nil {
			_ = archive.Close()
			return nil, fmt.Errorf("encode %s: %w", name, marshalErr)
		}
		contents = append(contents, '\n')
		if err := writeReportFile(archive, name, contents); err != nil {
			_ = archive.Close()
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("finish diagnosis report: %w", err)
	}
	return buffer.Bytes(), nil
}

func writeReportFile(archive *zip.Writer, name string, contents []byte) error {
	file, err := archive.Create(name)
	if err != nil {
		return fmt.Errorf("create report file %s: %w", name, err)
	}
	if _, err := file.Write(contents); err != nil {
		return fmt.Errorf("write report file %s: %w", name, err)
	}
	return nil
}

func validateHistoryReportRecord(record HistoryRecord) error {
	if !isHistoryReportKind(record.Kind) {
		return fmt.Errorf("unsupported history record kind %q", record.Kind)
	}
	if strings.TrimSpace(historyReportTarget(record)) == "" {
		return errors.New("history record has no diagnosis target")
	}
	switch record.Kind {
	case "integrated":
		if record.Integrated == nil {
			return errors.New("integrated diagnosis evidence is missing")
		}
	case "stability":
		if record.Stability == nil {
			return errors.New("stability evidence is missing")
		}
	case "site_patrol":
		if record.SitePatrol == nil {
			return errors.New("site patrol evidence is missing")
		}
	case "cdn_optimize":
		if record.CDNOptimize == nil {
			return errors.New("CF IP selection evidence is missing")
		}
	case "network_baseline":
		if record.NetworkBaseline == nil {
			return errors.New("network baseline evidence is missing")
		}
	case "path_compare":
		if record.PathCompare == nil {
			return errors.New("path comparison evidence is missing")
		}
	case "dns_diagnosis":
		if record.DNSDiagnosis == nil {
			return errors.New("DNS diagnosis evidence is missing")
		}
	case "port_test":
		if record.PortTest == nil {
			return errors.New("port test evidence is missing")
		}
	}
	return nil
}

func isHistoryReportKind(kind string) bool {
	switch kind {
	case "diagnosis", "integrated", "stability", "site_patrol", "cdn_optimize", "network_baseline", "path_compare", "dns_diagnosis", "port_test":
		return true
	default:
		return false
	}
}

func sanitiseHistoryRecord(record HistoryRecord) (HistoryRecord, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return HistoryRecord{}, fmt.Errorf("encode history record for privacy filtering: %w", err)
	}
	var generic interface{}
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return HistoryRecord{}, fmt.Errorf("decode history record for privacy filtering: %w", err)
	}
	generic = sanitiseReportValue("", generic)
	filtered, err := json.Marshal(generic)
	if err != nil {
		return HistoryRecord{}, fmt.Errorf("encode privacy-filtered history record: %w", err)
	}
	var sanitised HistoryRecord
	if err := json.Unmarshal(filtered, &sanitised); err != nil {
		return HistoryRecord{}, fmt.Errorf("decode privacy-filtered history record: %w", err)
	}
	return sanitised, nil
}

func sanitiseReportValue(key string, value interface{}) interface{} {
	if isSensitiveReportKey(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		if _, hasInterfaces := typed["interfaces"]; hasInterfaces && typed["os"] != nil && typed["arch"] != nil {
			if _, hasHostname := typed["hostname"]; hasHostname {
				typed["hostname"] = "[REDACTED]"
			}
		}
		for childKey, childValue := range typed {
			typed[childKey] = sanitiseReportValue(childKey, childValue)
		}
		return typed
	case []interface{}:
		for index, childValue := range typed {
			typed[index] = sanitiseReportValue(key, childValue)
		}
		return typed
	case string:
		return sanitiseReportString(typed)
	default:
		return value
	}
}

func isSensitiveReportKey(key string) bool {
	normalised := strings.ToLower(strings.NewReplacer("-", "_", " ", "_").Replace(key))
	if normalised == "hardware" {
		return true
	}
	for _, fragment := range []string{"password", "passwd", "authorization", "cookie", "token", "secret", "api_key", "access_key", "private_key"} {
		if strings.Contains(normalised, fragment) {
			return true
		}
	}
	return false
}

func sanitiseReportString(value string) string {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return value
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func buildHistoryReportSummary(record HistoryRecord) historyReportSummary {
	trace := record.Traceroute
	if trace == nil && record.Integrated != nil {
		trace = &record.Integrated.HTTP.Traceroute
	}
	diagnosis := historyReportDiagnosis(record)
	summary := historyReportSummary{
		FormatVersion: reportFormatVersion,
		Application:   "ATNetool",
		AppVersion:    appVersion,
		GeneratedAt:   time.Now().Format(time.RFC3339),
		HistoryID:     record.HistoryID,
		Kind:          record.Kind,
		KindLabel:     historyReportKindLabel(record.Kind),
		CheckedAt:     firstNonEmpty(record.CreatedAt, record.Result.CheckedAt),
		Target:        historyReportTarget(record),
		Success:       historyReportSuccess(record),
		Conclusion:    historyReportConclusion(record),
		Metrics:       historyReportMetrics(record),
		Proxy: map[string]interface{}{
			"mode":      diagnosis.ProxyMode,
			"status":    diagnosis.ProxyStatus,
			"detected":  diagnosis.ProxyDetected,
			"proxy_url": diagnosis.ProxyURL,
		},
	}
	if trace != nil {
		summary.Traceroute = map[string]interface{}{
			"success":      trace.Success,
			"reached":      trace.Reached,
			"hop_count":    len(trace.Hops),
			"geo_resolved": trace.GeoResolved,
			"duration_ms":  trace.DurationMS,
		}
	}
	return summary
}

func buildHistoryReportMarkdown(record HistoryRecord) string {
	summary := buildHistoryReportSummary(record)
	diagnosis := historyReportDiagnosis(record)
	var output strings.Builder
	fmt.Fprintf(&output, "# ATNetool 诊断报告\n\n")
	fmt.Fprintf(&output, "> 本报告由本机生成，敏感凭据与 URL 查询参数已脱敏。公网 IP、DNS 结果和路由跳点会保留，以便定位网络问题。\n\n")
	fmt.Fprintf(&output, "## 摘要\n\n")
	writeMarkdownRow(&output, "检测类型", summary.KindLabel)
	writeMarkdownRow(&output, "检测目标", summary.Target)
	writeMarkdownRow(&output, "检测时间", summary.CheckedAt)
	writeMarkdownRow(&output, "总体状态", map[bool]string{true: "正常", false: "需要关注"}[summary.Success])
	writeMarkdownRow(&output, "结论", summary.Conclusion)
	writeMarkdownRow(&output, "报告生成时间", summary.GeneratedAt)
	writeMarkdownRow(&output, "ATNetool 版本", summary.AppVersion)

	fmt.Fprintf(&output, "\n## 关键指标\n\n")
	writeHistoryMetricMarkdown(&output, record)
	if diagnosis.TargetURL != "" {
		fmt.Fprintf(&output, "\n## HTTP、TLS 与 Cloudflare\n\n")
		writeMarkdownRow(&output, "HTTP 状态", statusCodeLabel(diagnosis.StatusCode))
		writeMarkdownRow(&output, "HTTP 协议", valueOrDash(diagnosis.HTTPProtocol))
		writeMarkdownRow(&output, "重定向次数", fmt.Sprintf("%d", diagnosis.RedirectCount))
		writeMarkdownRow(&output, "TLS 版本", valueOrDash(diagnosis.TLSVersion))
		writeMarkdownRow(&output, "TLS ALPN", valueOrDash(diagnosis.TLSALPN))
		writeMarkdownRow(&output, "证书剩余有效期", certificateDaysLabel(diagnosis))
		writeMarkdownRow(&output, "CF-Ray", valueOrDash(diagnosis.CFRay))
		writeMarkdownRow(&output, "Cloudflare 节点", valueOrDash(diagnosis.CFColo))
		writeMarkdownRow(&output, "缓存状态", valueOrDash(diagnosis.CFCacheStatus))
	}
	fmt.Fprintf(&output, "\n## 代理与连接路径\n\n")
	writeMarkdownRow(&output, "代理策略", valueOrDash(diagnosis.ProxyMode))
	writeMarkdownRow(&output, "实际路径", valueOrDash(diagnosis.ProxyStatus))
	writeMarkdownRow(&output, "检测到代理", yesNo(diagnosis.ProxyDetected))
	writeMarkdownRow(&output, "代理地址", valueOrDash(diagnosis.ProxyURL))
	if diagnosis.RemoteIP != "" {
		writeMarkdownRow(&output, "远端 IP", diagnosis.RemoteIP)
	}

	trace := record.Traceroute
	if trace == nil && record.Integrated != nil {
		trace = &record.Integrated.HTTP.Traceroute
	}
	if trace != nil {
		fmt.Fprintf(&output, "\n## Traceroute 链路证据\n\n")
		writeMarkdownRow(&output, "目标", valueOrDash(trace.TargetHost))
		writeMarkdownRow(&output, "跳数", fmt.Sprintf("%d", len(trace.Hops)))
		writeMarkdownRow(&output, "到达目标", yesNo(trace.Reached))
		writeMarkdownRow(&output, "可定位跳点", fmt.Sprintf("%d", trace.GeoResolved))
		writeMarkdownRow(&output, "追踪耗时", milliseconds(trace.DurationMS))
	}

	if record.Environment.OS != "" || record.Environment.Arch != "" || len(record.Environment.Interfaces) > 0 || len(record.Environment.DNSServers) > 0 {
		fmt.Fprintf(&output, "\n## 本机网络环境\n\n")
		writeMarkdownRow(&output, "系统", strings.TrimSpace(record.Environment.OS+" "+record.Environment.Arch))
		writeMarkdownRow(&output, "活动接口", fmt.Sprintf("%d", len(record.Environment.Interfaces)))
		writeMarkdownRow(&output, "DNS 服务器", valueOrDash(strings.Join(record.Environment.DNSServers, ", ")))
		writeMarkdownRow(&output, "环境代理策略", valueOrDash(record.Environment.ProxyMode))
	}

	writeHistoryFindingsMarkdown(&output, record)
	fmt.Fprintf(&output, "\n## 文件说明\n\n- `summary.json`：便于工单系统读取的摘要。\n- `evidence/raw-result.json`：本次历史记录的完整脱敏证据。\n- `evidence/` 下其他文件：按诊断模块拆分的证据。\n")
	return output.String()
}

func writeHistoryMetricMarkdown(output *strings.Builder, record HistoryRecord) {
	metrics := historyReportMetrics(record)
	order := []string{"dns_lookup_ms", "tcp_connect_ms", "tls_handshake_ms", "ttfb_ms", "total_ms", "duration_ms", "availability_percent", "jitter_ms", "success_count", "total_count", "open_count", "responsive_count", "best_ip", "improvement_percent"}
	labels := map[string]string{
		"dns_lookup_ms": "DNS 解析", "tcp_connect_ms": "TCP 建连", "tls_handshake_ms": "TLS 握手", "ttfb_ms": "首包 TTFB", "total_ms": "HTTP 总耗时", "duration_ms": "任务耗时", "availability_percent": "可用率", "jitter_ms": "抖动", "success_count": "成功数", "total_count": "总数", "open_count": "开放端口", "responsive_count": "可响应候选", "best_ip": "优选 IP", "improvement_percent": "预计提升",
	}
	for _, key := range order {
		value, exists := metrics[key]
		if !exists {
			continue
		}
		unit := ""
		if strings.HasSuffix(key, "_ms") {
			unit = " ms"
		} else if strings.HasSuffix(key, "_percent") {
			unit = "%"
		}
		writeMarkdownRow(output, labels[key], fmt.Sprintf("%v%s", value, unit))
	}
	if len(metrics) == 0 {
		output.WriteString("暂无可汇总指标。\n")
	}
}

func writeHistoryFindingsMarkdown(output *strings.Builder, record HistoryRecord) {
	type finding struct{ status, title, detail string }
	findings := make([]finding, 0)
	switch record.Kind {
	case "integrated":
		for _, item := range record.Integrated.Findings {
			findings = append(findings, finding{item.Status, item.Title, item.Detail})
		}
	case "site_patrol":
		for _, item := range record.SitePatrol.Findings {
			findings = append(findings, finding{item.Level, item.Title, item.Message})
		}
	case "cdn_optimize":
		item := record.CDNOptimize.Finding
		findings = append(findings, finding{item.Level, item.Title, item.Message})
	case "network_baseline":
		for _, item := range record.NetworkBaseline.Findings {
			findings = append(findings, finding{item.Status, item.Title, item.Detail})
		}
	case "path_compare":
		item := record.PathCompare.Finding
		findings = append(findings, finding{item.Level, item.Title, item.Detail})
	case "dns_diagnosis":
		for _, item := range record.DNSDiagnosis.Findings {
			findings = append(findings, finding{item.Status, item.Title, item.Detail})
		}
	case "port_test":
		for _, item := range record.PortTest.Findings {
			findings = append(findings, finding{item.Status, item.Title, item.Detail})
		}
	case "stability":
		item := record.Stability.Finding
		findings = append(findings, finding{item.Level, item.Title, item.Message})
	}
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(output, "\n## 分层结论\n\n")
	for _, item := range findings {
		if strings.TrimSpace(item.title) == "" {
			continue
		}
		fmt.Fprintf(output, "- **%s** `%s`", escapeMarkdown(item.title), escapeMarkdown(valueOrDash(item.status)))
		if item.detail != "" {
			fmt.Fprintf(output, " — %s", escapeMarkdown(item.detail))
		}
		output.WriteByte('\n')
	}
}

func historyReportMetrics(record HistoryRecord) map[string]interface{} {
	metrics := make(map[string]interface{})
	diagnosis := historyReportDiagnosis(record)
	if diagnosis.DNSLookupMS != 0 {
		metrics["dns_lookup_ms"] = diagnosis.DNSLookupMS
	}
	if diagnosis.TCPConnectMS != 0 {
		metrics["tcp_connect_ms"] = diagnosis.TCPConnectMS
	}
	if diagnosis.TLSHandshakeMS != 0 {
		metrics["tls_handshake_ms"] = diagnosis.TLSHandshakeMS
	}
	if diagnosis.TTFBMS != 0 {
		metrics["ttfb_ms"] = diagnosis.TTFBMS
	}
	if diagnosis.TotalMS != 0 {
		metrics["total_ms"] = diagnosis.TotalMS
	}
	switch record.Kind {
	case "integrated":
		metrics["duration_ms"] = record.Integrated.DurationMS
	case "stability":
		metrics["duration_ms"] = record.Stability.DurationMS
		metrics["availability_percent"] = record.Stability.AvailabilityPercent
		metrics["jitter_ms"] = record.Stability.JitterMS
		metrics["success_count"] = record.Stability.SuccessCount
		metrics["total_count"] = record.Stability.SampleCount
	case "site_patrol":
		metrics["duration_ms"] = record.SitePatrol.DurationMS
		metrics["success_count"] = record.SitePatrol.SuccessCount
		metrics["total_count"] = record.SitePatrol.TotalCount
	case "cdn_optimize":
		metrics["duration_ms"] = record.CDNOptimize.DurationMS
		metrics["responsive_count"] = record.CDNOptimize.ResponsiveCount
		metrics["best_ip"] = record.CDNOptimize.BestIP
		metrics["improvement_percent"] = record.CDNOptimize.ImprovementPercent
	case "network_baseline":
		metrics["duration_ms"] = record.NetworkBaseline.DurationMS
	case "path_compare":
		metrics["duration_ms"] = record.PathCompare.DurationMS
	case "dns_diagnosis":
		metrics["duration_ms"] = record.DNSDiagnosis.DurationMS
		metrics["success_count"] = record.DNSDiagnosis.Comparison.ReachableResolvers
		metrics["total_count"] = record.DNSDiagnosis.Total
	case "port_test":
		metrics["duration_ms"] = record.PortTest.DurationMS
		metrics["open_count"] = record.PortTest.OpenCount
		metrics["total_count"] = record.PortTest.TotalCount
	}
	return metrics
}

func historyReportDiagnosis(record HistoryRecord) DiagResult {
	if record.Kind == "integrated" && record.Integrated != nil {
		return record.Integrated.HTTP.Diagnosis
	}
	return record.Result
}

func statusCodeLabel(status int) string {
	if status == 0 {
		return "未生成"
	}
	return fmt.Sprintf("%d", status)
}

func certificateDaysLabel(diagnosis DiagResult) string {
	if diagnosis.TLSVersion == "" && diagnosis.TLSCertExpiresAt == "" {
		return "未生成"
	}
	return fmt.Sprintf("%d 天", diagnosis.TLSCertDaysRemaining)
}

func historyReportSuccess(record HistoryRecord) bool {
	if record.Kind == "integrated" && record.Integrated != nil {
		return record.Integrated.Status == "completed" && record.Integrated.OverallStatus != "critical"
	}
	return record.Result.Success
}

func historyReportConclusion(record HistoryRecord) string {
	if strings.TrimSpace(record.Result.Message) != "" {
		return record.Result.Message
	}
	switch record.Kind {
	case "integrated":
		return record.Integrated.Message
	case "stability":
		return firstNonEmpty(record.Stability.Finding.Title, record.Stability.Message)
	case "site_patrol":
		return record.SitePatrol.Message
	case "cdn_optimize":
		return firstNonEmpty(record.CDNOptimize.Finding.Title, record.CDNOptimize.Message)
	case "network_baseline":
		return record.NetworkBaseline.Message
	case "path_compare":
		return firstNonEmpty(record.PathCompare.Finding.Title, record.PathCompare.Message)
	case "dns_diagnosis":
		return record.DNSDiagnosis.Message
	case "port_test":
		return record.PortTest.Message
	default:
		return "诊断已完成"
	}
}

func historyReportTarget(record HistoryRecord) string {
	switch record.Kind {
	case "integrated":
		if record.Integrated != nil {
			return record.Integrated.TargetURL
		}
	case "stability":
		if record.Stability != nil {
			return record.Stability.TargetURL
		}
	case "site_patrol":
		if record.SitePatrol != nil {
			return fmt.Sprintf("%d 个站点", record.SitePatrol.TotalCount)
		}
	case "cdn_optimize":
		if record.CDNOptimize != nil {
			return firstNonEmpty(record.CDNOptimize.TargetURL, record.CDNOptimize.Domain)
		}
	case "network_baseline":
		if record.NetworkBaseline != nil {
			return record.NetworkBaseline.TargetHost
		}
	case "path_compare":
		if record.PathCompare != nil {
			return record.PathCompare.TargetURL
		}
	case "dns_diagnosis":
		if record.DNSDiagnosis != nil {
			return record.DNSDiagnosis.Domain
		}
	case "port_test":
		if record.PortTest != nil {
			return record.PortTest.Target
		}
	}
	return record.Result.TargetURL
}

func historyReportKindEvidence(record HistoryRecord) (string, interface{}) {
	switch record.Kind {
	case "integrated":
		return "integrated", record.Integrated
	case "stability":
		return "stability", record.Stability
	case "site_patrol":
		return "site-patrol", record.SitePatrol
	case "cdn_optimize":
		return "cf-ip-selection", record.CDNOptimize
	case "network_baseline":
		return "network-baseline", record.NetworkBaseline
	case "path_compare":
		return "path-compare", record.PathCompare
	case "dns_diagnosis":
		return "dns-diagnosis", record.DNSDiagnosis
	case "port_test":
		return "port-test", record.PortTest
	default:
		return "diagnosis", record.Result
	}
}

func historyReportKindLabel(kind string) string {
	return map[string]string{
		"diagnosis": "链路诊断", "integrated": "综合诊断", "stability": "持续监控", "site_patrol": "多站点巡检", "cdn_optimize": "CF IP 优选", "network_baseline": "本地网络环境", "path_compare": "连接策略对照", "dns_diagnosis": "DNS 深度诊断", "port_test": "端口与协议检测",
	}[kind]
}

func historyReportFilename(record HistoryRecord) string {
	target := historyReportTarget(record)
	if parsed, err := url.Parse(target); err == nil && parsed.Hostname() != "" {
		target = parsed.Hostname()
	}
	target = unsafeReportFilenameCharacters.ReplaceAllString(target, "-")
	target = strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return '-'
		}
		return character
	}, target)
	target = strings.Trim(target, ".- ")
	if target == "" {
		target = "network"
	}
	if len([]rune(target)) > 48 {
		target = string([]rune(target)[:48])
	}
	stamp := time.Now().Format("20060102-150405")
	return fmt.Sprintf("ATNetool-%s-%s.zip", target, stamp)
}

func filepathExtension(path string) string {
	index := strings.LastIndexByte(path, '.')
	separator := strings.LastIndexAny(path, `/\\`)
	if index <= separator {
		return ""
	}
	return path[index:]
}

func sortedReportFileNames(files map[string]interface{}) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	// The archive layout stays deterministic for tests and support tooling.
	for left := 0; left < len(names); left++ {
		for right := left + 1; right < len(names); right++ {
			if names[right] < names[left] {
				names[left], names[right] = names[right], names[left]
			}
		}
	}
	return names
}

func writeMarkdownRow(output *strings.Builder, label, value string) {
	fmt.Fprintf(output, "- **%s：** %s\n", escapeMarkdown(label), escapeMarkdown(valueOrDash(value)))
}

func escapeMarkdown(value string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "\n", " ", "\r", " ").Replace(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func milliseconds(value int64) string {
	return fmt.Sprintf("%d ms", value)
}
