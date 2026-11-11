'use strict';

const fs = require('fs');
const path = require('path');
const { LOG_DIR, ensurePrivateDirectory, ensurePrivateFile } = require('../../runtime/paths');

const SENSITIVE_KEY_RE = /(?:^|_|-)(?:token|secret|password|api[_-]?key|authorization|cookie|session(?:id)?|sid|key)(?:$|_|-)/i;

function redactSecrets(input) {
  let text = String(input || '');
  if (!text) return text;
  return text
    .replace(/(\/(?:api\/public\/shares|s|devices\/authorize)\/)[^/?#\s]+/g, '$1[redacted]')
    .replace(/\b(Bearer\s+)[A-Za-z0-9._~+\/-]+=*/gi, '$1[redacted]')
    .replace(/([?&](?:token|access_token|api[_-]?key|secret|password|authorization|cookie|session|sid)=)([^&#\s]+)/gi, '$1[redacted]')
    .replace(/\b(token|access_token|authorization|api[_-]?key|secret|password|cookie|session(?:id)?|sid)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^,\s;]+)/gi, '$1=[redacted]');
}

function serializeValue(value, seen = new WeakSet()) {
  if (value instanceof Error) {
    return JSON.stringify({
      name: value.name,
      message: redactSecrets(value.message),
      stack: redactSecrets(value.stack),
      code: value.code,
    });
  }
  if (typeof value === 'bigint') return value.toString();
  if (typeof value === 'function') return `[Function ${value.name || 'anonymous'}]`;
  if (!value || typeof value !== 'object') return redactSecrets(String(value));
  if (seen.has(value)) return '[Circular]';
  seen.add(value);
  try {
    return JSON.stringify(value, (key, nested) => {
      if (SENSITIVE_KEY_RE.test(String(key || ''))) return '[redacted]';
      if (nested instanceof Error) {
        return { name: nested.name, message: redactSecrets(nested.message), code: nested.code };
      }
      if (typeof nested === 'bigint') return nested.toString();
      if (typeof nested === 'string') return redactSecrets(nested);
      return nested;
    });
  } catch (err) {
    return `[Unserializable object: ${redactSecrets(err.message)}]`;
  } finally {
    seen.delete(value);
  }
}

function formatLogArgs(args) {
  return Array.from(args).map((value) => serializeValue(value)).join(' ');
}

function logRequestSummary(level, req, message, extra = null) {
  const prefix = `[HTTP] ${req.method} ${redactSecrets(req.originalUrl || req.url)}`;
  console[level](redactSecrets(`${prefix} ${message}`), {
    ip: req.ip,
    userId: req.userId || req.session?.userId || null,
    ...extra,
  });
}

function createServiceLogger(serviceName) {
  const prefix = `[${String(serviceName || 'Service').trim() || 'Service'}]`;
  return {
    debug: (...args) => console.debug(prefix, ...args),
    info: (...args) => console.info(prefix, ...args),
    warn: (...args) => console.warn(prefix, ...args),
    error: (...args) => console.error(prefix, ...args),
  };
}

const LOG_FILE = path.join(LOG_DIR, 'server-logs.jsonl');
const MAX_LOG_HISTORY = 1000;
const TRIM_AT = 1100;

function loadPersistedLogs() {
  try {
    const entries = [];
    for (const line of fs.readFileSync(LOG_FILE, 'utf8').split('\n')) {
      if (!line.trim()) continue;
      try { entries.push(JSON.parse(line)); } catch {}
    }
    return entries.slice(-MAX_LOG_HISTORY);
  } catch {
    return [];
  }
}

/**
 * Mirrors console output into the last 1000 entries on disk, so the admin
 * page can show recent logs after a restart. Returns the live history array.
 */
function setupConsoleInterceptor() {
  try {
    ensurePrivateDirectory(LOG_DIR);
    fs.closeSync(fs.openSync(LOG_FILE, 'a', 0o600));
    ensurePrivateFile(LOG_FILE);
  } catch {}
  const history = loadPersistedLogs();

  function persist(type, args) {
    const entry = { type, message: formatLogArgs(args), timestamp: new Date().toISOString() };
    history.push(entry);
    try { fs.appendFileSync(LOG_FILE, `${JSON.stringify(entry)}\n`); } catch {}
    if (history.length > TRIM_AT) {
      history.splice(0, history.length - MAX_LOG_HISTORY);
      try { fs.writeFileSync(LOG_FILE, `${history.map((e) => JSON.stringify(e)).join('\n')}\n`); } catch {}
    }
  }

  for (const type of ['log', 'info', 'warn', 'error']) {
    const original = console[type].bind(console);
    console[type] = (...args) => {
      original(...args);
      persist(type, args);
    };
  }
  return history;
}

module.exports = {
  createServiceLogger,
  formatLogArgs,
  logRequestSummary,
  redactSecrets,
  setupConsoleInterceptor,
};
