'use strict';

// Whether a URL a person typed may be fetched by the server: http(s) only, and
// never into the server's own loopback or private network (SSRF).

const dns = require('node:dns');
const net = require('node:net');

const ALLOWED_SCHEMES = new Set(['http', 'https']);

function urlAbortError(signal) {
  if (signal?.reason instanceof Error) return signal.reason;
  const error = new Error('URL validation was aborted.');
  error.name = 'AbortError';
  error.code = 'ABORT_ERR';
  return error;
}

function isPrivateHost(hostname) {
  if (!hostname) return false;
  let h = hostname.toLowerCase().trim();
  // host[:port] is accepted too; drop the port before classifying.
  const bracketed = h.match(/^\[([^\]]+)\](?::\d+)?$/);
  if (bracketed) h = bracketed[1];
  else if (!net.isIPv6(h)) h = h.replace(/:\d+$/, '');

  if (h === 'localhost' || h === 'localhost.localdomain') return true;
  if (h.endsWith('.local') || h.endsWith('.internal') || h.endsWith('.localhost')) return true;

  // Address-range checks apply to IP literals only. Matching them against
  // hostnames would block real sites such as 0.gravatar.com.
  if (net.isIPv6(h)) {
    // Unwrap IPv4-mapped/compatible IPv6 (::ffff:127.0.0.1 or ::ffff:7f00:1)
    // so the embedded IPv4 address is checked against the ranges below.
    const mapped = h.match(/^::(?:ffff:)?(?:0:)?([0-9a-f.:]+)$/);
    if (mapped) {
      const tail = mapped[1];
      if (tail.includes('.')) {
        h = tail;
      } else {
        const groups = tail.split(':');
        if (groups.length === 2) {
          const hi = parseInt(groups[0], 16);
          const lo = parseInt(groups[1], 16);
          if (Number.isFinite(hi) && Number.isFinite(lo) && hi <= 0xffff && lo <= 0xffff) {
            h = `${(hi >> 8) & 0xff}.${hi & 0xff}.${(lo >> 8) & 0xff}.${lo & 0xff}`;
          }
        }
      }
    }
  }

  if (net.isIPv6(h)) {
    if (h === '::1' || h === '::') return true; // loopback, unspecified
    if (/^fe[89ab][0-9a-f]:/.test(h)) return true; // fe80::/10 link-local
    if (/^fe[c-f][0-9a-f]:/.test(h)) return true; // fec0::/10 deprecated site-local
    if (/^f[cd][0-9a-f]*:/.test(h)) return true; // fc00::/7 unique-local
    if (h.startsWith('ff')) return true; // multicast
    if (h.startsWith('100::')) return true; // discard-only prefix
    if (h.startsWith('2001:db8:')) return true; // documentation
    return false;
  }

  if (net.isIPv4(h)) {
    const [a, b, c] = h.split('.').map(Number);
    if (a === 0 || a === 10 || a === 127 || a >= 224) return true;
    if (a === 100 && b >= 64 && b <= 127) return true; // CGNAT
    if (a === 169 && b === 254) return true;
    if (a === 172 && b >= 16 && b <= 31) return true;
    if (a === 192 && b === 168) return true;
    if (a === 192 && b === 0 && c === 0) return true;
    if (a === 192 && b === 0 && c === 2) return true;
    if (a === 198 && (b === 18 || b === 19)) return true;
    if (a === 198 && b === 51 && c === 100) return true;
    if (a === 203 && b === 0 && c === 113) return true;
  }

  return false;
}

function blocked(reason) {
  return { allowed: false, reason };
}

// A scheme is letters/digits/+/-, so "example.com" or "example.com:443" has none.
function hasScheme(urlString) {
  return /^[a-z][a-z0-9+-]*:/i.test(urlString.trim());
}

function describeLookupFailure(hostname, error) {
  if (error?.code === 'DNS_TIMEOUT') {
    return `DNS lookup for ${hostname} timed out. Check the address and try again.`;
  }
  const code = error?.code ? ` (${error.code})` : '';
  return `Could not resolve ${hostname}${code}. Check that the domain is spelled correctly and exists.`;
}

/**
 * { allowed: true } for a URL the server may fetch, or { allowed: false,
 * reason } saying what to change. The reason never includes the address a
 * hostname resolved to.
 */
function validateCloudUrl(urlString) {
  if (!urlString || typeof urlString !== 'string') return blocked('No URL was given.');
  if (!hasScheme(urlString)) {
    return blocked(`"${urlString}" has no scheme. Use a full URL such as https://${urlString.trim()}`);
  }

  let parsed;
  try {
    parsed = new URL(urlString);
  } catch {
    return blocked(`"${urlString}" is not a valid URL.`);
  }

  const scheme = parsed.protocol.replace(/:$/, '').toLowerCase();
  if (!ALLOWED_SCHEMES.has(scheme)) {
    return blocked(`The ${scheme}: scheme is not allowed. Only http:// and https:// URLs can be used.`);
  }
  if (isPrivateHost(parsed.hostname)) {
    return blocked(`${parsed.hostname} is a local or private network address, which is not reachable from here.`);
  }
  return { allowed: true };
}

/** validateCloudUrl, plus every address the hostname resolves to. */
async function validateCloudUrlWithDns(urlString, options = {}) {
  if (options.signal?.aborted) throw urlAbortError(options.signal);
  const syntactic = validateCloudUrl(urlString);
  if (!syntactic.allowed) return syntactic;

  const hostname = new URL(urlString).hostname.replace(/^\[|\]$/g, '');
  if (net.isIP(hostname)) return { allowed: true };

  const lookup = options.lookup || dns.promises.lookup;
  const timeoutMs = Math.max(100, Math.min(Number(options.timeoutMs) || 5000, 30_000));
  let timer = null;
  let onAbort = null;
  try {
    const pending = [
      lookup(hostname, { all: true, verbatim: true }),
      new Promise((_, reject) => {
        timer = setTimeout(() => {
          const error = new Error('DNS lookup timed out.');
          error.code = 'DNS_TIMEOUT';
          reject(error);
        }, timeoutMs);
      }),
    ];
    if (options.signal) {
      pending.push(new Promise((_, reject) => {
        onAbort = () => reject(urlAbortError(options.signal));
        options.signal.addEventListener('abort', onAbort, { once: true });
      }));
    }
    const addresses = await Promise.race(pending);
    const resolved = Array.isArray(addresses) ? addresses : [addresses];
    if (resolved.length === 0) return blocked(describeLookupFailure(hostname, null));
    if (resolved.some((entry) => isPrivateHost(entry?.address || entry))) {
      return blocked(`${hostname} resolves to a local or private network address, which is not reachable from here.`);
    }
    return { allowed: true };
  } catch (error) {
    if (options.signal?.aborted) throw error;
    return blocked(describeLookupFailure(hostname, error));
  } finally {
    if (timer) clearTimeout(timer);
    if (onAbort) options.signal?.removeEventListener('abort', onAbort);
  }
}

module.exports = { isPrivateHost, validateCloudUrlWithDns };
