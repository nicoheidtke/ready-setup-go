'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { invocation } = require('./run.cjs');

test('passes paths and values as argv without shell interpretation or credentials', () => {
  const env = { INPUT_HELPER: 'C:\\with spaces\\cache-benchmark.exe', INPUT_GO: '/tool chain/go', INPUT_SOURCE: '$(not a shell)', INPUT_WORKLOAD: 'small', INPUT_STORAGE: 'chunked', 'INPUT_CHUNK-CONCURRENCY': '3', INPUT_VALIDATION: 'true', ACTIONS_RUNTIME_TOKEN: 'private-token' };
  const call = invocation(env);
  assert.equal(call.binary, env.INPUT_HELPER);
  assert.deepEqual(call.args, ['--go', '/tool chain/go', '--source', '$(not a shell)', '--workload', 'small', '--storage', 'chunked', '--chunk-concurrency', '3', '--validation']);
  assert.ok(!JSON.stringify(call).includes('private-token'));
});

test('requires a helper path', () => assert.throws(() => invocation({}), /helper path/));

test('passes independent download controls', () => {
  const call = invocation({ INPUT_HELPER: '/helper', 'INPUT_DOWNLOAD-CONCURRENCY': '1', 'INPUT_DOWNLOAD-CHUNK-BYTES': '1048576' });
  assert.deepEqual(call.args, ['--download-concurrency', '1', '--download-chunk-bytes', '1048576']);
});
