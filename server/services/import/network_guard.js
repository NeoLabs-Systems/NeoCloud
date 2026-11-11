'use strict';

// Imports make the server fetch a URL the user typed, so a plain account
// could otherwise use them to probe the server's own network (SSRF).
// Loopback, private, link-local and similar ranges are only reachable for
// admins — a Nextcloud on the home LAN is a normal case for them — or for
// everyone when NEOCLOUD_IMPORT_ALLOW_PRIVATE=true.
//
// The check runs twice: before every request and redirect hop (which also
// covers addresses typed as IP literals), and at connect time inside the
// dispatcher, where the name is resolved once, every address is checked and
// the socket connects to exactly those addresses. A name that answers a
// public address first and a private one a moment later (DNS rebinding)
// cannot slip between the two.

const dns = require('dns');
const net = require('net');
const { Agent } = require('undici');
const { isAdminUser } = require('../access/admin');
const { httpError } = require('../../utils/http_error');

const blocked = new net.BlockList();
for (const [address, prefix] of [
  ['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10], ['127.0.0.0', 8],
  ['169.254.0.0', 16], ['172.16.0.0', 12], ['192.0.0.0', 24], ['192.168.0.0', 16],
  ['198.18.0.0', 15], ['224.0.0.0', 4], ['240.0.0.0', 4],
]) blocked.addSubnet(address, prefix, 'ipv4');
for (const [address, prefix] of [
  ['::', 128], ['::1', 128], ['fc00::', 7], ['fe80::', 10], ['ff00::', 8],
]) blocked.addSubnet(address, prefix, 'ipv6');

function isPrivateAddress(address) {
  const family = net.isIPv6(address) ? 'ipv6' : 'ipv4';
  const mapped = family === 'ipv6' && /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(address);
  if (mapped) return blocked.check(mapped[1], 'ipv4');
  return blocked.check(address, family);
}

function privateAllowedFor(userId) {
  if (String(process.env.NEOCLOUD_IMPORT_ALLOW_PRIVATE || '').toLowerCase() === 'true') return true;
  return userId != null && isAdminUser(userId);
}

// Connections: 30 s to connect, 60 s for the answer to start, and at most
// 2 minutes of silence while a body streams.
const CONNECT_TIMEOUT_MS = 30 * 1000;
const HEADERS_TIMEOUT_MS = 60 * 1000;
const BODY_IDLE_TIMEOUT_MS = 2 * 60 * 1000;

function hostBlocked() {
  return httpError(403, 'Only an admin can import from a server on a private network.', 'IMPORT_HOST_BLOCKED');
}

/** Throws 403 IMPORT_HOST_BLOCKED when `url` points into a private network. */
async function assertPublicHost(url, { lookup = dns.promises.lookup } = {}) {
  const host = new URL(url).hostname.replace(/^\[|\]$/g, '');
  const addresses = net.isIP(host)
    ? [host]
    : (await lookup(host, { all: true, verbatim: true })).map((entry) => entry.address);
  if (addresses.some(isPrivateAddress)) throw hostBlocked();
}

/**
 * A `lookup` for net.connect: resolves once and refuses the connection when
 * any address is private, so the socket only ever reaches checked addresses.
 */
function publicOnlyLookup(hostname, options, callback) {
  dns.lookup(hostname, { ...options, all: true, verbatim: true }, (err, addresses) => {
    if (err) return callback(err);
    if (addresses.length === 0 || addresses.some((entry) => isPrivateAddress(entry.address))) return callback(hostBlocked());
    if (options.all) return callback(null, addresses);
    return callback(null, addresses[0].address, addresses[0].family);
  });
}

function createImportDispatcher(lookup) {
  return new Agent({
    connect: { timeout: CONNECT_TIMEOUT_MS, ...(lookup ? { lookup } : {}) },
    headersTimeout: HEADERS_TIMEOUT_MS,
    bodyTimeout: BODY_IDLE_TIMEOUT_MS,
  });
}

let publicDispatcher;
let openDispatcher;

/**
 * How a user's import client may reach the network: `hostGuard` (checked per
 * hop, null when unrestricted) and the `dispatcher` its requests go through.
 */
function importNetworkFor(userId) {
  if (privateAllowedFor(userId)) {
    openDispatcher ??= createImportDispatcher(null);
    return { hostGuard: null, dispatcher: openDispatcher };
  }
  publicDispatcher ??= createImportDispatcher(publicOnlyLookup);
  return { hostGuard: assertPublicHost, dispatcher: publicDispatcher };
}

module.exports = { assertPublicHost, importNetworkFor, isPrivateAddress };
