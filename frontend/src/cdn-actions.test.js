import test from 'node:test';
import assert from 'node:assert/strict';

import {
  getCFAccelerationBlockReason,
  getCDNGovernanceBlockReason,
} from './cdn-actions.js';

const verifiedResult = {
  best_ip: '104.16.1.20',
  target_validated: true,
  target_verification: { verified: true },
  effective_route: 'direct',
};

test('verified recommendation enables both CF actions', () => {
  assert.equal(getCFAccelerationBlockReason(verifiedResult), '');
  assert.equal(getCDNGovernanceBlockReason(verifiedResult), '');
});

test('failed recommendation validation returns an actionable reason', () => {
  const result = {
    ...verifiedResult,
    target_validated: false,
    target_validation: '推荐 IP 未通过目标域名验证：TLS 握手失败',
  };
  assert.match(getCFAccelerationBlockReason(result), /TLS 握手失败/);
  assert.match(getCFAccelerationBlockReason(result), /HTTPing/);
});

test('proxy result explains why only the compatibility gateway can be used', () => {
  assert.match(
    getCDNGovernanceBlockReason({ ...verifiedResult, effective_route: 'proxy' }),
    /不能生成直连治理配置/,
  );
  assert.equal(
    getCFAccelerationBlockReason({ ...verifiedResult, effective_route: 'proxy' }),
    '',
  );
});
