const MODULE_DESTINATIONS = {
  baseline: 'network-baseline',
  dns: 'dns-diagnosis',
  ports: 'port-test',
  http: 'diagnosis',
  paths: 'path-compare',
  proxy: 'path-compare',
  network: 'integrated',
};

const ACTION_DESTINATIONS = {
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

function number(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function comparableTarget(value) {
  return String(value || '').trim().replace(/\/$/, '').toLowerCase();
}

function routeScore(route) {
  const success = number(route?.success_rate);
  const latency = number(route?.p50_total_ms) || Number.MAX_SAFE_INTEGER;
  return { success, latency };
}

export function selectRecommendedRoute(pathsResult) {
  const paths = Array.isArray(pathsResult?.paths) ? pathsResult.paths : [];
  const recommendedID = pathsResult?.finding?.recommended_path_id;
  const explicit = paths.find((route) => route.id === recommendedID);
  if (explicit) return explicit;
  return [...paths].sort((left, right) => {
    const a = routeScore(left);
    const b = routeScore(right);
    return b.success - a.success || a.latency - b.latency;
  })[0] || null;
}

function selectCurrentRoute(result) {
  const paths = Array.isArray(result?.paths?.paths) ? result.paths.paths : [];
  const mode = result?.http?.diagnosis?.proxy_mode || 'system';
  return paths.find((route) => route.id === mode) || paths.find((route) => route.id === 'system') || null;
}

function improvementPercent(baseline, candidate) {
  const before = number(baseline);
  const after = number(candidate);
  if (before <= 0 || after <= 0) return 0;
  return ((before - after) / before) * 100;
}

function latestIntegrated(historyRecords, target = '') {
  const normalizedTarget = comparableTarget(target);
  return (Array.isArray(historyRecords) ? historyRecords : [])
    .filter((record) => record?.record_kind === 'integrated' && record.integrated)
    .filter((record) => !normalizedTarget || comparableTarget(record.target_url) === normalizedTarget)
    .sort((left, right) => new Date(right.checked_at || 0) - new Date(left.checked_at || 0));
}

function historyComparison(historyRecords, target) {
  const records = latestIntegrated(historyRecords, target);
  if (records.length < 2) return null;
  const after = records[0].integrated;
  const before = records[1].integrated;
  const beforeHTTP = before?.http?.diagnosis || {};
  const afterHTTP = after?.http?.diagnosis || {};
  return {
    beforeAt: records[1].checked_at || before.started_at || '',
    afterAt: records[0].checked_at || after.started_at || '',
    beforeHealth: before.overall_status || 'critical',
    afterHealth: after.overall_status || 'critical',
    totalBefore: number(beforeHTTP.total_ms),
    totalAfter: number(afterHTTP.total_ms),
    totalImprovement: improvementPercent(beforeHTTP.total_ms, afterHTTP.total_ms),
    ttfbBefore: number(beforeHTTP.ttfb_ms),
    ttfbAfter: number(afterHTTP.ttfb_ms),
    ttfbImprovement: improvementPercent(beforeHTTP.ttfb_ms, afterHTTP.ttfb_ms),
  };
}

function taskDestination(remediation) {
  return ACTION_DESTINATIONS[remediation?.action]
    || MODULE_DESTINATIONS[remediation?.module]
    || 'integrated';
}

function buildTasks(result, recommended, current) {
  const remediations = Array.isArray(result?.remediations) ? result.remediations : [];
  const tasks = remediations.map((item, index) => ({
    id: `${item.action || item.module || 'task'}-${index}`,
    priority: number(item.priority) || index + 1,
    title: item.title || '继续验证网络问题',
    detail: item.detail || '',
    action: item.action || '',
    module: item.module || 'network',
    destination: taskDestination(item),
    kind: 'guided',
  }));

  const routeChanged = recommended && current && recommended.id !== current.id;
  if (routeChanged && !tasks.some((task) => task.destination === 'path-compare')) {
    tasks.unshift({
      id: 'recommended-route',
      priority: 1,
      title: `使用${recommended.label || '推荐路径'}复测`,
      detail: `当前路径 ${current.label || current.id}，推荐路径 ${recommended.label || recommended.id}。先复测确认收益，不修改系统网络设置。`,
      action: 'retest_recommended_route',
      module: 'paths',
      destination: 'path-compare',
      kind: 'retest',
    });
  }

  return tasks
    .sort((left, right) => left.priority - right.priority)
    .slice(0, 8);
}

export function buildGovernanceCenterModel({
  integratedResult = null,
  historyRecords = [],
  accelerationStatus = null,
} = {}) {
  const fallbackRecord = latestIntegrated(historyRecords)[0];
  const result = integratedResult || fallbackRecord?.integrated || null;
  if (!result) {
    return {
      state: 'idle',
      health: 'idle',
      title: '等待诊断基线',
      target: '',
      tasks: [],
      stages: ['pending', 'pending', 'pending', 'pending'],
      route: null,
      comparison: null,
      acceleration: accelerationStatus || null,
    };
  }

  const target = result.target_url || fallbackRecord?.target_url || '';
  const recommended = selectRecommendedRoute(result.paths);
  const current = selectCurrentRoute(result);
  const routeImprovement = improvementPercent(current?.p50_total_ms, recommended?.p50_total_ms);
  const tasks = buildTasks(result, recommended, current);
  const comparison = historyComparison(historyRecords, target);
  const accelerationActive = Boolean(accelerationStatus?.active || accelerationStatus?.can_restore);
  const health = ['healthy', 'warning', 'critical'].includes(result.overall_status)
    ? result.overall_status
    : 'warning';

  return {
    state: tasks.length ? 'ready' : 'complete',
    health,
    title: health === 'critical'
      ? '先处理阻断问题'
      : health === 'warning'
        ? '已生成治理顺序'
        : '当前网络状态健康',
    target,
    checkedAt: result.finished_at || result.started_at || fallbackRecord?.checked_at || '',
    findingCount: Array.isArray(result.findings) ? result.findings.length : 0,
    attentionCount: (result.findings || []).filter((item) => ['critical', 'warning'].includes(item.status)).length,
    tasks,
    route: recommended ? {
      current,
      recommended,
      improvement: routeImprovement,
      mode: recommended.id || 'system',
      manualProxyURL: recommended.configured_url || '',
      changed: Boolean(current && recommended.id !== current.id),
    } : null,
    comparison,
    acceleration: accelerationStatus || null,
    stages: [
      'complete',
      recommended ? 'complete' : 'pending',
      accelerationActive ? 'active' : tasks.length ? 'ready' : 'complete',
      comparison ? 'complete' : 'pending',
    ],
  };
}

