import test from 'node:test';
import assert from 'node:assert/strict';

import {
  buildGovernanceCenterModel,
  selectRecommendedRoute,
} from './governance-center.js';

function diagnosis(overrides = {}) {
  return {
    target_url: 'https://example.com/',
    overall_status: 'warning',
    started_at: '2026-10-27T10:00:00Z',
    findings: [{ status: 'warning', title: '代理路径较慢' }],
    remediations: [{ priority: 1, action: 'compare_proxy', module: 'baseline', title: '对比连接路径', detail: '验证直连与代理。' }],
    http: { diagnosis: { proxy_mode: 'system', total_ms: 320, ttfb_ms: 240 } },
    paths: {
      finding: { recommended_path_id: 'direct' },
      paths: [
        { id: 'system', label: '系统代理', success_rate: 1, p50_total_ms: 320 },
        { id: 'direct', label: '强制直连', success_rate: 1, p50_total_ms: 160 },
      ],
    },
    ...overrides,
  };
}

test('selectRecommendedRoute honours backend recommendation', () => {
  const result = diagnosis().paths;
  assert.equal(selectRecommendedRoute(result)?.id, 'direct');
});

test('governance model turns remediations into routed tasks', () => {
  const model = buildGovernanceCenterModel({ integratedResult: diagnosis() });
  assert.equal(model.state, 'ready');
  assert.equal(model.route.mode, 'direct');
  assert.equal(Math.round(model.route.improvement), 50);
  assert.equal(model.tasks[0].destination, 'path-compare');
  assert.deepEqual(model.stages, ['complete', 'complete', 'ready', 'pending']);
});

test('governance model compares the latest two same-target runs', () => {
  const records = [
    {
      record_kind: 'integrated',
      checked_at: '2026-10-27T10:10:00Z',
      target_url: 'https://example.com/',
      integrated: diagnosis({
        overall_status: 'healthy',
        http: { diagnosis: { proxy_mode: 'direct', total_ms: 120, ttfb_ms: 80 } },
      }),
    },
    {
      record_kind: 'integrated',
      checked_at: '2026-10-27T10:00:00Z',
      target_url: 'https://example.com/',
      integrated: diagnosis(),
    },
  ];
  const model = buildGovernanceCenterModel({ historyRecords: records });
  assert.equal(model.comparison.totalBefore, 320);
  assert.equal(model.comparison.totalAfter, 120);
  assert.equal(Math.round(model.comparison.totalImprovement), 63);
  assert.equal(model.stages[3], 'complete');
});

test('governance model exposes recoverable acceleration state', () => {
  const model = buildGovernanceCenterModel({
    integratedResult: diagnosis(),
    accelerationStatus: { active: true, can_restore: true, domain: 'example.com' },
  });
  assert.equal(model.acceleration.active, true);
  assert.equal(model.stages[2], 'active');
});

test('governance model remains useful before the first diagnosis', () => {
  const model = buildGovernanceCenterModel();
  assert.equal(model.state, 'idle');
  assert.equal(model.tasks.length, 0);
  assert.deepEqual(model.stages, ['pending', 'pending', 'pending', 'pending']);
});
