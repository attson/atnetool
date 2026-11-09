const DESTINATIONS = {
  compare_proxy: 'path-compare',
  compare_routes: 'path-compare',
  verify_tunnel: 'path-compare',
  diagnose_dns: 'dns-diagnosis',
  change_dns: 'dns-diagnosis',
  use_doh: 'dns-diagnosis',
  inspect_override: 'dns-diagnosis',
  inspect_tls: 'diagnosis',
  check_listener: 'port-test',
  check_firewall: 'port-test',
};

const MODULE_DESTINATIONS = {
  baseline: 'network-baseline',
  dns: 'dns-diagnosis',
  ports: 'port-test',
  http: 'diagnosis',
  tls: 'diagnosis',
  paths: 'path-compare',
  proxy: 'path-compare',
  route: 'path-compare',
};

const MODULE_LAYERS = {
  baseline: ['baseline', 'local', 'network', 'egress', 'proxy', 'ipv4', 'ipv6'],
  dns: ['dns', 'availability', 'transport', 'integrity', 'scheduling'],
  ports: ['ports', 'port', 'tcp'],
  http: ['http'],
  tls: ['tls'],
  paths: ['paths', 'path', 'route', 'proxy'],
  proxy: ['proxy', 'paths', 'path'],
  route: ['route', 'paths', 'path'],
};

const PATH_ACTIONS = new Set(['compare_proxy', 'compare_routes', 'verify_tunnel', 'route']);
const TLS_ACTIONS = new Set(['inspect_tls']);

export function integratedResultLevel(status) {
  const value = String(status || '').toLowerCase();
  if (['failed', 'critical', 'timeout', 'rejected', 'error'].includes(value)) return 'critical';
  if (['warning', 'warn', 'partial', 'cancelled'].includes(value)) return 'warning';
  if (['healthy', 'good', 'ok', 'success'].includes(value)) return 'healthy';
  return 'neutral';
}

export function remediationDestination(remediation) {
  return DESTINATIONS[remediation?.action]
    || MODULE_DESTINATIONS[remediation?.module]
    || 'integrated';
}

function remediationGroup(remediation) {
  const action = String(remediation?.action || '');
  const module = String(remediation?.module || '');
  if (PATH_ACTIONS.has(action)) return 'path-validation';
  if (TLS_ACTIONS.has(action) || (action === 'isolate' && module === 'tls')) return 'tls-validation';
  if (['dns', 'diagnose_dns'].includes(action) || (action === 'isolate' && module === 'dns')) return 'dns-diagnosis';
  return `${action || module}:${String(remediation?.title || '').trim().toLowerCase()}`;
}

function findingMatchesModule(finding, remediation) {
  const layer = String(finding?.layer || '').toLowerCase();
  const module = String(remediation?.module || '').toLowerCase();
  if (!layer || !module) return false;
  if (layer === module) return true;
  return (MODULE_LAYERS[module] || [module]).includes(layer);
}

function relatedFinding(remediation, attentionFindings) {
  const action = String(remediation?.action || '');
  const module = String(remediation?.module || '');
  const findDNS = (terms = []) => attentionFindings.find((finding) =>
    ['dns', 'availability', 'transport', 'integrity', 'scheduling'].includes(finding.layer)
      && (!terms.length || terms.some((term) => String(finding.title || '').includes(term))));
  if (action === 'verify_policy') {
    return findDNS(['不可达', '通道', '出口']) || findDNS();
  }
  if (['use_doh', 'change_dns'].includes(action)) {
    return findDNS(['通道', '系统 DNS', 'DNS 解析']) || findDNS();
  }
  if (action === 'inspect_override') {
    return findDNS(['重写', '接管', '解析结果']) || findDNS();
  }
  if (action === 'compare_routes' && module === 'dns') {
    return findDNS(['不同', '差异']) || findDNS();
  }
  if (['dns', 'diagnose_dns', 'change_dns', 'use_doh', 'inspect_override', 'verify_policy'].includes(action)) {
    return findDNS(['不同', '差异', '解析']) || findDNS();
  }
  if (action === 'compare_proxy') {
    const proxyFinding = attentionFindings.find((finding) => finding.layer === 'proxy')
      || attentionFindings.find((finding) => ['paths', 'path', 'egress'].includes(finding.layer));
    if (proxyFinding) return proxyFinding;
  }
  if (PATH_ACTIONS.has(action)) {
    const pathFinding = attentionFindings.find((finding) =>
      ['paths', 'path', 'route', 'proxy', 'egress'].includes(finding.layer));
    if (pathFinding) return pathFinding;
  }
  if (TLS_ACTIONS.has(action)) {
    const tlsFinding = attentionFindings.find((finding) => ['tls', 'ports', 'tcp'].includes(finding.layer));
    if (tlsFinding) return tlsFinding;
  }
  return attentionFindings.find((finding) => findingMatchesModule(finding, remediation)) || null;
}

function severity(level) {
  return level === 'critical' ? 2 : level === 'warning' ? 1 : 0;
}

function mergeDNSWarnings(findings, result) {
  const warningIndexes = [];
  findings.forEach((finding, index) => {
    if (finding.layer === 'dns' && finding.level === 'warning') warningIndexes.push(index);
  });
  const resolvers = Array.isArray(result?.dns?.resolvers) ? result.dns.resolvers : [];
  const unreachable = resolvers.filter((resolver) => resolver.transport !== 'system' && !resolver.reachable);
  const hasDifference = Boolean(result?.dns?.comparison?.address_difference)
    || warningIndexes.some((index) => /差异|不同/.test(String(findings[index].title || '')));
  if (!warningIndexes.length || (warningIndexes.length === 1 && !(hasDifference && unreachable.length))) {
    return findings;
  }

  const title = hasDifference && unreachable.length
    ? `DNS 解析存在差异，${unreachable.length} 个公共解析器未响应`
    : hasDifference
      ? 'DNS 解析存在差异'
      : unreachable.length
        ? `${unreachable.length} 个公共解析器未响应`
        : 'DNS 检测存在需要关注的结果';
  const details = [];
  if (resolvers.length) {
    const reachable = resolvers.filter((resolver) => resolver.reachable).length;
    details.push(`${reachable}/${resolvers.length} 个解析器返回响应`);
  }
  if (unreachable.length) details.push(`${unreachable.map((resolver) => resolver.name || resolver.id).join('、')} 未响应`);
  if (hasDifference) details.push('地址差异可能来自 CDN 调度或解析策略');
  if (!details.length) {
    warningIndexes.forEach((index) => {
      const detail = String(findings[index].detail || '').trim();
      if (detail && !details.includes(detail)) details.push(detail);
    });
  }
  if (!details.length) details.push('请展开查看各解析器结果');
  const merged = {
    ...findings[warningIndexes[0]],
    title,
    detail: `${details.join('；')}。`,
  };
  const warningSet = new Set(warningIndexes);
  return findings.flatMap((finding, index) => {
    if (index === warningIndexes[0]) return [merged];
    return warningSet.has(index) ? [] : [finding];
  });
}

function componentHasResults(component, collection) {
  return Boolean(component && (String(component.status || '').toLowerCase() === 'completed'
    || (Array.isArray(component[collection]) && component[collection].length > 0)));
}

function completedByIntegratedCheck(remediation, result) {
  const action = String(remediation?.action || '');
  if (PATH_ACTIONS.has(action) && componentHasResults(result?.paths, 'paths')) return true;
  if (action === 'diagnose_dns' && componentHasResults(result?.dns, 'resolvers')) return true;
  return false;
}

function prepareActions(remediations, attentionFindings, result) {
  const ranked = (Array.isArray(remediations) ? remediations : [])
    .filter((item) => !completedByIntegratedCheck(item, result))
    .map((item, index) => {
    const trigger = relatedFinding(item, attentionFindings);
    return {
      ...item,
      trigger,
      destination: remediationDestination(item),
      originalIndex: index,
      severity: severity(trigger?.level),
    };
  }).sort((left, right) =>
    right.severity - left.severity
      || Number(left.priority || 99) - Number(right.priority || 99)
      || left.originalIndex - right.originalIndex,
  );

  const seen = new Set();
  return ranked.filter((item) => {
    const group = remediationGroup(item);
    if (seen.has(group)) return false;
    seen.add(group);
    return true;
  });
}

export function buildIntegratedResultPresentation(result = {}) {
  const rawFindings = (Array.isArray(result.findings) ? result.findings : []).map((finding) => ({
    ...finding,
    layer: String(finding?.layer || 'network').toLowerCase(),
    level: integratedResultLevel(finding?.status),
  })).filter((finding) => finding.layer !== 'egress');
  const findings = mergeDNSWarnings(rawFindings, result);
  const pathComparisonComplete = componentHasResults(result.paths, 'paths');
  const isPathContext = (finding) => pathComparisonComplete
    && finding.layer === 'proxy'
    && ['系统代理已配置', '识别到本地代理或 TCP 服务'].includes(String(finding.title || ''));
  const attention = findings.filter((finding) =>
    ['critical', 'warning'].includes(finding.level) && !isPathContext(finding));
  const verified = findings.filter((finding) =>
    !['critical', 'warning'].includes(finding.level) || isPathContext(finding));
  const criticalCount = attention.filter((finding) => finding.level === 'critical').length;
  const warningCount = attention.filter((finding) => finding.level === 'warning').length;
  const lead = attention[0] || null;
  const actions = prepareActions(result.remediations, attention, result);
  const reportedLevel = integratedResultLevel(result.overall_status);
  const level = criticalCount || reportedLevel === 'critical'
    ? 'critical'
    : warningCount || (!findings.length && reportedLevel === 'warning')
      ? 'warning'
      : 'healthy';

  return {
    level,
    attention,
    verified,
    lead,
    actions,
    criticalCount,
    warningCount,
    statusTitle: criticalCount
      ? `${criticalCount} 个阻断问题，${warningCount} 项需要关注`
      : warningCount
        ? `${warningCount} 项需要关注`
        : level === 'critical'
          ? '检测未完整完成'
          : level === 'warning'
            ? '检测结果需要确认'
            : '未发现需要处理的问题',
    statusDetail: lead
      ? `${lead.title}${lead.detail ? `：${lead.detail}` : ''}`
      : result.message || (level === 'healthy'
        ? '关键链路检查完成，当前无需调整设置。'
        : '未获得完整诊断证据，请重新检测。'),
  };
}
