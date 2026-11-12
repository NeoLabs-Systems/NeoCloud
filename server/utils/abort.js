'use strict';

// AbortSignal helpers shared by the AI provider calls.

function createAbortError(signalOrReason = null, fallback = 'Operation aborted.') {
  const reason = signalOrReason && typeof signalOrReason === 'object'
    && 'aborted' in signalOrReason
    ? signalOrReason.reason
    : signalOrReason;
  if (reason instanceof Error) return reason;
  const error = new Error(String(reason || fallback));
  error.name = 'AbortError';
  error.code = 'ABORT_ERR';
  return error;
}

function isAbortError(error, signal = null) {
  return signal?.aborted === true
    || error?.name === 'AbortError'
    || error?.code === 'ABORT_ERR';
}

function throwIfAborted(signal, fallback = 'Operation aborted.') {
  if (signal?.aborted) throw createAbortError(signal, fallback);
}

module.exports = { createAbortError, isAbortError, throwIfAborted };
