'use strict';

const { version } = require('../../package.json');

function getServerVersion() {
  return { name: 'NeoCloud', version, node: process.version };
}

module.exports = { getServerVersion };
