# NeoCloud clients — shared contract

Everything the native clients agree on with the server: the Flutter app on
Android and iOS (`flutter_app/`, which also serves the web app under `/app`)
and the Go lite client, which is the desktop client for Windows, macOS and
Linux (`clients/lite/`). The Flutter app has no desktop build. Routes not
listed here are in [api.md](api.md).

## 1. Device login (OAuth-style device code)

Clients without a browser session get a bearer token by having the user
approve them in the web app.

1. `POST /api/devices/authorize` (no auth, rate-limited)
   `{ name, platform }` → `200 { deviceCode, userCode, verificationUrl, expiresIn, interval }`
   - `userCode` looks like `ABCD-EFGH` (A–Z without I/O, 2–9), `deviceCode` is
     a long random secret. Codes live 10 minutes (`expiresIn: 600`), the
     client polls every `interval` seconds (3).
   - `verificationUrl` is `<public origin>/app/device?code=ABCD-EFGH`.
     The lite client opens or shows it only when it is an `http(s)` URL on
     the host of the server it signs in to; otherwise it builds
     `<server>/app/device?code=<userCode>` itself, so a server cannot make
     the desktop open another URI (`file:`, UNC paths, custom schemes).
2. The user opens the URL, signs in, sees the device name/platform and
   approves or denies:
   - `GET /api/account/devices/authorize/:userCode` (session) → `{ name, platform, createdAt, expiresAt }`
   - `POST /api/account/devices/authorize/:userCode` (session) `{ approve: true|false }` → `{ success: true }`
3. `POST /api/devices/token` (no auth, rate-limited) `{ deviceCode }`:
   - `428 { code: "AUTHORIZATION_PENDING" }` — keep polling
   - `403 { code: "ACCESS_DENIED" }` — user said no
   - `410 { code: "EXPIRED_TOKEN" }` — start over
   - `200 { token: "nc_…", device: { id, name, platform }, user: { id, username } }` — issued exactly once; the code is spent afterwards.

Clients send `Authorization: Bearer nc_…` from then on. `GET /api/devices/me`
(bearer) → `{ device: { id, name, platform, createdAt, lastSeenAt }, user: { id, username }, quota: { used, limit } }`
checks a stored token (`limit` -1 = unlimited). A revoked token gets `401`.

Unknown or already used device codes also answer `410 EXPIRED_TOKEN`. The
web app's `GET`/`POST /api/account/devices/authorize/:userCode` answer `404
NOT_FOUND` for an unknown, expired or already decided code.

## 2. Sync protocol

1. `GET /api/sync/tree` → `{ cursor, rootId, nodes: [{ id, parentId, type, name, size, etag, version, modifiedAt, path, … }] }`
   (`path` is relative to the root, `/`-separated, no leading slash).
2. Follow `GET /api/sync/changes?cursor=N&limit=1000` →
   `{ changes: [{ seq, op, nodeId, at, node | null }], cursor, hasMore }`.
   `op` ∈ create, update, rename, move, trash, restore, delete. `node` is the
   node *as it is now* (null when gone for good); a trashed node has
   `trashedAt`. Paths are not in changes: resolve them from the client's own
   id→(parentId, name) map.
3. Content: `GET /api/files/:id/content` (Range supported). Uploads: single
   `PUT /api/upload?parentId=&name=&conflict=fail|replace` below 8 MiB, the
   resumable `/api/uploads` flow above. `etag` = SHA-256 hex of the plaintext.
   Replacing uploads carry the etag the client last saw (`If-Match: "<etag>"`
   on single uploads, `{ baseEtag }` on `POST /api/uploads/:id/complete`);
   the server answers `412 ETAG_MISMATCH` when the file changed since.
4. Folders: `POST /api/folders { parentId, name }`; rename/move:
   `PATCH /api/nodes/:id { name?, parentId? }`; delete:
   `DELETE /api/nodes/:id` (goes to trash).
5. Live hint (optional): socket.io `files:changed` for session clients;
   bearer clients poll `changes` (every 15 s). A poll only starts a pass for
   a pair when an entry concerns it: the node or its parent (before or
   after the change) is the pair's root, a node the pair tracks, or inside
   the pair's remote folder. Entries elsewhere just move the cursor on.

### Client state and conflict rules (Go lite client)

- Local SQLite state per sync pair: `relative_path, node_id, etag, size,
  local_mtime, local_size, local_hash, state` + the pair's cursor, the remote
  folder id the rows describe, unfinished resumable uploads
  (`relative_path, upload_id, sha256, size`, resumed after a restart) and a
  few per-pair values (marker written, approved deletions, last report).
- Paths are compared in Unicode NFC (macOS writes NFD names). Files keep
  their spelling on disk; new local names are written in NFC.
- A file is *locally changed* when size or mtime differs from the stored
  row and its SHA-256 differs from the stored `etag`.
- Both changed since the last sync → keep both: the local file is renamed to
  `name (conflict <device> YYYY-MM-DD HHmm).ext` and uploaded, the remote
  one is downloaded under the original name.
- Uploads never overwrite blindly: a new file is sent with `conflict=fail`,
  a changed one with `conflict=replace` plus its base etag (two-way pairs;
  upload-only pairs replace without it). `412` or `409 NAME_EXISTS` means
  the server changed meanwhile and becomes a conflict (above), never a
  replace.
- Remote delete + local edit → re-upload the local file. Local delete +
  remote edit → download again.
- Renames and moves (same node id at a new remote path, or the same content
  at a new local path) are applied as renames on the other side, before
  anything else; they never count as deletes.
- Downloads go to `<name>.<first 16 hex of the etag>.ncpart` in the same
  folder: written, fsynced, hash-checked, renamed into place, then the
  folder is fsynced, and only then the state row is committed. A part left
  by a dropped connection is resumed with `Range: bytes=<size>-` (the etag
  in its name ties it to one version; a `206` must start at that offset and
  carry the same `ETag`, anything else starts over); the whole file is hashed
  again at the end. Server errors and hash mismatches discard the part.
- Right before replacing or deleting a local file the engine stats it again
  and skips it when it changed since the scan.
- Ignored by default: `.DS_Store`, `Thumbs.db`, `desktop.ini`, `~$*`,
  `*.tmp`, `*.ncpart`. Pairs add their own glob patterns. A `.neocloud`
  file or folder in any case (`.NeoCloud`, …) is always ignored on both
  sides; it is not a pattern and cannot be removed.
- Server paths are not trusted: a node whose path relative to the pair is
  absolute, has an empty, `.` or `..` segment, or contains `\` or NUL is
  reported as `invalidName` and never written or deleted locally. Every
  joined local path is checked to stay inside the pair's folder before it
  is touched, and stored rows with such paths are forgotten.
- Vault folders (`vaultId` set) are skipped by the sync engine.

### Data safety

- **Mass-delete guard.** Before a pass changes anything destructive, it
  counts the files it would delete on the server and locally (a folder
  counts the tracked files inside it). When either count is at least 20 and
  more than 50 % of the pair's tracked files or more than 500, or when the
  server folder is suddenly empty while files are tracked (`remoteEmpty`,
  any count), no deletion runs: the pair waits with
  `needsConfirmation { remoteDeletes, localDeletes, reason: massDelete|remoteEmpty, digest }`
  (`digest`: SHA-256 over the held deletions, side and path).
  Everything else in the pass (uploads, downloads, renames) still runs, and
  folders whose deletion waits are not recreated. The user approves with
  `neocloud-lite pair confirm-deletes <id>` or on the lite settings page.
  The approval is bound to its reason and digest: the next pass deletes
  only when its plan holds exactly the same deletions (any other plan asks
  again), and the first pass that evaluates the approval spends it, whether
  or not that pass has other errors.
- **Replaced remote folder.** When the pair's remote folder id changes (the
  folder was deleted and created again), the rows are dropped and both
  sides are merged like a first sync: nothing is deleted on either side.
- **Root marker.** The first sync writes `.neocloud/pair-id` (the pair id)
  into the local folder. If a pair that wrote it finds it missing or
  holding another id, the pair stops with "folder looks unmounted or
  replaced"; nothing is synced until the pair is removed and added again.
  A missing local folder stops a synced pair the same way.
- **Local trash.** Remote-driven local deletes and overwrites never unlink
  user data: the old file (or folder) moves to
  `<root>/.neocloud/trash/<YYYY-MM-DD>/<relative path>` (` 2`, ` 3`… when
  taken). Day folders older than 30 days are pruned at the start of a pass.
  A folder deleted on the server moves as a whole, ignored files included,
  when everything in it may go (unchanged tracked files, ignored files);
  one that only holds empty folders and OS junk (`.DS_Store`, `Thumbs.db`,
  `desktop.ini`) is removed, and those are the only files ever removed
  without a trash copy. A folder that still holds something to keep (a
  file changed since the scan, new local content) stays.
- **One instance per pair.** A pass holds an exclusive lock file in the
  state folder (`locks/<pair>.lock`); a second instance reports
  "Another NeoCloud instance is syncing this folder right now."

### Not synced

Paths a pass leaves alone on both sides (a tracked file that turns into one
is not deleted remotely). Each comes with a reason code and a text:

| reason | when |
| --- | --- |
| `symlink` | a symbolic link (never followed) |
| `specialFile` | a socket, pipe or device |
| `nameClash` | differs from another name only by case or Unicode normalization (`not synced: name clashes with X`); the tracked one, else the one on the other side, else the first in sort order keeps syncing |
| `invalidName` | a server path that would leave the pair's folder (absolute, an empty, `.` or `..` segment, `\` or NUL); on Windows also `CON`, `PRN`, `AUX`, `NUL`, `COM1–9`, `LPT1–9` (any extension), a trailing dot or space, `<>:"/\|?*` or control characters |
| `pathTooLong` | Windows only: the local path would exceed 259 characters |

The lite client lists them in `status --json` (`pairs[].report.notSynced`)
and on its settings page.

### Scheduling

The journal is polled every `poll_seconds` (15) and a pass runs only for
pairs it touches; fsnotify events (1.5 s settle) and the `rescan_seconds`
timer (5 min) cover local changes.

### Settings page (lite)

`neocloud-lite run` serves its settings page on `127.0.0.1` at a random
port, which every local user can reach, so every request needs a session.
The tray opens a one-time launch URL (`/launch?code=…`, valid for 2
minutes, `run --headless` prints one that stays valid until used); the code
works once and is exchanged for an `HttpOnly`, `SameSite=Strict` session
cookie, and no lasting secret ever appears on a command line. API calls
also need an `X-Requested-With` header and are refused from another site
(`Sec-Fetch-Site`) or another `Host`.

## 3. Share links

- `POST /api/shares { nodeId, mode?, expiresAt?, password?, maxDownloads?, maxUploadBytes?, note? }`
  → `{ id, token, url, nodeId, … }`. `mode` is `view`, `view_upload` or
  `upload_only` (`allowUpload: true` still means `view_upload`); passwords
  have at least 8 characters.
- `GET /api/shares` (own links; `?nodeId=` for one node's usable links, so a
  share dialog reuses a link instead of minting a new one),
  `PATCH /api/shares/:id` (edit in place, `null` clears), `DELETE /api/shares/:id`
- Public, no account: `/s/:token` (page), `GET /api/public/shares/:token`
  (meta + listing, `?folderId=` inside a shared folder),
  `POST /api/public/shares/:token/unlock { password }` (sets a short-lived
  cookie), `GET /api/public/shares/:token/files/:nodeId/content` (Range),
  `GET /api/public/shares/:token/zip`, and `PUT /api/public/shares/:token/upload?name=`
  in the upload modes. Vault folders cannot be shared.
- A share: `{ id, token, url, nodeId, nodeName, nodeType, mode, allowUpload,
  expiresAt, expired, hasPassword, note, maxDownloads, downloadCount,
  exhausted, maxUploadBytes, uploadedBytes, createdAt }`. `expiresAt` is sent
  as ISO and comes back as UTC `YYYY-MM-DD HH:MM:SS` like every timestamp.
- Meta: `{ share: { name, type, size, mode, allowUpload, expiresAt, hasPassword, sharedBy, host, note },
  node, path: [{ id, name }], items: [{ id, type, name, size, mime, modifiedAt }] }`.
  `zip` and `upload` also take `?folderId=` (a folder inside the share; default
  the shared node). Uploads never replace: a taken name becomes `name (1)`.
- Errors: unknown link or a node outside the share `404 NOT_FOUND`; revoked
  `410 SHARE_REVOKED`; expired `410 SHARE_EXPIRED`; download limit reached
  `410 SHARE_EXHAUSTED`; upload-only link asked for contents `403
  SHARE_UPLOAD_ONLY`; too many wrong passwords `429 SHARE_LOCKED` (with
  `retryAfter` seconds); password needed
  `401 SHARE_PASSWORD_REQUIRED`; wrong password `401 SHARE_PASSWORD_INVALID`;
  upload not allowed `403 UPLOAD_NOT_ALLOWED`; sharing a vault node
  `409 VAULT_NOT_SHAREABLE`. The unlock cookie (`nc_share_unlock`) lasts one
  hour and is scoped to the link's path.

## 4. Vault folders (end-to-end encrypted)

The server stores only ciphertext; no previews, thumbnails or search on
vault contents.

- `POST /api/vaults { parentId, name, salt, iterations, wrappedKey }` creates
  a folder with `vaultId` → `201 { id, folderId, salt, iterations, wrappedKey, createdAt, updatedAt, folder }`;
  `GET /api/vaults/:id` → `{ id, folderId, salt, iterations, wrappedKey, createdAt, updatedAt }`;
  `PUT /api/vaults/:id/key { wrappedKey, salt, iterations }` (passphrase change).
  Bad key material (salt ≠ 16 B, wrappedKey ≠ 60 B, iterations outside
  600 000–10 000 000) is `400 VAULT_PARAMS_INVALID`. Vaults created earlier
  with fewer iterations keep working; clients re-wrap the key with ≥ 600 000
  (`PUT …/key`) after the next unlock. The vault folder's own name is
  plaintext; vaults don't nest.
- Everything inside the folder inherits `vaultId` (every node has `vaultId`,
  null outside vaults). Moving or copying nodes in or out of a vault, or
  copying a vault folder, is refused (`409 VAULT_BOUNDARY`); the vault folder
  itself moves freely. Vault files get `404` for thumbnails and previews.

Crypto (all base64 is standard with padding unless noted):

- **Vault key**: 32 random bytes. **KEK** = PBKDF2-HMAC-SHA256(passphrase,
  salt 16 B, iterations ≥ 600 000, 32 B). `wrappedKey` = base64(nonce 12 B ‖
  AES-256-GCM(KEK, vaultKey) ‖ tag 16 B). A wrong passphrase fails the tag.
- **Names**: nameKey = HKDF-SHA256(vaultKey, salt empty, info `"ncv1-name"`,
  32 B). Stored name = base64url-no-padding(nonce 12 B ‖ ciphertext ‖ tag).
- **Content** (`NCV1`): header = ASCII `NCV1` ‖ 8 random bytes (`prefix`).
  fileKey = HKDF-SHA256(vaultKey, salt = prefix, info `"ncv1-file"`, 32 B).
  Plaintext split into 65 536-byte segments (the last may be shorter; an
  empty file is one empty segment). Segment *i*: nonce = prefix ‖ uint32
  big-endian *i*, AAD = one byte `0x01` for the last segment else `0x00`,
  output ciphertext ‖ tag. Size of ciphertext = 12 + n·16 + plaintext size.

## 5. QR pairing

Two directions, both ending in a normal device token (`nc_…`).

**Phone scans the web app** (mobile clients):
- Web, Settings › Devices › *Pair a phone*: `POST /api/account/devices/pairing`
  (session) `{ name?, platform? }` → `{ code, expiresAt, server, qrPayload }`.
  `code` is 32+ random base64url chars, single use, valid 5 minutes; only
  its hash is stored. `qrPayload` = `neocloud://pair?server=<public origin>&code=<code>`.
  `GET /api/account/devices/pairing/:code` (session) → `{ status: pending|used|expired, device? }`
  so the web page can show "Paired with Pixel 9 ✓".
- Phone: scans the QR, then `POST /api/devices/pair` (no auth, rate-limited)
  `{ code, name, platform }` → `200 { token, device, user }` like
  `/api/devices/token`; `410 { code: "EXPIRED_TOKEN" }` for used/expired/unknown.

**The lite client shows a QR** (a desktop with a screen but often no
camera): the device-code flow from §1, with `verificationUrl` rendered as a
QR code next to the user code, so a phone that is signed in to the web app
scans it and approves. The Go lite client shows it on its settings page and
as a terminal QR on `neocloud-lite login`.

## 6. WebDAV

- Endpoint `/dav/files/<username>/` plus the alias `/remote.php/dav/files/<username>/`
  and `/remote.php/webdav/` (Nextcloud-compatible paths for existing apps).
  `/.well-known/webdav` and `/.well-known/caldav`-style discovery redirect only for webdav.
- Auth: HTTP Basic, username + an app password (a device token created in
  Settings › Devices, platform `webdav`). Session cookies are not accepted.
  Failed Basic auth is rate-limited per IP and per username.
- Methods: OPTIONS, PROPFIND (Depth 0/1; `infinity` → 403), GET/HEAD (Range,
  ETag, Last-Modified), PUT (streamed, quota, disk guard, new version on
  overwrite, `X-OC-Mtime` honoured), MKCOL, DELETE (to trash), MOVE, COPY
  (`Overwrite`, `Destination`), PROPPATCH (`getlastmodified` accepted, other
  props stored as no-ops → 200 per prop), LOCK/UNLOCK (in-memory, per path,
  for Finder/Windows/Office; never block NeoCloud's own clients).
- Props: displayname, getcontentlength, getcontenttype, getetag, getlastmodified,
  creationdate, resourcetype, quota-used-bytes, quota-available-bytes,
  `oc:fileid`, `oc:favorite`, `oc:size`, `nc:has-preview`=false.
- Vault folders are hidden from WebDAV.

## 7. Imports (Nextcloud and other WebDAV servers)

- `POST /api/imports` (session) `{ source: "nextcloud"|"webdav", url, username, password, sourcePath?: "/", targetFolderId?, includeFavorites?: true }`
  → `201 job`. `url` is the server base (Nextcloud: `https://cloud.example.com`;
  the importer derives `/remote.php/dav/files/<username>/`). The password should
  be a Nextcloud app password; it is stored encrypted and wiped when the job ends.
- `POST /api/imports/test` same body → `{ ok, files, bytes, error? }` (a quick
  PROPFIND of the root + a size estimate, nothing written).
- `GET /api/imports` → `{ items: [job] }`; `GET /api/imports/:id`;
  `POST /api/imports/:id/cancel`; `POST /api/imports/:id/resume`; `DELETE /api/imports/:id` (only finished/cancelled).
- job: `{ id, source, url, username, sourcePath, targetFolderId, targetPath, status: queued|scanning|running|paused|done|failed|cancelled, totalFiles, totalBytes, doneFiles, doneBytes, skippedFiles, failedFiles, errors: [{ path, message }] (last 50), startedAt, finishedAt, createdAt }`.
- Behaviour: folder structure and mtimes preserved; quota checked up front and
  per file; a file already present with the same size (and same mtime or hash)
  is skipped, so resume and re-runs are idempotent; one bad file never stops
  the job; Nextcloud favorites become NeoCloud favorites; 2 parallel
  downloads; streams straight into the blob store (no full buffering).

## 8. Full-text search

- Admin setting `fullTextSearch` (off by default) in `/api/admin/config`;
  users can opt out in their settings (`fullTextSearch: true|false`, default true).
- Indexed: text/markdown/code/csv/json/xml/html (≤ 10 MB), PDF (via
  `pdftotext` when installed, otherwise the bundled extractor), docx/xlsx/pptx/odt/ods/odp
  (XML inside the zip). Never vault files, never trashed files. Extracted text
  is capped at 1 MB per file.
- Storage: an FTS5 table with `detail=column`, contentless (no plaintext copy
  in the index), plus the extracted text encrypted with the user's key for
  snippets. SECURITY.md documents that the index reveals which words a file
  contains to anyone holding the database.
- Indexing runs in the background (1 worker, low priority), driven by the
  change journal; `GET /api/admin/search-index` → `{ enabled, indexed, pending, failed, bytes, available: { pdftotext, office } }`,
  `POST /api/admin/search-index/rebuild`.
- `GET /api/search?q=…&content=1` (plus all existing filters) returns name
  matches and content matches merged: each item gets `match: "name"|"content"|"both"`
  and, for content, `snippet` with `\u0002`/`\u0003` around matched terms.
  Ranking: name matches first as today, then BM25.

## 9. Mobile app (Android and iOS)

- **Device binding.** The app keeps the server, the device token and the
  account id (`user.id` from `/api/devices/token` or `/pair`) in the
  Keychain / Keystore.
- **Camera upload** is bound to `<server>#<account id>`: its settings and
  its ledger (which photos are done) belong to that account on that server
  and are ignored for any other. Disconnecting, or a revoked token, turns
  it off and cancels the background task. After signing in again, or to
  another server or account, the app asks once whether to set it up again;
  with another account it starts with no folder and an empty ledger.
- **Share to NeoCloud (Android).** `MainActivity` handles `ACTION_SEND` and
  `ACTION_SEND_MULTIPLE` itself (`ShareIntake.kt`, channels
  `systems.neolabs.neocloud/share` and `…/share/events`). Only `content://`
  streams from other apps are taken; `file://` and anything else is
  refused. Each stream is copied to
  `cacheDir/share-intake/<random uuid>/<name>`, where the name is the
  sender's display name reduced to its last segment, without NUL, control
  characters or path separators, at most 255 bytes (`.`, `..` or empty
  become `shared-file`), and the resolved path must stay in that folder. A
  share over 2 GB is refused as a whole. The Dart side accepts only regular
  files at exactly `<intake>/<folder>/<name>`, asks for a target folder per
  share, uploads exactly the files that prompt named (a share arriving
  meanwhile gets its own prompt) and deletes the copies once their upload
  is done, skipped or cancelled, or the prompt was cancelled; leftovers are
  cleared on the next start.
- **Share to NeoCloud (iOS)** needs a share extension, which is not
  implemented yet; the iOS app receives no shared files.
