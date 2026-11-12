'use strict';

const { createAbortError } = require('../../utils/abort');
const {
  RETRYABLE_HTTP_STATUS,
  RETRYABLE_NETWORK_CODES,
  abortableDelay,
  computeBackoffMs,
  getErrorCode,
  getHttpStatus,
  retryAfterMilliseconds,
} = require('../../utils/retry');
const { createServiceLogger } = require('../../utils/logger');

const logger = createServiceLogger('AiRetry');

// A transient blip (rate limit, provider overload, brief network failure)
// retries the same model with a short backoff.
const DEFAULTS = {
  maxAttempts: 3, // total attempts including the first
  baseDelayMs: 500,
  maxDelayMs: 8000,
  // A provider that asks for a longer wait is reporting exhausted quota, not
  // a blip; the call fails instead of parking on that timer.
  maxRetryAfterMs: 60_000,
};

// SDKs disagree on where they put the HTTP status; getHttpStatus checks all.
function isTransientError(err) {
  if (!err) return false;
  const status = getHttpStatus(err);
  if (status !== null) return RETRYABLE_HTTP_STATUS.has(status);
  const code = getErrorCode(err);
  if (code && RETRYABLE_NETWORK_CODES.has(String(code))) return true;
  const name = String(err.name || '');
  if (name === 'APIConnectionError' || name === 'APIConnectionTimeoutError') return true;
  const message = String(err.message || '').toLowerCase();
  return /\b(overloaded|rate limit|timed? ?out|timeout|temporarily unavailable|connection (?:reset|refused|error)|socket hang up|network (?:error|timeout)|service unavailable)\b/.test(message);
}

function retryAfterMs(err) {
  if (!err || typeof err !== 'object') return null;
  return retryAfterMilliseconds(err.headers || err.response?.headers);
}

/**
 * Runs `fn(attempt)` and retries transient failures with backoff, honoring a
 * provider's Retry-After up to a cap.
 */
async function withProviderRetry(fn, options = {}) {
  const { maxAttempts, baseDelayMs, maxDelayMs, maxRetryAfterMs } = { ...DEFAULTS, ...options };
  const label = options.label || 'ProviderRetry';
  let attempt = 0;
  for (;;) {
    if (options.signal?.aborted) throw createAbortError(options.signal, 'Provider retry aborted.');
    attempt += 1;
    try {
      return await fn(attempt);
    } catch (err) {
      if (attempt >= maxAttempts || !isTransientError(err)) throw err;
      const requestedWaitMs = retryAfterMs(err);
      if (requestedWaitMs !== null && requestedWaitMs > maxRetryAfterMs) throw err;
      const waitMs = requestedWaitMs ?? computeBackoffMs(attempt, baseDelayMs, maxDelayMs);
      logger.warn(`${label}: transient failure on attempt ${attempt}/${maxAttempts}; retrying in ${waitMs}ms`);
      await abortableDelay(waitMs, options.signal);
    }
  }
}

module.exports = { withProviderRetry };
