'use strict';

// A directory on this machine: a second disk, or a mounted NAS share. Every
// file is fsynced before it is renamed into place and its folder after, so
// once the manifest exists everything it lists is on the disk.

const fs = require('fs');
const path = require('path');
const { pipeline } = require('stream/promises');
const { httpError } = require('../../../utils/http_error');
const { syncDirectory, syncFile } = require('../../../utils/durable_fs');

function createLocalAdapter({ path: root }) {
  const base = path.resolve(String(root || ''));
  if (!root || !path.isAbsolute(String(root))) throw httpError(400, 'Enter an absolute folder path.', 'BACKUP_CONFIG_INVALID');

  function resolve(key) {
    const target = path.resolve(base, key);
    if (target !== base && !target.startsWith(base + path.sep)) throw new Error('Key escapes the backup folder.');
    return target;
  }

  async function walk(dir, prefix, out) {
    let entries;
    try {
      entries = await fs.promises.readdir(dir, { withFileTypes: true });
    } catch (err) {
      if (err.code === 'ENOENT') return;
      throw err;
    }
    for (const entry of entries) {
      const key = prefix ? `${prefix}/${entry.name}` : entry.name;
      if (entry.isDirectory()) await walk(path.join(dir, entry.name), key, out);
      else if (!entry.name.startsWith('.tmp-')) out.push(key);
    }
  }

  return {
    async open() {
      await fs.promises.mkdir(base, { recursive: true });
    },
    async close() {},
    async list(prefix) {
      const out = [];
      await walk(resolve(prefix), prefix.replace(/\/$/, ''), out);
      return out;
    },
    async put(key, openSource) {
      const target = resolve(key);
      await fs.promises.mkdir(path.dirname(target), { recursive: true });
      const temporary = path.join(path.dirname(target), `.tmp-${path.basename(target)}`);
      await pipeline(openSource(), fs.createWriteStream(temporary));
      await syncFile(temporary);
      await fs.promises.rename(temporary, target);
      await syncDirectory(path.dirname(target));
    },
    async get(key) {
      return fs.createReadStream(resolve(key));
    },
    async delete(key) {
      await fs.promises.rm(resolve(key), { force: true });
    },
    async deletePrefix(prefix) {
      await fs.promises.rm(resolve(prefix), { recursive: true, force: true });
    },
  };
}

module.exports = { createLocalAdapter };
