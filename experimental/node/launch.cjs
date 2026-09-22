'use strict';
const { spawnSync } = require('node:child_process');
const path = require('node:path');
const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
if (!arch || !['linux', 'darwin', 'win32'].includes(process.platform)) {
  throw new Error('Unsupported platform');
}
const platform = process.platform === 'win32' ? 'windows' : process.platform;
const root = path.resolve(__dirname, '../..');
const binary = path.join(root, 'bin', `${platform}-${arch}`, `setup-go-better${platform === 'windows' ? '.exe' : ''}`);
const result = spawnSync(binary, [
  'setup', '--go-version', process.env['INPUT_GO-VERSION'] || process.env.SETUP_GO_BETTER_VERSION || '',
  `--check-latest=${process.env['INPUT_CHECK-LATEST'] || process.env.SETUP_GO_BETTER_LATEST || 'false'}`,
  ...process.argv.slice(2),
], { stdio: 'inherit', shell: false });
if (result.error) throw result.error;
process.exit(result.status === null ? 1 : result.status);
