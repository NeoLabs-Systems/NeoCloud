'use strict';

// The background pass of the signing feature: expires documents that ran out
// of time and retries finalizations that failed. Starts with the server,
// stops with it; the timer never keeps the process alive.

const { createServiceLogger } = require('../../utils/logger');
const { expireDueDocuments } = require('./expiry');
const { retryDueFinalizations } = require('./finalize');

const logger = createServiceLogger('Signing');
const INTERVAL_MS = 30 * 1000;
const FIRST_PASS_MS = 10 * 1000;

async function runPass() {
  try {
    expireDueDocuments();
    await retryDueFinalizations();
  } catch (err) {
    logger.error(`Background pass failed: ${err.message}`);
  }
}

/** Starts the worker. Returns the function that stops it. */
function startSigningWorker() {
  let stopped = false;
  let timer = null;
  const schedule = (delay) => {
    timer = setTimeout(async () => {
      await runPass();
      if (!stopped) schedule(INTERVAL_MS);
    }, delay);
    timer.unref();
  };
  schedule(FIRST_PASS_MS);
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}

module.exports = { runPass, startSigningWorker };
