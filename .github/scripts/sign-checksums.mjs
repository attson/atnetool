#!/usr/bin/env node

import crypto from 'node:crypto';
import fs from 'node:fs';

const sumsPath = process.argv[2];
if (!sumsPath) {
  console.error('usage: sign-checksums.mjs <SHA256SUMS path>');
  process.exit(2);
}

const privateKey = process.env.ATNETOOL_UPDATE_SIGNING_PRIVATE_KEY;
if (!privateKey) {
  console.error('ATNETOOL_UPDATE_SIGNING_PRIVATE_KEY is not configured');
  process.exit(2);
}

const checksums = fs.readFileSync(sumsPath);
const key = crypto.createPrivateKey({ key: privateKey, format: 'pem' });
const signature = crypto.sign(null, checksums, key);
fs.writeFileSync(`${sumsPath}.sig`, `${signature.toString('base64')}\n`);
console.log(`signed ${sumsPath} with Ed25519`);
