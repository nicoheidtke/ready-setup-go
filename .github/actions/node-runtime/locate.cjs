'use strict';
require('node:fs').appendFileSync(process.env.GITHUB_OUTPUT, `path=${process.execPath}\n`);
