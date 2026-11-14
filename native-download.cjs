"use strict";
// Bootstrap only: acquire the platform's pinned executable before entering Go.
const fs = require('node:fs');
const fsp = fs.promises;
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { Readable, Transform } = require('node:stream');
const { pipeline } = require('node:stream/promises');
const { setTimeout: delay } = require('node:timers/promises');

const manifestName = 'NATIVE_DOWNLOADS.json';
const platforms = new Set(['linux/amd64', 'linux/arm64', 'darwin/amd64', 'darwin/arm64', 'windows/amd64', 'windows/arm64']);
const basePattern = /^https:\/\/github\.com\/[A-Za-z0-9][A-Za-z0-9_.-]*\/[A-Za-z0-9][A-Za-z0-9_.-]*\/releases\/download\/[A-Za-z0-9][A-Za-z0-9._-]*$/;
const cdnHosts = new Set(['github.com', 'release-assets.githubusercontent.com', 'objects.githubusercontent.com']);
const retryStatus = new Set([408, 429, 500, 502, 503, 504]);

function fail(code) { const err = new Error(code); err.code = code; return err; }
function parseManifest(data, platform) {
  if (data.length > 1024 * 1024) throw fail('manifest');
  const m = JSON.parse(data);
  if (!m || m.format !== 'sgf-native-v1' || typeof m.base_url !== 'string' || !basePattern.test(m.base_url) || new URL(m.base_url).href !== m.base_url
      || !m.platforms || Array.isArray(m.platforms) || Object.keys(m.platforms).length > 6 || !platforms.has(platform)) throw fail('manifest');
  for (const [key, a] of Object.entries(m.platforms)) {
    if (!platforms.has(key) || !a || typeof a.sha256 !== 'string' || a.sha256.length !== 64 || !/^[a-f0-9]{64}$/.test(a.sha256) || !Number.isSafeInteger(a.size) || a.size < 1 || a.size > 256 * 1024 * 1024) throw fail('manifest');
  }
  const asset = m.platforms[platform];
  if (!asset) throw fail('platform');
  const name = `setup-go-faster-${platform.replace('/', '-')}${platform.startsWith('windows/') ? '.exe' : ''}`;
  return { ...asset, url: `${m.base_url}/${name}` };
}

async function verified(file, asset, signal) {
  let st;
  try { st = await fsp.lstat(file); } catch (err) { if (err.code === 'ENOENT') return false; throw err; }
  if (!st.isFile() || st.isSymbolicLink() || st.size !== asset.size) throw fail('existing');
  const hash = crypto.createHash('sha256');
  let count = 0;
  for await (const chunk of fs.createReadStream(file, { signal })) {
    count += chunk.length;
    if (count > asset.size) throw fail('existing');
    hash.update(chunk);
  }
  if (count !== asset.size || hash.digest('hex') !== asset.sha256) throw fail('existing');
  return true;
}

async function responseFor(url, fetcher, signal) {
  for (let redirects = 0; redirects <= 5; redirects++) {
    const u = new URL(url);
    if (u.protocol !== 'https:' || u.username || u.password || u.port || !cdnHosts.has(u.hostname)) throw fail('redirect');
    let response;
    try {
      response = await fetcher(u.href, { redirect: 'manual', signal, credentials: 'omit', headers: {
        Accept: 'application/octet-stream', 'Accept-Encoding': 'identity', 'User-Agent': 'setup-go-faster-native'
      } });
    } catch (err) { if (signal.aborted) throw fail('cancelled'); throw fail('network'); }
    if ([301, 302, 303, 307, 308].includes(response.status)) {
      const location = response.headers.get('location');
      if (response.body) await response.body.cancel();
      if (!location || redirects === 5) throw fail('redirect');
      url = new URL(location, u).href;
      continue;
    }
    return response;
  }
  throw fail('redirect');
}

async function download(file, asset, fetcher, signal) {
  for (let attempt = 0; attempt < 3; attempt++) {
    let retryDelay = 250 * (2 ** attempt);
    try {
      const response = await responseFor(asset.url, fetcher, signal);
      if (response.status !== 200) {
        const value = response.headers.get('retry-after');
        if (value && /^\d+$/.test(value)) retryDelay = Math.min(5000, Math.max(retryDelay, Number(value) * 1000));
        if (response.body) await response.body.cancel();
        throw fail(retryStatus.has(response.status) ? 'transient' : 'http');
      }
      const length = response.headers.get('content-length');
      const encoding = response.headers.get('content-encoding');
      if ((length !== null && (!/^\d+$/.test(length) || Number(length) !== asset.size)) || (encoding && encoding !== 'identity') || !response.body) {
        if (response.body) await response.body.cancel();
        throw fail('size');
      }
      let count = 0;
      const hash = crypto.createHash('sha256');
      const check = new Transform({ transform(chunk, _encoding, done) {
        count += chunk.length;
        if (count > asset.size) return done(fail('size'));
        hash.update(chunk); done(null, chunk);
      } });
      await pipeline(Readable.fromWeb(response.body), check, fs.createWriteStream(file, { flags: 'wx', mode: 0o700 }), { signal });
      if (count !== asset.size) throw fail('size');
      if (hash.digest('hex') !== asset.sha256) throw fail('digest');
      await fsp.chmod(file, 0o700);
      return;
    } catch (err) {
      await fsp.rm(file, { force: true });
      if (signal.aborted) throw fail('cancelled');
      if (attempt === 2 || !['network', 'transient'].includes(err.code)) throw err;
      await delay(retryDelay, undefined, { signal });
    }
  }
}

async function ensureNative(root, platform, options = {}) {
  const env = options.env || process.env;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 120000);
  const signal = options.signal ? AbortSignal.any([options.signal, controller.signal]) : controller.signal;
  let staging;
  try {
    const manifest = path.join(root, manifestName);
    const st = await fsp.lstat(manifest);
    if (!st.isFile() || st.isSymbolicLink() || st.size > 1024 * 1024) throw fail('manifest');
    const asset = parseManifest(await fsp.readFile(manifest, 'utf8'), platform);
    const base = path.resolve(env.RUNNER_TEMP || os.tmpdir(), 'setup-go-faster-native');
    await fsp.mkdir(base, { recursive: true, mode: 0o700 });
    const target = path.join(base, `${asset.sha256}-${platform.replace('/', '-')}`);
    const name = platform.startsWith('windows/') ? 'setup-go-faster.exe' : 'setup-go-faster';
    const binary = path.join(target, name);
    try {
      const existing = await fsp.lstat(target);
      if (!existing.isDirectory() || existing.isSymbolicLink()) throw fail('existing');
      if (!await verified(binary, asset, signal)) throw fail('existing');
      await fsp.chmod(binary, 0o700);
      return binary;
    } catch (err) { if (err.code !== 'ENOENT') throw err; }
    staging = await fsp.mkdtemp(path.join(base, '.native-'));
    await download(path.join(staging, name), asset, options.fetch || globalThis.fetch, signal);
    signal.throwIfAborted();
    try { await fsp.rename(staging, target); staging = undefined; }
    catch (err) {
      const winner = await fsp.lstat(target);
      if (!winner.isDirectory() || winner.isSymbolicLink() || !await verified(binary, asset, signal)) throw err;
    }
    return binary;
  } finally {
    clearTimeout(timeout);
    // mkdtemp is our only source for this path; never remove a caller path.
    if (staging) await fsp.rm(staging, { recursive: true, force: true });
  }
}

module.exports = { manifestName, parseManifest, ensureNative };
