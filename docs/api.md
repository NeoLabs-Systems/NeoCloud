# NeoCloud API

All endpoints live under `/api` and speak JSON unless noted. Errors look like
`{ "error": "Readable message", "code": "MACHINE_CODE" }`; branch on `code`.

## Authentication

| Client | How |
| --- | --- |
| Web app | Session cookie `neocloud.sid` from `/api/auth/login` or `/api/auth/register`. |
| Mobile app, lite (desktop) client | `Authorization: Bearer nc_…` from the device login (below) or `POST /api/account/devices`. Reaches files, transfers, storage, sync, shares, members and vaults only. |
| Share link visitors | No account: the link token in the path, plus an unlock cookie for password links. |

Browsers on another origin must be listed in `ALLOWED_ORIGINS`; state-changing
requests from any other origin get `403 ORIGIN_NOT_ALLOWED`.

Node ids are UUIDs. `root` stands for the account's root folder wherever a
folder id is expected. A node of another account always answers `404`,
unless that account shared it (or a folder above it) with you: see
[Sharing with accounts](#sharing-with-accounts--session-or-bearer).

## Health — `GET /api/health` (no account)

`{ status: "ok" | "degraded", db: "ok" | "error", storage: "ok" | "error" }`,
`503` when not ok. Runs `SELECT 1` and checks that the data and storage
directories are writable; nothing else, so it is safe to poll (Docker's
HEALTHCHECK, load balancers, `neocloud start`). `GET /api/version` →
`{ name, version, node }`.

While the server shuts down (up to 30 s), new uploads get
`503 SERVER_STOPPING` with `Retry-After: 30`; requests already running finish.

## Auth — `/api/auth`

| Method | Path | Body | Notes |
| --- | --- | --- | --- |
| GET | `/status` | | `hasUser`, `registrationOpen`, `authenticated`, `user` |
| POST | `/register` | `{ username, email, password }` | First account becomes admin. `403 SIGNUP_DISABLED` while sign-up is off (`NEOCLOUD_ALLOW_SIGNUP=false`); the first account can always register |
| POST | `/login` | `{ username, password }` | `username` also takes the email. May answer `{ requiresTwoFactor: true }` |
| POST | `/login/2fa` | `{ code }` | TOTP or recovery code, within 10 minutes and at most 5 codes per password sign-in (then `401 TWO_FACTOR_ATTEMPTS_EXCEEDED` or `TWO_FACTOR_EXPIRED`: sign in with the password again) |
| POST | `/webauthn/login/options` | `{ username? }` | → `{ options }` for `navigator.credentials.get`. Without a username the browser offers any discoverable key (passkey). `409 NO_SECURITY_KEYS` while no key is registered on this host |
| POST | `/webauthn/login/verify` | `{ response }` | The assertion as JSON (base64url fields). Answers like `/login`, `{ requiresTwoFactor: true }` included: a key that verified the user (PIN or biometric) stands in for the second factor, a presence-only key does not |
| POST | `/logout` | | Ends the session and closes its socket.io connections |
| GET | `/me` | | |

Besides 20 tries per 15 minutes per address, `/login` and `/login/2fa` are
throttled per account, whatever address a try comes from (a name no account
has is throttled the same way). From the 5th failed password or code in 15
minutes each next try waits twice as long (2 s, 4 s, …); the 10th locks
sign-in until the oldest of those failures is 15 minutes old. A throttled try,
right or wrong, answers `429 LOGIN_LOCKED` with `Retry-After` and
`retryAfter` (seconds) in the body. A right password clears its own
failures; wrong second-factor codes count until a full sign-in succeeds.
A security key sign-in counts like a password try for the key's account.

Security keys: the relying party is the host of `PUBLIC_URL` when set, else
the origin the browser is on (this server or one in `ALLOWED_ORIGINS`); a key
only works on the host it was registered on. Each ceremony's challenge lives
in the session for 5 minutes and is spent by the first verify, right or
wrong (`400 SECURITY_KEY_CHALLENGE_EXPIRED`). Other codes:
`401 SECURITY_KEY_UNKNOWN`, `SECURITY_KEY_REJECTED` (bad signature, wrong
origin, or a signature counter that went backwards), `409 SECURITY_KEY_EXISTS`.
Both routes allow 40 requests per 15 minutes per address.

Passwords need at least 8 characters and a strength score of 3 of 4, and may
not contain the username or email, common words, repeated characters or
obvious sequences: `400 PASSWORD_TOO_SHORT` / `PASSWORD_WEAK` with the reason.

The `user` payload includes `storage`: `usedBytes`, `quotaBytes` (-1 = unlimited), `trashBytes`, `versionBytes`.

## Account — `/api/account` (session only)

| Method | Path | Body |
| --- | --- | --- |
| GET | `/` | |
| GET | `/usage` | |
| PUT | `/display-name` | `{ displayName }` |
| PUT | `/password` | `{ currentPassword, newPassword }` — signs out every other session (and closes their sockets), revokes every device token and WebDAV app password, and voids approved device logins and open pairing codes not yet redeemed. This session stays under a new id (a new `neocloud.sid` cookie) |
| POST | `/2fa/setup` | `{ currentPassword, code? }` → `{ otpauthUrl, manualKey, status }`. With 2FA already on, `code` (current TOTP or a recovery code) is required: `401 TWO_FACTOR_REQUIRED` / `INVALID_CODE` |
| POST | `/2fa/enable` | `{ code, currentCode? }` — `code` from the new secret; with 2FA already on, `currentCode` from the factor being replaced is required as well → `{ recoveryCodes, status }` |
| POST | `/2fa/disable` · `/2fa/recovery-codes` | `{ currentPassword, code }` |
| GET / DELETE | `/sessions` · `/sessions/:id` | Revoking a session also closes its socket.io connections |
| GET | `/security-keys` | → `{ securityKeys: [{ id, label, createdAt, lastUsedAt, transports, backedUp, rpId }] }`; `GET /` carries the same list |
| POST | `/security-keys/register/options` | → `{ options }` for `navigator.credentials.create`. At most 20 keys (`400 SECURITY_KEY_LIMIT`) |
| POST | `/security-keys/register/verify` | `{ response, label? }` → `{ success, credentials }`; `label` up to 48 characters, default "Security key N" |
| PUT / DELETE | `/security-keys/:id` | `{ label }` to rename → `{ success, credentials }`; another account's key is `404` |
| GET / POST / DELETE | `/devices` · `/devices/:id` | `{ name, platform }`; the token is returned once |
| GET | `/devices/authorize/:userCode` | → `{ name, platform, createdAt, expiresAt }` of a pending device login |
| POST | `/devices/authorize/:userCode` | `{ approve: true\|false }` → `{ success: true }` |

The user code is accepted in any case, with or without the dash. Unknown,
expired or already decided codes answer `404 NOT_FOUND`; both routes allow 30
requests per 15 minutes per address.

QR pairing ([clients.md](clients.md#5-qr-pairing)), the web side:

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| POST | `/devices/pairing` (30 / 15 min) | `{ name?, platform? }` suggested for the phone | `{ code, expiresAt, server, qrPayload }` |
| GET | `/devices/pairing/:code` | | `{ status: "pending" }` · `{ status: "expired" }` · `{ status: "used", device: { id, name, platform } }` |

`code` is 43 base64url characters, single use and valid 5 minutes; only its
SHA-256 is stored. `qrPayload` is `neocloud://pair?server=<origin>&code=<code>`.
An account has at most 5 open (pending, unexpired) codes; one more is `429
TOO_MANY_PAIRING_CODES`. Another account's or an unknown code is `404
NOT_FOUND`. When a phone redeems a code, the account's web sessions get the
socket.io event `devices:paired` `{ device: { id, name, platform } }`.

## Device login — `/api/devices`

The OAuth-style device code flow from [clients.md](clients.md#1-device-login-oauth-style-device-code).

| Method | Path | Auth | Body | Answer |
| --- | --- | --- | --- | --- |
| POST | `/authorize` | none (30 / 15 min) | `{ name, platform }` | `{ deviceCode, userCode, verificationUrl, expiresIn: 600, interval: 3 }` |
| POST | `/token` | none (600 / 15 min) | `{ deviceCode }` | `428 AUTHORIZATION_PENDING` · `403 ACCESS_DENIED` · `410 EXPIRED_TOKEN` (also unknown or already used codes) · `200 { token, device: { id, name, platform }, user: { id, username } }` once |
| POST | `/pair` | none (30 / 15 min) | `{ code, name, platform }` (`name`/`platform` fall back to the web page's suggestion) | `200 { token, device: { id, name, platform }, user: { id, username } }` · `410 EXPIRED_TOKEN` for used, expired or unknown codes |
| GET | `/me` | bearer only | | `{ device: { id, name, platform, createdAt, lastSeenAt }, user: { id, username }, quota: { used, limit } }`; `limit` -1 = unlimited |

`platform` is one of `windows`, `macos`, `linux`, `android`, `ios`, `lite`, `webdav` (an app password for WebDAV)
or empty. `verificationUrl` is `<PUBLIC_URL origin or request origin>/app/device?code=ABCD-EFGH`.

## Settings — `/api/settings` (session only)

`GET /` and `PUT /` with any of: `language` (`de`/`en`/`null`), `theme`
(`system`/`light`/`dark`), `fileView` (`list`/`grid`), `sortField`
(`name`/`size`/`modified`/`type`), `sortOrder` (`asc`/`desc`),
`showHiddenFiles`, `confirmDelete`, `uploadConflict` (`ask`/`replace`/`rename`).

## Files

| Method | Path | Body / query | Notes |
| --- | --- | --- | --- |
| GET | `/folders/:id/children` | `sort, order, limit, cursor` (or `offset`) | Folders first. Returns `folder`, `path` (breadcrumbs), `items`, `total`, `nextCursor` |
| POST | `/folders` | `{ parentId, name, conflict? }` or `{ parentId, path: "a/b/c" }` | `path` creates missing folders and returns the deepest |
| GET | `/nodes/:id` | | Node plus breadcrumbs |
| PATCH | `/nodes/:id` | `{ name?, parentId?, conflict? }` | Rename and/or move |
| POST | `/nodes/:id/copy` | `{ parentId?, name?, conflict? }` | Defaults to "name (1)" beside the original. Copies share storage. More than `NEOCLOUD_MAX_COPY_NODES` (50 000) files and folders: `413 COPY_TOO_LARGE`, nothing copied |
| DELETE | `/nodes/:id` | | To the trash |
| GET | `/files/:id/versions` | | |
| POST | `/files/:id/versions/:version/restore` | | |
| GET | `/search` | `q, type, after, before, minSize, maxSize, parentId, limit` | `{ items: [node + path] }`, see below |
| GET | `/recent` | `by=opened\|modified, limit` | `{ items: [node + path + lastOpenedAt] }`, see below |
| PUT · DELETE | `/nodes/:id/favorite` | | Marks / unmarks; answers the node. DELETE also works on a trashed node |
| GET | `/favorites` | | `{ items: [node + path + favoritedAt] }`, most recently marked first |
| POST | `/nodes/:id/opened` | | `204`. Records that the account opened the node (Quick Access) |
| GET | `/trash` | | `{ items: [node + originalPath + expiresAt] }` |
| POST | `/trash/:id/restore` | | Back to its folder, or the root if that is gone |
| DELETE | `/trash/:id` · `/trash` | | Delete for good |

`conflict` is `fail` (default, `409 NAME_EXISTS`), `rename` (`name (1)`) or
`replace` (files: new version; folders: reuse the existing one).

An account holds at most `NEOCLOUD_MAX_NODES` (1 000 000) files and folders,
trash included. Anything that would create more (new folders, uploads,
copies, imports, WebDAV) answers `507 NODE_LIMIT`. `POST /folders`,
`PATCH /nodes/:id` and `POST /nodes/:id/copy` are rate-limited per account:
600 per minute together, copies also 100 per 15 minutes (`429 RATE_LIMITED`).

Every node carries `vaultId` (null outside a vault) and, in everything the
account itself reads (listings, node, search, recent, favorites, trash,
storage, the result of rename/move/copy/upload/restore), `favorite: bool`
and `shared: bool` (shared with at least one other account). The sync feeds
(`/api/sync/*`) leave both out; members of a shared folder never see
`shared`.

**Paging.** `limit` defaults to 500, at most 2000. While more follow,
`nextCursor` is a string to pass back as `cursor` (with the same `sort` and
`order`, else `400 INVALID_CURSOR`); it is `null` on the last page. Pages are
keyset-based: every sort (`name`, `size`, `modified`, `type`; `asc`/`desc`)
is a total order (ties by name, then id), so items added or removed meanwhile
never shift or repeat a page. `offset` still works without a cursor.

**Search.** Every word of `q` must appear in the name (case-insensitive).
Ranking: exact name (or exact name without extension), then names starting
with `q`, then names with a word starting with `q` (after a space, `-`, `_`,
`.` or `(`), then the rest; within each, most recently modified first. `q`
may be empty when a filter is set; with neither the answer is empty.
Filters, all optional and combined with AND:

- `type`: `image`, `video`, `audio`, `document`, `pdf`, `archive` or
  `folder`; several comma-separated mean any of them (`400 INVALID_TYPE`).
  Files are grouped by MIME type and extension (`document` is text, Office,
  OpenDocument, RTF, JSON/XML, Pages/Numbers/Keynote, EPUB; `pdf` is
  separate).
- `after`, `before`: ISO dates on `modifiedAt`; `after` inclusive, `before`
  exclusive (`400 INVALID_DATE`).
- `minSize`, `maxSize`: bytes, inclusive (`400 INVALID_SIZE`).
- `parentId`: only nodes below that folder, at any depth (`404` if not the
  account's live folder).
- `limit`: default 100, at most 500.

`path` on search, recent, favorites and storage results is the folder the
node sits in, like the trash's `originalPath`: `"/Photos/2026"`, `"/"` for
the root.

**Recent.** `by=opened` (default): files the account opened
(`POST /nodes/:id/opened`), most recent first, filled up with the most
recently modified files never opened. `by=modified`: by modification time
only. `limit` default 50, at most 200. `lastOpenedAt` is null for files never
opened; anything else is `400 INVALID_PARAM`.

**Trash.** `expiresAt` is when the retention policy
(`trashRetentionDays`) deletes the item for good, in the same UTC format as
the other timestamps; null when trash is kept until emptied by hand.

Search, recent files and duplicates leave vault nodes out; the favorites list
leaves out nodes inside a vault but lists a vault's own folder. Moving or copying across a vault's border, copying a
vault folder, or restoring a vault file whose vault folder is in the trash
answers `409 VAULT_BOUNDARY`.

## Vaults — `/api/vaults` (session or bearer)

End-to-end encrypted folders; the crypto is in [clients.md](clients.md#4-vault-folders-end-to-end-encrypted).

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| POST | `/` | `{ parentId, name, salt, iterations, wrappedKey }` | `201 { id, folderId, salt, iterations, wrappedKey, createdAt, updatedAt, folder }` |
| GET | `/:id` | | `{ id, folderId, salt, iterations, wrappedKey, createdAt, updatedAt }` |
| PUT | `/:id/key` | `{ wrappedKey, salt, iterations }` | the vault as above |

`salt` is 16 bytes and `wrappedKey` 60 bytes (standard base64), `iterations`
an integer from 600 000 to 10 000 000; anything else is `400
VAULT_PARAMS_INVALID`. Vaults created earlier with fewer iterations keep
working (`GET` returns them as stored); clients re-wrap with `PUT …/key` and
at least 600 000 after the next unlock. Vaults don't nest (`409 VAULT_BOUNDARY`). Nodes created
in a vault folder (folders, single and resumable uploads, copies) inherit its
`vaultId`. Vault files have no thumbnail or preview (`404`).

## Share links — `/api/shares` (session or bearer)

| Method | Path | Body / query | Answer |
| --- | --- | --- | --- |
| POST | `/` | `{ nodeId, mode?, expiresAt?, password?, maxDownloads?, maxUploadBytes?, note? }` | `201` share |
| GET | `/` | `?nodeId=` | `{ shares: [share] }`, links not revoked, newest first. With `nodeId`: only that node's links still usable (not expired, not used up) |
| PATCH | `/:id` | `{ mode?, expiresAt?, password?, maxDownloads?, maxUploadBytes?, note? }` | the share; the token and URL stay |
| DELETE | `/:id` | | revokes the link |

A share is `{ id, token, url, nodeId, nodeName, nodeType, mode, allowUpload,
expiresAt, expired, hasPassword, note, maxDownloads, downloadCount,
exhausted, maxUploadBytes, uploadedBytes, createdAt }`; `url` is
`<origin>/s/<token>`.

- `mode`: `view` (default; browse and download), `view_upload` (also
  upload) or `upload_only` (upload into the folder without seeing it). The
  older `allowUpload: true` is still accepted and means `view_upload`;
  `allowUpload` in the answer is `mode !== "view"`. Upload modes need a
  folder (`400 NOT_A_FOLDER`); anything else is `400 INVALID_MODE`.
- `expiresAt`: an ISO date in the future (`400 INVALID_EXPIRY`).
- `password`: 8 to 200 characters (`400 PASSWORD_TOO_SHORT` /
  `PASSWORD_TOO_LONG`). Setting, changing or clearing it signs out every
  visitor who unlocked the link and resets its unlock throttle.
- `maxDownloads`: 1 to 1 000 000 downloads (`400 INVALID_MAX_DOWNLOADS`).
  A download is a file fetched with `?download=1` or a ZIP; inline views and
  `HEAD` don't count. A counted download sets a 12-hour `nc_share_continue`
  cookie scoped to that file; a Range request that starts past byte 0 (suffix
  ranges resolved first) and carries it resumes the download uncounted. Any
  other `?download=1` request, including a Range that covers byte 0, is a new
  download. When `downloadCount` reaches it the link answers `410
  SHARE_EXHAUSTED` (`exhausted: true`); only a download already under way
  (with its cookie) may finish its Range requests.
- `maxUploadBytes`: what visitors may upload through the link in total
  (`400 INVALID_SIZE`); none by default (only the owner's quota applies).
  Over it: `413 SHARE_UPLOAD_LIMIT`. With a limit set, uploads must send
  `Content-Length` (`411 LENGTH_REQUIRED`): the size is reserved before the
  body is read, so parallel uploads can't together go past it, and a failed
  upload gives it back. `uploadedBytes` is what came in so far (reservations
  of uploads in progress included).
- `note`: up to 500 characters shown to visitors (`400 NOTE_TOO_LONG`),
  trimmed; empty means none.

In `PATCH`, only the fields present change; `null` clears `expiresAt`,
`password`, `maxDownloads`, `maxUploadBytes` and `note`. Another account's
link, or a revoked one, is `404`. The root can't be shared (`400
ROOT_IMMUTABLE`), vault nodes neither (`409 VAULT_NOT_SHAREABLE`).

## Sharing with accounts — session or bearer

Files and folders can be shared with other accounts on the same server. A
share is permanent unless the owner sets `expiresAt`, and the member is
named only by their exact email address or username: there is no account
search, list or suggestion anywhere in the API.

**Owner side** (only the node's owner; anyone else gets `404`):

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| POST | `/api/nodes/:id/members` | `{ recipient, permission?, expiresAt? }` | `201` member |
| GET | `/api/nodes/:id/members` | | `{ members: [member] }`, oldest first |
| PATCH | `/api/nodes/:id/members/:memberId` | `{ permission?, expiresAt? }` | the member; `expiresAt: null` clears it |
| DELETE | `/api/nodes/:id/members/:memberId` | | `{ success: true }`; access ends at once |

A member is `{ id, nodeId, recipient, displayName, permission, createdAt,
expiresAt, expired }`. `recipient` is what the owner typed; `displayName` is
the member's display name (null when they set none). The member's email or
username is never sent unless the owner typed it.

- `recipient`: an email address (has an `@`) or a username, matched exactly,
  case-insensitively (`400 INVALID_RECIPIENT` when empty or over 254
  characters). An unknown account, the owner's own account and an account
  that can't receive shares all answer the same `404 RECIPIENT_NOT_FOUND`.
  `POST` is limited to 30 per account in 15 minutes (`429 RATE_LIMITED`).
- `permission`: `read` (default) or `write` (`400 INVALID_PERMISSION`).
  Files can only be shared `read` (`400 WRITE_NEEDS_FOLDER`).
- `expiresAt`: an ISO date in the future (`400 INVALID_EXPIRY`); none by
  default. An expired member stays listed (`expired: true`) but has no access.
- The root can't be shared (`400 ROOT_IMMUTABLE`), vault folders and vault
  contents neither (`409 VAULT_NOT_SHAREABLE`). The same account twice on
  one node is `409 ALREADY_MEMBER`. An unknown member id is `404
  MEMBER_NOT_FOUND`.

**Member side:**

| Method | Path | Answer |
| --- | --- | --- |
| GET | `/api/shared-with-me` | `{ shares: [{ id, permission, ownerName, sharedAt, expiresAt, node }] }`, newest first |
| DELETE | `/api/shared-with-me/:id` | leaves the share; access ends at once |

`ownerName` is the owner's display name, or their username without one.
Shares whose node is in the owner's trash, or that expired, are left out.

**What a member can do** with a shared node and everything below it, through
the usual [Files](#files) and [Transfers](#transfers) routes and the same
node ids:

| | read | write |
| --- | --- | --- |
| `GET /folders/:id/children`, `GET /nodes/:id`, content (with Range), thumbnail, preview, ZIP | yes | yes |
| Upload (single or resumable) into a folder, `POST /folders` | | yes |
| Rename, move within the share, copy, `DELETE /nodes/:id`, `POST /trash/:id/restore` | | yes |

- Writes land in the owner's tree: they count against the owner's quota and
  node limit and show up in the owner's sync feed. A read member's write is
  `403 SHARE_READ_ONLY`.
- Breadcrumbs (`path`) start at the shared folder, and the shared node's
  `parentId` is `null`. `favorite` is always `false`. Vault folders inside a
  shared folder are left out of listings, `total` and ZIPs, and answer
  `404`.
- The shared node itself can't be renamed, moved or deleted by a member
  (`403 SHARE_ROOT_IMMUTABLE`); leave the share instead.
- Moves stay inside the shared folder (`409 SHARE_BOUNDARY` across its
  border, in either direction). A copy across it is a real copy owned by,
  and charged to, the account that receives it (`413 QUOTA_EXCEEDED`,
  `413 COPY_TOO_LARGE`, checked before anything is written; files are then
  copied one by one, so a failure midway keeps what was copied). Vault
  nodes are never copied out.
- `POST /trash/:id/restore` works for a node trashed directly inside a folder
  the member can write to; it goes back there. Listing and emptying the trash
  stay the owner's.
- A member's resumable upload belongs to the owner and can only be continued
  by that member while they may still write to its folder.
- A ZIP holds either your own nodes or nodes of one other owner
  (`400 ZIP_MIXED_OWNERS`).
- Owner-only, `404` for members: versions (and `?version=` on content),
  members, share links, vaults, favorites, `POST /nodes/:id/opened`, search,
  recent files, trash listing, sync and WebDAV. A member can't re-share.

**Live updates.** Members get the socket event `files:changed` with the
folder ids they can see when a shared folder changes, like the owner.
Adding, changing or removing a member, and leaving, send `shares:changed`
(no payload) to the member and, on leaving, to the owner.

## Public share access — `/api/public/shares/:token` (no account)

The page for people is `GET /s/:token`. Every node id is checked to lie inside
the shared subtree, live and outside any vault; anything else is `404`.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/` `?folderId=` | `{ share: { name, type, size, mode, allowUpload, expiresAt, hasPassword, sharedBy, host, note }, node, path, items }`. `path` runs from the shared folder down to `folderId`; `items` is empty for a file link and for `upload_only` |
| POST | `/unlock` | `{ password }` → `{ success: true }` and a 1-hour `nc_share_unlock` cookie scoped to the link's path. 20 / 15 min per address, plus the per-link throttle below |
| GET | `/files/:nodeId/content` `?download=1` | Range, `If-None-Match`, `HEAD`, sandbox CSP as for owners. Counts as a download only with `download=1` |
| GET | `/zip` `?folderId=` | the shared folder (or a folder inside it, or the shared file) as ZIP; vault folders are left out. Counts as a download |
| PUT | `/upload?name=&folderId=` | raw body, only in `view_upload` and `upload_only` (`403 UPLOAD_NOT_ALLOWED`). Never replaces: a taken name becomes `name (1)`. Answers the new item; on an `upload_only` link just `{ name, size }` with the name as sent. Counts against the owner's quota (`413`) and the link's `maxUploadBytes`. Optional `X-Content-SHA256`. 100 / 15 min |

`sharedBy` is the owner's username, `host` the host name the link points to
(`PUBLIC_URL`, or the one the request came in on). On an `upload_only` link,
listing a folder inside it, downloads, ZIPs and uploads into a subfolder
answer `403 SHARE_UPLOAD_ONLY`. Uploads are single requests; the public route
has no resumable parts.

**Unlock throttle (per link).** Wrong passwords are counted per link over 30
minutes. From the 5th on, the next try has to wait 2 s, then 4 s, 8 s, …
after the last one; the 10th locks the link until the oldest of them is 30
minutes old. A try that comes too early is `429 SHARE_LOCKED` with a
`Retry-After` header and `retryAfter` (seconds) in the body, and is not
counted. The right password clears the count.

Errors: unknown link `404 NOT_FOUND`, revoked `410 SHARE_REVOKED`, expired
`410 SHARE_EXPIRED`, used up `410 SHARE_EXHAUSTED`, password needed `401
SHARE_PASSWORD_REQUIRED`, wrong password `401 SHARE_PASSWORD_INVALID`.
Reads allow 600 requests per 15 minutes per address.

## Storage — `/api/storage` (session or bearer)

| Method | Path | Answer |
| --- | --- | --- |
| GET | `/breakdown` | `{ total, files, trash, versions, byType: [{ type, bytes, count }], byFolder: [{ id, name, bytes }], largest: [node + path] }` |
| GET | `/duplicates` | `{ groups: [{ sha256, size, reclaimable, nodes: [node + path] }], reclaimable }` |

`total` is what counts against the quota (each stored blob once); `files`
the live files' sizes (a copy counted again); `trash` and `versions` as in
the account usage. `byType` covers live files by the search types
(`image`, `video`, `audio`, `document`, `pdf`, `archive`) plus `other` and
`vault`, biggest first. `byFolder` lists the root's folders by the live
files below them; files directly in the root are `{ id: <root id>, name: "" }`.
`largest` is the 20 largest live files outside vaults.

Duplicates are live files outside vaults with the same SHA-256 content, empty
files left out; at most 200 groups, the biggest saving first. `reclaimable`
(per group and in total) is what deleting all but one would free: copies made
in NeoCloud already share one stored blob and free nothing.

## Transfers

**Single request** — `PUT /api/upload?parentId=&name=&conflict=` with the
file as the raw body. Optional headers: `X-Content-SHA256` (verified, `422
CHECKSUM_MISMATCH`), `If-Match: "<etag>"` with `replace` (`412
ETAG_MISMATCH` if the file changed meanwhile).

**Resumable** (recommended above ~16 MB):

1. `POST /api/uploads` `{ parentId, name, size, conflict?, sha256? }` →
   `{ id, partSize, partCount, receivedParts }`. The size is reserved
   against the quota.
2. `PUT /api/uploads/:id/parts/:index` with exactly `partSize` bytes (the
   last part: the rest). Any order, repeat freely.
3. `GET /api/uploads/:id` shows which parts arrived (resume after a drop).
4. `POST /api/uploads/:id/complete` `{ baseEtag? }` → the file node.
5. `DELETE /api/uploads/:id` cancels. Unfinished uploads expire after 24 h.

`413 QUOTA_EXCEEDED` or `413 FILE_TOO_LARGE` when it does not fit.
`507 DISK_FULL` when the server's storage disk would drop below
`NEOCLOUD_MIN_FREE_DISK` (default 1 GiB): single uploads, parts, completing
a resumable upload and uploads into share links.

**Download** — `GET /api/files/:id/content?download=1&version=N`. Supports
`Range` (206/416), `If-None-Match` (304) and `HEAD`. HTML, SVG and other
active types are always sent as attachments; every response carries a
sandbox CSP.

**Thumbnail** — `GET /api/files/:id/thumbnail?size=256|1024`: WebP of an
image, cached after the first request. `415 PREVIEW_UNAVAILABLE` for other
types, `404` for vault files (also for the document preview).

**Document preview** — `GET /api/files/:id/preview`: a PDF of an Office file
(needs LibreOffice on the server; see SECURITY.md for how the converter is
confined). `GET /api/capabilities` says whether the
server can: `{ imageThumbnails, officePreview, fullTextSearch }` (`fullTextSearch` is the admin switch for content search).

**ZIP** — `GET /api/download/zip?ids=a,b,c` streams folders and files as one archive.

## Sync

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/sync/tree` | Every live node with its relative `path`, plus the journal `cursor` |
| GET | `/api/sync/changes?cursor=&limit=` | `{ changes: [{ seq, op, nodeId, node }], cursor, hasMore }`. `cursor=latest` answers `{ changes: [], cursor: <newest seq>, hasMore: false }`, to start following from now |

`op` is `create`, `update`, `rename`, `move`, `trash`, `restore` or `delete`.
`node` is the node as it is now (null once deleted for good). A trashed folder
takes its whole subtree with it. File `etag` is the SHA-256 of the content.

The web app also receives a socket.io event `files:changed` `{ folderIds }`.

## Imports — `/api/imports` (session only)

Imports from hosts that resolve to a private, loopback or link-local address
are admin-only (`403 IMPORT_HOST_BLOCKED`) unless `NEOCLOUD_IMPORT_ALLOW_PRIVATE=true`.

Copies a Nextcloud account or any WebDAV folder into NeoCloud, in the
background. The contract is [clients.md §7](clients.md#7-imports-nextcloud-and-other-webdav-servers).

| Method | Path | Body | Notes |
| --- | --- | --- | --- |
| POST | `/imports/test` | `{ source, url, username, password, sourcePath? }` | → `{ ok, files, bytes, error?, code? }`. Rate-limited (20 per 15 min per address). Writes nothing |
| POST | `/imports` | `{ source: "nextcloud"\|"webdav", url, username, password, sourcePath?: "/", targetFolderId?, includeFavorites?: true }` | `201 job`, queued at once |
| GET | `/imports` | | `{ items: [job] }`, newest first |
| GET | `/imports/:id` | | job |
| POST | `/imports/:id/pause` | | queued/scanning/running → `paused`; keeps the password |
| POST | `/imports/:id/resume` | `{ password? }` | paused/failed/cancelled → `queued`. `password` is required once the job has ended (`400 PASSWORD_REQUIRED`). Failed files are tried again |
| POST | `/imports/:id/cancel` | | → `cancelled`, password wiped |
| DELETE | `/imports/:id` | | Only `done`, `failed` or `cancelled` (`409 IMPORT_STATE`); imported files stay |

- `url`: Nextcloud's base address (`https://cloud.example.com`, a pasted
  `…/index.php/apps/files` is cut back); the importer reads
  `/remote.php/dav/files/<username>/` and falls back to `/remote.php/webdav/`
  when that is 404. For `webdav`, the WebDAV folder URL itself. Credentials in
  the URL are refused (`400 INVALID_URL`).
- `password` should be a Nextcloud app password. It is stored with
  `encryptValue` and set to NULL when the job is done, failed, cancelled or
  deleted; it never appears in a response.
- `targetFolderId` defaults to a folder `Nextcloud` (`WebDAV import`) in the
  root, reused when it exists, so running the same import again skips what is
  there. A folder inside a vault is refused (`409 VAULT_BOUNDARY`); another
  account's folder is `404`.
- job: `{ id, source, url, username, sourcePath, targetFolderId, targetPath,
  includeFavorites, status, totalFiles, totalBytes, doneFiles, doneBytes,
  skippedFiles, failedFiles, errors: [{ path, message }], startedAt,
  finishedAt, createdAt }`. `status` is `queued|scanning|running|paused|done|failed|cancelled`.
  `doneFiles` counts imported and skipped files (`skippedFiles` is the
  skipped part); totals grow while the job is `scanning`. `errors` holds the
  last 50, oldest first; `path` `"/"` is about the job itself.
- `/test`: `files` is the number of entries directly in `sourcePath`,
  `bytes` the folder's total size as the server reports it (Nextcloud's
  `oc:size`), else the sum of those entries. `ok: false` comes with `error`
  and a `code`: `REMOTE_AUTH_FAILED`, `REMOTE_FORBIDDEN`, `REMOTE_NOT_FOUND`,
  `REMOTE_RATE_LIMITED`, `REMOTE_ERROR`, `REMOTE_UNREACHABLE`,
  `REMOTE_NOT_WEBDAV`, `REMOTE_TOO_MANY_REDIRECTS`.

Behaviour: one job runs at a time per account, with 2 downloads in parallel,
each streamed straight into an encrypted blob. Folders are listed with
PROPFIND `Depth: 1`, breadth-first. Redirects are followed (credentials only
to the same origin: scheme, host and port); 429 and 5xx are retried with backoff (`Retry-After`
honoured), 401 once. Folder structure, file and folder modification times and
Nextcloud favorites (`oc:favorite`) are kept. A file already present with the
same name, size and modification time is skipped. Quota is checked up front
(the job fails with `QUOTA_EXCEEDED` in `errors`) and again per file; a full
disk pauses the job. One bad file is recorded in `errors` and the job goes on.
Jobs running when the server stops go back to the queue at the next start and
continue after the last finished file. A same-name file that differs is
stored as a new version of it; a name that differs only in case is stored as
`name (1)`.

Limits: a folder listing may be at most 16 MB of XML and must arrive within
2 minutes (else `REMOTE_ERROR` or `REMOTE_UNREACHABLE`; XML that is not a
WebDAV multistatus is `REMOTE_NOT_WEBDAV`). Connections time out after 30 s,
an answer must start within 60 s, and a download may stall for at most
2 minutes. A scan fails the job past 250,000 files and folders
(`IMPORT_TOO_LARGE`) or at a path deeper than 64 folders or longer than
4,096 characters (`IMPORT_TOO_DEEP`), with the message in `errors`. The
private-network check also runs when each connection is made, against the
address actually connected to.

The web app receives a socket.io event `imports:changed` `{ id }` on status
changes and at most once a second while a job makes progress.

## Admin — `/api/admin` (admin session)

| Method | Path | Body |
| --- | --- | --- |
| GET | `/health` · `/logs` · `/audit` · `/version` | `/health`: counts, disks, `database` (`ok`\|`damaged`\|`pending`, the last daily `quick_check`), `databaseCheckedAt`, `damagedBlobs` (weekly blob scrub), `blobScrubCycleAt`, `minFreeDiskBytes` |
| GET | `/users?q=` | |
| PUT | `/users/:id/quota` | `{ mode: "default"\|"unlimited"\|"custom", bytes? }` (`"50GB"` works) |
| PUT | `/users/:id/admin` | `{ isAdmin }` |
| POST | `/users/:id/password` | `{ password }` — signs the account out everywhere: browser sessions (and their sockets), device tokens, WebDAV app passwords, approved device logins and open pairing codes |
| POST | `/users/:id/usage/recalculate` | |
| DELETE | `/users/:id` | Not for admins; deletes all their files |
| GET / PUT | `/config/storage` | `defaultQuotaBytes`, `maxUploadBytes`, `trashRetentionDays`, `versionRetention` |
| POST | `/storage/recalculate` | |
| GET | `/access` · PUT `/access/signup` | `{ allowSignup }` — off refuses every registration but the first account's |
| POST | `/keys/master` | `{ password }` → `{ masterKey }` |
| GET | `/backups` | `{ targets, runs }`; secrets come back only as `passwordSet` etc. Each target carries `lastRun` and `lastVerification` (below, or null) |
| POST | `/backups/targets` | `{ name, type: local\|sftp\|s3\|webdav, config, schedule: manual\|daily\|weekly, hour, weekday, retention }` |
| PUT / DELETE | `/backups/targets/:id` | an empty secret keeps the stored one |
| POST | `/backups/targets/:id/test` · `/backups/targets/:id/run` | run answers 202 and works in the background |
| POST | `/backups/targets/:id/verify` | checks the newest snapshot → `{ verifiedAt, ok, snapshot, database, blobsChecked, missingBlobs, error }` |
| GET | `/backups/targets/:id/snapshots` | finished snapshots, newest first |

Backup `config` per type: `local { path }`, `sftp { host, port, username,
password | privateKey, passphrase, path }`, `s3 { endpoint, region, bucket,
accessKeyId, secretAccessKey, prefix, forcePathStyle }`, `webdav { url,
username, password }`.

Verify (and `neocloud backup restore`) refuse a snapshot whose manifest is
over 64 MB, names a blob other than `<user id>/<blob id>`, or whose database
does not decrypt (only NeoCloud's own segment size is accepted) or is not a
NeoCloud database (empty, no schema version, no users table); `error` says
which. A run fails instead of pruning when a kept snapshot's manifest is
invalid. WebDAV targets: redirects are not followed, a PROPFIND answer may be
at most 64 MB, listings stop at 16 levels and 2,000,000 entries, and names
that are empty, `.`, `..` or contain a slash are refused (SFTP too).

## WebDAV — `/dav/files/<username>/` (app password)

The contract is [clients.md §6](clients.md#6-webdav). The same tree as
`/api`, served to WebDAV clients (Finder, Windows Explorer, rclone, Obsidian,
Joplin, KeePassXC, Infuse, Nextcloud-compatible apps).

| Path | Notes |
| --- | --- |
| `/dav/files/<username>/…` | the account's root folder |
| `/remote.php/dav/files/<username>/…` | Nextcloud alias |
| `/remote.php/webdav/…` | old ownCloud/Nextcloud alias; serves the signed-in account |
| `/.well-known/webdav` | `301` to `/remote.php/webdav/` (any method) |

**Auth.** HTTP Basic: the account's username (any case) and an app password,
a device token created with `POST /api/account/devices { name, platform:
"webdav" }` (Settings › Devices). Tokens of other platforms, session cookies
and wrong usernames get `401` with `WWW-Authenticate: Basic realm="NeoCloud"`.
Failed sign-ins are limited to 60 per address and 30 per username in 15
minutes (`429`); requests without credentials don't count. A username in the
URL that isn't the signed-in account answers `404`. A request with a foreign
`Origin` is `403`. OPTIONS answers without credentials (`DAV: 1, 2`).

WebDAV is mounted before the API middleware: no CORS, CSRF, session or body
parser touches it. PUT bodies stream into the blob store; XML bodies
(PROPFIND, PROPPATCH, LOCK) above 1 MB are `413`; DOCTYPE/entity declarations
are `400`. A single tag longer than 8 KB, or attributes that are not
`name="value"` pairs, are `400 XML_INVALID`.

| Method | Behaviour |
| --- | --- |
| OPTIONS | `200`, `DAV: 1, 2`, `Allow`, `MS-Author-Via: DAV` |
| PROPFIND | `Depth: 0` or `1` (missing = `1`); `infinity` → `403` with `<d:propfind-finite-depth/>`. Body: empty/`allprop` (+`include`), `propname` or `prop`. `207`; unknown or absent props in a 404 propstat |
| GET / HEAD | files via the normal download path: `Range` (`206`/`416`), `ETag`, `If-None-Match` (`304`), `Last-Modified`, sandbox CSP. A folder answers `200` with a short text |
| PUT | streamed; `201` new, `204` replaced (new version, same node). Parent missing `409`, a folder at the path `405`, quota or disk full `507`, over the per-file limit `413`. `If-Match: "<etag>"` / `If-None-Match: *` → `412`. `X-OC-Mtime: <unix seconds>` sets the modified time (answer `X-OC-MTime: accepted`); `X-Expected-Entity-Length` (Finder) is checked against the quota up front. Answers `ETag`, `OC-ETag`, `OC-FileId` |
| MKCOL | `201`; exists `405`, parent missing `409`, with a body `415` |
| DELETE | to the trash, `204`; the root `403` |
| MOVE / COPY | `Destination` (absolute URL or path, any of the three mounts, same account, else `400`/`403`), `Overwrite: T` (default) or `F` (`412` if the target exists). `201` new, `204` replaced. A file over a file becomes a new version of the target (MOVE keeps the source's modified time and removes the source); anything else over an existing target sends that target to the trash first. Same resource `403` (a MOVE that only changes letter case is a rename); into its own subfolder `409`; COPY of a folder with `Depth: 0` copies the folder only |
| PROPPATCH | `207` with one status per prop. `DAV:getlastmodified` / `DAV:lastmodified` (HTTP date, ISO date or Unix seconds) set the modified time; everything else (Win32* etc.) is accepted as a no-op. An invalid time fails it with `409` and the rest with `424`, nothing applied |
| LOCK | exclusive or shared write locks, `Depth: 0` or `infinity`, `Timeout: Second-n` (max 3600). `200` + `Lock-Token`; an unmapped URL becomes an empty file (`201`); a conflicting lock `423`. Without a body and with `If: (<token>)` it refreshes |
| UNLOCK | `Lock-Token: <token>` → `204`, unknown token `409` |

Locks live in memory (lost on restart) and only affect WebDAV: a WebDAV write
(PUT, DELETE, MOVE, COPY target, PROPPATCH, MKCOL) under someone else's lock
without the token in `If` is `423`. The REST API and sync clients ignore them.

Properties: `displayname`, `getcontentlength`, `getcontenttype`, `getetag`
(files: SHA-256 of the content; folders: changes when a direct child
changes), `getlastmodified`, `creationdate`, `resourcetype`, `supportedlock`,
`lockdiscovery`, `quota-used-bytes` and `quota-available-bytes` (folders; the
account's usage and what it may still add, never more than the disk takes),
`oc:fileid` (node id), `oc:favorite`, `oc:size` (folders: total of their
files), `nc:has-preview` (`false`). Quota and `oc:`/`nc:` props come only when
asked for by name.

Paths are percent-encoded UTF-8; names are matched in NFC and without regard
to letter case, like in the rest of NeoCloud. Hrefs in answers use the mount
the client called. Vault folders and their contents are invisible (`404`, and
`409` for writes below them).

Errors carry a sabre-style body: `<d:error><s:message>…</s:message></d:error>`.

## Full-text search

Off until an admin turns it on; each account can opt out.

| Method | Path | Body / query | Notes |
| --- | --- | --- | --- |
| GET / PUT | `/api/admin/config` | `{ fullTextSearch: bool }` | Admin session. Off by default (`NEOCLOUD_FULL_TEXT_SEARCH` in `.env`). Off pauses the indexer and search ignores content; the index is kept |
| GET | `/api/admin/search-index` | | Admin session. `{ enabled, indexed, pending, failed, bytes, available: { pdftotext, office } }`. `bytes`: encrypted texts plus FTS pages. `office` is always `true` (built-in zip reader) |
| POST | `/api/admin/search-index/rebuild` | | Admin session. `202` with the status: the index is emptied and every file read again in the background |
| GET / PUT | `/api/settings` | `{ fullTextSearch: bool }` | Per account, default `true`. `false` drops the account's entries on the next indexer pass and stops indexing its files |
| GET | `/api/search` | `content=1` plus everything above | Adds content matches, see below |

**Content search.** With `content=1` (when the admin switch is on and the
account didn't opt out), files whose text matches come after the name
matches: name matches first, ranked as above, then content-only matches by
BM25, most relevant first. Every item carries `match`: `"name"`, `"content"`
or `"both"`; `content` and `both` items carry `snippet`, an excerpt of about
220 characters around the first match with each matched word between
`\u0002` and `\u0003` (`…` marks cut text). Without `content=1` the response
is unchanged (no `match`).

Each word of `q` must occur in the text as a word or word start (`rep`
finds "report"), regardless of case and accents (`creme` finds "crème");
`"quoted words"` must occur in that order as whole words. Nothing in `q` is
read as query syntax: `OR`, `NEAR`, `*`, `:` and the like are just words or
separators. In this mode quote marks don't count toward name matching. All
filters (`type`, `after`, `before`, `minSize`, `maxSize`, `parentId`) and
`limit` apply to the merged list.

**What is indexed.** Text, Markdown, code, CSV, JSON, XML, YAML and similar
plain text (any charset: BOM, UTF-16, UTF-8, a declared charset, else
Windows-1252) and HTML (without scripts and styles), up to 10 MB; PDF (by
`pdftotext` when installed, otherwise the bundled extractor, up to 2000
pages) and docx/xlsx/pptx/odt/ods/odp (the XML inside the zip), up to 100 MB.
At most 1 MB of text per file; each file gets 60 s. Never vault files,
never trashed files. A file that can't be read counts as `failed` and is
tried again when its content changes or on a rebuild. A changed file drops
out of content results until it has been read again.

The indexer is one background loop reading one file at a time. It follows
the change journal (trash, rename and move drop what must go;
deleting a file removes its entry with it) and reads every live file that
has no entry for its current content, so it resumes after a restart and
catches up after being paused.
