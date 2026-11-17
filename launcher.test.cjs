'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { invocation, run } = require('./launcher.cjs');
const { EventEmitter } = require('node:events');

function childResult(code = 0, error) {
  const child = new EventEmitter();
  queueMicrotask(() => error ? child.emit('error', error) : child.emit('close', code));
  return child;
}

test('all six platforms invoke the native executable without a shell', async () => {
  for (const platform of ['linux', 'darwin', 'win32']) {
    for (const arch of ['x64', 'arm64']) {
      const env = { 'INPUT_GO-VERSION': '1.26.8', 'INPUT_CHECK-LATEST': 'true' };
      const call = invocation('/path with spaces', false, env, platform, arch, ['--cache=off', '--json', 'report with spaces.json']);
      assert.deepEqual(call.args, ['setup', '--go-version', '1.26.8', '--go-version-file', '', '--check-latest=true', '--cache=off', '--json', 'report with spaces.json']);
      assert.ok(call.binary.includes(arch === 'x64' ? 'amd64' : 'arm64'));
      assert.equal(call.binary.endsWith('.exe'), platform === 'win32');
    }
  }
  await run(__dirname, false, (_binary, _args, options) => {
    assert.deepEqual(options, { stdio: 'inherit', shell: false });
    return childResult();
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

test('spawn errors and signal termination fail the action', async () => {
  const messages = [];
  const oldError = console.error;
  console.error = value => messages.push(value);
  try {
    await run(__dirname, false, () => childResult(0, new Error('missing executable\n::error::private-token')));
  } finally {
    console.error = oldError;
  }
  assert.equal(process.exitCode, 1);
  assert.match(messages[0], /Pin a bundled distribution/);
  assert.ok(!messages[0].includes('private-token'));
  await run(__dirname, false, () => childResult(null));
  assert.equal(process.exitCode, 1);
  process.exitCode = 0;
});

test('metadata uses unique per-action job-status policy and both launchers are packaged', () => {
  const metadata = fs.readFileSync(path.join(__dirname, 'action.yml'), 'utf8');
  const alias = fs.readFileSync(path.join(__dirname, 'experimental/node/action.yml'), 'utf8');
  assert.equal(metadata, alias);
  assert.match(metadata, /  summary:\r?\n    description: [^\r\n]+\r?\n    default: 'true'/);
  assert.ok(metadata.includes("post-if: \"!cancelled() && (success() || env[format('READY_SETUP_GO_SAVE_ON_FAILURE_{0}', github.action)] == 'true')\""));
  assert.ok(!metadata.includes('inputs.cache-save-on-failure'));
  const packaging = fs.readFileSync(path.join(__dirname, 'cmd/package/manifest.go'), 'utf8');
  for (const file of ['launcher.cjs', 'launch.cjs', 'post.cjs', 'experimental/node/post.cjs', 'experimental/composite/action.yml']) assert.ok(packaging.includes(`"${file}"`));
});

test('launcher forwards cancellation, waits for child cleanup, and removes listeners', async () => {
  for (const signal of ['SIGINT', 'SIGTERM']) {
    const before = ['SIGINT', 'SIGTERM'].map(name => process.listenerCount(name));
    const child = new EventEmitter();
    const received = [];
    child.kill = name => received.push(name);
    let finished = false;
    const pending = run(__dirname, false, () => child).then(() => { finished = true; });
    process.emit(signal);
    await Promise.resolve();
    assert.deepEqual(received, [signal]);
    assert.equal(finished, false, 'launcher must join native cleanup');
    child.emit('close', 0); // Even a non-strict native cancellation must fail the action.
    await pending;
    assert.equal(process.exitCode, 1);
    assert.deepEqual(['SIGINT', 'SIGTERM'].map(name => process.listenerCount(name)), before);
    process.exitCode = 0;
  }
});
require('./native-download.test.cjs');
