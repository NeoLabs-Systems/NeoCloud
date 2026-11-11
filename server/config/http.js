'use strict';

const { readEnvBool } = require('../../runtime/env');

// Proxies on this host or a private network (the Docker bridge included).
// A client connecting from the internet never matches, so it can't set its
// own X-Forwarded-For.
const PRIVATE_PROXIES = Object.freeze(['loopback', 'linklocal', 'uniquelocal']);

/**
 * TRUST_PROXY: which proxies may set X-Forwarded-For / -Proto.
 *   true / false        one hop / none
 *   2                   that many hops in front of NeoCloud
 *   10.0.0.2,loopback   these addresses or subnets (Express names allowed)
 * Unset: proxies on loopback, link-local and private addresses when
 * PUBLIC_URL is https, else none. Never a bare hop count by default: that
 * trusts whatever peer connects, including a client that reaches the port
 * directly.
 * Returns false, a hop count or a list, as Express's "trust proxy" takes them.
 */
function parseTrustProxy(raw, fallback) {
  const value = String(raw ?? '').trim();
  if (!value) return fallback ? [...PRIVATE_PROXIES] : false;
  const lower = value.toLowerCase();
  if (['true', 'yes', 'on'].includes(lower)) return 1;
  if (['false', 'no', 'off', '0'].includes(lower)) return false;
  if (/^\d+$/.test(value)) return Number(value);
  return value.split(',').map((entry) => entry.trim()).filter(Boolean);
}

// Secure cookies and proxy trust follow PUBLIC_URL unless set explicitly.
function readHttpRuntimeConfig(env = process.env) {
  const publicUrl = String(env.PUBLIC_URL || '').trim();
  const isHttps = publicUrl.startsWith('https://');
  const secureCookies = env.SECURE_COOKIES ? readEnvBool('SECURE_COOKIES', isHttps) : isHttps;
  const trustProxy = parseTrustProxy(env.TRUST_PROXY, secureCookies);
  return { publicUrl: publicUrl || null, secureCookies, trustProxy };
}

module.exports = { parseTrustProxy, readHttpRuntimeConfig };
