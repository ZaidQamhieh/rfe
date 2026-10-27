// The /pair and /register handlers accept only the certificate-bound proof (security.DeviceProofMessageV2),
// so the browser must sign exactly this layout. Audit F-168.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { deviceProofMessageV2 } from '../src/lib/deviceIdentity.ts';

test('the bound proof message matches the agent layout', () => {
  assert.equal(deviceProofMessageV2('AB12', 'nonce1'), 'rfe-device-proof-v2\nab12\nnonce1');
});
