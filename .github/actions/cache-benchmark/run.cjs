'use strict';
const { spawnSync } = require('node:child_process');

function invocation(env = process.env) {
  const flags = ['go', 'seed-go', 'source', 'workload', 'scenario', 'samples', 'mode', 'layout', 'storage', 'chunk-concurrency', 'compression', 'dependency', 'max-bytes', 'upload-concurrency', 'upload-chunk-bytes', 'download-concurrency', 'download-chunk-bytes', 'build-max-age', 'out'];
  const args = [];
  for (const flag of flags) {
    const value = env[`INPUT_${flag.toUpperCase()}`];
    if (value) args.push(`--${flag}`, value);
  }
  if (env.INPUT_VALIDATION === 'true') args.push('--validation');
  if (!env.INPUT_HELPER) throw new Error('Native cache benchmark helper path is required');
  return { binary: env.INPUT_HELPER, args };
}

if (require.main === module) {
  const call = invocation();
  const child = spawnSync(call.binary, call.args, { stdio: 'inherit', shell: false });
  if (child.error) throw new Error('Native cache benchmark process could not start');
  process.exitCode = child.status === null ? 1 : child.status;
}
module.exports = { invocation };
