'use strict';

// Whether the local server answers, for commands that start, stop or must
// not run alongside it.

const { DEFAULT_PORT } = require('../setup/contract');
const { runningServerPid } = require('../setup/service_adapters');

function port() {
  return Number(process.env.PORT) || DEFAULT_PORT;
}

async function fetchHealth() {
  try {
    const response = await fetch(`http://127.0.0.1:${port()}/api/health`, { signal: AbortSignal.timeout(2000) });
    return response.ok;
  } catch {
    return false;
  }
}

async function waitFor(condition, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await condition()) return true;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  return false;
}

function waitForHealth(timeoutMs = 15000) {
  return waitFor(fetchHealth, timeoutMs);
}

// The server stops answering at once but finishes in-flight requests for up
// to 30 s before its process exits.
async function waitForStop(timeoutMs = 40000) {
  return waitFor(async () => !runningServerPid() && !(await fetchHealth()), timeoutMs);
}

module.exports = { fetchHealth, port, waitForHealth, waitForStop };
