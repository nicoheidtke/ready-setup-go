'use strict';
// Runner lifecycle only. Setup, cache policy, archives, and networking stay in Go.
const { spawnSync } = require('node:child_process');
const path = require('node:path');
const fs = require('node:fs');

function invocation(root, post, env = process.env, platform = process.platform, cpu = process.arch, extra = process.argv.slice(2)) {
  if (post && !env.STATE_cache_state) return null;
  const arch = { x64: 'amd64', arm64: 'arm64' }[cpu];
  if (!arch || !['linux', 'darwin', 'win32'].includes(platform)) throw new Error('Unsupported platform');
  const os = platform === 'win32' ? 'windows' : platform;
  const binary = path.join(root, 'bin', `${os}-${arch}`, `setup-go-better${os === 'windows' ? '.exe' : ''}`);
  const args = post ? ['post'] : [
    'setup', '--go-version', env['INPUT_GO-VERSION'] || env.SETUP_GO_BETTER_VERSION || '',
    '--go-version-file', env['INPUT_GO-VERSION-FILE'] || env.SETUP_GO_BETTER_VERSION_FILE || '',
    `--check-latest=${env['INPUT_CHECK-LATEST'] || env.SETUP_GO_BETTER_LATEST || 'false'}`, ...extra,
  ];
  return { binary, args };
}

function reportStartError(download) {
  // Never print raw network/spawn errors, arguments, URLs or paths.
  const message = download
    ? 'Unable to acquire the verified native action. Check public GitHub release asset access, runner temporary storage, and the pinned distribution manifest; use a bundled distribution for offline or private-release use.'
    : 'Unable to start the native action. Pin a bundled distribution (source-only commits lack executables), use Linux/macOS/Windows on amd64/arm64, and check executable permissions.';
  console.error(process.env.GITHUB_ACTIONS === 'true'
    ? `::error title=Start setup-go-better::${message}` : `ERROR Start setup-go-better: ${message}`);
  process.exitCode = 1;
}

function start(call, spawn) {
  const child = spawn(call.binary, call.args, { stdio: 'inherit', shell: false });
  if (child.error) throw child.error;
  process.exitCode = child.status === null ? 1 : child.status;
}

function run(root, post, spawn = spawnSync) {
  let call;
  try {
    call = invocation(root, post);
    if (!call) return;
    if (!fs.existsSync(path.join(root, 'NATIVE_DOWNLOADS.json'))) {
      start(call, spawn);
      return;
    }
  } catch { reportStartError(false); return; }
  let ensureNative;
  try { ({ ensureNative } = require('./native-download.cjs')); }
  catch { reportStartError(true); return; }
  const controller = new AbortController();
  const cancel = () => controller.abort();
  process.once('SIGINT', cancel);
  process.once('SIGTERM', cancel);
  const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
  const os = process.platform === 'win32' ? 'windows' : process.platform;
  return ensureNative(root, `${os}/${arch}`, { signal: controller.signal })
    .then(binary => start({ ...call, binary }, spawn))
    .catch(() => reportStartError(true))
    .finally(() => { process.removeListener('SIGINT', cancel); process.removeListener('SIGTERM', cancel); });
}
module.exports = { invocation, run };
