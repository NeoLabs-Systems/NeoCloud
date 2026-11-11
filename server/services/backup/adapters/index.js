'use strict';

// Every backup target speaks the same small interface:
//   open() / close()                connect and make sure the base folder exists
//   list(prefix)    -> [key]        every file below prefix, keys relative to the base
//   put(key, open)                  write a file; open() returns a Readable
//   get(key)        -> Readable
//   delete(key) / deletePrefix(prefix)
// Keys always use "/" and never start with one.

const { createLocalAdapter } = require('./local');
const { createS3Adapter } = require('./s3');
const { createSftpAdapter } = require('./sftp');
const { createWebdavAdapter } = require('./webdav');

const FACTORIES = {
  local: createLocalAdapter,
  sftp: createSftpAdapter,
  s3: createS3Adapter,
  webdav: createWebdavAdapter,
};

function createAdapter(type, config) {
  const factory = FACTORIES[type];
  if (!factory) throw new Error(`Unknown backup target type: ${type}`);
  return factory(config);
}

module.exports = { BACKUP_TYPES: Object.keys(FACTORIES), createAdapter };
