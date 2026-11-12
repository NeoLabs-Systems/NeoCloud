'use strict';

// Bounded HTTP for the AI providers: a deadline, a size cap on the answer,
// and the caller's abort signal.
//
// Network guard: an address someone typed (a personal base URL, their own
// NeoRouter or Ollama) may not reach the server's own loopback or private
// network. Checking the name before the request is not enough (it may
// redirect, or resolve elsewhere a moment later), so every connection to
// such an address goes through a dispatcher that resolves the name once,
// refuses private addresses (IP literals too, on every redirect hop) and
// connects to exactly what it checked. Only the addresses the admin
// configured in the environment, and the admin's own NeoRouter sign-in,
// reach the LAN.

const dns = require('node:dns');
const net = require('node:net');
const { Agent, buildConnector, fetch } = require('undici');
const { createAbortError } = require('../../utils/abort');
const { isPrivateHost } = require('../../utils/cloud_security');

const DEFAULT_HTTP_TIMEOUT_MS = 15000;
const DEFAULT_MAX_RESPONSE_BYTES = 10 * 1024 * 1024;

function timeoutError(serviceName, timeoutMs) {
  const error = new Error(`${serviceName} request timed out after ${timeoutMs}ms.`);
  error.code = 'HTTP_TIMEOUT';
  return error;
}

function responseTooLargeError(serviceName, maxBytes) {
  const error = new Error(`${serviceName} response exceeded the ${maxBytes}-byte safety limit.`);
  error.code = 'HTTP_RESPONSE_TOO_LARGE';
  return error;
}

async function readResponseBuffer(response, { maxResponseBytes, serviceName }) {
  const contentLength = Number(response.headers.get('content-length'));
  if (Number.isFinite(contentLength) && contentLength > maxResponseBytes) {
    await response.body?.cancel().catch(() => {});
    throw responseTooLargeError(serviceName, maxResponseBytes);
  }
  const chunks = [];
  let totalBytes = 0;
  for await (const value of response.body || []) {
    const chunk = Buffer.from(value);
    totalBytes += chunk.byteLength;
    if (totalBytes > maxResponseBytes) throw responseTooLargeError(serviceName, maxResponseBytes);
    chunks.push(chunk);
  }
  return Buffer.concat(chunks, totalBytes);
}

// The request's own deadline governs, so the pool's idle timers must not end
// a longer request first.
let dispatcher = null;
function deadlineOwnedDispatcher() {
  dispatcher ??= new Agent({ headersTimeout: 0, bodyTimeout: 0 });
  return dispatcher;
}

// Addresses the admin set (Admin › AI, .env); these may point into the LAN.
const OPERATOR_URL_ENV_KEYS = Object.freeze([
  'NEOCLOUD_OPENAI_BASE_URL',
  'NEOCLOUD_OPENAI_COMPATIBLE_BASE_URL',
  'NEOCLOUD_ANTHROPIC_BASE_URL',
  'NEOCLOUD_OLLAMA_URL',
  'NEOROUTER_BASE_URL',
  'NEOCLOUD_AI_REFERENCE_URL',
]);

function originOf(url) {
  try {
    return new URL(String(url)).origin;
  } catch {
    return null;
  }
}

function isOperatorUrl(url) {
  const origin = originOf(url);
  if (!origin || origin === 'null') return false;
  return OPERATOR_URL_ENV_KEYS.some((key) => originOf(String(process.env[key] || '').trim()) === origin);
}

function privateAddressRefused(host) {
  const error = new Error(`${host} is a local or private network address, which is not reachable from here.`);
  error.code = 'URL_NOT_ALLOWED';
  return error;
}

// net.connect's lookup: resolve once, refuse if any address is private.
function publicOnlyLookup(hostname, options, callback) {
  dns.lookup(hostname, { ...options, all: true, verbatim: true }, (err, addresses) => {
    if (err) return callback(err);
    if (addresses.length === 0 || addresses.some((entry) => isPrivateHost(entry.address))) {
      return callback(privateAddressRefused(hostname));
    }
    if (options.all) return callback(null, addresses);
    return callback(null, addresses[0].address, addresses[0].family);
  });
}

// A lookup never runs for an IP literal, so those are checked here.
let publicConnector = null;
function publicOnlyConnect(options, callback) {
  const host = String(options.hostname || '').replace(/^\[|\]$/g, '');
  if (isPrivateHost(host)) return callback(privateAddressRefused(host), null);
  publicConnector ??= buildConnector({ lookup: publicOnlyLookup });
  return publicConnector(options, callback);
}

let publicDispatcher = null;
function publicOnlyDispatcher() {
  publicDispatcher ??= new Agent({ headersTimeout: 0, bodyTimeout: 0, connect: publicOnlyConnect });
  return publicDispatcher;
}

/**
 * `fetch` for the provider SDKs: unchanged for the admin's addresses, through
 * the public-only dispatcher for anything else.
 */
function aiFetch(input, init = {}) {
  const url = typeof input === 'string' || input instanceof URL ? input : input?.url;
  if (isOperatorUrl(url)) return globalThis.fetch(input, init);
  return globalThis.fetch(input, { ...init, dispatcher: publicOnlyDispatcher() });
}

/** { response, text }: the whole answer, read within `timeoutMs`. */
async function fetchResponseText(url, options = {}) {
  const {
    serviceName = 'Remote service',
    timeoutMs = DEFAULT_HTTP_TIMEOUT_MS,
    maxResponseBytes = DEFAULT_MAX_RESPONSE_BYTES,
    signal: callerSignal = null,
    // The admin's own NeoRouter sign-in: the address isn't in .env yet.
    trustedNetwork = false,
    ...fetchOptions
  } = options;
  if (callerSignal?.aborted) throw createAbortError(callerSignal);

  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort(timeoutError(serviceName, timeoutMs));
  }, timeoutMs);
  const abortFromCaller = () => controller.abort(createAbortError(callerSignal));
  callerSignal?.addEventListener('abort', abortFromCaller, { once: true });

  try {
    const response = await fetch(url, {
      ...fetchOptions,
      dispatcher: trustedNetwork || isOperatorUrl(url) ? deadlineOwnedDispatcher() : publicOnlyDispatcher(),
      signal: controller.signal,
    });
    const body = await readResponseBuffer(response, { maxResponseBytes, serviceName });
    return { response, text: body.toString('utf8') };
  } catch (error) {
    if (callerSignal?.aborted) throw createAbortError(callerSignal);
    if (timedOut) throw timeoutError(serviceName, timeoutMs);
    throw error;
  } finally {
    clearTimeout(timer);
    callerSignal?.removeEventListener('abort', abortFromCaller);
  }
}

module.exports = { aiFetch, fetchResponseText };
