'use strict';

// The background loop of signing mail: sends what the outbox holds, picks up
// mail an earlier run left half-sent, and queues automatic reminders and
// expiry warnings. Starts with the server, stops with it; the timers never
// keep the process alive.

const { createServiceLogger } = require('../../../utils/logger');
const { pruneLedger } = require('./limits');
const { processDue, recoverInterrupted } = require('./outbox');
const { runReminderPass } = require('./reminders');

const logger = createServiceLogger('SigningMail');
const SEND_INTERVAL_MS = 15 * 1000;
const REMINDER_INTERVAL_MS = 5 * 60 * 1000;
const FIRST_PASS_MS = 5 * 1000;

async function runMailPass({ now = Date.now(), reminders = true } = {}) {
  try {
    if (reminders) {
      runReminderPass({ now });
      pruneLedger(now);
    }
    await processDue({ now });
  } catch (err) {
    logger.error(`Mail pass failed: ${err.message}`);
  }
}

/** Starts the worker. Returns the function that stops it. */
function startMailWorker() {
  let stopped = false;
  let timer = null;
  let lastReminderPass = 0;
  // Mail "sending" when the last run ended was cut off: it goes out again.
  recoverInterrupted(Date.now(), { all: true });
  const schedule = (delay) => {
    timer = setTimeout(async () => {
      const due = Date.now() - lastReminderPass >= REMINDER_INTERVAL_MS;
      if (due) lastReminderPass = Date.now();
      await runMailPass({ reminders: due });
      if (!stopped) schedule(SEND_INTERVAL_MS);
    }, delay);
    timer.unref();
  };
  schedule(FIRST_PASS_MS);
  return () => {
    stopped = true;
    clearTimeout(timer);
  };
}

module.exports = { runMailPass, startMailWorker };
