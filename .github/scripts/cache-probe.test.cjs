'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { probe } = require('./cache-probe.cjs');

test('warm probe requires a complete hit and the reviewed exact keys', () => {
  const env = { CACHE_HIT: 'true', CACHE_BYTES: '123', MATCHED_KEY: 'modules-seed,build-seed', EXPECT_HIT: 'true', EXPECTED_MATCH: 'modules-seed,build-seed', GITHUB_REF: 'refs/heads/probe' };
  assert.equal(probe(env).ref, 'refs/heads/probe');
  assert.throws(() => probe({ ...env, CACHE_HIT: 'false' }), /every snapshot/);
  assert.throws(() => probe({ ...env, CACHE_BYTES: '0' }), /positive payload/);
  assert.throws(() => probe({ ...env, MATCHED_KEY: 'unrelated-seed' }), /reviewed seed/);
});

test('miss probe rejects partial hits and discarded downloaded snapshots', () => {
  const env = { CACHE_HIT: 'false', CACHE_BYTES: '0', MATCHED_KEY: '', EXPECT_MISS: 'true' };
  assert.equal(probe(env).hit, 'false');
  assert.throws(() => probe({ ...env, MATCHED_KEY: 'partial-module-hit' }), /clean miss/);
  assert.throws(() => probe({ ...env, CACHE_BYTES: '100' }), /clean miss/);
  assert.throws(() => probe({ ...env, EXPECT_HIT: 'true' }), /mutually exclusive/);
});

test('probe records only allowlisted provenance and no credentials', () => {
  const result = probe({ CACHE_HIT: 'false', ACTIONS_RUNTIME_TOKEN: 'secret', ACTIONS_RESULTS_URL: 'https://private', GITHUB_SHA: 'source-sha', CACHE_KEY: 'fresh-key', PROBE_OUTCOME: 'failure', PROBE_SAVE_FAILURE: 'true' });
  assert.equal(result.revision, 'source-sha');
  assert.equal(result.fresh_keys, 'fresh-key');
  assert.equal(result.requested_outcome, 'failure');
  assert.equal(result.save_on_failure, 'true');
  assert.ok(!JSON.stringify(result).includes('secret'));
  assert.ok(!JSON.stringify(result).includes('https://private'));
});
