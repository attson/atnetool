import assert from 'node:assert/strict';
import test from 'node:test';

import { formatCloudflareColo } from './cloudflare-colos.js';

test('formats known Cloudflare colo codes as Chinese locations', () => {
  assert.equal(formatCloudflareColo('nrt'), '东京（NRT）');
  assert.equal(formatCloudflareColo('SIN'), '新加坡（SIN）');
});

test('supports English and preserves unknown colo codes', () => {
  assert.equal(formatCloudflareColo('ams', 'en-US'), 'Amsterdam (AMS)');
  assert.equal(formatCloudflareColo('xyz'), 'XYZ');
  assert.equal(formatCloudflareColo(''), '--');
});
