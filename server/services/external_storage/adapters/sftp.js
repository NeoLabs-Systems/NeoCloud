'use strict';

// A folder on another machine over SFTP, password or private-key login. The
// host is resolved once by the guard in network.js and the connection goes
// to that address. The server's host key is remembered when the mount is
// first tested (`hostKey`, a SHA-256 fingerprint) and must match from then
// on. Symbolic links are not listed, so a link can never lead out of the
// folder the user attached.

const posix = require('path').posix;
const SftpClient = require('ssh2-sftp-client');
const { httpError } = require('../../../utils/http_error');
const { resolveAllowed } = require('../network');
const { isSafeListedName } = require('../paths');

const READY_TIMEOUT_MS = 20 * 1000;

function translate(err) {
  if (err.status) return err;
  if (err.code === 'EXTERNAL_HOST_BLOCKED') return err;
  if (err.level === 'client-authentication') return httpError(502, 'The server refused the user name, password or key.', 'EXTERNAL_AUTH_FAILED');
  if (err.code === 2 || /No such file/i.test(err.message || '')) return httpError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
  if (err.code === 3 || /Permission denied/i.test(err.message || '')) return httpError(502, 'The server denied access.', 'EXTERNAL_FORBIDDEN');
  if (/host key|host denied|verification failed/i.test(err.message || '')) return httpError(502, 'The server’s identity changed. Remove and add the connection again if you trust the change.', 'EXTERNAL_HOST_KEY');
  return httpError(502, 'Could not reach the server.', 'EXTERNAL_UNREACHABLE');
}

function create(config, { userId, clientFactory = () => new SftpClient('neocloud-external') }) {
  const client = clientFactory();
  const seen = { fingerprint: null };
  const base = posix.resolve('/', config.path || '/');
  const remote = (path) => (path ? posix.join(base, path) : base);
  // The checks below are belt and braces: normalizePath already refused "..".
  const inside = (target) => target === base || target.startsWith(base === '/' ? '/' : `${base}/`);
  const target = (path) => {
    const full = remote(path);
    if (!inside(full)) throw httpError(400, 'That path is not valid.', 'INVALID_PATH');
    return full;
  };
  const guard = (action) => async (...args) => {
    try {
      return await action(...args);
    } catch (err) {
      throw translate(err);
    }
  };

  return {
    get fingerprint() {
      return seen.fingerprint;
    },
    open: guard(async () => {
      const [{ address }] = await resolveAllowed(config.host, userId);
      await client.connect({
        host: address,
        port: Number(config.port) || 22,
        username: config.username,
        password: config.password || undefined,
        privateKey: config.privateKey || undefined,
        passphrase: config.passphrase || undefined,
        readyTimeout: READY_TIMEOUT_MS,
        hostHash: 'sha256',
        hostVerifier: (hash) => {
          seen.fingerprint = hash;
          return !config.hostKey || config.hostKey === hash;
        },
      });
      if (!(await client.exists(base))) throw httpError(404, 'That folder does not exist on the server.', 'EXTERNAL_NOT_FOUND');
    }),
    async close() {
      await client.end().catch(() => {});
    },
    list: guard(async (path, { limit }) => {
      const entries = [];
      for (const item of await client.list(target(path))) {
        if (item.type === 'l' || !isSafeListedName(item.name)) continue;
        entries.push({ name: item.name, type: item.type === 'd' ? 'folder' : 'file', size: item.type === 'd' ? 0 : item.size || 0, mtime: item.modifyTime ? new Date(item.modifyTime).toISOString() : null });
      }
      return { entries: entries.slice(0, limit), truncated: entries.length > limit };
    }),
    stat: guard(async (path) => {
      const kind = await client.exists(target(path));
      if (!kind || kind === 'l') return null;
      const info = await client.stat(target(path));
      return { type: kind === 'd' ? 'folder' : 'file', size: kind === 'd' ? 0 : info.size || 0, mtime: info.modifyTime ? new Date(info.modifyTime).toISOString() : null };
    }),
    read: guard(async (path, range) => client.createReadStream(target(path), range ? { start: range.start, end: range.end } : {})),
    write: guard(async (path, stream) => {
      await client.put(stream, target(path));
    }),
    mkdir: guard(async (path) => {
      await client.mkdir(target(path), false);
    }),
    move: guard(async (from, to) => {
      if (await client.exists(target(to))) throw httpError(409, 'An item with that name already exists.', 'NAME_EXISTS');
      await client.rename(target(from), target(to));
    }),
    remove: guard(async (path) => {
      const kind = await client.exists(target(path));
      if (!kind) throw httpError(404, 'Not found at the provider.', 'EXTERNAL_NOT_FOUND');
      if (kind === 'd') await client.rmdir(target(path), true);
      else await client.delete(target(path));
    }),
  };
}

module.exports = {
  type: 'sftp',
  fields: [
    { name: 'host', required: true, max: 255 },
    { name: 'port', kind: 'number' },
    { name: 'username', required: true, max: 256 },
    { name: 'password', secret: true, max: 1024 },
    { name: 'privateKey', secret: true, max: 16384 },
    { name: 'passphrase', secret: true, max: 1024 },
    { name: 'path', max: 1024 },
    { name: 'hostKey', max: 128, internal: true },
  ],
  validate(config) {
    if (!config.password && !config.privateKey) throw httpError(400, 'Enter a password or a private key.', 'EXTERNAL_CONFIG_INVALID');
    if (config.port !== undefined && !(Number.isInteger(config.port) && config.port > 0 && config.port < 65536)) {
      throw httpError(400, 'The port must be a number between 1 and 65535.', 'EXTERNAL_CONFIG_INVALID');
    }
    if (/[\u0000-\u001f]/.test(config.path || '') || (config.path || '').split('/').includes('..')) {
      throw httpError(400, 'That folder is not valid.', 'EXTERNAL_CONFIG_INVALID');
    }
  },
  hostOf: (config) => config.host,
  create,
};
