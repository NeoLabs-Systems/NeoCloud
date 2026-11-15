'use strict';

// Every provider is one module with the same shape and is listed here:
//   type                 the name clients send
//   fields               the settings it takes: { name, kind?, required?, secret?, max?, internal? }
//   hostOf(config)       the host to check against the network rules (null: none)
//   create(config, ctx)  -> adapter, with ctx = { userId }
//
// An adapter speaks one small interface; paths are mount-relative ("" is the
// root, "a/b" below it) and were normalised before they get here:
//   open() / close()
//   list(path, { limit })      -> { entries: [{ name, type: 'file'|'folder', size, mtime }], truncated }
//   stat(path)                 -> { type, size, mtime } | null
//   read(path, range?)         -> Readable (range: { start, end }, inclusive)
//   write(path, readable, { size })
//   mkdir(path) / move(from, to) / remove(path)

const s3 = require('./s3');
const sftp = require('./sftp');
const webdav = require('./webdav');

const PROVIDERS = Object.fromEntries([s3, webdav, sftp].map((provider) => [provider.type, provider]));

module.exports = { PROVIDERS, PROVIDER_TYPES: Object.keys(PROVIDERS) };
