// Migrations mirror the history of the Node.js server, so its databases open
// unchanged: add new ones at the end and never edit released ones.
package schema

import (
	"context"
	"database/sql"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

const Version = 37

// Steps returns the SQL-only migrations in the same order as the Node app.
func Steps() []dbx.Step {
	return []dbx.Step{
		// 1: initial schema
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 2: previews and backups
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 3: device login, share links and vaults
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 4: favorites, opened files, share modes and unlock throttling
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 5: QR pairing codes
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 6: imports from Nextcloud and WebDAV
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 7: full-text search index
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 8: sign-in failure accounting and node counts
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 9: security keys replace sign-up keys
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 10: sharing with other accounts
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
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
      `); err != nil {
				return err
			}
			return nil
		},
		// 11: QR sign-in challenges
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- A signed-out browser shows a QR code; a signed-in phone scans and
        -- approves it, and the browser swaps its poll token for a session.
        -- Only SHA-256 hashes of the poll token and the approve secret are
        -- stored. status: pending, scanned, approved, claimed, declined or
        -- expired.
        CREATE TABLE user_qr_login_challenges (
          id TEXT PRIMARY KEY,
          poll_token_hash TEXT NOT NULL UNIQUE,
          approve_secret_hash TEXT NOT NULL,
          status TEXT NOT NULL DEFAULT 'pending',
          request_user_agent TEXT,
          request_ip_address TEXT,
          request_metadata_json TEXT NOT NULL DEFAULT '{}',
          scanned_metadata_json TEXT NOT NULL DEFAULT '{}',
          approved_by_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
          approved_session_hash TEXT,
          approved_device_id INTEGER REFERENCES device_tokens(id) ON DELETE SET NULL,
          approved_metadata_json TEXT NOT NULL DEFAULT '{}',
          expires_at TEXT NOT NULL,
          scanned_at TEXT,
          approved_at TEXT,
          claimed_at TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX user_qr_login_challenges_status ON user_qr_login_challenges(status, expires_at);
      `); err != nil {
				return err
			}
			return nil
		},
		// 12: disabled accounts
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- Set by an admin: the account can't sign in by any route, its
        -- sessions and device tokens stop working and its public links stop
        -- serving. NULL is an active account.
        ALTER TABLE users ADD COLUMN disabled_at TEXT;
      `); err != nil {
				return err
			}
			return nil
		},
		// 13: AI providers, usage and exclusions
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- Models an admin granted this account beyond the server-wide list
        -- (JSON array of provider::model ids), and its own monthly AI token
        -- budget (NULL follows NEOCLOUD_AI_MONTHLY_TOKENS, 0 is no limit).
        ALTER TABLE users ADD COLUMN allowed_models TEXT;
        ALTER TABLE users ADD COLUMN ai_monthly_tokens INTEGER;

        -- Set by the owner on a folder: nothing in it (subfolders included)
        -- is ever sent to an AI provider.
        ALTER TABLE nodes ADD COLUMN ai_excluded INTEGER NOT NULL DEFAULT 0;

        -- One row per AI call: metadata only, never a prompt, a file name or
        -- an answer.
        CREATE TABLE ai_usage (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          feature TEXT NOT NULL,
          provider TEXT NOT NULL,
          model TEXT NOT NULL,
          input_tokens INTEGER NOT NULL DEFAULT 0,
          output_tokens INTEGER NOT NULL DEFAULT 0,
          total_tokens INTEGER NOT NULL DEFAULT 0,
          latency_ms INTEGER NOT NULL DEFAULT 0,
          ok INTEGER NOT NULL DEFAULT 1,
          error_code TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX ai_usage_user_created ON ai_usage(user_id, created_at);
        CREATE INDEX ai_usage_created ON ai_usage(created_at);
      `); err != nil {
				return err
			}
			return nil
		},
		// 14: AI suggestions, jobs, rules and file metadata
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- Work for the AI worker: one row per file and request. kinds is a
        -- JSON array of names|folders|tags. explicit: the person asked for
        -- this file, so a name that already reads well is renamed too.
        -- status: pending, running, done, skipped, failed. Only an error
        -- code is kept, never content.
        CREATE TABLE ai_jobs (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          kinds TEXT NOT NULL,
          source TEXT NOT NULL CHECK (source IN ('upload', 'request', 'backfill')),
          explicit INTEGER NOT NULL DEFAULT 0,
          batch_id TEXT,
          status TEXT NOT NULL DEFAULT 'pending',
          attempts INTEGER NOT NULL DEFAULT 0,
          run_after TEXT NOT NULL DEFAULT (datetime('now')),
          error_code TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX ai_jobs_queue ON ai_jobs(status, run_after);
        CREATE INDEX ai_jobs_user ON ai_jobs(user_id, status);
        CREATE INDEX ai_jobs_batch ON ai_jobs(batch_id) WHERE batch_id IS NOT NULL;

        -- The worker's place in the change journal.
        CREATE TABLE ai_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);

        -- What the extraction call found in a file (for the content with
        -- this etag): fields with confidences, a caption for photos, and the
        -- tags the owner accepted. search_text is the lowercased words search
        -- matches (tags, issuer, subject, type, caption), like a file name.
        CREATE TABLE ai_node_meta (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          etag TEXT NOT NULL,
          fields_json TEXT NOT NULL DEFAULT '{}',
          tags_json TEXT NOT NULL DEFAULT '[]',
          search_text TEXT NOT NULL DEFAULT '',
          model TEXT,
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX ai_node_meta_user ON ai_node_meta(user_id);

        -- On-demand summaries, encrypted under the account's key like the
        -- search text, valid for the content with this etag.
        CREATE TABLE ai_summaries (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          etag TEXT NOT NULL,
          summary_enc BLOB NOT NULL,
          model TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );

        -- The review queue. from_json/to_json: a name (rename), a folder
        -- { id, path } (move) or a tag list (tags). basis_*: the node when
        -- the suggestion was made (stale once it changes); applied_json: the
        -- node right after applying, so undo can tell whether it changed
        -- since. signal_json: { type, issuer } for rule proposals.
        CREATE TABLE ai_suggestions (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          kind TEXT NOT NULL CHECK (kind IN ('rename', 'move', 'tags')),
          from_json TEXT NOT NULL,
          to_json TEXT NOT NULL,
          reason TEXT NOT NULL DEFAULT '',
          confidence REAL NOT NULL,
          tier TEXT NOT NULL CHECK (tier IN ('high', 'medium', 'low')),
          status TEXT NOT NULL DEFAULT 'pending'
            CHECK (status IN ('pending', 'accepted', 'rejected', 'applied', 'undone', 'stale')),
          source TEXT NOT NULL DEFAULT 'ai' CHECK (source IN ('ai', 'rule')),
          rule_id INTEGER,
          model TEXT,
          signal_json TEXT NOT NULL DEFAULT '{}',
          basis_name TEXT NOT NULL,
          basis_parent_id TEXT,
          basis_etag TEXT NOT NULL,
          applied_json TEXT,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          decided_at TEXT
        );
        CREATE INDEX ai_suggestions_user ON ai_suggestions(user_id, status, kind);
        CREATE INDEX ai_suggestions_node ON ai_suggestions(node_id);

        -- The owner's sorting rules: every set condition must match
        -- (type, issuer, extension: case-insensitive equality; name_pattern:
        -- a glob with * and ?). The target goes with its folder.
        CREATE TABLE ai_rules (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          name TEXT NOT NULL,
          match_type TEXT,
          match_issuer TEXT,
          match_extension TEXT,
          match_name_pattern TEXT,
          target_folder_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          enabled INTEGER NOT NULL DEFAULT 1,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX ai_rules_user ON ai_rules(user_id);

        -- Accepted moves per (type, issuer, folder): three make a rule
        -- proposal. dismissed: the owner said no to that proposal.
        CREATE TABLE ai_move_stats (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          doc_type TEXT NOT NULL,
          issuer TEXT NOT NULL,
          target_folder_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          accepts INTEGER NOT NULL DEFAULT 0,
          dismissed INTEGER NOT NULL DEFAULT 0,
          UNIQUE (user_id, doc_type, issuer, target_folder_id)
        );

        -- Folder profiles for sorting (name, path, sample child names),
        -- embedded once per profile text: signature is its hash.
        CREATE TABLE ai_folder_profiles (
          folder_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          signature TEXT NOT NULL,
          model TEXT NOT NULL,
          vector BLOB NOT NULL,
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX ai_folder_profiles_user ON ai_folder_profiles(user_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 15: backup run heartbeat
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- Set every few seconds while a run is going. The server and the CLI
        -- are separate processes: a run whose heartbeat stopped is dead, one
        -- with a fresh heartbeat holds the target however long it takes.
        ALTER TABLE backup_runs ADD COLUMN heartbeat_at TEXT;
      `); err != nil {
				return err
			}
			return nil
		},
		// 16: photos timeline
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- One row per live photo or video of an account (no vault, no
        -- trash), for the Photos timeline. taken_at is the wall-clock time
        -- it was taken, "YYYY-MM-DDTHH:MM:SS": EXIF, the video's own date,
        -- the file name, or the modification time, in that order. probed:
        -- 0 the date is a guess from the name or modification time, 1 the
        -- file was read, 2 reading it failed (the guess stays).
        CREATE TABLE photo_items (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          blob_id TEXT NOT NULL,
          kind TEXT NOT NULL CHECK (kind IN ('photo', 'video')),
          taken_at TEXT NOT NULL,
          width INTEGER,
          height INTEGER,
          duration_seconds REAL,
          probed INTEGER NOT NULL DEFAULT 0
        );
        CREATE INDEX photo_items_timeline ON photo_items(user_id, taken_at DESC, node_id DESC);
        CREATE INDEX photo_items_unprobed ON photo_items(user_id) WHERE probed = 0;

        -- The indexer's place in the change journal.
        CREATE TABLE photo_index_state (
          key TEXT PRIMARY KEY,
          value TEXT NOT NULL
        );
      `); err != nil {
				return err
			}
			return nil
		},
		// 17: backup run totals
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- What a run has to upload, set once it knows: the admin page shows
        -- "13 GB of 21 GB" instead of an endless "Backing up".
        ALTER TABLE backup_runs ADD COLUMN blobs_total INTEGER;
        ALTER TABLE backup_runs ADD COLUMN bytes_total INTEGER;
      `); err != nil {
				return err
			}
			return nil
		},
		// 18: duplicate photos
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- A 64-bit perceptual hash (dHash, 16 hex digits) of an image, valid
        -- for the content with this etag. dhash NULL: the image could not be
        -- read. Filled on demand by the Duplicates page.
        CREATE TABLE image_hashes (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          etag TEXT NOT NULL,
          dhash TEXT
        );
        CREATE INDEX image_hashes_user ON image_hashes(user_id);

        -- Groups the owner said are not duplicates: hidden while every file
        -- of a group is among node_ids_json.
        CREATE TABLE duplicate_dismissals (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_ids_json TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX duplicate_dismissals_user ON duplicate_dismissals(user_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 19: ai created folders and new-folder votes
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- 1 for a folder made by accepting an AI move into a new folder, so
        -- empty ones can be offered for removal later.
        ALTER TABLE nodes ADD COLUMN created_by_ai INTEGER NOT NULL DEFAULT 0;

        -- A file the model would put into a folder that doesn't exist yet.
        -- Held here, not shown, until enough files (3) of one job would go
        -- into the same new folder; then they become suggestions together.
        -- batch_key is the job's batch id, or "upload" for uploads.
        -- folder_key is the folder name normalized (case, accents, plural,
        -- synonyms and translations), draft_json the move it would be.
        CREATE TABLE ai_new_folder_votes (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          batch_key TEXT NOT NULL,
          parent_id TEXT NOT NULL,
          folder_key TEXT NOT NULL,
          folder_name TEXT NOT NULL,
          draft_json TEXT NOT NULL,
          basis_etag TEXT NOT NULL,
          -- 1 once the file's move became a suggestion.
          released INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          UNIQUE (user_id, node_id, batch_key)
        );
        CREATE INDEX ai_new_folder_votes_key ON ai_new_folder_votes(user_id, batch_key, parent_id, folder_key);
      `); err != nil {
				return err
			}
			return nil
		},
		// 20: photo date sources
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- The dates a client gave for a file's content when uploading it
        -- ("YYYY-MM-DDTHH:MM:SS", wall-clock): when the photo was taken or
        -- the file created, and when it was last changed on the device.
        -- Valid for the content with this blob_id only.
        CREATE TABLE file_client_dates (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          blob_id TEXT NOT NULL,
          created_at TEXT,
          modified_at TEXT
        );
        ALTER TABLE upload_sessions ADD COLUMN client_created_at TEXT;
        ALTER TABLE upload_sessions ADD COLUMN client_modified_at TEXT;

        -- Where taken_at came from: exif (or the video's own date), client
        -- (the device's capture or creation date), filename, mtime (the
        -- device's modification date) or upload (when it reached the
        -- server: the date is unknown). NULL on rows from before this
        -- version until the indexer works it out.
        ALTER TABLE photo_items ADD COLUMN taken_at_source TEXT;
        -- 2 the file wasn't read yet (the date may still move), 1 dated,
        -- 0 date unknown. The timeline shows them in that order.
        ALTER TABLE photo_items ADD COLUMN date_state INTEGER NOT NULL DEFAULT 1;
        UPDATE photo_items SET date_state = 2 WHERE probed = 0;
        DROP INDEX photo_items_timeline;
        CREATE INDEX photo_items_timeline ON photo_items(user_id, date_state DESC, taken_at DESC, node_id DESC);
        CREATE INDEX photo_items_unsourced ON photo_items(node_id) WHERE taken_at_source IS NULL;
      `); err != nil {
				return err
			}
			return nil
		},
		// 21: e-signature documents
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- A signing request: one frozen PDF (snapshot_blob_id keeps a
        -- reference on the blob, so editing or deleting the source file
        -- changes nothing), its parties and fields. Times are ISO strings.
        -- status: draft | sent | in_progress | finalizing | completed |
        -- declined | expired | canceled.
        CREATE TABLE sign_documents (
          id TEXT PRIMARY KEY,
          owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          source_node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
          source_name TEXT NOT NULL,
          source_parent_id TEXT,
          title TEXT NOT NULL,
          status TEXT NOT NULL DEFAULT 'draft',
          signing_order TEXT NOT NULL DEFAULT 'preserved',
          locale TEXT NOT NULL DEFAULT 'en',
          expires_at TEXT,
          snapshot_blob_id TEXT NOT NULL REFERENCES blobs(id),
          snapshot_sha256 TEXT NOT NULL,
          snapshot_size INTEGER NOT NULL,
          page_count INTEGER NOT NULL,
          pages_json TEXT NOT NULL,
          final_node_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
          final_sha256 TEXT,
          consent_text_version INTEGER,
          -- Finalization retries: how often it failed, the last failure
          -- (short, no secrets) and when the worker tries again.
          finalize_attempts INTEGER NOT NULL DEFAULT 0,
          finalize_error TEXT,
          finalize_next_at TEXT,
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL,
          sent_at TEXT,
          completed_at TEXT,
          canceled_at TEXT
        );
        CREATE INDEX sign_documents_owner ON sign_documents(owner_id, updated_at DESC, id DESC);
        CREATE INDEX sign_documents_expiry ON sign_documents(expires_at)
          WHERE status IN ('sent', 'in_progress') AND expires_at IS NOT NULL;
        CREATE INDEX sign_documents_finalizing ON sign_documents(finalize_next_at) WHERE status = 'finalizing';

        -- One signer role with its own link. The link's token is never
        -- stored: token_hash is its SHA-256 (NULL before sending and after a
        -- revoke), token_hint the last characters, for the owner's screen.
        -- status: pending | waiting | opened | completed | declined.
        CREATE TABLE sign_parties (
          id TEXT PRIMARY KEY,
          document_id TEXT NOT NULL REFERENCES sign_documents(id) ON DELETE CASCADE,
          position INTEGER NOT NULL,
          name TEXT NOT NULL,
          color TEXT NOT NULL,
          email TEXT,
          token_hash TEXT UNIQUE,
          token_hint TEXT,
          status TEXT NOT NULL DEFAULT 'pending',
          opened_at TEXT,
          consented_at TEXT,
          completed_at TEXT,
          declined_at TEXT,
          decline_reason TEXT,
          signer_name TEXT,
          -- The signer's own time zone (IANA name and UTC offset in
          -- minutes), as their browser reported it when they consented.
          timezone TEXT,
          tz_offset_minutes INTEGER,
          ip TEXT,
          user_agent TEXT,
          created_at TEXT NOT NULL
        );
        CREATE INDEX sign_parties_document ON sign_parties(document_id, position);

        -- A box on the page. areas_json: [{page, x, y, w, h}], page counted
        -- from 0, the rest fractions (0..1) of the page as displayed, origin
        -- top left. default_value and prefs_json are JSON.
        CREATE TABLE sign_fields (
          id TEXT PRIMARY KEY,
          document_id TEXT NOT NULL REFERENCES sign_documents(id) ON DELETE CASCADE,
          party_id TEXT NOT NULL REFERENCES sign_parties(id) ON DELETE CASCADE,
          type TEXT NOT NULL,
          name TEXT NOT NULL DEFAULT '',
          required INTEGER NOT NULL DEFAULT 1,
          readonly INTEGER NOT NULL DEFAULT 0,
          default_value TEXT,
          prefs_json TEXT NOT NULL DEFAULT '{}',
          areas_json TEXT NOT NULL,
          position INTEGER NOT NULL
        );
        CREATE INDEX sign_fields_document ON sign_fields(document_id, position);
        CREATE INDEX sign_fields_party ON sign_fields(party_id);

        -- What a party entered. Text-like values live in value_json; images
        -- and attachments are encrypted blobs of the owner (blob_ref), with
        -- their description in value_json.
        CREATE TABLE sign_values (
          id TEXT PRIMARY KEY,
          document_id TEXT NOT NULL REFERENCES sign_documents(id) ON DELETE CASCADE,
          field_id TEXT NOT NULL UNIQUE REFERENCES sign_fields(id) ON DELETE CASCADE,
          party_id TEXT NOT NULL REFERENCES sign_parties(id) ON DELETE CASCADE,
          value_json TEXT,
          blob_ref TEXT REFERENCES blobs(id),
          updated_at TEXT NOT NULL
        );
        CREATE INDEX sign_values_document ON sign_values(document_id);

        -- The audit trail. hash = sha256(prev_hash + canonical JSON of the
        -- rest of the row), so changing or removing an entry breaks every
        -- hash after it.
        CREATE TABLE sign_events (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          document_id TEXT NOT NULL REFERENCES sign_documents(id) ON DELETE CASCADE,
          party_id TEXT,
          type TEXT NOT NULL,
          at TEXT NOT NULL,
          ip TEXT,
          user_agent TEXT,
          data_json TEXT NOT NULL DEFAULT '{}',
          prev_hash TEXT NOT NULL,
          hash TEXT NOT NULL
        );
        CREATE INDEX sign_events_document ON sign_events(document_id, id);

        -- The server's own certificate that seals finished PDFs (PAdES):
        -- one row, generated when the first document is finalized. The PKCS#12
        -- bundle and its passphrase are encrypted with the master key.
        CREATE TABLE sign_seal_keys (
          id INTEGER PRIMARY KEY CHECK (id = 1),
          p12_enc TEXT NOT NULL,
          passphrase_enc TEXT NOT NULL,
          created_at TEXT NOT NULL
        );
      `); err != nil {
				return err
			}
			return nil
		},
		// 22: signed file lookup
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE INDEX sign_documents_final ON sign_documents(final_node_id) WHERE final_node_id IS NOT NULL;
      `); err != nil {
				return err
			}
			return nil
		},
		// 23: signed file without certificate pages
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        ALTER TABLE sign_documents ADD COLUMN clean_blob_id TEXT REFERENCES blobs(id);
        ALTER TABLE sign_documents ADD COLUMN clean_sha256 TEXT;
        ALTER TABLE sign_documents ADD COLUMN clean_size INTEGER;
      `); err != nil {
				return err
			}
			return nil
		},
		// 24: service email: verified addresses and single-use tokens
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        ALTER TABLE users ADD COLUMN email_verified_at TEXT;
        UPDATE users
        SET email_verified_at = COALESCE(created_at, datetime('now'))
        WHERE email IS NOT NULL AND trim(email) != '';

        CREATE TABLE user_email_tokens (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          type TEXT NOT NULL CHECK (type IN ('signup_confirmation', 'email_change_confirmation', 'password_reset')),
          email TEXT NOT NULL,
          token_hash TEXT NOT NULL UNIQUE,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL,
          consumed_at TEXT
        );
        CREATE INDEX user_email_tokens_user ON user_email_tokens(user_id, type, consumed_at);
      `); err != nil {
				return err
			}
			return nil
		},
		// 25: signing mail, grants and signing profiles
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        -- Mail of a signing request. message / reminder_days come from the
        -- send dialog; deliver_email is 0 when the owner chose link-only
        -- (or no mail was configured), so nobody is mailed behind their back.
        ALTER TABLE sign_documents ADD COLUMN message TEXT;
        ALTER TABLE sign_documents ADD COLUMN reminder_days INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE sign_documents ADD COLUMN deliver_email INTEGER NOT NULL DEFAULT 0;

        -- token_enc: the link's token encrypted with the server's secret
        -- key, kept ONLY for parties with an email, so later mails
        -- (turn, reminder, completion) can link to the same page. The hash
        -- stays the verification path. user_id: the account that claimed the
        -- party (a signer with an account). is_self: the owner signs this one.
        ALTER TABLE sign_parties ADD COLUMN token_enc TEXT;
        ALTER TABLE sign_parties ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE SET NULL;
        ALTER TABLE sign_parties ADD COLUMN is_self INTEGER NOT NULL DEFAULT 0;
        CREATE INDEX sign_parties_user ON sign_parties(user_id) WHERE user_id IS NOT NULL;
        CREATE INDEX sign_parties_email ON sign_parties(lower(email)) WHERE email IS NOT NULL;

        -- Mail waiting to go out or already gone. payload_json holds the
        -- parameters of the template (a message, a name), never a link or a
        -- token: the mail is rendered when it is sent, from the party's
        -- current link. status: queued | sending | sent | failed | canceled.
        CREATE TABLE sign_mail_outbox (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          document_id TEXT NOT NULL REFERENCES sign_documents(id) ON DELETE CASCADE,
          party_id TEXT REFERENCES sign_parties(id) ON DELETE CASCADE,
          kind TEXT NOT NULL,
          to_email TEXT NOT NULL,
          payload_json TEXT NOT NULL DEFAULT '{}',
          status TEXT NOT NULL DEFAULT 'queued',
          attempts INTEGER NOT NULL DEFAULT 0,
          next_attempt_at TEXT NOT NULL,
          last_error TEXT,
          created_at TEXT NOT NULL,
          sent_at TEXT
        );
        CREATE INDEX sign_mail_outbox_due ON sign_mail_outbox(status, next_attempt_at);
        CREATE INDEX sign_mail_outbox_party ON sign_mail_outbox(document_id, party_id, kind);

        -- Short-lived stand-ins for a party's link, handed to a signed-in
        -- account (the inbox, "sign yourself"). Only the SHA-256 is stored.
        CREATE TABLE sign_party_grants (
          id TEXT PRIMARY KEY,
          party_id TEXT NOT NULL REFERENCES sign_parties(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          token_hash TEXT NOT NULL UNIQUE,
          expires_at TEXT NOT NULL,
          created_at TEXT NOT NULL,
          first_used_at TEXT
        );
        CREATE INDEX sign_party_grants_party ON sign_party_grants(party_id);
        CREATE INDEX sign_party_grants_user ON sign_party_grants(user_id);

        -- An account's signing profile: the personal details and presets are
        -- one encrypted blob (data_enc); the switches stay readable columns.
        CREATE TABLE sign_profiles (
          user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
          data_enc TEXT,
          auto_fill INTEGER NOT NULL DEFAULT 1,
          auto_save INTEGER NOT NULL DEFAULT 1,
          save_folder_id TEXT REFERENCES nodes(id) ON DELETE SET NULL,
          notify_each_signature INTEGER NOT NULL DEFAULT 0,
          updated_at TEXT NOT NULL
        );

        -- Saved signatures and initials: PNGs in the account's own encrypted
        -- blob storage (blob_id).
        CREATE TABLE sign_profile_images (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          kind TEXT NOT NULL CHECK (kind IN ('signature', 'initials')),
          method TEXT NOT NULL CHECK (method IN ('draw', 'type', 'upload')),
          label TEXT NOT NULL DEFAULT '',
          typed_text TEXT,
          is_default INTEGER NOT NULL DEFAULT 0,
          blob_id TEXT NOT NULL REFERENCES blobs(id),
          size INTEGER NOT NULL,
          created_at TEXT NOT NULL
        );
        CREATE INDEX sign_profile_images_user ON sign_profile_images(user_id, kind);
      `); err != nil {
				return err
			}
			return nil
		},
		// 26: connected apps (NeoAgent companion OAuth)
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE oauth_clients (
          id TEXT PRIMARY KEY,
          kind TEXT NOT NULL,
          name TEXT NOT NULL,
          redirect_uri TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          revoked_at TEXT
        );
        CREATE INDEX oauth_clients_redirect ON oauth_clients(kind, redirect_uri);

        CREATE TABLE oauth_codes (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          code_hash TEXT NOT NULL UNIQUE,
          redirect_uri TEXT NOT NULL,
          scopes_json TEXT NOT NULL,
          code_challenge TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL,
          consumed_at TEXT
        );

        CREATE TABLE oauth_access_tokens (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          token_hash TEXT NOT NULL UNIQUE,
          scopes_json TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL,
          last_used_at TEXT,
          revoked_at TEXT
        );
        CREATE INDEX oauth_access_tokens_user ON oauth_access_tokens(user_id, client_id);

        CREATE TABLE oauth_refresh_tokens (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          access_token_id INTEGER REFERENCES oauth_access_tokens(id) ON DELETE SET NULL,
          client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          token_hash TEXT NOT NULL UNIQUE,
          scopes_json TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL,
          revoked_at TEXT
        );
        CREATE INDEX oauth_refresh_tokens_user ON oauth_refresh_tokens(user_id, client_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 27: signing mail ledger (abuse limits survive deleting a document)
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE sign_mail_ledger (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          recipient_hash TEXT NOT NULL,
          kind TEXT NOT NULL,
          mail_id INTEGER,
          at TEXT NOT NULL
        );
        CREATE INDEX sign_mail_ledger_user ON sign_mail_ledger(user_id, at);
        CREATE INDEX sign_mail_ledger_recipient ON sign_mail_ledger(recipient_hash, at);
        CREATE INDEX sign_mail_ledger_mail ON sign_mail_ledger(mail_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 28: activity feed and comments on files
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE activity (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          actor_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          kind TEXT NOT NULL,
          node_id TEXT NOT NULL,
          parent_id TEXT,
          node_type TEXT NOT NULL,
          name TEXT NOT NULL,
          detail TEXT,
          count INTEGER NOT NULL DEFAULT 1,
          at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX activity_owner ON activity(owner_id, id);
        CREATE INDEX activity_node ON activity(node_id, id);
        CREATE INDEX activity_parent ON activity(parent_id, id);
        CREATE INDEX activity_at ON activity(at);

        CREATE TABLE node_comments (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          body TEXT NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          edited_at TEXT
        );
        CREATE INDEX node_comments_node ON node_comments(node_id, id);
        CREATE INDEX node_comments_user ON node_comments(user_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 29: remote wipe marker on device tokens
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        ALTER TABLE device_tokens ADD COLUMN wipe_requested_at TEXT;
        ALTER TABLE device_tokens ADD COLUMN wipe_erase_files INTEGER NOT NULL DEFAULT 0;
        ALTER TABLE device_tokens ADD COLUMN wipe_delivered_at TEXT;
      `); err != nil {
				return err
			}
			return nil
		},
		// 30: external storage mounts (per-user cloud storage)
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE external_mounts (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          name TEXT NOT NULL,
          provider TEXT NOT NULL,
          config_enc TEXT NOT NULL,
          read_only INTEGER NOT NULL DEFAULT 0,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE UNIQUE INDEX external_mounts_name ON external_mounts(user_id, name COLLATE NOCASE);
      `); err != nil {
				return err
			}
			return nil
		},
		// 31: who saved a file’s content (document editor)
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        ALTER TABLE nodes ADD COLUMN modified_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
        ALTER TABLE file_versions ADD COLUMN modified_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
      `); err != nil {
				return err
			}
			return nil
		},
		// 32: send to device (handoff offers and their staged files)
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE handoff_offers (
          id TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          sender_key TEXT NOT NULL,
          sender_name TEXT NOT NULL,
          sender_platform TEXT NOT NULL DEFAULT '',
          target_key TEXT NOT NULL,
          target_name TEXT NOT NULL,
          status TEXT NOT NULL DEFAULT 'uploading',
          total_size INTEGER NOT NULL,
          created_at TEXT NOT NULL DEFAULT (datetime('now')),
          updated_at TEXT NOT NULL DEFAULT (datetime('now')),
          expires_at TEXT NOT NULL
        );
        CREATE INDEX handoff_offers_user ON handoff_offers(user_id, status);
        CREATE INDEX handoff_offers_expiry ON handoff_offers(expires_at);
        CREATE TABLE handoff_files (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          offer_id TEXT NOT NULL REFERENCES handoff_offers(id) ON DELETE CASCADE,
          position INTEGER NOT NULL,
          name TEXT NOT NULL,
          mime TEXT NOT NULL DEFAULT 'application/octet-stream',
          size INTEGER NOT NULL,
          blob_id TEXT,
          source_node_id TEXT,
          fetched_at TEXT
        );
        CREATE INDEX handoff_files_offer ON handoff_files(offer_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 33: WOPI lock state shared across application instances
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE wopi_locks (
          node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
          lock_value TEXT NOT NULL,
          expires_at INTEGER NOT NULL
        );
        CREATE INDEX wopi_locks_expiry ON wopi_locks(expires_at);
      `); err != nil {
				return err
			}
			return nil
		},
		// 34: sync journal records affected parent folders
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE changes ADD COLUMN parent_ids_json TEXT NOT NULL DEFAULT '[]';`); err != nil {
				return err
			}
			return nil
		},
		// 35: persist one-use security key sign-in challenges
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE security_key_login_challenges (
          challenge_id TEXT PRIMARY KEY,
          stage TEXT NOT NULL CHECK(stage IN ('assertion', 'second_factor')),
          pending_json TEXT,
          user_id INTEGER,
          device_name TEXT NOT NULL DEFAULT '',
          platform TEXT NOT NULL DEFAULT '',
          expires_at INTEGER NOT NULL,
          failures INTEGER NOT NULL DEFAULT 0
        );
        CREATE INDEX security_key_login_challenges_expiry ON security_key_login_challenges(expires_at);
      `); err != nil {
				return err
			}
			return nil
		},
		// 36: persist damaged encrypted blob scrub status
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE maintenance_state (
          key TEXT PRIMARY KEY,
          value TEXT NOT NULL
        );
        CREATE TABLE blob_scrub_damaged (
          blob_id TEXT PRIMARY KEY REFERENCES blobs(id) ON DELETE CASCADE,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          problem TEXT NOT NULL,
          checked_at TEXT NOT NULL DEFAULT (datetime('now'))
        );
        CREATE INDEX blob_scrub_damaged_user ON blob_scrub_damaged(user_id);
      `); err != nil {
				return err
			}
			return nil
		},
		// 37: persist WebDAV locks across Go server instances
		func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
        CREATE TABLE webdav_locks (
          token TEXT PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
          key TEXT NOT NULL,
          href TEXT NOT NULL,
          depth TEXT NOT NULL CHECK (depth IN ('0', 'infinity')),
          scope TEXT NOT NULL CHECK (scope IN ('exclusive', 'shared')),
          owner TEXT NOT NULL DEFAULT '',
          timeout_ms INTEGER NOT NULL CHECK (timeout_ms > 0),
          expires_at INTEGER NOT NULL
        );
        CREATE INDEX webdav_locks_user_expiry ON webdav_locks(user_id, expires_at);
      `); err != nil {
				return err
			}
			return nil
		},
	}
}
