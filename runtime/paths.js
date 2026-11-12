'use strict';

// Every on-disk location NeoCloud uses, resolved once. Nothing else in the
// codebase builds paths under ~/.neocloud by hand.

const fs = require('fs');
const crypto = require('crypto');
const os = require('os');
const path = require('path');
const { parseEnv } = require('./env');

const APP_DIR = path.resolve(__dirname, '..');
const HOME_DIR = os.homedir();
const RUNTIME_HOME = path.resolve(process.env.NEOCLOUD_HOME || path.join(HOME_DIR, '.neocloud'));
const DATA_DIR = path.resolve(process.env.NEOCLOUD_DATA_DIR || path.join(RUNTIME_HOME, 'data'));
const LOG_DIR = path.join(DATA_DIR, 'logs');
const ENV_FILE = path.resolve(process.env.NEOCLOUD_ENV_FILE || path.join(RUNTIME_HOME, '.env'));
const DATABASE_FILE = path.join(DATA_DIR, 'neocloud.db');
const PID_FILE = path.join(DATA_DIR, 'neocloud.pid');
// Encrypted database snapshots kept on this machine: the daily default when no
// backup target is scheduled, and the one `neocloud update` takes first.
const LOCAL_BACKUP_DIR = path.join(DATA_DIR, 'backups');
// Small JSON documents the server keeps between restarts (scrub progress,
// backup verification results).
const STATE_DIR = path.join(DATA_DIR, 'state');

// Encrypted file contents. Defaults inside DATA_DIR; point NEOCLOUD_STORAGE_DIR
// at a bigger disk to keep the database and the blobs apart.
const STORAGE_DIR = path.resolve(process.env.NEOCLOUD_STORAGE_DIR || path.join(DATA_DIR, 'storage'));
const BLOB_DIR = path.join(STORAGE_DIR, 'blobs');
// In-flight uploads. Same filesystem as BLOB_DIR so finishing one is a rename.
const UPLOAD_TMP_DIR = path.join(STORAGE_DIR, 'tmp');
// Generated renditions (thumbnails, PDFs of Office files), encrypted like blobs.
const PREVIEW_DIR = path.join(STORAGE_DIR, 'previews');

function ensurePrivateDirectory(dir) {
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  if (process.platform !== 'win32') {
    fs.chmodSync(dir, 0o700);
  }
}

function ensurePrivateFile(filePath) {
  if (!fs.existsSync(filePath) || process.platform === 'win32') return;
  fs.chmodSync(filePath, 0o600);
}

function ensureRuntimeDirs() {
  for (const dir of [RUNTIME_HOME, DATA_DIR, LOG_DIR, STORAGE_DIR, BLOB_DIR, UPLOAD_TMP_DIR, PREVIEW_DIR]) {
    ensurePrivateDirectory(dir);
  }
}

function applySecureUmask() {
  if (process.platform !== 'win32' && typeof process.umask === 'function') {
    process.umask(0o077);
  }
}

function readEnvFileRaw(envFile = ENV_FILE) {
  try {
    return fs.readFileSync(envFile, 'utf8');
  } catch {
    return '';
  }
}

function writeEnvFileAtomic(envFile, content) {
  const resolved = path.resolve(envFile);
  const directory = path.dirname(resolved);
  const temporary = path.join(
    directory,
    `.${path.basename(resolved)}.${process.pid}.${crypto.randomBytes(8).toString('hex')}.tmp`,
  );
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  let descriptor = null;
  try {
    descriptor = fs.openSync(temporary, 'wx', 0o600);
    fs.writeFileSync(descriptor, content, 'utf8');
    fs.fsyncSync(descriptor);
    fs.closeSync(descriptor);
    descriptor = null;
    fs.renameSync(temporary, resolved);
  } finally {
    if (descriptor !== null) {
      try { fs.closeSync(descriptor); } catch {}
    }
    try { fs.rmSync(temporary, { force: true }); } catch {}
  }
}

function normalizeEnvKey(key) {
  const normalized = String(key).replace(/[\r\n]/g, '');
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(normalized)) {
    throw new Error(`Invalid environment variable name: ${normalized || '(empty)'}`);
  }
  return normalized;
}

function upsertEnvValue(envFile, key, value) {
  const safeKey = normalizeEnvKey(key);
  const safeValue = String(value).replace(/[\r\n]/g, '');
  const raw = readEnvFileRaw(envFile);
  const lines = raw ? raw.split('\n') : [];
  let replaced = false;

  for (let i = 0; i < lines.length; i++) {
    if (lines[i].startsWith(`${safeKey}=`)) {
      lines[i] = `${safeKey}=${safeValue}`;
      replaced = true;
      break;
    }
  }
  if (!replaced) lines.push(`${safeKey}=${safeValue}`);

  const output = lines.filter((line, idx, arr) => idx !== arr.length - 1 || line !== '').join('\n') + '\n';
  writeEnvFileAtomic(envFile, output);
}

function generateSecret(bytes = 32) {
  return crypto.randomBytes(bytes).toString('hex');
}

const SESSION_SECRET_PLACEHOLDERS = new Set([
  'change-me',
  'change-this-to-a-random-secret-in-production',
]);

function isValidMasterKey(value) {
  return /^[0-9a-f]{64}$/i.test(String(value || '').trim());
}

/**
 * Whether this install already holds data encrypted under a master key: an
 * account in the database, or any stored blob. Then a missing key must never
 * be replaced by a new one -- that would leave every existing file
 * unreadable and encrypt new ones under a key the old data doesn't know.
 */
function hasEncryptedData({ databaseFile = DATABASE_FILE, blobDir = BLOB_DIR } = {}) {
  try {
    if (fs.readdirSync(blobDir).length > 0) return true;
  } catch {
    // No blob folder yet.
  }
  if (!fs.existsSync(databaseFile)) return false;
  let database;
  try {
    const Database = require('better-sqlite3');
    database = new Database(databaseFile, { readonly: true, fileMustExist: true });
    return Boolean(database.prepare('SELECT 1 FROM users LIMIT 1').get());
  } catch (err) {
    // No users table yet: a database from a start that never got further.
    return !/no such table/i.test(err.message);
  } finally {
    database?.close();
  }
}

/**
 * Fills in the secrets a fresh install needs. The master key is generated
 * exactly once, on an install without data: every stored file is encrypted
 * under it, so it is never replaced once it exists -- a malformed or missing
 * value on an install with data stops the server instead.
 */
function ensureSecureRuntimeEnv({ envFile = ENV_FILE, env = process.env, logger = console, dataPaths = {} } = {}) {
  const parsed = parseEnv(readEnvFileRaw(envFile));
  const changes = [];

  let sessionSecret = String(env.SESSION_SECRET || parsed.get('SESSION_SECRET') || '').trim();
  if (!sessionSecret || SESSION_SECRET_PLACEHOLDERS.has(sessionSecret)) {
    sessionSecret = generateSecret(32);
    upsertEnvValue(envFile, 'SESSION_SECRET', sessionSecret);
    changes.push('SESSION_SECRET');
  }
  env.SESSION_SECRET = sessionSecret;

  let masterKey = String(env.NEOCLOUD_MASTER_KEY || parsed.get('NEOCLOUD_MASTER_KEY') || '').trim();
  if (!masterKey) {
    if (hasEncryptedData(dataPaths)) {
      throw new Error(
        `NEOCLOUD_MASTER_KEY is missing (not in the environment, not in ${envFile}), but this install already `
        + 'holds encrypted data. Put the original key back (from `neocloud keys export` or your copy of .env). '
        + 'A new key would make every stored file unreadable, so none is created.',
      );
    }
    masterKey = generateSecret(32);
    upsertEnvValue(envFile, 'NEOCLOUD_MASTER_KEY', masterKey);
    changes.push('NEOCLOUD_MASTER_KEY');
  } else if (!isValidMasterKey(masterKey)) {
    throw new Error(
      `NEOCLOUD_MASTER_KEY in ${envFile} is not 64 hex characters. Restore the original key; `
      + 'without it, stored files cannot be decrypted.',
    );
  }
  env.NEOCLOUD_MASTER_KEY = masterKey;

  ensurePrivateFile(envFile);
  if (changes.length > 0 && logger?.info) {
    logger.info(`Initialized runtime secrets: ${changes.join(', ')}`);
  }
  return { changes };
}

module.exports = {
  APP_DIR,
  HOME_DIR,
  RUNTIME_HOME,
  DATA_DIR,
  LOG_DIR,
  ENV_FILE,
  DATABASE_FILE,
  PID_FILE,
  LOCAL_BACKUP_DIR,
  STATE_DIR,
  STORAGE_DIR,
  BLOB_DIR,
  UPLOAD_TMP_DIR,
  PREVIEW_DIR,
  applySecureUmask,
  ensurePrivateDirectory,
  ensurePrivateFile,
  ensureRuntimeDirs,
  ensureSecureRuntimeEnv,
  hasEncryptedData,
  isValidMasterKey,
  readEnvFileRaw,
  upsertEnvValue,
};
