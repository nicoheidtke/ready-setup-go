'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

test('runtime locator writes an exact newline-terminated GitHub output', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'node runtime output '));
  try {
    const output = path.join(directory, 'github-output');
    execFileSync(process.execPath, [path.join(__dirname, 'locate.cjs')], {
      env: { ...process.env, GITHUB_OUTPUT: output },
    });
    assert.equal(fs.readFileSync(output, 'utf8'), `path=${process.execPath}\n`);
  } finally {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});
