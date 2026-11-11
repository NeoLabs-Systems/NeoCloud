'use strict';

// The one database handle. Every query in the server goes through it.

const Database = require('better-sqlite3');
const { DATABASE_FILE, ensurePrivateFile, ensureRuntimeDirs } = require('../../runtime/paths');
const { runVersionedMigrations } = require('../../lib/schema_migrations');

ensureRuntimeDirs();
const db = new Database(DATABASE_FILE);
ensurePrivateFile(DATABASE_FILE);

try {
  db.pragma('journal_mode = WAL');
} catch {
  db.pragma('journal_mode = DELETE');
}
db.pragma('foreign_keys = ON');
db.pragma('busy_timeout = 5000');
db.pragma('synchronous = NORMAL');
// A checkpointed WAL is cut back to 64 MB instead of keeping its peak size.
db.pragma('journal_size_limit = 67108864');
// Up to 64 MB of page cache (negative means KiB).
db.pragma('cache_size = -65536');

runVersionedMigrations(db);
// Cheap on open: analyzes only tables whose statistics are missing or stale.
db.pragma('optimize = 0x10002');

module.exports = db;
