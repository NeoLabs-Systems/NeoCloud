'use strict';

// Another machine over SSH. Password or private-key login. Uploads are
// fsynced on the server (OpenSSH's fsync@openssh.com) before the rename when
// it supports that, so the manifest never lands before the data it lists.

const path = require('path').posix;
const SftpClient = require('ssh2-sftp-client');
const { httpError } = require('../../../utils/http_error');

function createSftpAdapter({ host, port = 22, username, password, privateKey, passphrase, path: root }) {
  if (!host || !username || !root) throw httpError(400, 'Host, username and folder are required.', 'BACKUP_CONFIG_INVALID');
  if (!password && !privateKey) throw httpError(400, 'Enter a password or a private key.', 'BACKUP_CONFIG_INVALID');
  const client = new SftpClient('neocloud-backup');
  const base = String(root).replace(/\/+$/, '') || '/';
  const remote = (key) => path.join(base, key);

  // Best effort: servers without the OpenSSH extension skip it.
  async function fsyncRemote(target) {
    const sftp = client.sftp;
    if (!sftp?.ext_openssh_fsync) return;
    const call = (method, ...args) => new Promise((resolve, reject) => {
      sftp[method](...args, (err, value) => (err ? reject(err) : resolve(value)));
    });
    let handle;
    try {
      handle = await call('open', target, 'r+');
      await call('ext_openssh_fsync', handle);
    } catch {
      // Unsupported or refused: the rename still makes the write atomic.
    } finally {
      if (handle) await call('close', handle).catch(() => {});
    }
  }

  async function walk(dir, prefix, out) {
    let entries;
    try {
      entries = await client.list(dir);
    } catch (err) {
      if (err.code === 2 || /No such file/i.test(err.message)) return;
      throw err;
    }
    for (const entry of entries) {
      if (entry.name === '.' || entry.name === '..') continue;
      // A name the server makes up must not lead a key above the base folder.
      if (!entry.name || /[/\\]/.test(entry.name)) throw new Error('SFTP listing has an unsafe name.');
      const key = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.type === 'd') await walk(path.join(dir, entry.name), key, out);
      else if (!entry.name.startsWith('.tmp-')) out.push(key);
    }
  }

  return {
    async open() {
      await client.connect({
        host,
        port: Number(port) || 22,
        username,
        password: password || undefined,
        privateKey: privateKey || undefined,
        passphrase: passphrase || undefined,
        readyTimeout: 20000,
      });
      await client.mkdir(base, true);
    },
    async close() {
      await client.end().catch(() => {});
    },
    async list(prefix) {
      const out = [];
      await walk(remote(prefix), prefix.replace(/\/$/, ''), out);
      return out;
    },
    async put(key, openSource) {
      const target = remote(key);
      const temporary = path.join(path.dirname(target), `.tmp-${path.basename(target)}`);
      await client.mkdir(path.dirname(target), true);
      await client.put(openSource(), temporary);
      await fsyncRemote(temporary);
      if (await client.exists(target)) await client.delete(target);
      await client.rename(temporary, target);
    },
    async get(key) {
      return client.createReadStream(remote(key));
    },
    async delete(key) {
      await client.delete(remote(key), true);
    },
    async deletePrefix(prefix) {
      if (await client.exists(remote(prefix))) await client.rmdir(remote(prefix), true);
    },
  };
}

module.exports = { createSftpAdapter };
