'use strict';

// Getting a file's text for the index: the blob is decrypted into memory,
// then handed to `pdftotext` (PDFs, when installed; it reads stdin) or to the
// extractor worker thread. Every run is time-boxed and the text is capped.
// Nothing is written to disk in plaintext.

const os = require('os');
const path = require('path');
const { spawn } = require('child_process');
const { Worker } = require('worker_threads');
const { openBlob } = require('../storage/blob_store');
const { MAX_SOURCE_BYTES, indexableKind } = require('./extractors');

const MAX_TEXT_BYTES = 1024 * 1024;
const EXTRACT_TIMEOUT_MS = 60 * 1000;
const LOW_PRIORITY = 10;

class ExtractError extends Error {}

// --- pdftotext ----------------------------------------------------------------

let pdftotextAvailable;

/** Whether poppler's pdftotext is on PATH, checked once. */
function hasPdftotext() {
  pdftotextAvailable ??= new Promise((resolve) => {
    const child = spawn('pdftotext', ['-v'], { stdio: 'ignore' });
    child.once('error', () => resolve(false));
    // Old versions exit with 99 after printing the version; either way it runs.
    child.once('exit', () => resolve(true));
  });
  return pdftotextAvailable;
}

function runPdftotext(buffer) {
  return new Promise((resolve, reject) => {
    const child = spawn('pdftotext', ['-q', '-enc', 'UTF-8', '-', '-'], { stdio: ['pipe', 'pipe', 'ignore'] });
    try {
      os.setPriority(child.pid, LOW_PRIORITY);
    } catch {
      // Not allowed here; it still runs.
    }
    const chunks = [];
    let size = 0;
    const timer = setTimeout(() => child.kill('SIGKILL'), EXTRACT_TIMEOUT_MS);
    child.stdout.on('data', (chunk) => {
      if (size >= MAX_TEXT_BYTES * 2) return;
      chunks.push(chunk);
      size += chunk.length;
    });
    child.stdin.on('error', () => {
      // pdftotext stops reading early on a broken file; the exit code says so.
    });
    child.once('error', (err) => {
      clearTimeout(timer);
      reject(err);
    });
    child.once('close', (code, signal) => {
      clearTimeout(timer);
      if (signal) reject(new ExtractError('pdftotext took too long.'));
      else if (code !== 0) reject(new ExtractError(`pdftotext failed (${code}).`));
      else resolve(Buffer.concat(chunks).toString('utf8'));
    });
    child.stdin.end(buffer);
  });
}

// --- The worker thread --------------------------------------------------------

let worker = null;
let nextJobId = 0;
const jobs = new Map();

function failAll(err) {
  for (const job of jobs.values()) job.reject(err);
  jobs.clear();
}

function getWorker() {
  if (worker) return worker;
  const thread = new Worker(path.join(__dirname, 'extract_worker.js'));
  thread.unref();
  thread.on('message', ({ id, text, error }) => {
    const job = jobs.get(id);
    if (!job) return;
    jobs.delete(id);
    if (error) job.reject(new ExtractError(error));
    else job.resolve(text);
  });
  const gone = (message) => {
    if (worker !== thread) return;
    worker = null;
    failAll(new ExtractError(message));
  };
  thread.on('error', (err) => gone(err.message));
  thread.on('exit', () => gone('The extractor stopped.'));
  worker = thread;
  return thread;
}

function runInWorker(buffer, kind, name) {
  const id = ++nextJobId;
  const thread = getWorker();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      jobs.delete(id);
      reject(new ExtractError('Extraction took too long.'));
      // Whatever it was stuck on goes with the thread; the next file gets a new one.
      if (worker === thread) worker = null;
      thread.terminate();
    }, EXTRACT_TIMEOUT_MS);
    jobs.set(id, {
      resolve: (text) => {
        clearTimeout(timer);
        resolve(text);
      },
      reject: (err) => {
        clearTimeout(timer);
        reject(err);
      },
    });
    // A copy that can be transferred; the caller's buffer may be pooled.
    const bytes = new Uint8Array(buffer);
    thread.postMessage({ id, buffer: bytes, kind, name, maxChars: MAX_TEXT_BYTES }, [bytes.buffer]);
  });
}

/** Stops the worker thread (server shutdown). */
async function stopExtractor() {
  if (worker) await worker.terminate();
}

// --- Entry point ----------------------------------------------------------------

async function readBlob(userId, node) {
  const chunks = [];
  for await (const chunk of openBlob(userId, node.blob_id, node.size)) chunks.push(chunk);
  return Buffer.concat(chunks);
}

// At most MAX_TEXT_BYTES of UTF-8, without a broken character at the end.
function capText(raw) {
  const text = raw.replace(/\u0000/g, ' ');
  const bytes = Buffer.from(text, 'utf8');
  if (bytes.length <= MAX_TEXT_BYTES) return text;
  return bytes.subarray(0, MAX_TEXT_BYTES).toString('utf8').replace(/�$/, '');
}

/**
 * The text of a file node (a row with name, mime, size, blob_id). Throws an
 * ExtractError for files that can't be read (too large, damaged, too slow).
 */
async function extractNodeText(userId, node) {
  const kind = indexableKind(node.name, node.mime);
  if (!kind) throw new ExtractError('Not a file type the index reads.');
  if (node.size > MAX_SOURCE_BYTES[kind]) throw new ExtractError('File is too large to index.');
  const buffer = await readBlob(userId, node);
  if (kind === 'pdf' && await hasPdftotext()) return capText(await runPdftotext(buffer));
  return capText(await runInWorker(buffer, kind, node.name));
}

module.exports = { ExtractError, extractNodeText, hasPdftotext, stopExtractor };
