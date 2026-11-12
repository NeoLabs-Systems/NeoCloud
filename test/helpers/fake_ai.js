'use strict';

// A fake OpenAI-compatible provider and a fake NeoRouter for the AI tests,
// each on its own loopback port. They record what they were sent so tests
// can check which key, model and options reached them.

const http = require('http');

function readBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on('data', (chunk) => chunks.push(chunk));
    req.on('end', () => {
      const text = Buffer.concat(chunks).toString('utf8');
      if (!text) return resolve({});
      if (String(req.headers['content-type'] || '').includes('application/x-www-form-urlencoded')) {
        return resolve(Object.fromEntries(new URLSearchParams(text)));
      }
      try {
        return resolve(JSON.parse(text));
      } catch {
        return resolve({});
      }
    });
  });
}

function send(res, status, body) {
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(JSON.stringify(body));
}

function bearer(req) {
  return String(req.headers.authorization || '').replace(/^Bearer\s+/i, '');
}

function listen(server) {
  return new Promise((resolve) => server.listen(0, '127.0.0.1', () => resolve(`http://127.0.0.1:${server.address().port}`)));
}

// `reply(body)`, when a test sets one, writes the answer's content.
function chatAnswer(body, reply) {
  const json = body.response_format?.type === 'json_object';
  const scripted = reply ? reply(body) : undefined;
  return {
    id: 'chatcmpl-fake',
    object: 'chat.completion',
    model: body.model,
    choices: [{
      index: 0,
      finish_reason: 'stop',
      message: {
        role: 'assistant',
        content: scripted ?? (json ? '```json\n{"title":"Invoice March"}\n```' : `Hello from ${body.model}`),
      },
    }],
    usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 },
  };
}

// `vector(text)`, when a test sets one, gives each input's embedding.
function embeddingAnswer(body, model, vector) {
  const inputs = Array.isArray(body.input) ? body.input : [body.input];
  return {
    object: 'list',
    model,
    data: inputs.map((text, index) => ({ object: 'embedding', index, embedding: vector ? vector(text) : [0.1, 0.2, index] })),
    usage: { prompt_tokens: 4, total_tokens: 4 },
  };
}

/** The OpenAI-compatible provider, plus the public reference catalog at /reference/models. */
async function startFakeProvider() {
  const state = { keys: new Set(), chats: [], embeddings: [], modelListKeys: [], failNext: 0, reply: null, vector: null };
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://fake');
    const body = await readBody(req);
    if (url.pathname === '/reference/models') {
      return send(res, 200, {
        data: [
          { id: 'vendor/fake-chat', pricing: { prompt: '0.0000001' }, architecture: { input_modalities: ['text'] } },
          { id: 'vendor/fake-vision', pricing: { prompt: '0.000001' }, architecture: { input_modalities: ['text', 'image'] } },
          { id: 'vendor/fake-big', pricing: { prompt: '0.00001' }, architecture: { input_modalities: ['text'] } },
        ],
      });
    }
    if (!state.keys.has(bearer(req))) return send(res, 401, { error: { message: 'Invalid API key' } });
    if (req.method === 'GET' && url.pathname === '/v1/models') {
      state.modelListKeys.push(bearer(req));
      return send(res, 200, {
        object: 'list',
        data: ['fake-chat', 'fake-vision', 'fake-big', 'fake-embed'].map((id) => ({ id, object: 'model', created: 1700000000 })),
      });
    }
    if (req.method === 'POST' && url.pathname === '/v1/chat/completions') {
      if (state.failNext > 0) {
        state.failNext -= 1;
        return send(res, 503, { error: { message: 'overloaded' } });
      }
      state.chats.push({ key: bearer(req), body });
      return send(res, 200, chatAnswer(body, state.reply));
    }
    if (req.method === 'POST' && url.pathname === '/v1/embeddings') {
      state.embeddings.push({ key: bearer(req), body });
      return send(res, 200, embeddingAnswer(body, body.model, state.vector));
    }
    return send(res, 404, { error: { message: 'not found' } });
  });
  const origin = await listen(server);
  return { origin, baseUrl: `${origin}/v1`, referenceUrl: `${origin}/reference/models`, state, close: () => server.close() };
}

const ROUTER_MODELS = {
  chat: [
    { id: 'auto', name: 'Auto (key default: Balanced)', neorouter: { auto: true } },
    { id: 'auto:fast', name: 'Auto (Fast)', neorouter: { auto: true } },
    { id: 'auto:smart', name: 'Auto (Smartest)', neorouter: { auto: true } },
    { id: 'router-chat', name: 'Router Chat', supports_vision: true, pricing: { prompt: '0.000002', completion: '0.000004' }, neorouter: { kind: 'chat', vision: true, tools: true } },
  ],
  embedding: [
    { id: 'auto', name: 'Auto (key default: Balanced)', neorouter: { auto: true } },
    { id: 'router-embed', name: 'Router Embed', pricing: { prompt: '0.0000001' }, neorouter: { kind: 'embedding' } },
  ],
  transcription: [{ id: 'auto', name: 'Auto', neorouter: { auto: true } }],
  image: [],
};

/** A NeoRouter: device sign-in (approved on the second poll) and /v1. */
async function startFakeRouter() {
  const state = { issued: [], devices: new Map(), chats: [], embeddings: [], deviceRequests: [] };
  let counter = 0;
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, 'http://fake');
    const body = await readBody(req);
    if (req.method === 'POST' && url.pathname === '/oauth/device/code') {
      counter += 1;
      const deviceCode = `device-${counter}`;
      state.deviceRequests.push(body);
      state.devices.set(deviceCode, { polls: 0 });
      return send(res, 200, {
        device_code: deviceCode,
        user_code: 'ABCD-EFGH',
        verification_uri: `${origin}/app/connect`,
        verification_uri_complete: `${origin}/app/connect?code=ABCD-EFGH`,
        interval: 1,
        expires_in: 600,
      });
    }
    if (req.method === 'POST' && url.pathname === '/oauth/token') {
      const device = state.devices.get(body.device_code);
      if (!device || body.client_id !== 'neocloud') return send(res, 400, { error: 'invalid_grant' });
      device.polls += 1;
      if (device.polls < 2) return send(res, 400, { error: 'authorization_pending' });
      const token = `nr-key-${body.device_code}`;
      state.issued.push(token);
      return send(res, 200, { access_token: token, token_type: 'Bearer' });
    }
    if (!state.issued.includes(bearer(req))) return send(res, 401, { error: { message: 'Invalid key' } });
    if (req.method === 'GET' && url.pathname === '/v1/models') {
      return send(res, 200, { object: 'list', data: ROUTER_MODELS[url.searchParams.get('type')] || [], neorouter: { not_allowed_for_key: { paid: 2 } } });
    }
    if (req.method === 'POST' && url.pathname === '/v1/chat/completions') {
      state.chats.push({ key: bearer(req), body });
      return send(res, 200, chatAnswer(body));
    }
    if (req.method === 'POST' && url.pathname === '/v1/embeddings') {
      state.embeddings.push({ key: bearer(req), body });
      return send(res, 200, embeddingAnswer(body, 'google-2/gemini-embedding-001'));
    }
    return send(res, 404, { error: { message: 'not found' } });
  });
  const origin = await listen(server);
  return { origin, state, close: () => server.close() };
}

module.exports = { startFakeProvider, startFakeRouter };
