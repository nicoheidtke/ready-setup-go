'use strict';
// Runner lifecycle only. Setup, cache policy, archives, and networking stay in Go.
const { spawnSync } = require('node:child_process');
const path = require('node:path');

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

function run(root, post, spawn = spawnSync) {
  const call = invocation(root, post);
  if (!call) return;
  const child = spawn(call.binary, call.args, { stdio: 'inherit', shell: false });
  if (child.error) throw child.error;
  process.exitCode = child.status === null ? 1 : child.status;
}
module.exports = { invocation, run };
