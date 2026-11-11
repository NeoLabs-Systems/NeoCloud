'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { assertPublicHost, isPrivateAddress } = require('../../server/services/import/network_guard');

test('import guard: private, loopback and link-local addresses are recognised', () => {
  for (const address of ['127.0.0.1', '10.1.2.3', '172.20.0.1', '192.168.1.10', '169.254.169.254', '100.64.0.1', '::1', 'fd00::1', 'fe80::1', '::ffff:192.168.0.1', '0.0.0.0']) {
    assert.equal(isPrivateAddress(address), true, address);
  }
  for (const address of ['1.1.1.1', '93.184.216.34', '2606:4700::1111', '::ffff:8.8.8.8']) {
    assert.equal(isPrivateAddress(address), false, address);
  }
});

test('import guard: names that resolve into a private network are refused', async () => {
  const lookup = async () => [{ address: '203.0.113.5' }, { address: '10.0.0.7' }];
  await assert.rejects(assertPublicHost('https://cloud.example.com/', { lookup }), (error) => error.code === 'IMPORT_HOST_BLOCKED');
  await assert.rejects(assertPublicHost('http://[::1]:8080/'), (error) => error.code === 'IMPORT_HOST_BLOCKED');
  await assertPublicHost('https://cloud.example.com/', { lookup: async () => [{ address: '93.184.216.34' }] });
});
