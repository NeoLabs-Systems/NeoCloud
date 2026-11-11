# NeoCloud — Engineering Guidelines

NeoCloud is a Node.js/Express backend with a Flutter client (web under `/app`,
Android and iOS) and a Go lite client, which is the desktop client (Windows,
macOS, Linux). Self-hosted, multi-user, encrypted file storage. Conventions follow NeoAgent; where the two
differ, NeoAgent is the reference unless this file says otherwise.

## Directory Map

```
server/
  app.js            Express app factory (used by index.js and tests)
  index.js          Startup: env, secrets, HTTP + socket.io, maintenance
  config/           Origins, HTTP flags, storage policy, sign-up policy
  db/database.js    The one SQLite handle
  http/             Middleware, route registry, socket, static, errors
  middleware/       Auth guards (session, device token, admin)
  routes/           HTTP only, one file per domain
  services/
    account/        Credentials, sessions, 2FA, security keys
    access/         Admin flag, audit log
    admin/          Admin use cases
    crypto/         Key hierarchy and segmented AES-GCM
    storage/        Blob files, blob references, quotas
    files/          Tree reads, operations, trash, versions, vaults, change journal
    transfer/       Uploads (single + resumable) and downloads (range, zip)
    preview/        Thumbnails and Office→PDF renditions, cached encrypted
    backup/         Targets, adapters (local, sftp, s3, webdav), runner, scheduler
    sync/           Snapshots for sync clients
    devices/        Bearer tokens and device-code sign-in for native clients
    shares/         Share links (owner side) and public access to them
    members/        Sharing with other accounts: grants, the access resolver, member file/transfer ops
    settings/       Per-account preferences
    import/         Imports from Nextcloud/WebDAV: read-only client, jobs, runner
  utils/            Pure helpers
runtime/            Paths and env parsing
lib/                CLI (manager.js), schema migrations, service setup
bin/neocloud.js     CLI entry
landing/            Landing page at / (NeoLabs styles.css + NeoCloud visuals)
test/               unit, integration, security (node:test + supertest)
docs/api.md         Endpoint reference — keep it in step with the routes
```

## Core Principles

- **No duplication.** Search `server/utils/`, `runtime/` and the service folder before adding a helper.
- **No premature abstraction.** Abstract on the fourth copy, not the second.
- **No speculative features.** Build what the task needs; no flags or shims for callers that don't exist.
- **No dead code.** No unused exports, no commented-out blocks.
- **Readable over clever.** No nested ternaries or dense one-liners.

## Node.js / Server

- CommonJS only; every file starts with `'use strict';`.
- **Routes** parse the request, call a service, return the result (usually via `jsonHandler`). No business logic.
- **Services** own the logic. Split a file past ~300 lines.
- **Utils** are pure: no database, no service imports.

### Database

- All access goes through `server/db/database.js`. One file, one instance.
- Synchronous better-sqlite3 API, parameterized queries only. The only interpolated SQL fragments are whitelisted column names (see `tree.listChildren`).
- Schema changes: append to `VERSIONED_MIGRATIONS` in `lib/schema_migrations.js`. Never edit a shipped migration.

### Files, blobs and quotas — invariants

- **Every node lookup is scoped to the account** (`getLiveNode(userId, id)`). Another account's id must behave like a missing one: `404`, never `403`.
- **Nothing is written to disk in plaintext.** Uploads, upload parts, blobs and previews all go through `writeTemporaryBlob` (segmented AES-GCM under the account's key). The one exception is the Office→PDF conversion, which LibreOffice can only do from a file: it uses a private 0700 temp folder removed in `finally`, and `NEOCLOUD_OFFICE_PREVIEW=false` turns it off.
- **Backups leave the server encrypted.** Blobs are copied as they are; the database snapshot is encrypted with the backup key. A new adapter implements the interface in `services/backup/adapters/index.js` and nothing else.
- **A blob file is moved into place before its row is committed**, and deleted again if the transaction fails. Blob files whose last reference goes away are deleted *after* the transaction commits.
- **`used_bytes` changes only in `blob_refs.js`** (`recordBlob`, `releaseBlobs`). Copies share blobs; versions and trash count.
- **Every tree mutation appends to `changes`** in the same transaction and calls `announceChanges` after commit. Sync clients depend on it.
- `NEOCLOUD_MASTER_KEY` is generated once and never rotated or replaced by code.

### Errors

- Throw `httpError(status, message, CODE)` for anything a client should handle; codes are part of the API.
- Validate at boundaries (request bodies, query strings, env values), not between internal functions.
- Non-admins never see technical error text (`redactErrorsForNonAdmins`).

### Security

- Never log secrets, tokens, file names of other accounts, or file contents.
- Origin checks go through `server/config/origins.js` only.
- Device tokens reach `files`, `transfers`, `storage`, `sync`, `shares` and `vaults` routes only (`requireAuthOrDevice`); account, settings and admin require a session.
- File routes resolve node ids through `services/members/access.js` (`resolveAccess`), so shared nodes are reached only through a live grant; keep owner-only features (versions, vaults, links, sync, WebDAV) on the account-scoped services.
- Public share routes check every node id against the shared subtree (`services/shares/public.js`); never serve a share node through another path.
- Import passwords (another server's credentials) are stored only with `encryptValue` and set to NULL when the job ends; imports are session-only.
- Downloads always carry the sandbox CSP; active types are always attachments.

### Paths, env, logging

- Paths come from `runtime/paths.js`. Never hardcode `~/.neocloud`.
- Server-wide settings live in `.env`; the admin API writes them with `persistEnv`.
- Use `createServiceLogger('Name')`; plain `console.log` only during startup.

## Flutter / Dart

The client copies NeoRouter's design 1:1 — theme, palette, spacing, login/
register, app shell and sidebar, settings and admin scaffolds — and only
changes the product name, the logo and the pages that are NeoCloud's own.
Gold marks primary actions and the selection only; status is a dot plus a
word, never a colored fill.

- The Flutter app targets web, Android and iOS only. Desktop belongs to `clients/lite`; don't add Flutter desktop platforms or desktop-only plugins.
- `main_controller.dart` is the single root `ChangeNotifier`; no other state packages.
- `lib/main_<feature>.dart` is thin UI; logic lives in `lib/src/<feature>/`.
- Platform code uses `_io` / `_web` / `_stub` conditional exports.
- Strings come from ARB files (`de`, `en`); colors from the palette, never hardcoded.
- Never run `flutter build web` from an agent session; web builds are done by hand.

## Naming

| Context | Convention |
| --- | --- |
| JS files | `snake_case` |
| JS variables / functions | `camelCase` |
| Dart files / classes / members | `snake_case` / `UpperCamelCase` / `lowerCamelCase` |
| Env vars | `SCREAMING_SNAKE_CASE`, prefixed `NEOCLOUD_` |
| HTTP paths | `kebab-case` |
| SQL tables / columns | `snake_case` |

## Tests

`npm test` runs unit, integration and security suites. Each test file gets its
own temporary `NEOCLOUD_HOME` through `test/helpers/app.js` — a test must never
touch `~/.neocloud`. Add a security test for every new endpoint that takes a
node id.

## Hard Rules

- No second SQLite database, no SQL string building from input.
- No plaintext file contents on disk (the documented Office conversion aside).
- Do not commit `.env`, `data/`, or anything in `.gitignore`.
- Do not bypass git hooks with `--no-verify`.
- Do not create `CHANGELOG.md`; GitHub Releases are the history.
