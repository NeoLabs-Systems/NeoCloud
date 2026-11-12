'use strict';

// The whole database schema. Each entry in VERSIONED_MIGRATIONS runs once, in
// order, inside a transaction; PRAGMA user_version records how far an install
// got. Append new entries -- never edit or reorder shipped ones.

const VERSIONED_MIGRATIONS = [
  {
    version: 1,
    name: 'initial schema',
    up(db) {
      db.exec(`
        CREATE TABLE users (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          username TEXT UNIQUE NOT NULL,
          display_name TEXT,
          email TEXT UNIQUE,
          password TEXT NOT NULL,
          is_admin INTEGER NOT NULL DEFAULT 0,
          -- NULL: the server default quota. -1: unlimited. Otherwise bytes.
          quota_bytes INTEGER,
          -- Bytes of every blob the account owns (current files, old versions, trash).
          used_bytes INTEGER NOT NULL DEFAULT 0,
          -- The account's data key, wrapped with the master key.
          wrapped_dek TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          last_login TEXT
        );
        CREATE UNIQUE INDEX users_username_nocase ON users(lower(username));
        CREATE UNIQUE INDEX users_email_nocase ON users(lower(email)) WHERE email IS NOT NULL;

        -- The first account on an install administers it.
        CREATE TRIGGER users_first_account_is_admin
        AFTER INSERT ON users
        WHEN (SELECT COUNT(*) FROM users) = 1
        BEGIN
          UPDATE users SET is_admin = 1 WHERE id = NEW.id;
        END;

        CREATE TABLE sessions (sid PRIMARY KEY, sess, expire);

        CREATE TABLE user_sessions (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          session_hash TEXT NOT NULL UNIQUE,
          ip_address TEXT,
          user_agent TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          last_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT,
          revoked_at TEXT
        );
        CREATE INDEX user_sessions_user ON user_sessions(user_id);

        CREATE TABLE user_two_factor (
          user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
          secret TEXT,
          pending_secret TEXT,
          enabled INTEGER NOT NULL DEFAULT 0,
          enabled_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE TABLE user_recovery_codes (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          code_hash TEXT NOT NULL,
          used_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE TABLE user_settings (
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          key TEXT NOT NULL,
          value TEXT NOT NULL,
          updated_at TEXT NOT NULL DEFAULT (datetime('now')),
          PRIMARY KEY (user_id, key)
        );

        CREATE TABLE signup_keys (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          code TEXT NOT NULL UNIQUE,
          label TEXT NOT NULL DEFAULT '',
          created_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          used_at TEXT,
          used_by_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
          revoked_at TEXT
        );

        CREATE TABLE access_audit_log (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          action TEXT NOT NULL,
          actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
          subject_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
          detail_json TEXT NOT NULL DEFAULT '{}',
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        -- Encrypted file contents. Several nodes may share one blob (copies,
        -- versions); ref_count says how many, and the blob goes at zero.
        CREATE TABLE blobs (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          size INTEGER NOT NULL,
          sha256 TEXT NOT NULL,
          enc_version INTEGER NOT NULL,
          ref_count INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX blobs_user ON blobs(user_id);

        -- Files and folders. Every account has one root folder (parent_id NULL,
        -- name ''); everything else hangs below it. A trashed node keeps its
        -- place; its descendants are trashed with it implicitly.
        CREATE TABLE nodes (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          parent_id TEXT REFERENCES nodes(id) ON DELETE CASCADE,
          type TEXT NOT NULL CHECK (type IN ('folder', 'file')),
          name TEXT NOT NULL,
          size INTEGER NOT NULL DEFAULT 0,
          mime TEXT,
          blob_id TEXT REFERENCES blobs(id),
          etag TEXT NOT NULL,
          version INTEGER NOT NULL DEFAULT 1,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          modified_at TEXT NOT NULL DEFAULT (datetime('now')),
          trashed_at TEXT,
          -- Reserved for end-to-end encrypted vault folders.
          vault_id TEXT
        );
        CREATE UNIQUE INDEX nodes_root ON nodes(user_id) WHERE parent_id IS NULL;
        CREATE UNIQUE INDEX nodes_unique_name
          ON nodes(parent_id, lower(name)) WHERE trashed_at IS NULL AND parent_id IS NOT NULL;
        CREATE INDEX nodes_user_parent ON nodes(user_id, parent_id);
        CREATE INDEX nodes_user_trashed ON nodes(user_id, trashed_at) WHERE trashed_at IS NOT NULL;
        CREATE INDEX nodes_user_modified ON nodes(user_id, modified_at);

        CREATE TABLE file_versions (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          blob_id TEXT NOT NULL REFERENCES blobs(id),
          version INTEGER NOT NULL,
          size INTEGER NOT NULL,
          etag TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          UNIQUE (node_id, version)
        );

        -- Resumable uploads. Parts are encrypted on disk like any blob.
        CREATE TABLE upload_sessions (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          parent_id TEXT NOT NULL,
          name TEXT NOT NULL,
          conflict TEXT NOT NULL,
          total_size INTEGER NOT NULL,
          chunk_size INTEGER NOT NULL,
          sha256 TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL
        );

        CREATE TABLE upload_parts (
          upload_id TEXT NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
          part_index INTEGER NOT NULL,
          size INTEGER NOT NULL,
          PRIMARY KEY (upload_id, part_index)
        );

        -- Append-only journal of tree changes; sync clients read it from a cursor.
        CREATE TABLE changes (
          seq INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL,
          op TEXT NOT NULL,
          at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX changes_user_seq ON changes(user_id, seq);

        -- Desktop, mobile and lite clients authenticate with a bearer token.
        CREATE TABLE device_tokens (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          name TEXT NOT NULL,
          platform TEXT NOT NULL DEFAULT '',
          token_hash TEXT NOT NULL UNIQUE,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          last_seen_at TEXT,
          revoked_at TEXT
        );
        CREATE INDEX device_tokens_user ON device_tokens(user_id);
      `);
    },
  },
  {
    version: 2,
    name: 'previews and backups',
    up(db) {
      db.exec(`
        -- Generated renditions of a blob (thumbnails, PDF of an Office file),
        -- stored encrypted next to the blobs. Content-addressed by blob, so a
        -- new version of a file gets new previews and old ones go with the blob.
        CREATE TABLE previews (
          blob_id TEXT NOT NULL REFERENCES blobs(id) ON DELETE CASCADE,
          variant TEXT NOT NULL,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          size INTEGER NOT NULL,
          mime TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          PRIMARY KEY (blob_id, variant)
        );

        CREATE TABLE backup_targets (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          name TEXT NOT NULL,
          type TEXT NOT NULL CHECK (type IN ('local', 'sftp', 's3', 'webdav')),
          -- JSON, encrypted with the master key (holds passwords and keys).
          config_enc TEXT NOT NULL,
          schedule TEXT NOT NULL DEFAULT 'manual' CHECK (schedule IN ('manual', 'daily', 'weekly')),
          hour INTEGER NOT NULL DEFAULT 3,
          weekday INTEGER NOT NULL DEFAULT 0,
          retention INTEGER NOT NULL DEFAULT 7,
          enabled INTEGER NOT NULL DEFAULT 1,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        CREATE TABLE backup_runs (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          target_id INTEGER NOT NULL REFERENCES backup_targets(id) ON DELETE CASCADE,
          started_at TEXT NOT NULL DEFAULT (datetime('now')),
          finished_at TEXT,
          status TEXT NOT NULL CHECK (status IN ('running', 'ok', 'failed')),
          snapshot TEXT,
          bytes_uploaded INTEGER NOT NULL DEFAULT 0,
          blobs_uploaded INTEGER NOT NULL DEFAULT 0,
          error TEXT
        );
        CREATE INDEX backup_runs_target ON backup_runs(target_id, id);
      `);
    },
  },
  {
    version: 3,
    name: 'device login, share links and vaults',
    up(db) {
      db.exec(`
        -- Device-code sign-in: a client asks for a code pair, the user approves
        -- the short user_code in the web app, the client swaps its secret
        -- device code (only the SHA-256 is kept) for a bearer token once.
        CREATE TABLE device_authorizations (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          device_code_hash TEXT NOT NULL UNIQUE,
          user_code TEXT NOT NULL UNIQUE,
          name TEXT NOT NULL,
          platform TEXT NOT NULL DEFAULT '',
          status TEXT NOT NULL DEFAULT 'pending'
            CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
          user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
          expires_at TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        -- Public links to a file or folder. The token is the link; the owner
        -- can list their links again, so it is kept as is.
        CREATE TABLE shares (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          token TEXT NOT NULL UNIQUE,
          password_hash TEXT,
          allow_upload INTEGER NOT NULL DEFAULT 0,
          expires_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          revoked_at TEXT,
          download_count INTEGER NOT NULL DEFAULT 0
        );
        CREATE INDEX shares_user ON shares(user_id);

        -- End-to-end encrypted folders. The server keeps the vault key only
        -- wrapped with a key derived from the user's passphrase.
        CREATE TABLE vaults (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          folder_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE CASCADE,
          salt TEXT NOT NULL,
          iterations INTEGER NOT NULL,
          wrapped_key TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX vaults_user ON vaults(user_id);
      `);
    },
  },
  {
    version: 4,
    name: 'favorites, opened files, share modes and unlock throttling',
    up(db) {
      db.exec(`
        CREATE TABLE favorites (
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          PRIMARY KEY (user_id, node_id)
        );
        CREATE INDEX favorites_node ON favorites(node_id);

        -- When an account last opened a file, for Quick Access.
        CREATE TABLE node_opens (
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          last_opened_at TEXT NOT NULL DEFAULT (datetime('now')),
          open_count INTEGER NOT NULL DEFAULT 1,
          PRIMARY KEY (user_id, node_id)
        );
        CREATE INDEX node_opens_recent ON node_opens(user_id, last_opened_at);
        CREATE INDEX node_opens_node ON node_opens(node_id);

        -- What a link lets visitors do. allow_upload stays in step with mode
        -- (1 for view_upload and upload_only).
        ALTER TABLE shares ADD COLUMN mode TEXT NOT NULL DEFAULT 'view'
          CHECK (mode IN ('view', 'view_upload', 'upload_only'));
        UPDATE shares SET mode = 'view_upload' WHERE allow_upload = 1;
        ALTER TABLE shares ADD COLUMN max_downloads INTEGER;
        ALTER TABLE shares ADD COLUMN note TEXT;
        ALTER TABLE shares ADD COLUMN max_upload_bytes INTEGER;
        ALTER TABLE shares ADD COLUMN uploaded_bytes INTEGER NOT NULL DEFAULT 0;
        CREATE INDEX shares_node ON shares(node_id);

        -- Wrong passwords per link (milliseconds since the epoch), for the
        -- per-link unlock throttle.
        CREATE TABLE share_unlock_failures (
          share_id INTEGER NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
          failed_at_ms INTEGER NOT NULL
        );
        CREATE INDEX share_unlock_failures_share ON share_unlock_failures(share_id, failed_at_ms);
      `);
    },
  },
  {
    version: 5,
    name: 'QR pairing codes',
    up(db) {
      db.exec(`
        -- Single-use codes a signed-in web page shows as a QR for a phone to
        -- redeem (docs/clients.md section 5). Only the code's hash is kept.
        CREATE TABLE device_pairings (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          code_hash TEXT NOT NULL UNIQUE,
          -- Optional name/platform suggested by the web page.
          name TEXT,
          platform TEXT NOT NULL DEFAULT '',
          status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'used')),
          device_id INTEGER REFERENCES device_tokens(id) ON DELETE SET NULL,
          expires_at TEXT NOT NULL,
          used_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX device_pairings_user ON device_pairings(user_id, status, expires_at);
      `);
    },
  },
  {
    version: 6,
    name: 'imports from Nextcloud and WebDAV',
    up(db) {
      db.exec(`
        -- One import from another server (docs/clients.md section 7).
        CREATE TABLE import_jobs (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          source TEXT NOT NULL CHECK (source IN ('nextcloud', 'webdav')),
          url TEXT NOT NULL,
          username TEXT NOT NULL,
          -- encryptValue() of the app password; NULL once the job has ended.
          password_enc TEXT,
          source_path TEXT NOT NULL DEFAULT '/',
          target_folder_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          include_favorites INTEGER NOT NULL DEFAULT 1,
          status TEXT NOT NULL DEFAULT 'queued'
            CHECK (status IN ('queued', 'scanning', 'running', 'paused', 'done', 'failed', 'cancelled')),
          -- 1 once the remote tree has been listed completely.
          scanned INTEGER NOT NULL DEFAULT 0,
          started_at TEXT,
          finished_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX import_jobs_user ON import_jobs(user_id, created_at);
        CREATE INDEX import_jobs_status ON import_jobs(status);

        -- Every remote file and folder of a job, by path below the source folder.
        CREATE TABLE import_items (
          job_id TEXT NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
          path TEXT NOT NULL,
          type TEXT NOT NULL CHECK (type IN ('file', 'folder')),
          size INTEGER NOT NULL DEFAULT 0,
          -- Remote modification time, UTC "YYYY-MM-DD HH:MM:SS"; NULL if unknown.
          mtime TEXT,
          favorite INTEGER NOT NULL DEFAULT 0,
          state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'done', 'skipped', 'failed')),
          PRIMARY KEY (job_id, path)
        );
        CREATE INDEX import_items_state ON import_items(job_id, state);

        CREATE TABLE import_errors (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          job_id TEXT NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
          path TEXT NOT NULL,
          message TEXT NOT NULL
        );
        CREATE INDEX import_errors_job ON import_errors(job_id, id);
      `);
    },
  },
  {
    version: 7,
    name: 'full-text search index',
    up(db) {
      db.exec(`
        -- One row per indexed file. id is the file's rowid in search_fts.
        -- The extracted text is kept only here, deflated and encrypted with
        -- the account's data key, for result snippets.
        CREATE TABLE search_docs (
          id INTEGER PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL UNIQUE REFERENCES nodes(id) ON DELETE CASCADE,
          -- The content that was indexed; a new version makes the row stale.
          blob_id TEXT NOT NULL,
          status TEXT NOT NULL CHECK (status IN ('indexed', 'failed')),
          text_enc BLOB,
          text_bytes INTEGER NOT NULL DEFAULT 0,
          indexed_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX search_docs_user ON search_docs(user_id, status);

        -- Contentless: the index knows which words each file contains, never
        -- the text itself. detail=column keeps it small (no word positions).
        CREATE VIRTUAL TABLE search_fts USING fts5(
          body,
          content = '',
          contentless_delete = 1,
          detail = column,
          prefix = '2 3',
          tokenize = 'unicode61 remove_diacritics 2'
        );

        -- A file deleted for good (or its account) takes its index entry along.
        CREATE TRIGGER search_docs_unindex AFTER DELETE ON search_docs BEGIN
          DELETE FROM search_fts WHERE rowid = OLD.id;
        END;

        -- The indexer's position in the change journal.
        CREATE TABLE search_index_state (
          key TEXT PRIMARY KEY,
          value TEXT NOT NULL
        );
      `);
    },
  },
  {
    version: 8,
    name: 'sign-in failure accounting and node counts',
    up(db) {
      db.exec(`
        -- Failed sign-ins per account, independent of the client address.
        -- account_key is "user:<id>", or "name:<what was typed>" for a name
        -- no account has, so both are throttled alike. challenge_id ties a
        -- second-factor failure to its pending sign-in. Rows older than the
        -- window are pruned on every write.
        CREATE TABLE login_failures (
          account_key TEXT NOT NULL,
          kind TEXT NOT NULL CHECK (kind IN ('password', 'second_factor')),
          challenge_id TEXT,
          failed_at_ms INTEGER NOT NULL
        );
        CREATE INDEX login_failures_account ON login_failures(account_key, failed_at_ms);
        CREATE INDEX login_failures_challenge ON login_failures(challenge_id) WHERE challenge_id IS NOT NULL;
        CREATE INDEX login_failures_age ON login_failures(failed_at_ms);

        -- Nodes per account (trash included), kept by triggers so the
        -- NEOCLOUD_MAX_NODES check is one row read, not a count.
        ALTER TABLE users ADD COLUMN node_count INTEGER NOT NULL DEFAULT 0;
        UPDATE users SET node_count = (SELECT COUNT(*) FROM nodes WHERE nodes.user_id = users.id);
        CREATE TRIGGER nodes_count_insert AFTER INSERT ON nodes BEGIN
          UPDATE users SET node_count = node_count + 1 WHERE id = NEW.user_id;
        END;
        CREATE TRIGGER nodes_count_delete AFTER DELETE ON nodes BEGIN
          UPDATE users SET node_count = node_count - 1 WHERE id = OLD.user_id;
        END;
      `);
    },
  },  {
    version: 9,
    name: 'security keys replace sign-up keys',
    up(db) {
      db.exec(`
        -- Registration is now only on or off (NEOCLOUD_ALLOW_SIGNUP).
        DROP TABLE IF EXISTS signup_keys;

        -- WebAuthn credentials (security keys and passkeys). credential_id
        -- and public_key are base64url; rp_id is the host the key was made
        -- on, since a key only works there.
        CREATE TABLE user_webauthn_credentials (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          credential_id TEXT NOT NULL UNIQUE,
          public_key TEXT NOT NULL,
          counter INTEGER NOT NULL DEFAULT 0,
          rp_id TEXT NOT NULL,
          transports_json TEXT NOT NULL DEFAULT '[]',
          device_type TEXT,
          backed_up INTEGER NOT NULL DEFAULT 0,
          user_verified INTEGER NOT NULL DEFAULT 0,
          label TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          last_used_at TEXT
        );
        CREATE INDEX user_webauthn_credentials_user ON user_webauthn_credentials(user_id, rp_id, created_at DESC);
      `);
    },
  },
  {
    version: 10,
    name: 'sharing with other accounts',
    up(db) {
      db.exec(`
        -- A file or folder shared with another account on this server.
        -- recipient is what the owner typed (email or username), shown back
        -- to the owner only. No expiry unless the owner sets one.
        CREATE TABLE user_shares (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          grantee_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          permission TEXT NOT NULL CHECK (permission IN ('read', 'write')),
          recipient TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT,
          UNIQUE (node_id, grantee_id)
        );
        CREATE INDEX user_shares_grantee ON user_shares(grantee_id);
        CREATE INDEX user_shares_owner ON user_shares(owner_id);

        -- A member's resumable upload into a shared folder belongs to the
        -- owner (their quota, their key); created_by is the member who may
        -- continue it.
        ALTER TABLE upload_sessions ADD COLUMN created_by INTEGER REFERENCES users(id) ON DELETE CASCADE;
      `);
    },
  },
];

function latestSchemaVersion() {
  return Math.max(...VERSIONED_MIGRATIONS.map((migration) => migration.version));
}

// A database written by a newer NeoCloud has tables this code doesn't know;
// running on it would quietly lose data. Stop with a way out instead.
function assertSchemaNotNewer(current) {
  const latest = latestSchemaVersion();
  if (current > latest) {
    const error = new Error(
      `The database has schema version ${current}, but this NeoCloud only knows up to ${latest}: `
      + 'it was last opened by a newer version. Update NeoCloud again, or run `neocloud rollback` '
      + 'to restore the database snapshot taken before the update.',
    );
    error.code = 'SCHEMA_TOO_NEW';
    throw error;
  }
}

function runVersionedMigrations(db) {
  const current = db.pragma('user_version', { simple: true });
  assertSchemaNotNewer(current);
  for (const migration of VERSIONED_MIGRATIONS) {
    if (migration.version <= current) continue;
    db.transaction(() => {
      migration.up(db);
      db.pragma(`user_version = ${Number(migration.version)}`);
    })();
  }
}

module.exports = { VERSIONED_MIGRATIONS, assertSchemaNotNewer, latestSchemaVersion, runVersionedMigrations };
