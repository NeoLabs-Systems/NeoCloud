'use strict';

// The indexer's worker thread: turns one file's bytes into text at a time.
// The main thread terminates it when a file takes too long.

const { parentPort } = require('worker_threads');
const { extractText } = require('./extractors');

parentPort.on('message', async ({ id, buffer, kind, name, maxChars }) => {
  try {
    const text = await extractText(Buffer.from(buffer), kind, name, { maxChars });
    parentPort.postMessage({ id, text: text.slice(0, maxChars) });
  } catch (err) {
    parentPort.postMessage({ id, error: String(err?.message || err) });
  }
});
