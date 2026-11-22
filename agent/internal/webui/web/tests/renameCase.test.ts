// Run with `npm test`. The web client has no filesystem: a case-only rename (Report.txt -> report.txt) reaches the
// agent as an ordinary rename whose src and dst differ only in case. Whether the host treats them as the same file
// is decided by the agent (agent/internal/fsops), not here; this pins that the client does not collapse them.
import assert from 'node:assert/strict';
import { test } from 'node:test';

import { renameBody } from '../src/lib/requests.ts';

test('a case-only rename is sent with src and dst that differ only in case', () => {
  assert.deepEqual(renameBody('/x/Report.txt', 'report.txt'), { src: '/x/Report.txt', dst: '/x/report.txt' });
  assert.deepEqual(renameBody('C:\\docs\\Report.txt', 'REPORT.TXT'), { src: 'C:\\docs\\Report.txt', dst: 'C:\\docs\\REPORT.TXT' });
});
