'use strict';

const { fetchResponseText } = require('../http');

// Calls to a NeoRouter's OpenAI-style API with a connection from
// connection.js.
async function call(connection, path, { signal, timeoutMs = 30000 } = {}) {
  const { response, text } = await fetchResponseText(`${connection.apiBase}${path}`, {
    method: 'GET',
    headers: { Authorization: `Bearer ${connection.apiKey}`, Accept: 'application/json' },
    maxResponseBytes: 4 * 1024 * 1024,
    serviceName: 'NeoRouter',
    signal,
    timeoutMs,
  });
  let data = null;
  try {
    data = JSON.parse(text || 'null');
  } catch {
    data = null;
  }
  if (!response.ok) {
    const error = new Error(`NeoRouter: ${data?.error?.message || `HTTP ${response.status}`}`);
    error.status = response.status;
    throw error;
  }
  return data;
}

// The models of a kind the key may use, and how many more the router has
// that the key's free/plan/paid choice leaves out.
async function modelList(connection, type = 'chat', { signal } = {}) {
  const data = await call(connection, `/models?type=${encodeURIComponent(type)}`, { signal });
  return { models: Array.isArray(data?.data) ? data.data : [], notAllowed: data?.neorouter?.not_allowed_for_key || {} };
}

module.exports = { modelList };
