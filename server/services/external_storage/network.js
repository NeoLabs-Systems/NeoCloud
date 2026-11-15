'use strict';

// Where a mount may connect. Hosts are typed by the account holder, so a
// plain account could otherwise use a mount to probe the server's own
// network (SSRF). Loopback, private and link-local ranges are refused unless
// the server runs with NEOCLOUD_EXTERNAL_ALLOW_PRIVATE=true (a Nextcloud or
// MinIO on the home network) or the account is an admin. Cloud metadata
// addresses stay refused even then.
//
// The name is resolved once, every address is checked, and the connection
// goes to exactly those addresses: `guardedLookup` is handed to the socket
// (HTTP agents) or the resolved address is used directly (SSH), so DNS
// rebinding between check and connect cannot reach a private address.

const dns = require('dns');
const net = require('net');
const { Agent } = require('undici');
const { isAdminUser } = require('../access/admin');
const { isPrivateAddress } = require('../import/network_guard');
const { httpError } = require('../../utils/http_error');

const metadata = new net.BlockList();
metadata.addSubnet('169.254.0.0', 16, 'ipv4');
metadata.addAddress('100.100.100.200', 'ipv4');
metadata.addAddress('fd00:ec2::254', 'ipv6');
metadata.addSubnet('fe80::', 10, 'ipv6');

const CONNECT_TIMEOUT_MS = 20 * 1000;
const HEADERS_TIMEOUT_MS = 60 * 1000;
const BODY_IDLE_TIMEOUT_MS = 2 * 60 * 1000;

function allowPrivate(userId) {
  if (String(process.env.NEOCLOUD_EXTERNAL_ALLOW_PRIVATE || '').toLowerCase() === 'true') return true;
  return userId != null && isAdminUser(userId);
}

function hostBlocked() {
  return httpError(403, 'This address is on a private network, which the server does not connect to.', 'EXTERNAL_HOST_BLOCKED');
}

function unwrapMapped(address) {
  const mapped = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(address);
  return mapped ? mapped[1] : address;
}

function isMetadataAddress(address) {
  const plain = unwrapMapped(address);
  return metadata.check(plain, net.isIPv6(plain) ? 'ipv6' : 'ipv4');
}

function assertAddresses(addresses, userId) {
  if (addresses.length === 0) throw hostBlocked();
  for (const address of addresses) {
    if (isMetadataAddress(address)) throw hostBlocked();
    if (isPrivateAddress(address) && !allowPrivate(userId)) throw hostBlocked();
  }
}

/** Resolves `hostname` (an IP literal stays as it is) and checks every address. */
async function resolveAllowed(hostname, userId) {
  const host = String(hostname).replace(/^\[|\]$/g, '');
  const addresses = net.isIP(host)
    ? [{ address: host, family: net.isIPv6(host) ? 6 : 4 }]
    : await dns.promises.lookup(host, { all: true, verbatim: true });
  assertAddresses(addresses.map((entry) => entry.address), userId);
  return addresses;
}

/** A `lookup` for sockets that resolves once and refuses any disallowed address. */
function guardedLookup(userId) {
  return (hostname, options, callback) => {
    resolveAllowed(hostname, userId).then((addresses) => {
      if (options.all) return callback(null, addresses);
      return callback(null, addresses[0].address, addresses[0].family);
    }, callback);
  };
}

/** An undici dispatcher for WebDAV whose connections go through guardedLookup. */
function dispatcherFor(userId) {
  return new Agent({
    connect: { timeout: CONNECT_TIMEOUT_MS, lookup: guardedLookup(userId) },
    headersTimeout: HEADERS_TIMEOUT_MS,
    bodyTimeout: BODY_IDLE_TIMEOUT_MS,
  });
}

module.exports = { allowPrivate, dispatcherFor, guardedLookup, resolveAllowed };
