import test from 'node:test';
import assert from 'node:assert/strict';

import {
  buildImportedReportReview,
  escapeImportedReportHTML,
  unwrapRouteLongitudes,
} from './imported-report-review.js';

test('integrated report exposes overview, five evidence modules, and offline route data', () => {
  const record = {
    record_kind: 'integrated',
    history_id: 'integrated-1',
    checked_at: '2026-10-26T08:00:00Z',
    target_url: 'https://example.com',
    integrated: {
      overall_status: 'warning',
      message: '综合诊断完成',
      target_url: 'https://example.com',
      duration_ms: 1680,
      completed: 5,
      total: 5,
      findings: [
        { layer: 'dns', status: 'warning', title: '公共解析器结果不同', detail: '建议核对解析策略' },
      ],
      remediations: [
        { priority: 1, module: 'dns', action: 'review-dns', title: '检查 DNS', detail: '对照系统与公共解析器' },
      ],
      baseline: { status: 'completed', message: '本地环境已采集', duration_ms: 120 },
      dns: { status: 'completed', message: 'DNS 分析完成', duration_ms: 210, total: 3 },
      ports: { status: 'completed', message: '端口检测完成', duration_ms: 160, total_count: 2, open_count: 2 },
      http: {
        status: 'completed',
        diagnosis: { success: true, total_ms: 430, ttfb_ms: 180, status_code: 200 },
        traceroute: {
          target_host: 'example.com',
          reached: true,
          hops: [
            { hop: 1, addresses: ['192.168.1.1'], rtt_ms: [1.2] },
            { hop: 2, addresses: ['203.0.113.8'], rtt_ms: [22.5], geo: { city: 'Shanghai', country: 'China', latitude: 31.23, longitude: 121.47 } },
          ],
        },
      },
      paths: { status: 'completed', message: '路径对照完成', duration_ms: 760, paths: [] },
    },
  };

  const review = buildImportedReportReview(record, { kind_label: '综合诊断' });

  assert.equal(review.kind, 'integrated');
  assert.equal(review.health, 'warning');
  assert.equal(review.target, 'https://example.com');
  assert.deepEqual(review.metrics.map((metric) => metric.id), [
    'http_total',
    'ttfb',
    'duration',
    'modules',
  ]);
  assert.deepEqual(review.modules.map((module) => module.id), [
    'baseline',
    'dns',
    'ports',
    'http',
    'paths',
  ]);
  assert.equal(review.findings.length, 1);
  assert.equal(review.remediations.length, 1);
  assert.equal(review.route.hops.length, 2);
  assert.equal(review.route.points.length, 1);
  assert.deepEqual(review.route.points[0].coordinates, [121.47, 31.23]);
});

test('all imported history kinds expose type-specific metrics and evidence', () => {
  const cases = [
    {
      kind: 'diagnosis',
      record: {
        success: true,
        target_url: 'https://example.com',
        dns_lookup_ms: 12,
        tcp_connect_ms: 30,
        tls_handshake_ms: 45,
        ttfb_ms: 90,
        total_ms: 140,
        status_code: 200,
        proxy_status: 'direct',
        tls_version: 'TLS 1.3',
      },
      metrics: ['dns', 'tcp', 'tls', 'ttfb', 'total'],
      modules: ['request', 'proxy', 'tls'],
    },
    {
      kind: 'stability',
      record: {
        target_url: 'https://example.com',
        stability: {
          status: 'completed', availability_percent: 99.5, p50_ms: 120, p95_ms: 260,
          jitter_ms: 18, sample_count: 20, incidents: [{ started_at: '2026-10-26T08:03:00Z' }],
          samples: [{ sequence: 1, success: true, total_ms: 120 }],
          finding: { level: 'warning', title: '存在轻微波动', detail: 'P95 偏高' },
        },
      },
      metrics: ['availability', 'p50', 'p95', 'jitter', 'samples'],
      modules: ['summary', 'incidents', 'samples'],
    },
    {
      kind: 'site_patrol',
      record: {
        site_patrol: {
          status: 'completed', total_count: 3, success_count: 2, average_ms: 180,
          results: [{
            index: 0,
            target: { id: 'example', name: 'Example', url: 'https://example.com', category: 'global' },
            result: { success: true, status_code: 200, total_ms: 120, proxy_status: 'direct', remote_ip: '93.184.216.34' },
          }],
          findings: [{ level: 'warning', title: '一个站点异常', detail: '需要复核' }],
        },
      },
      metrics: ['pass_rate', 'average', 'success', 'total'],
      modules: ['sites'],
    },
    {
      kind: 'cdn_optimize',
      record: {
        target_url: 'example.com',
        cdn_optimize: {
          status: 'completed', domain: 'example.com', best_ip: '1.1.1.1', baseline_ip: '1.0.0.1',
          improvement_percent: 32.4, candidate_count: 100, eligible_count: 8,
          finding: { level: 'good', title: '发现更优节点', detail: '改善明显' },
          governance: [{ action: 'apply', title: '应用优选结果', detail: '仅用于当前路径' }],
          results: [{
            candidate: { ip: '1.1.1.1', source: 'official-ipv4' },
            eligible: true,
            rank: 1,
            average_latency_ms: 42,
            p50_total_ms: 48,
          }],
        },
      },
      metrics: ['baseline_ip', 'best_ip', 'improvement', 'eligible'],
      modules: ['selection', 'candidates', 'path'],
    },
    {
      kind: 'network_baseline',
      record: {
        network_baseline: {
          status: 'completed', duration_ms: 480,
          ip_stacks: [{ family: 'IPv4', available: true }, { family: 'IPv6', available: false }],
          local_network: { active_interfaces: ['en0'], default_route_count: 1 },
          dns_probes: [{ resolver: 'system', success: true, duration_ms: 12 }],
          proxy_listeners: [{ endpoint: '127.0.0.1:7890' }],
          findings: [{ status: 'warning', title: 'IPv6 不可用', detail: '当前仅 IPv4 可用' }],
          remediations: [{ action: 'check-ipv6', title: '检查 IPv6', detail: '核对路由器配置' }],
        },
      },
      metrics: ['duration', 'interfaces', 'ipv4', 'ipv6', 'proxies'],
      modules: ['local', 'dns', 'proxy'],
    },
    {
      kind: 'path_compare',
      record: {
        path_compare: {
          status: 'completed', target_url: 'https://example.com',
          finding: { level: 'good', title: '直连更快', detail: '建议使用直连', recommended_path_id: 'direct', improvement_percent: 18.2 },
          paths: [{ id: 'direct', label: '强制直连', success_rate: 1, p50_total_ms: 100, jitter_ms: 4 }],
        },
      },
      metrics: ['recommended', 'p50', 'success', 'jitter', 'improvement'],
      modules: ['routes'],
    },
    {
      kind: 'dns_diagnosis',
      record: {
        dns_diagnosis: {
          status: 'completed', domain: 'example.com', duration_ms: 85, total: 3,
          comparison: { reachable_resolvers: 3, public_address_sets: 1, system_matches_public: true },
          resolvers: [{
            id: 'system',
            name: '系统 DNS',
            transport: 'system',
            reachable: true,
            duration_ms: 12,
            queries: [{
              type: 'A',
              responded: true,
              success: true,
              duration_ms: 12,
              records: [{ type: 'A', name: 'example.com.', value: '93.184.216.34', ttl: 300 }],
            }],
          }],
          findings: [{ status: 'healthy', title: '解析一致', detail: '未发现异常' }],
          remediations: [],
        },
      },
      metrics: ['duration', 'resolvers', 'reachable', 'address_sets', 'consistent'],
      modules: ['resolvers', 'records'],
    },
    {
      kind: 'port_test',
      record: {
        port_test: {
          status: 'completed', target: 'example.com', duration_ms: 190, total_count: 2,
          open_count: 1, refused_count: 1, timeout_count: 0,
          results: [{ port: 443, service: 'HTTPS', reachable: true, latency_ms: 28, tls_version: 'TLS 1.3' }],
          findings: [{ status: 'warning', title: '部分端口关闭', detail: '443 可达' }],
          remediations: [{ action: 'check-port', title: '核对端口策略', detail: '确认预期开放范围' }],
        },
      },
      metrics: ['duration', 'total', 'open', 'refused', 'timeout'],
      modules: ['summary', 'ports'],
    },
  ];

  for (const item of cases) {
    const review = buildImportedReportReview({ record_kind: item.kind, ...item.record });
    assert.equal(review.kind, item.kind, item.kind);
    assert.deepEqual(review.metrics.map((entry) => entry.id), item.metrics, `${item.kind} metrics`);
    assert.deepEqual(review.modules.map((entry) => entry.id), item.modules, `${item.kind} modules`);
  }
});

test('real Go JSON nesting is mapped into site, candidate, and DNS record evidence', () => {
  const patrol = buildImportedReportReview({
    record_kind: 'site_patrol',
    site_patrol: {
      status: 'completed', total_count: 1, success_count: 1,
      results: [{
        index: 0,
        target: { id: 'example', name: 'Example', url: 'https://example.com' },
        result: { success: true, status_code: 204, total_ms: 86, remote_ip: '93.184.216.34' },
      }],
    },
  });
  const cdn = buildImportedReportReview({
    record_kind: 'cdn_optimize',
    cdn_optimize: {
      status: 'completed', best_ip: '1.1.1.1',
      results: [{ candidate: { ip: '1.1.1.1', source: 'official-ipv4' }, rank: 1, eligible: true }],
    },
  });
  const dns = buildImportedReportReview({
    record_kind: 'dns_diagnosis',
    dns_diagnosis: {
      status: 'completed',
      resolvers: [{
        id: 'cloudflare-udp', name: 'Cloudflare DNS', transport: 'udp',
        queries: [{ type: 'AAAA', success: true, duration_ms: 18, records: [{ name: 'example.com.', value: '2606:2800:220:1:248:1893:25c8:1946', ttl: 240 }] }],
      }],
    },
  });

  assert.deepEqual(patrol.modules[0].details[0], {
    name: 'Example',
    target_url: 'https://example.com',
    success: true,
    status_code: 204,
    total_ms: 86,
    proxy_status: undefined,
    remote_ip: '93.184.216.34',
    message: '',
  });
  assert.equal(cdn.modules.find((item) => item.id === 'candidates').details[0].ip, '1.1.1.1');
  assert.equal(cdn.modules.find((item) => item.id === 'candidates').details[0].source, 'official-ipv4');
  assert.deepEqual(dns.modules.find((item) => item.id === 'records').details[0], {
    resolver: 'Cloudflare DNS',
    transport: 'udp',
    record_type: 'AAAA',
    name: 'example.com.',
    value: '2606:2800:220:1:248:1893:25c8:1946',
    ttl: 240,
    duration_ms: 18,
  });
});

test('route longitudes unwrap across the international date line', () => {
  assert.deepEqual(unwrapRouteLongitudes([
    { coordinates: [170, 35] },
    { coordinates: [-175, 37] },
    { coordinates: [-165, 38] },
  ]), [170, 185, 195]);
  assert.deepEqual(unwrapRouteLongitudes([
    { coordinates: [-170, 35] },
    { coordinates: [175, 37] },
  ]), [-170, -185]);
});

test('missing optional values remain uncollected and partial route coordinates stay offline', () => {
  const review = buildImportedReportReview({
    record_kind: 'diagnosis',
    success: false,
    target_url: 'https://old.example',
    message: '旧报告',
    traceroute: {
      hops: [
        { hop: 1, addresses: ['10.0.0.1'], timed_out: false },
        { hop: 2, addresses: ['203.0.113.2'], geo: { city: 'Tokyo', latitude: 35.68, longitude: 139.69 } },
        { hop: 3, addresses: ['203.0.113.3'], geo: { city: 'Unknown', latitude: 0, longitude: 0 } },
      ],
    },
  });

  assert.equal(review.metrics.find((entry) => entry.id === 'dns').value, null);
  assert.equal(review.route.hops.length, 3);
  assert.equal(review.route.points.length, 1);
  assert.equal(review.route.points[0].label, 'Tokyo');
});

test('unsupported report types fail explicitly without inventing evidence', () => {
  assert.throws(
    () => buildImportedReportReview({ record_kind: 'future_kind' }),
    /Unsupported imported report kind: future_kind/,
  );
});

test('report strings are escaped before being inserted into review markup', () => {
  assert.equal(
    escapeImportedReportHTML('<img src=x onerror="alert(1)">&'),
    '&lt;img src=x onerror=&quot;alert(1)&quot;&gt;&amp;',
  );
});

test('review health preserves HTTP failures and patrol findings instead of reporting false healthy', () => {
  const serverFailure = buildImportedReportReview({
    record_kind: 'diagnosis',
    success: true,
    status_code: 503,
    target_url: 'https://example.com',
  });
  const slowResponse = buildImportedReportReview({
    record_kind: 'diagnosis',
    success: true,
    status_code: 200,
    ttfb_ms: 1200,
    target_url: 'https://example.com',
  });
  const patrolFinding = buildImportedReportReview({
    record_kind: 'site_patrol',
    site_patrol: {
      status: 'completed',
      total_count: 2,
      success_count: 2,
      findings: [{ level: 'critical', title: '共同故障', message: '两个站点均存在错误响应' }],
    },
  });

  assert.equal(serverFailure.health, 'critical');
  assert.equal(serverFailure.modules.find((item) => item.id === 'request').status, 'critical');
  assert.equal(slowResponse.health, 'warning');
  assert.equal(patrolFinding.health, 'critical');
});

test('zero-value Go finding structs do not create phantom warnings', () => {
  const stability = buildImportedReportReview({
    record_kind: 'stability',
    stability: {
      status: 'completed', success_count: 3, failure_rate: 0,
      finding: { code: '', level: '', title: '', message: '' },
    },
  });
  const path = buildImportedReportReview({
    record_kind: 'path_compare',
    path_compare: {
      status: 'completed',
      finding: { code: '', level: '', title: '', detail: '', recommended_path_id: '' },
      paths: [{ id: 'direct', success_count: 3, success_rate: 1 }],
    },
  });

  assert.equal(stability.health, 'healthy');
  assert.deepEqual(stability.findings, []);
  assert.equal(path.health, 'healthy');
  assert.deepEqual(path.findings, []);
});

test('failed structured tasks and zero-success monitoring remain critical', () => {
  const failedDNS = buildImportedReportReview({
    record_kind: 'dns_diagnosis',
    dns_diagnosis: { status: 'failed', findings: [] },
  });
  const emptyStability = buildImportedReportReview({
    record_kind: 'stability',
    stability: {
      status: 'completed', success_count: 0, failure_rate: 1,
      finding: { code: '', level: '', title: '', message: '' },
    },
  });

  assert.equal(failedDNS.health, 'critical');
  assert.equal(emptyStability.health, 'critical');
});
