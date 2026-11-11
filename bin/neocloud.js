#!/usr/bin/env node
'use strict';

const { runCLI } = require('../lib/manager');
const ui = require('../lib/cli/ui');

runCLI(process.argv.slice(2)).catch((err) => {
  ui.error(err.message);
  process.exit(1);
});
