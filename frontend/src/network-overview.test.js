import assert from 'node:assert/strict';
import test from 'node:test';
import { summariseNetworkOverview } from './network-overview.js';

test('missing baseline cannot produce a healthy overall result', () => {
  const summary = summariseNetworkOverview({
    baseline: null,
    patrol: { status: 'completed', results: [{ result: { success: true } }] },
    selectedCount: 1,
    successCount: 1,
  });

  assert.equal(summary.level, 'warning');
  assert.equal(summary.headline, '整体检查未完整完成');
  assert.ok(summary.issues.some((issue) => issue.title === '网络基线未完整完成'));
});

test('missing site patrol cannot produce a healthy overall result', () => {
  const summary = summariseNetworkOverview({
    baseline: { status: 'completed' },
    patrol: null,
    selectedCount: 2,
  });

  assert.equal(summary.level, 'warning');
  assert.equal(summary.headline, '整体检查未完整完成');
  assert.ok(summary.issues.some((issue) => issue.title === '常用站点巡检未完整完成'));
});

test('healthy requires both tasks to complete without issues', () => {
  const summary = summariseNetworkOverview({
    baseline: { status: 'completed' },
    patrol: { status: 'completed', results: [{ result: { success: true } }] },
    selectedCount: 1,
    successCount: 1,
  });

  assert.deepEqual(summary, {
    level: 'healthy',
    headline: '当前网络与常用站点状态正常',
    detail: '1 / 1 个站点通过巡检',
    incomplete: false,
    issues: [],
  });
});

test('critical failures take precedence over an incomplete state', () => {
  const summary = summariseNetworkOverview({
    baseline: null,
    patrol: { status: 'completed', results: [{ result: { success: false } }] },
    baselineIssues: [{ status: 'critical', title: '默认路由不可用' }],
    selectedCount: 1,
  });

  assert.equal(summary.level, 'critical');
  assert.equal(summary.headline, '当前网络存在阻断问题');
});
