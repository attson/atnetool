const EMPTY_VALUE = null;

export function escapeImportedReportHTML(value) {
  return String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

const asArray = (value) => (Array.isArray(value) ? value : []);

function hasValue(value) {
  return value !== null && value !== undefined && value !== '';
}

function metric(id, label, value, unit = '') {
  return { id, label, value: hasValue(value) ? value : EMPTY_VALUE, unit };
}

function row(label, value, options = {}) {
  return {
    label,
    value: hasValue(value) ? value : EMPTY_VALUE,
    ...options,
  };
}

function normaliseHealth(value, fallback = 'critical') {
  const status = String(value || '').toLowerCase();
  if (['healthy', 'good', 'success', 'completed', 'open'].includes(status)) return 'healthy';
  if (['warning', 'partial', 'degraded', 'suspected'].includes(status)) return 'warning';
  if (['critical', 'failed', 'error', 'closed', 'timeout'].includes(status)) return 'critical';
  return fallback;
}

function hasFindingContent(item) {
  return item && typeof item === 'object' && [
    item.code,
    item.layer,
    item.status,
    item.level,
    item.title,
    item.message,
    item.detail,
    item.description,
  ].some(hasValue);
}

function findings(items = []) {
  return asArray(items).filter(hasFindingContent).map((item, index) => ({
    id: item.code || item.layer || `finding-${index + 1}`,
    status: normaliseHealth(item.status || item.level, 'warning'),
    title: item.title || item.message || '未提供结论',
    detail: item.detail || item.message || item.description || '',
    layer: item.layer || '',
  }));
}

function evidenceHealth(source, fallback = 'healthy') {
  if (!source || typeof source !== 'object') return fallback;
  if (source.diagnosis && typeof source.diagnosis === 'object') return diagnosisHealth(source.diagnosis);
  const assessment = findings([
    ...asArray(source.findings),
    ...(hasFindingContent(source.finding) ? [source.finding] : []),
  ]);
  if (assessment.some((item) => item.status === 'critical')) return 'critical';
  if (assessment.some((item) => item.status === 'warning')) return 'warning';
  if (source.success === false) return 'critical';
  if (hasValue(source.overall_status)) return normaliseHealth(source.overall_status, 'critical');
  if (hasValue(source.status)) return normaliseHealth(source.status, 'critical');
  return fallback;
}

function remediations(items = []) {
  return asArray(items).map((item, index) => ({
    id: item.action || `remediation-${index + 1}`,
    priority: Number(item.priority) || index + 1,
    title: item.title || item.action || '建议复核',
    detail: item.detail || item.description || '',
    module: item.module || '',
  })).sort((left, right) => left.priority - right.priority);
}

function moduleEvidence(id, title, source, rows, options = {}) {
  return {
    id,
    title,
    status: options.status || evidenceHealth(source),
    message: source?.message || '',
    rows: rows.filter(Boolean),
    details: options.details || [],
  };
}

function reviewBase(kind, record, source, metadata = {}) {
  return {
    kind,
    kindLabel: metadata.kind_label || '',
    title: source?.message || record.message || '',
    health: 'critical',
    target: source?.target_url || source?.domain || source?.target || source?.target_host || record.target_url || '',
    checkedAt: record.checked_at || source?.finished_at || source?.started_at || '',
    metrics: [],
    findings: [],
    remediations: [],
    modules: [],
    route: buildOfflineRoute(record.traceroute),
  };
}

function ratio(numerator, denominator, scale = 100) {
  const left = Number(numerator);
  const right = Number(denominator);
  return Number.isFinite(left) && Number.isFinite(right) && right > 0 ? (left / right) * scale : EMPTY_VALUE;
}

function availableStack(stacks, family) {
  const stack = asArray(stacks).find((item) => String(item.family || item.name).toLowerCase() === family.toLowerCase());
  return stack ? Boolean(stack.available) : EMPTY_VALUE;
}

function diagnosisHealth(record) {
  if (!record.success || Number(record.status_code) >= 500 || (record.tls_version && record.tls_cert_valid === false)) return 'critical';
  if (
    record.proxy_status === 'suspected'
    || Number(record.dns_lookup_ms) > 300
    || Number(record.tcp_connect_ms) > 600
    || Number(record.tls_handshake_ms) > 900
    || Number(record.ttfb_ms) > 800
    || Number(record.total_ms) > 3000
    || Number(record.status_code) >= 400
    || Number(record.redirect_count) > 3
    || (record.tls_version && Number(record.tls_cert_days_remaining) < 30)
  ) return 'warning';
  return 'healthy';
}

export function buildOfflineRoute(trace) {
  if (!trace || typeof trace !== 'object' || !Array.isArray(trace.hops) || trace.hops.length === 0) return null;
  const hops = trace.hops.map((hop, index) => ({
    hop: Number(hop.hop) || index + 1,
    addresses: asArray(hop.addresses).map(String),
    rttMS: asArray(hop.rtt_ms).map(Number).filter(Number.isFinite),
    timedOut: Boolean(hop.timed_out),
    reached: Boolean(hop.reached),
    geo: hop.geo && typeof hop.geo === 'object' ? { ...hop.geo } : null,
  }));
  const points = hops.flatMap((hop) => {
    const latitude = Number(hop.geo?.latitude);
    const longitude = Number(hop.geo?.longitude);
    if (!Number.isFinite(latitude) || !Number.isFinite(longitude) || latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 || (latitude === 0 && longitude === 0)) return [];
    return [{
      hop: hop.hop,
      label: [hop.geo?.city, hop.geo?.region, hop.geo?.country].filter(Boolean).join(' · ') || hop.addresses[0] || `Hop ${hop.hop}`,
      address: hop.geo?.ip || hop.addresses[0] || '',
      coordinates: [longitude, latitude],
      confidence: hop.geo?.confidence || '',
    }];
  });
  return {
    targetHost: trace.target_host || '',
    targetIPs: asArray(trace.target_ips),
    reached: Boolean(trace.reached),
    durationMS: hasValue(trace.duration_ms) ? Number(trace.duration_ms) : EMPTY_VALUE,
    tool: trace.tool || '',
    provider: trace.geo_provider || '',
    hops,
    points,
  };
}

// Keep routes that cross the international date line spatially local. The
// returned longitudes may fall outside [-180, 180]; the canvas projection
// shifts world geometry into the same unwrapped coordinate space.
export function unwrapRouteLongitudes(points) {
  let previous = null;
  return asArray(points).map((point) => {
    let longitude = Number(point?.coordinates?.[0]);
    if (!Number.isFinite(longitude)) return longitude;
    if (previous !== null) {
      while (longitude - previous > 180) longitude -= 360;
      while (longitude - previous < -180) longitude += 360;
    }
    previous = longitude;
    return longitude;
  });
}

function buildIntegrated(record, metadata) {
  const result = record.integrated || {};
  const diagnosis = result.http?.diagnosis || {};
  const route = buildOfflineRoute(result.http?.traceroute || record.traceroute);
  return {
    kind: 'integrated',
    kindLabel: metadata.kind_label || '综合诊断',
    title: result.message || record.message || '综合诊断复盘',
    health: normaliseHealth(result.overall_status, record.success ? 'healthy' : 'critical'),
    target: result.target_url || record.target_url || '',
    checkedAt: record.checked_at || result.finished_at || result.started_at || '',
    metrics: [
      metric('http_total', 'HTTP 总耗时', diagnosis.total_ms, 'ms'),
      metric('ttfb', 'TTFB', diagnosis.ttfb_ms, 'ms'),
      metric('duration', '完整诊断耗时', result.duration_ms, 'ms'),
      metric('modules', '已完成模块', hasValue(result.completed) && hasValue(result.total) ? `${result.completed}/${result.total}` : EMPTY_VALUE),
    ],
    findings: findings(result.findings),
    remediations: remediations(result.remediations),
    modules: [
      moduleEvidence('baseline', '本地环境', result.baseline, [
        row('检测状态', result.baseline?.status),
        row('检测耗时', result.baseline?.duration_ms, { unit: 'ms' }),
        row('活动接口', asArray(result.baseline?.local_network?.active_interfaces).join(', ')),
      ]),
      moduleEvidence('dns', 'DNS 分析', result.dns, [
        row('检测状态', result.dns?.status),
        row('检测耗时', result.dns?.duration_ms, { unit: 'ms' }),
        row('解析器数量', result.dns?.total),
      ], { details: asArray(result.dns?.resolvers) }),
      moduleEvidence('ports', '端口协议', result.ports, [
        row('检测状态', result.ports?.status),
        row('开放端口', hasValue(result.ports?.open_count) && hasValue(result.ports?.total_count) ? `${result.ports.open_count}/${result.ports.total_count}` : EMPTY_VALUE),
        row('检测耗时', result.ports?.duration_ms, { unit: 'ms' }),
      ], { details: asArray(result.ports?.results) }),
      moduleEvidence('http', 'HTTP 链路', result.http, [
        row('HTTP 状态', diagnosis.status_code),
        row('总耗时', diagnosis.total_ms, { unit: 'ms' }),
        row('TTFB', diagnosis.ttfb_ms, { unit: 'ms' }),
        row('远端地址', diagnosis.remote_ip || diagnosis.remote_address),
      ]),
      moduleEvidence('paths', '路径对照', result.paths, [
        row('检测状态', result.paths?.status),
        row('检测耗时', result.paths?.duration_ms, { unit: 'ms' }),
        row('路径数量', asArray(result.paths?.paths).length),
      ], { details: asArray(result.paths?.paths) }),
    ],
    route,
  };
}

function buildDiagnosis(record, metadata) {
  const review = reviewBase('diagnosis', record, record, metadata);
  review.kindLabel ||= '域名诊断';
  review.title ||= record.success ? '域名访问正常' : '域名访问存在异常';
  review.health = diagnosisHealth(record);
  review.metrics = [
    metric('dns', 'DNS 解析', record.dns_lookup_ms, 'ms'),
    metric('tcp', 'TCP 连接', record.tcp_connect_ms, 'ms'),
    metric('tls', 'TLS 握手', record.tls_handshake_ms, 'ms'),
    metric('ttfb', 'TTFB', record.ttfb_ms, 'ms'),
    metric('total', '总耗时', record.total_ms, 'ms'),
  ];
  review.findings = record.finding ? findings([record.finding]) : record.message ? findings([{
    status: review.health,
    title: record.message,
    detail: record.error || '',
  }]) : [];
  review.remediations = remediations(record.remediations);
  review.modules = [
    moduleEvidence('request', '请求链路', record, [
      row('最终地址', record.final_url || record.target_url),
      row('HTTP 状态', record.status_code),
      row('HTTP 协议', record.http_protocol),
      row('重定向次数', record.redirect_count),
      row('远端地址', record.remote_ip || record.remote_address),
      row('Cloudflare 节点', record.cf_colo),
      row('CF-Ray', record.cf_ray),
    ], { details: asArray(record.redirect_chain), status: review.health }),
    moduleEvidence('proxy', '代理路径', record, [
      row('检测模式', record.proxy_mode),
      row('实际路径', record.proxy_status),
      row('代理地址', record.proxy_url),
      row('路径说明', record.proxy_message),
    ], { status: review.health }),
    moduleEvidence('tls', 'TLS 证据', record, [
      row('TLS 版本', record.tls_version),
      row('ALPN', record.tls_alpn),
      row('证书主题', record.tls_cert_subject),
      row('证书签发者', record.tls_cert_issuer),
      row('证书到期时间', record.tls_cert_expires_at),
      row('证书有效', hasValue(record.tls_cert_valid) ? Boolean(record.tls_cert_valid) : EMPTY_VALUE),
    ], { status: review.health }),
  ];
  review.route = buildOfflineRoute(record.traceroute);
  return review;
}

function buildStability(record, metadata) {
  const result = record.stability || {};
  const review = reviewBase('stability', record, result, metadata);
  review.kindLabel ||= '持续监测';
  review.health = result.status && result.status !== 'completed'
    ? 'critical'
    : result.finding?.level === 'critical' || !Number(result.success_count) || Number(result.failure_rate) > 0.2
      ? 'critical'
      : result.finding?.level === 'warning' || Number(result.failure_rate) > 0 || Number(result.p95_ms) > 500 || Number(result.jitter_ms) > 100
        ? 'warning'
        : 'healthy';
  review.metrics = [
    metric('availability', '可用率', result.availability_percent, '%'),
    metric('p50', '延迟 P50', result.p50_ms, 'ms'),
    metric('p95', '延迟 P95', result.p95_ms, 'ms'),
    metric('jitter', '抖动', result.jitter_ms, 'ms'),
    metric('samples', '采样数', result.sample_count),
  ];
  review.findings = findings([result.finding]);
  review.modules = [
    moduleEvidence('summary', '稳定性摘要', result, [
      row('成功次数', result.success_count),
      row('失败率', hasValue(result.failure_rate) ? Number(result.failure_rate) * 100 : EMPTY_VALUE, { unit: '%' }),
      row('最慢请求', result.maximum_ms, { unit: 'ms' }),
      row('慢请求数', result.slow_count),
      row('最长连续失败', result.longest_failure_streak),
    ], { status: review.health }),
    moduleEvidence('incidents', '异常时间线', result, [
      row('异常数量', asArray(result.incidents).length),
      row('路径变化', asArray(result.route_changes).length),
    ], { details: [...asArray(result.incidents), ...asArray(result.route_changes)], status: review.health }),
    moduleEvidence('samples', '采样证据', result, [
      row('采样间隔', result.interval_ms, { unit: 'ms' }),
      row('延迟阈值', result.latency_threshold_ms, { unit: 'ms' }),
    ], { details: asArray(result.samples), status: review.health }),
  ];
  return review;
}

function buildSitePatrol(record, metadata) {
  const result = record.site_patrol || {};
  const review = reviewBase('site_patrol', record, result, metadata);
  review.kindLabel ||= '站点巡检';
  review.target ||= `${Number(result.total_count) || 0} 个站点`;
  const assessment = findings(result.findings);
  review.health = assessment.some((item) => item.status === 'critical') || Number(result.success_count) === 0
    ? 'critical'
    : assessment.some((item) => item.status === 'warning') || Number(result.success_count) < Number(result.total_count)
      ? 'warning'
      : 'healthy';
  review.metrics = [
    metric('pass_rate', '通过率', ratio(result.success_count, result.total_count), '%'),
    metric('average', '平均耗时', result.average_ms, 'ms'),
    metric('success', '通过站点', result.success_count),
    metric('total', '站点总数', result.total_count),
  ];
  review.findings = assessment;
  review.remediations = remediations(result.remediations);
  review.modules = [moduleEvidence('sites', '逐站证据', result, [
    row('代理模式', result.proxy_mode),
    row('代理地址', result.proxy_url),
    row('检测耗时', result.duration_ms, { unit: 'ms' }),
  ], { details: asArray(result.results).map((item) => ({
    name: item.target?.name || item.name || '',
    target_url: item.target?.url || item.target_url || '',
    success: item.result?.success ?? item.success,
    status_code: item.result?.status_code ?? item.status_code,
    total_ms: item.result?.total_ms ?? item.total_ms,
    proxy_status: item.result?.proxy_status ?? item.proxy_status,
    remote_ip: item.result?.remote_ip ?? item.remote_ip,
    message: item.result?.message || item.message || '',
  })), status: review.health })];
  return review;
}

function buildCDNOptimize(record, metadata) {
  const result = record.cdn_optimize || {};
  const review = reviewBase('cdn_optimize', record, result, metadata);
  review.kindLabel ||= 'CF IP 优选';
  review.health = result.best_ip ? normaliseHealth(result.finding?.level, 'healthy') : 'critical';
  review.metrics = [
    metric('baseline_ip', '当前 IP', result.baseline_ip),
    metric('best_ip', '优选 IP', result.best_ip),
    metric('improvement', '改善幅度', result.improvement_percent, '%'),
    metric('eligible', '有效候选', hasValue(result.eligible_count) && hasValue(result.candidate_count) ? `${result.eligible_count}/${result.candidate_count}` : EMPTY_VALUE),
  ];
  review.findings = findings([result.finding]);
  review.remediations = remediations(result.governance);
  review.modules = [
    moduleEvidence('selection', '优选结论', result, [
      row('目标域名', result.domain),
      row('验证状态', result.target_validation),
      row('可信度', result.confidence),
      row('验证范围', result.validation_scope),
      row('P95 改善', result.p95_improvement_percent, { unit: '%' }),
    ], { status: review.health }),
    moduleEvidence('candidates', '候选结果', result, [
      row('候选数量', result.candidate_count),
      row('已响应', result.responsive_count),
      row('已验证', result.verified_count),
      row('已过滤', result.filtered_count),
    ], { details: asArray(result.results).map((item) => ({
      ip: item.candidate?.ip || item.ip || '',
      source: item.candidate?.source || item.source || '',
      rank: item.rank,
      eligible: item.eligible,
      recommended: item.recommended,
      success_rate: item.success_rate,
      p50_total_ms: item.p50_total_ms,
      p95_total_ms: item.p95_total_ms,
      jitter_ms: item.jitter_ms,
      loss_rate: item.loss_rate,
      download_speed_mbps: item.download_speed_mbps,
      cf_colo: item.cf_colo,
      filter_reason: item.filter_reason,
    })), status: review.health }),
    moduleEvidence('path', '路径适用范围', result, [
      row('请求路径', result.requested_route),
      row('实际路径', result.effective_route),
      row('上游代理', result.upstream_proxy_url),
      row('路径说明', result.path_evidence?.message || result.path_evidence?.detail),
    ], { status: review.health }),
  ];
  return review;
}

function buildNetworkBaseline(record, metadata) {
  const result = record.network_baseline || {};
  const review = reviewBase('network_baseline', record, result, metadata);
  review.kindLabel ||= '网络基线';
  review.health = evidenceHealth(result);
  review.metrics = [
    metric('duration', '检测耗时', result.duration_ms, 'ms'),
    metric('interfaces', '活动接口', asArray(result.local_network?.active_interfaces).length),
    metric('ipv4', 'IPv4', availableStack(result.ip_stacks, 'IPv4')),
    metric('ipv6', 'IPv6', availableStack(result.ip_stacks, 'IPv6')),
    metric('proxies', '代理监听', asArray(result.proxy_listeners).length),
  ];
  review.findings = findings(result.findings);
  review.remediations = remediations(result.remediations);
  review.modules = [
    moduleEvidence('local', '本地网络', result, [
      row('活动接口', asArray(result.local_network?.active_interfaces).join(', ')),
      row('默认路由', result.local_network?.default_route_count),
      row('路由接管', result.local_network?.route_takeover),
      row('DNS 模式', result.local_network?.dns_mode),
      row('MTU 风险', asArray(result.local_network?.mtu_risks).join(', ')),
    ], { details: [...asArray(result.routes), ...asArray(result.environment?.interfaces)] }),
    moduleEvidence('dns', 'DNS 对照', result, [
      row('解析器数量', asArray(result.dns_probes).length),
      row('成功解析器', result.dns_comparison?.successful_resolvers),
      row('结果一致', result.dns_comparison?.matching_resolvers),
    ], { details: asArray(result.dns_probes) }),
    moduleEvidence('proxy', '代理监听', result, [
      row('监听数量', asArray(result.proxy_listeners).length),
    ], { details: asArray(result.proxy_listeners) }),
  ];
  return review;
}

function buildPathCompare(record, metadata) {
  const result = record.path_compare || {};
  const recommended = asArray(result.paths).find((item) => item.id === result.finding?.recommended_path_id) || asArray(result.paths)[0] || {};
  const review = reviewBase('path_compare', record, result, metadata);
  review.kindLabel ||= '路径对照';
  review.health = asArray(result.paths).some((item) => Number(item.success_count) > 0 || Number(item.success_rate) > 0)
    ? normaliseHealth(result.finding?.level, 'healthy') : 'critical';
  review.metrics = [
    metric('recommended', '推荐路径', recommended.label || result.finding?.recommended_path_id),
    metric('p50', '总耗时 P50', recommended.p50_total_ms, 'ms'),
    metric('success', '成功率', hasValue(recommended.success_rate) ? Number(recommended.success_rate) * 100 : EMPTY_VALUE, '%'),
    metric('jitter', '抖动', recommended.jitter_ms, 'ms'),
    metric('improvement', '改善幅度', result.finding?.improvement_percent, '%'),
  ];
  review.findings = findings([result.finding]);
  review.modules = [moduleEvidence('routes', '路径采样', result, [
    row('每路径采样', result.sample_count),
    row('完成采样', hasValue(result.completed) && hasValue(result.total) ? `${result.completed}/${result.total}` : EMPTY_VALUE),
  ], { details: asArray(result.paths), status: review.health })];
  return review;
}

function buildDNSDiagnosis(record, metadata) {
  const result = record.dns_diagnosis || {};
  const review = reviewBase('dns_diagnosis', record, result, metadata);
  review.kindLabel ||= 'DNS 分析';
  review.health = evidenceHealth(result);
  review.metrics = [
    metric('duration', '检测耗时', result.duration_ms, 'ms'),
    metric('resolvers', '解析器', result.total ?? asArray(result.resolvers).length),
    metric('reachable', '可达解析器', result.comparison?.reachable_resolvers),
    metric('address_sets', '公共地址组', result.comparison?.public_address_sets),
    metric('consistent', '系统与公共解析一致', result.comparison?.system_matches_public),
  ];
  review.findings = findings(result.findings);
  review.remediations = remediations(result.remediations);
  review.modules = [
    moduleEvidence('resolvers', '解析器证据', result, [
      row('UDP 可用', result.comparison?.udp_available),
      row('DoH 可用', result.comparison?.doh_available),
    ], { details: asArray(result.resolvers) }),
    moduleEvidence('records', '地址集合', result, [
      row('系统与公共解析一致', result.comparison?.system_matches_public),
      row('公共地址组', result.comparison?.public_address_sets),
    ], { details: asArray(result.resolvers).flatMap((resolver) => asArray(resolver.queries).flatMap((query) => {
      let records = asArray(query.records);
      if (records.length) return records.map((record) => ({
        resolver: resolver.name || resolver.id,
        transport: resolver.transport,
        record_type: record.type || query.type,
        name: record.name,
        value: record.value,
        ttl: record.ttl,
        duration_ms: query.duration_ms,
      }));
      return [{
        resolver: resolver.name || resolver.id,
        transport: resolver.transport,
        record_type: query.type,
        success: query.success,
        duration_ms: query.duration_ms,
        error: query.error || '',
      }];
    })) }),
  ];
  return review;
}

function buildPortTest(record, metadata) {
  const result = record.port_test || {};
  const review = reviewBase('port_test', record, result, metadata);
  review.kindLabel ||= '端口协议';
  review.health = evidenceHealth(result);
  review.metrics = [
    metric('duration', '检测耗时', result.duration_ms, 'ms'),
    metric('total', '检测端口', result.total_count),
    metric('open', '开放端口', result.open_count),
    metric('refused', '明确拒绝', result.refused_count),
    metric('timeout', '静默超时', result.timeout_count),
  ];
  review.findings = findings(result.findings);
  review.remediations = remediations(result.remediations);
  review.modules = [
    moduleEvidence('summary', '端口汇总', result, [
      row('目标地址', result.target),
      row('解析地址', asArray(result.resolved_ips).join(', ')),
      row('DNS 错误', result.dns_error),
    ]),
    moduleEvidence('ports', '逐端口证据', result, [
      row('已完成', result.completed_count),
    ], { details: asArray(result.results) }),
  ];
  return review;
}

export function buildImportedReportReview(record, metadata = {}) {
  if (!record || typeof record !== 'object') throw new TypeError('Imported history record is required');
  const builders = {
    integrated: buildIntegrated,
    diagnosis: buildDiagnosis,
    stability: buildStability,
    site_patrol: buildSitePatrol,
    cdn_optimize: buildCDNOptimize,
    network_baseline: buildNetworkBaseline,
    path_compare: buildPathCompare,
    dns_diagnosis: buildDNSDiagnosis,
    port_test: buildPortTest,
  };
  if (builders[record.record_kind]) return builders[record.record_kind](record, metadata);
  throw new RangeError(`Unsupported imported report kind: ${record.record_kind || 'unknown'}`);
}

export { EMPTY_VALUE };
