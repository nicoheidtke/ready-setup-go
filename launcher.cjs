'use strict';
// Runner lifecycle only. Setup, cache policy, archives, and networking stay in Go.
const { spawn } = require('node:child_process');
const path = require('node:path');
const fs = require('node:fs');

function invocation(root, post, env = process.env, platform = process.platform, cpu = process.arch, extra = process.argv.slice(2)) {
  if (post && !env.STATE_cache_state) return null;
  const arch = { x64: 'amd64', arm64: 'arm64' }[cpu];
  if (!arch || !['linux', 'darwin', 'win32'].includes(platform)) throw new Error('Unsupported platform');
  const os = platform === 'win32' ? 'windows' : platform;
  const binary = path.join(root, 'bin', `${os}-${arch}`, `setup-go-faster${os === 'windows' ? '.exe' : ''}`);
  const args = post ? ['post'] : [
    'setup', '--go-version', env['INPUT_GO-VERSION'] || env.SETUP_GO_FASTER_VERSION || '',
    '--go-version-file', env['INPUT_GO-VERSION-FILE'] || env.SETUP_GO_FASTER_VERSION_FILE || '',
    `--check-latest=${env['INPUT_CHECK-LATEST'] || env.SETUP_GO_FASTER_LATEST || 'false'}`, ...extra,
  ];
  return { binary, args };
}

function reportStartError(download) {
  // Never print raw network/spawn errors, arguments, URLs or paths.
  const message = download
    ? 'Unable to acquire the verified native action. Check public GitHub release asset access, runner temporary storage, and the pinned distribution manifest; use a bundled distribution for offline or private-release use.'
    : 'Unable to start the native action. Pin a bundled distribution (source-only commits lack executables), use Linux/macOS/Windows on amd64/arm64, and check executable permissions.';
  console.error(process.env.GITHUB_ACTIONS === 'true'
    ? `::error title=Start setup-go-faster::${message}` : `ERROR Start setup-go-faster: ${message}`);
  process.exitCode = 1;
}

async function start(call, spawnChild) {
  const child = spawnChild(call.binary, call.args, { stdio: 'inherit', shell: false });
  let cancelled = false;
  const interrupt = () => { cancelled = true; child.kill('SIGINT'); };
  const terminate = () => { cancelled = true; child.kill('SIGTERM'); };
  // Keep Node responsive while native workers cancel, join, and clean staging.
  // Windows process.kill uses forceful termination; graceful POSIX signal
  // behavior is tested separately and is not promised on Windows.
  process.on('SIGINT', interrupt);
  process.on('SIGTERM', terminate);
  try {
    const status = await new Promise((resolve, reject) => {
      child.once('error', reject);
      child.once('close', (code) => resolve(code));
    });
    process.exitCode = cancelled || status === null ? 1 : status;
  } finally {
    process.removeListener('SIGINT', interrupt);
    process.removeListener('SIGTERM', terminate);
  }
}

async function run(root, post, spawnChild = spawn) {
  let call;
  try {
    call = invocation(root, post);
    if (!call) return;
    if (fs.existsSync(path.join(root, 'NATIVE_DOWNLOADS.json'))) {
      const controller = new AbortController();
      const cancel = () => controller.abort();
      process.once('SIGINT', cancel);
      process.once('SIGTERM', cancel);
      try {
        const { ensureNative } = require('./native-download.cjs');
        const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
        const os = process.platform === 'win32' ? 'windows' : process.platform;
        call.binary = await ensureNative(root, `${os}/${arch}`, { signal: controller.signal });
        controller.signal.throwIfAborted();
      } catch { reportStartError(true); return; }
      finally {
        process.removeListener('SIGINT', cancel);
        process.removeListener('SIGTERM', cancel);
      }
    }
    await start(call, spawnChild);
  } catch { reportStartError(false); }
}
module.exports = { invocation, run };
