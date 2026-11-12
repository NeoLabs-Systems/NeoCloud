'use strict';

// Bounded HTTP for the AI providers: a deadline, a size cap on the answer,
// and the caller's abort signal.

const { Agent, fetch } = require('undici');
const { createAbortError } = require('../../utils/abort');

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

/** { response, text }: the whole answer, read within `timeoutMs`. */
async function fetchResponseText(url, options = {}) {
  const {
    serviceName = 'Remote service',
    timeoutMs = DEFAULT_HTTP_TIMEOUT_MS,
    maxResponseBytes = DEFAULT_MAX_RESPONSE_BYTES,
    signal: callerSignal = null,
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
      dispatcher: deadlineOwnedDispatcher(),
      ...fetchOptions,
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

module.exports = { fetchResponseText };
