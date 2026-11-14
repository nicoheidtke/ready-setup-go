"use strict";
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const fsp = fs.promises;
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { ensureNative, parseManifest } = require('./native-download.cjs');

const payload = Buffer.from('verified native fixture');
const digest = crypto.createHash('sha256').update(payload).digest('hex');
function metadata(platform = 'linux/amd64', change = {}) {
  return { format: 'sgf-native-v1', base_url: 'https://github.com/owner/action/releases/download/v1.2.3', platforms: {
    [platform]: { sha256: digest, size: payload.length, ...change }
  } };
}
async function fixture(t, manifest = metadata()) {
  const root = await fsp.mkdtemp(path.join(os.tmpdir(), 'native-download-test-'));
  t.after(() => fsp.rm(root, { recursive: true, force: true }));
  await fsp.writeFile(path.join(root, 'NATIVE_DOWNLOADS.json'), JSON.stringify(manifest));
  return { root, env: { RUNNER_TEMP: path.join(root, 'runner temp') } };
}
function response(data = payload, status = 200, headers = {}) { return new Response(data, { status, headers }); }

test('native metadata selects exact platform assets and rejects unsafe locators or bounds', () => {
  for (const platform of ['linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64', 'windows/arm64']) {
    const asset = parseManifest(JSON.stringify(metadata(platform)), platform);
    assert.equal(asset.url, `https://github.com/owner/action/releases/download/v1.2.3/setup-go-faster-${platform.replace('/', '-')}${platform.startsWith('windows/') ? '.exe' : ''}`);
  }
  for (const base of ['http://github.com/a/b/releases/download/v1', 'https://user:token@github.com/a/b/releases/download/v1', 'https://github.com/a/b/releases/latest/download', 'https://evil.test/a/b/releases/download/v1', 'https://github.com/a/b/releases/download/v1?token=secret']) {
    const m = metadata(); m.base_url = base;
    assert.throws(() => parseManifest(JSON.stringify(m), 'linux/amd64'));
  }
  for (const change of [{ size: 0 }, { size: 256 * 1024 * 1024 + 1 }, { sha256: 'bad' }]) {
    assert.throws(() => parseManifest(JSON.stringify(metadata('linux/amd64', change)), 'linux/amd64'));
  }
  assert.throws(() => parseManifest(JSON.stringify(metadata()), 'linux/arm64'));
});

test('downloads one platform, verifies before publication and reuses main/post bytes offline', async t => {
  const { root, env } = await fixture(t);
  const calls = [];
  const binary = await ensureNative(root, 'linux/amd64', { env, fetch: async (url, options) => {
    calls.push(url);
    assert.equal(options.redirect, 'manual');
    assert.equal(options.credentials, 'omit');
    assert.ok(!Object.keys(options.headers).some(k => k.toLowerCase() === 'authorization'));
    return response(payload, 200, { 'content-length': payload.length });
  } });
  assert.equal(calls.length, 1);
  assert.ok(calls[0].endsWith('/setup-go-faster-linux-amd64'));
  assert.deepEqual(await fsp.readFile(binary), payload);
  const again = await ensureNative(root, 'linux/amd64', { env, fetch: () => { throw new Error('must stay offline'); } });
  assert.equal(binary, again);
  await fsp.writeFile(binary, Buffer.alloc(payload.length, 120));
  await assert.rejects(ensureNative(root, 'linux/amd64', { env, fetch: () => { throw new Error('do not overwrite'); } }), { code: 'existing' });
  assert.deepEqual(await fsp.readFile(binary), Buffer.alloc(payload.length, 120));
});

test('failed digest, lengths, body errors and cancellation never publish partial executables', async t => {
  for (const kind of ['digest', 'short', 'long', 'header', 'encoding', 'stream', 'cancel']) {
    await t.test(kind, async t => {
      const { root, env } = await fixture(t);
      const controller = new AbortController();
      const fetch = async () => {
        if (kind === 'cancel') { controller.abort(); throw new Error('secret URL'); }
        if (kind === 'digest') return response(Buffer.alloc(payload.length, 120));
        if (kind === 'short') return response(payload.subarray(1));
        if (kind === 'long') return response(Buffer.concat([payload, payload]));
        if (kind === 'header') return response(payload, 200, { 'content-length': '999' });
        if (kind === 'encoding') return response(payload, 200, { 'content-encoding': 'gzip' });
        return new Response(new ReadableStream({ start(c) { c.enqueue(payload.subarray(0, 3)); c.error(new Error('broken')); } }));
      };
      await assert.rejects(ensureNative(root, 'linux/amd64', { env, fetch, signal: controller.signal }));
      assert.deepEqual(await fsp.readdir(path.join(env.RUNNER_TEMP, 'setup-go-faster-native')), []);
    });
  }
});

test('follows bounded GitHub asset redirects and refuses credential-bearing or external redirects', async t => {
  for (const destination of ['https://release-assets.githubusercontent.com/release/asset?signature=value', 'https://objects.githubusercontent.com/asset', 'https://evil.test/asset', 'http://github.com/asset', 'https://token@github.com/asset']) {
    await t.test(destination.split('/')[2], async t => {
      const { root, env } = await fixture(t);
      let calls = 0;
      const run = ensureNative(root, 'linux/amd64', { env, fetch: async () => {
        calls++;
        return calls === 1 ? response('', 302, { location: destination }) : response();
      } });
      if (destination.includes('release-assets.') || destination.includes('objects.')) { await run; assert.equal(calls, 2); }
      else { await assert.rejects(run); assert.equal(calls, 1); }
    });
  }
  const { root, env } = await fixture(t);
  let calls = 0;
  await assert.rejects(ensureNative(root, 'linux/amd64', { env, fetch: async () => { calls++; return response('', 302, { location: '/loop' }); } }), { code: 'redirect' });
  assert.equal(calls, 6);
});

test('retries only bounded transient responses, not authentication or integrity failures', async t => {
  const { root, env } = await fixture(t);
  let calls = 0;
  await ensureNative(root, 'linux/amd64', { env, fetch: async () => ++calls < 3 ? response('', 503) : response() });
  assert.equal(calls, 3);
  const other = await fixture(t, metadata('linux/arm64'));
  calls = 0;
  await assert.rejects(ensureNative(other.root, 'linux/arm64', { env: other.env, fetch: async () => { calls++; return response('', 403); } }), { code: 'http' });
  assert.equal(calls, 1);
});

test('concurrent invocations retain a single valid native publication', async t => {
  const { root, env } = await fixture(t);
  const results = await Promise.all(Array.from({ length: 4 }, () => ensureNative(root, 'linux/amd64', { env, fetch: async () => response() })));
  assert.equal(new Set(results).size, 1);
  assert.deepEqual(await fsp.readFile(results[0]), payload);
  assert.equal((await fsp.readdir(path.join(env.RUNNER_TEMP, 'setup-go-faster-native'))).length, 1);
});


test('download launch failures are sanitized and post without state performs no acquisition', async t => {
  const platform = `${process.platform === 'win32' ? 'windows' : process.platform}/${{x64:'amd64',arm64:'arm64'}[process.arch]}`;
  const { root, env } = await fixture(t, metadata(platform));
  const { run } = require('./launcher.cjs');
  const previous = { fetch: globalThis.fetch, error: console.error, temp: process.env.RUNNER_TEMP, state: process.env.STATE_cache_state };
  const messages = [];
  let requests = 0;
  try {
    process.env.RUNNER_TEMP = env.RUNNER_TEMP;
    delete process.env.STATE_cache_state;
    globalThis.fetch = async () => { requests++; return response('private-token https://signed.test/token', 403); };
    console.error = message => messages.push(message);
    await run(root, true, () => { throw Error('must not spawn'); });
    assert.equal(requests, 0);
    await run(root, false, () => { throw Error('must not spawn'); });
    assert.equal(process.exitCode, 1);
    assert.equal(requests, 1);
    assert.match(messages[0], /verified native action/);
    assert.ok(!messages[0].includes('private-token') && !messages[0].includes('signed.test'));
  } finally {
    globalThis.fetch = previous.fetch; console.error = previous.error; process.exitCode = 0;
    if (previous.temp === undefined) delete process.env.RUNNER_TEMP; else process.env.RUNNER_TEMP = previous.temp;
    if (previous.state === undefined) delete process.env.STATE_cache_state; else process.env.STATE_cache_state = previous.state;
  }
});


test('missing lightweight bootstrap reports a sanitized failure', async t => {
  const { root } = await fixture(t);
  await fsp.copyFile(path.join(__dirname, 'launcher.cjs'), path.join(root, 'launcher.cjs'));
  const { run } = require(path.join(root, 'launcher.cjs'));
  const messages = [];
  const oldError = console.error;
  try {
    console.error = message => messages.push(message);
    await run(root, false, () => { throw Error('must not spawn'); });
    assert.equal(process.exitCode, 1);
    assert.match(messages[0], /verified native action/);
    assert.ok(!messages[0].includes(root) && !messages[0].includes('MODULE_NOT_FOUND'));
  } finally { console.error = oldError; process.exitCode = 0; }
});
