import test from 'node:test';
import assert from 'node:assert/strict';

import {
  buildIntegratedResultPresentation,
  integratedResultLevel,
  remediationDestination,
} from './integrated-result.js';

test('presentation separates issues from verified findings', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'tls', status: 'critical', title: 'TLS 握手失败', detail: '证书校验未完成' },
      { layer: 'dns', status: 'warning', title: 'DNS 结果存在差异' },
      { layer: 'local', status: 'healthy', title: '活动接口正常' },
      { layer: 'route', status: 'neutral', title: '路由未显示完整终点' },
    ],
  });

  assert.equal(model.level, 'critical');
  assert.equal(model.criticalCount, 1);
  assert.equal(model.warningCount, 1);
  assert.equal(model.attention.length, 2);
  assert.equal(model.verified.length, 2);
  assert.match(model.statusTitle, /1 个阻断问题/);
  assert.match(model.statusDetail, /TLS 握手失败/);
});

test('presentation removes path actions after the integrated check already compared paths', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'tls', status: 'critical', title: '目标请求未完成' },
      { layer: 'proxy', status: 'warning', title: '系统代理已配置' },
      { layer: 'paths', status: 'warning', title: '强制直连响应更快' },
    ],
    remediations: [
      { priority: 1, action: 'compare_proxy', module: 'baseline', title: '分别验证代理与直连' },
      { priority: 2, action: 'route', module: 'paths', title: '采用路径对照结论验证' },
      { priority: 1, action: 'isolate', module: 'tls', title: '处理 TLS 失败' },
      { priority: 2, action: 'inspect_tls', module: 'ports', title: '检查 TLS 配置' },
    ],
    paths: {
      status: 'completed',
      paths: [{ id: 'system' }, { id: 'direct' }],
    },
  });

  assert.deepEqual(model.actions.map((item) => item.title), [
    '处理 TLS 失败',
  ]);
  assert.equal(model.actions[0].trigger.title, '目标请求未完成');
  assert.equal(model.attention.some((item) => item.title === '系统代理已配置'), false);
  assert.equal(model.verified.some((item) => item.title === '系统代理已配置'), true);
});

test('presentation removes DNS deep diagnosis after resolver results are available', () => {
  const model = buildIntegratedResultPresentation({
    findings: [{ layer: 'dns', status: 'warning', title: 'DNS 结果存在差异' }],
    remediations: [{ action: 'diagnose_dns', module: 'baseline', title: '运行 DNS 深度诊断' }],
    dns: { status: 'completed', resolvers: [{ id: 'system' }] },
  });

  assert.equal(model.actions.length, 0);
});

test('configured proxy is context rather than an unresolved issue after path comparison', () => {
  const model = buildIntegratedResultPresentation({
    overall_status: 'warning',
    findings: [{ layer: 'proxy', status: 'warning', title: '系统代理已配置' }],
    paths: { status: 'completed', paths: [{ id: 'system' }, { id: 'direct' }] },
  });

  assert.equal(model.level, 'healthy');
  assert.equal(model.statusTitle, '未发现需要处理的问题');
});

test('presentation excludes generic public egress probes from a target site check', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'egress', status: 'warning', title: '公网访问依赖代理' },
      { layer: 'paths', status: 'healthy', title: '目标网站直连正常' },
    ],
  });

  assert.equal(model.attention.length, 0);
  assert.deepEqual(model.verified.map((item) => item.title), ['目标网站直连正常']);
});

test('presentation merges DNS difference and unreachable resolver warnings', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'dns', status: 'warning', title: 'DNS 结果存在差异' },
      { layer: 'dns', status: 'warning', title: '部分公共解析器不可达' },
    ],
    dns: {
      comparison: { address_difference: true },
      resolvers: [
        { name: '系统 DNS', transport: 'system', reachable: true },
        { name: 'Cloudflare DNS', transport: 'udp', reachable: false },
        { name: 'AliDNS', transport: 'udp', reachable: true },
      ],
    },
  });

  assert.equal(model.attention.length, 1);
  assert.equal(model.attention[0].title, 'DNS 解析存在差异，1 个公共解析器未响应');
  assert.match(model.attention[0].detail, /Cloudflare DNS 未响应/);
});

test('merged legacy DNS warnings retain a useful detail without resolver evidence', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'dns', status: 'warning', title: 'DNS 通道异常', detail: 'UDP 查询失败' },
      { layer: 'dns', status: 'warning', title: 'DNS 请求不稳定', detail: '部分请求超时' },
    ],
  });

  assert.equal(model.attention.length, 1);
  assert.match(model.attention[0].detail, /UDP 查询失败/);
  assert.match(model.attention[0].detail, /部分请求超时/);
});

test('DNS actions attach to DNS findings instead of broad baseline findings', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'egress', status: 'warning', title: '公网访问依赖代理' },
      { layer: 'dns', status: 'warning', title: 'DNS 结果存在差异' },
    ],
    remediations: [
      { priority: 2, action: 'diagnose_dns', module: 'baseline', title: '运行 DNS 深度诊断' },
    ],
  });

  assert.equal(model.actions[0].trigger.title, 'DNS 结果存在差异');
});

test('specific actions attach to the most relevant issue', () => {
  const model = buildIntegratedResultPresentation({
    findings: [
      { layer: 'dns', status: 'warning', title: 'DNS 结果存在差异' },
      { layer: 'proxy', status: 'warning', title: '系统代理已配置' },
      { layer: 'dns', status: 'warning', title: '部分公共解析器不可达' },
    ],
    remediations: [
      { action: 'compare_proxy', module: 'baseline', title: '分别验证系统代理与强制直连' },
      { action: 'verify_policy', module: 'dns', title: '核对 DNS 出口策略' },
    ],
  });

  assert.equal(model.actions[0].trigger.title, '系统代理已配置');
  assert.equal(model.actions[1].trigger.title, 'DNS 解析存在差异');
});

test('actions route to the relevant specialist module', () => {
  assert.equal(remediationDestination({ action: 'diagnose_dns', module: 'baseline' }), 'dns-diagnosis');
  assert.equal(remediationDestination({ action: 'inspect_tls', module: 'ports' }), 'diagnosis');
  assert.equal(remediationDestination({ module: 'ports' }), 'port-test');
  assert.equal(integratedResultLevel('completed'), 'neutral');
});

test('reported failures cannot be presented as healthy when findings are incomplete', () => {
  const model = buildIntegratedResultPresentation({
    overall_status: 'critical',
    message: '部分模块未返回结果',
    findings: [{ layer: 'local', status: 'healthy', title: '本地网络正常' }],
  });

  assert.equal(model.level, 'critical');
  assert.equal(model.statusTitle, '检测未完整完成');
  assert.equal(model.statusDetail, '部分模块未返回结果');
});
