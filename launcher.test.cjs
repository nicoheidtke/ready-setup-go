'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { invocation, run } = require('./launcher.cjs');

test('all six platforms invoke the native executable without a shell', () => {
  for (const platform of ['linux', 'darwin', 'win32']) {
    for (const arch of ['x64', 'arm64']) {
      const env = { 'INPUT_GO-VERSION': '1.26.8', 'INPUT_CHECK-LATEST': 'true' };
      const call = invocation('/path with spaces', false, env, platform, arch, ['--cache=off', '--json', 'report with spaces.json']);
      assert.deepEqual(call.args, ['setup', '--go-version', '1.26.8', '--go-version-file', '', '--check-latest=true', '--cache=off', '--json', 'report with spaces.json']);
      assert.ok(call.binary.includes(arch === 'x64' ? 'amd64' : 'arm64'));
      assert.equal(call.binary.endsWith('.exe'), platform === 'win32');
    }
  }
  run(__dirname, false, (_binary, _args, options) => {
    assert.deepEqual(options, { stdio: 'inherit', shell: false });
    return { status: 0 };
  });
});

test('version file paths reach Go intact without shell interpolation', () => {
  const file = 'nested module/go.mod';
  const call = invocation('/root', false, { 'INPUT_GO-VERSION-FILE': file }, 'linux', 'x64', []);
  assert.deepEqual(call.args, ['setup', '--go-version', '', '--go-version-file', file, '--check-latest=false']);
});

test('post with no saved state does not start a process', () => {
  assert.equal(invocation('/root', true, {}, 'unsupported', 'unsupported'), null);
  assert.deepEqual(invocation('/root', true, { STATE_cache_state: '/private/state' }, 'linux', 'arm64', []).args, ['post']);
});

test('spawn errors and signal termination fail the action', () => {
  const messages = [];
  const oldError = console.error;
  console.error = value => messages.push(value);
  try {
    run(__dirname, false, () => ({ error: new Error('missing executable\n::error::private-token') }));
  } finally {
    console.error = oldError;
  }
  assert.equal(process.exitCode, 1);
  assert.match(messages[0], /Pin a bundled distribution/);
  assert.ok(!messages[0].includes('private-token'));
  run(__dirname, false, () => ({ status: null }));
  assert.equal(process.exitCode, 1);
  process.exitCode = 0;
});

test('metadata uses unique per-action job-status policy and both launchers are packaged', () => {
  const metadata = fs.readFileSync(path.join(__dirname, 'action.yml'), 'utf8');
  const alias = fs.readFileSync(path.join(__dirname, 'experimental/node/action.yml'), 'utf8');
  assert.equal(metadata, alias);
  assert.match(metadata, /  summary:\r?\n    description: [^\r\n]+\r?\n    default: 'true'/);
  assert.ok(metadata.includes("post-if: \"!cancelled() && (success() || env[format('SETUP_GO_BETTER_SAVE_ON_FAILURE_{0}', github.action)] == 'true')\""));
  assert.ok(!metadata.includes('inputs.cache-save-on-failure'));
  const packaging = fs.readFileSync(path.join(__dirname, 'cmd/package/manifest.go'), 'utf8');
  for (const file of ['launcher.cjs', 'launch.cjs', 'post.cjs', 'experimental/node/post.cjs', 'experimental/composite/action.yml']) assert.ok(packaging.includes(`"${file}"`));
});
