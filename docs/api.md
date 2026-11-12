# NeoCloud API

All endpoints live under `/api` and speak JSON unless noted. Errors look like
`{ "error": "Readable message", "code": "MACHINE_CODE" }`; branch on `code`.
A body that isn't valid JSON answers `400 INVALID_JSON`, one over 1 MB
`413 BODY_TOO_LARGE`, any other unreadable body `INVALID_BODY`; an unknown
`/api` path `404 NOT_FOUND`.

## Authentication

| Client | How |
| --- | --- |
| Web app | Session cookie `neocloud.sid` from `/api/auth/login` or `/api/auth/register`. |
| Phone app (platform `android`/`ios`) | `Authorization: Bearer nc_…` from QR pairing, the device login or a password sign-in swapped for a token. A full client: everything a session reaches, account, settings, imports and admin (for an admin account) included. A handful of session-bound routes refuse it (see [Account](#account--apiaccount-session-or-phone)). |
| Lite (desktop) client, other tokens | `Authorization: Bearer nc_…` from the device login (below) or `POST /api/account/devices`. Reaches files, transfers, storage, sync, shares, members and vaults only; account, settings, imports, AI and NeoRouter answer `403 PHONE_REQUIRED`, admin `403 ADMIN_REQUIRED`. WebDAV app passwords reach WebDAV only (below); sent as a bearer token they answer `401`. |
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
| POST | `/qr-login/challenge` (60 / 15 min) | `{ requestMetadata? }` | QR sign-in, see below → `{ challengeId, pollToken, expiresAt, status: "pending", qrPayload, backendUrl }` |
| POST | `/qr-login/challenge/:id/status` (400 / 15 min) | `{ token, knownStatus? }` | → `{ challengeId, status, expiresAt, approvedAt, claimedAt }`. With `knownStatus` the request is held open (up to 25 s) until the status differs, and the answer carries `longPoll: true` |
| POST | `/qr-login/challenge/:id/cancel` (400 / 15 min) | `{ token }` | → `{ success: true }`; a pending or scanned code becomes `declined` |
| POST | `/qr-login/challenge/:id/claim` (40 / 15 min) | `{ token }` | Once approved: answers like `/login` and sets the session cookie. `409 QR_LOGIN_NOT_APPROVED` before, `QR_LOGIN_USED` after the first claim |

QR sign-in: a signed-out browser shows a code, the signed-in phone app scans
and approves it (`/api/account/qr-login/*`), and the browser claims a session.
`qrPayload` is
`neocloud://qr-login?v=1&backend=<origin>&challenge=<challengeId>&secret=<approve secret>`.
The browser keeps `pollToken`; the QR code carries the approve secret; only
the SHA-256 of each is stored. A code lives 2 minutes. `status` is `pending`,
`scanned` (the phone opened the preview; the answer then adds `scannedAt`,
`scannedBy: { label, platformLabel, deviceClass }`, `requestedDevice: { label }`
and `requestLocation: { label }`), `approved`, `claimed`, `declined` or
`expired`; an unknown poll token reads as `expired`. `requestMetadata`
(`deviceLabel`, `platformLabel`, `browserLabel`, `deviceClass`, `appMode`,
`platform`) labels the browser for the phone; otherwise it is read from the
User-Agent. Changing or resetting the password declines approved codes not
yet claimed.

Besides 20 tries per 15 minutes per address, `/login` and `/login/2fa` are
throttled per account, whatever address a try comes from (a name no account
has is throttled the same way). From the 5th failed password or code in 15
minutes each next try waits twice as long (2 s, 4 s, …); the 10th locks
sign-in until the oldest of those failures is 15 minutes old. A throttled try,
right or wrong, answers `429 LOGIN_LOCKED` with `Retry-After` and
`retryAfter` (seconds) in the body. A right password clears its own
failures; wrong second-factor codes count until a full sign-in succeeds.
A security key sign-in counts like a password try for the key's account.

A TOTP code is accepted once: a code of a time step the account already
used (at sign-in or any confirmation) is `INVALID_CODE`, so two prompts in
a row need two codes. A recovery code is used up by the first request that
gets it, even when two arrive at once.

Every place a signed-in account confirms its password or code again (the
`/api/account` routes taking `currentPassword` or `code`, and
`POST /api/admin/keys/master`) is throttled per account the same way, under
a key of its own: `429 LOGIN_LOCKED` with `Retry-After` and `retryAfter`.

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

## Account — `/api/account` (session or phone)

Every route takes a browser session or the phone app's device bearer token
(platform `android` or `ios`); other tokens get `403 PHONE_REQUIRED`. With a
phone token: `/sessions` marks no session `current`, `/devices` marks the
phone itself `current: true` and won't revoke it (`400 CURRENT_DEVICE`, the
app disconnects instead), a password change keeps the phone's token instead
of the session, and adding a security key answers `403 BROWSER_REQUIRED`
(the ceremony lives in the browser session and the web origin). The current
password and 2FA code are asked for exactly as with a session.

| Method | Path | Body |
| --- | --- | --- |
| GET | `/` | |
| GET | `/usage` | |
| PUT | `/display-name` | `{ displayName }` |
| PUT | `/password` | `{ currentPassword, newPassword }` — signs out every other session (and closes their sockets), revokes every device token and WebDAV app password, and voids approved device logins and open pairing codes not yet redeemed. This session stays under a new id (a new `neocloud.sid` cookie); from a phone, the phone's token stays |
| POST | `/2fa/setup` | `{ currentPassword, code? }` → `{ otpauthUrl, manualKey, status }`. With 2FA already on, `code` (current TOTP or a recovery code) is required: `401 TWO_FACTOR_REQUIRED` / `INVALID_CODE` |
| POST | `/2fa/enable` | `{ code, currentCode? }` — `code` from the new secret; with 2FA already on, `currentCode` from the factor being replaced is required as well → `{ recoveryCodes, status }` |
| POST | `/2fa/disable` · `/2fa/recovery-codes` | `{ currentPassword, code }` |
| GET / DELETE | `/sessions` · `/sessions/:id` | Revoking a session also closes its socket.io connections |
| GET | `/security-keys` | → `{ securityKeys: [{ id, label, createdAt, lastUsedAt, transports, backedUp, rpId }] }`; `GET /` carries the same list |
| POST | `/security-keys/register/options` | `{ currentPassword, code? }` → `{ options }` for `navigator.credentials.create`. A key signs in on its own and is kept through password changes and sign-out-everywhere, so this asks for the password again, and a code when 2FA is on (`401 WRONG_PASSWORD`, `TWO_FACTOR_REQUIRED`, `INVALID_CODE`). At most 20 keys (`400 SECURITY_KEY_LIMIT`). Browser only: `403 BROWSER_REQUIRED` for a phone token (also `/register/verify`) |
| POST | `/security-keys/register/verify` | `{ response, label? }` → `{ success, credentials }`; `label` up to 48 characters, default "Security key N". Recorded in the admin audit log (`account.security_key_added`; removing one, `account.security_key_removed`) |
| PUT / DELETE | `/security-keys/:id` | `{ label }` to rename → `{ success, credentials }`; another account's key is `404` |
| GET / POST / DELETE | `/devices` · `/devices/:id` | `{ name, platform }`; the token is returned once. `GET` → `{ devices: [{ id, name, platform, createdAt, lastSeenAt, current }] }`; `DELETE` of the phone asking → `400 CURRENT_DEVICE` |
| GET | `/devices/authorize/:userCode` | → `{ name, platform, createdAt, expiresAt }` of a pending device login |
| POST | `/devices/authorize/:userCode` | `{ approve: true\|false }` → `{ success: true }` |
| POST | `/export` (5 / min) | `{ currentPassword, code? }` → the account's data as a JSON download (`Content-Disposition: attachment`, `Cache-Control: no-store`), see below |
| POST | `/delete` (5 / min) | `{ currentPassword, code?, confirmUsername }` → `{ success: true }`; deletes the account with everything in it and ends this session or the phone's token (see below) |

The user code is accepted in any case, with or without the dash. Unknown,
expired or already decided codes answer `404 NOT_FOUND`; both routes allow 30
requests per 15 minutes per address.

QR sign-in, the phone side (session or bearer, 30 / 15 min, see
[Auth](#auth--apiauth)); each takes `{ challengeId, secret }` from the scanned
code:

| Method | Path | Extra body | Answer |
| --- | --- | --- | --- |
| POST | `/qr-login/resolve` | `{ scannerMetadata? }` (labels as in `requestMetadata`) | Marks the code `scanned` and returns the preview: `{ challengeId, status, requestedAt, expiresAt, scannedAt, approvedAt, claimedAt, scannedBy, requestedDevice: { label, platformLabel, browserLabel, deviceClass, userAgent, metadata }, requestLocation: { label, ipAddress } }`. A scanned code reads `pending` here |
| POST | `/qr-login/approve` | `{ approvalMetadata? }` | → the preview with `status: "approved"` |
| POST | `/qr-login/decline` | | → `{ success: true }` |

A wrong secret, an unknown or an expired code is `404 NOT_FOUND`; a declined
or cancelled one `409 QR_LOGIN_DECLINED`, a claimed one `409 QR_LOGIN_USED`,
and declining an approved one `409 QR_LOGIN_APPROVED`. The location is the
address the browser asked from; NeoCloud does no geo lookup.

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
| POST | `/webauthn/options` | none (40 / 15 min) | `{ username? }` | Security key sign-in, see [clients.md](clients.md#10-security-key-sign-in) → `{ challengeId, origin, options }`; `options` are for `navigator.credentials.get`, `origin` is the public origin the key's `clientDataJSON` must name. `409 NO_SECURITY_KEYS` while no key is registered for this host |
| POST | `/webauthn/verify` | none (40 / 15 min) | `{ challengeId, response, name, platform }` | `200 { token, device: { id, name, platform }, user: { id, username } }` like `/token` · `200 { requiresTwoFactor: true, challengeId }` for a presence-only key on an account with 2FA · `400 SECURITY_KEY_CHALLENGE_EXPIRED` (unknown, used or older than 5 minutes; every challenge is spent by its first verify) · `401 SECURITY_KEY_UNKNOWN` · `401 SECURITY_KEY_REJECTED` (wrong origin, RP ID, challenge or signature) · `400 NAME_REQUIRED` |
| POST | `/webauthn/2fa` | none (40 / 15 min) | `{ challengeId, code }` | TOTP or recovery code within 10 minutes, at most 5 per sign-in → the token answer above · `401 INVALID_CODE` · `401 TWO_FACTOR_EXPIRED` (also after the fifth wrong code) · `429 LOGIN_LOCKED` |
| GET | `/me` | bearer only | | `{ device: { id, name, platform, createdAt, lastSeenAt }, user: { id, username, isAdmin }, quota: { used, limit } }`; `limit` -1 = unlimited |

`platform` is one of `windows`, `macos`, `linux`, `android`, `ios`, `lite`, `webdav` (an app password for WebDAV)
or empty. `verificationUrl` is `<PUBLIC_URL origin or request origin>/app/device?code=ABCD-EFGH`.

## Settings — `/api/settings` (session or phone)

`GET /` and `PUT /` with any of: `language` (`de`/`en`/`null`), `theme`
(`system`/`light`/`dark`), `fileView` (`list`/`grid`), `sortField`
(`name`/`size`/`modified`/`type`), `sortOrder` (`asc`/`desc`),
`showHiddenFiles`, `confirmDelete`, `uploadConflict` (`ask`/`replace`/`rename`),
and the AI keys ([docs/ai.md](ai.md)): `ai_opt_in` (boolean, default `false`),
`ai_model_text` · `ai_model_smart` · `ai_model_vision` · `ai_model_embedding`
· `ai_model_transcription` (`"auto"` or a `provider::model` id, default
`"auto"`), `ai_features` (an object of up to 32 booleans, default `{}`; a
missing key takes its default: `names`, `folders`, `tags`, `summaries` and
`newTags` default `true`, `onUpload`, `autoApplyHighConfidence` and
`newFolders` default `false`, see [AI](#ai--apiai-session-or-phone)) and
`ai_naming_template` (1–200 characters, no slashes, default
`"{date} {issuer} – {subject}"`; placeholders `{date}` `{year}` `{month}`
`{issuer}` `{subject}` `{type}` `{amount}` `{currency}` `{original}`). Unknown keys
answer `400 UNKNOWN_SETTING`, bad values `400 INVALID_SETTING_VALUE`.

`fileView`, `sortField` and `sortOrder` are the defaults. `folder_views`
(default `{}`) holds what was chosen per folder or page:
`{ <key>: { sort, dir, view, at } }`, the key a folder id, `root`, or one of
the pages `recent`, `favorites`, `shared`, `trash`, `search` (1–64 characters
of `A–Z a–z 0–9 _ : -`). `sort` is `name`/`size`/`modified`/`type`/`relevance`
(`relevance` for search results), `dir` `asc`/`desc`, `view` `list`/`grid`;
`at` (milliseconds) is set by the server. A PUT sends only the entries that
changed, without `at` (up to 500; `null` removes one), and they are merged
into what is stored, so devices don't overwrite each other's folders. At most
500 entries are kept; the least recently changed go first. Entries for
deleted folders are not removed until they age out.

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| GET | `/meta/models` | | `{ models: [model] }`, every kind this account can see (below) |
| GET | `/meta/ai-providers` | | `{ providers: [{ id, label, description, authentication, supportsApiKey, supportsBaseUrl, requiresBaseUrl, defaultBaseUrl, available, connected, healthy, credentialConfigured, baseUrlConfigured, baseUrl, customLabel, isByok, status, statusLabel, availabilityReason, modelCount, availableModelCount }] }`. A base URL from the server's environment is shown to admins only |
| GET | `/byok` | | `{ providers: [byokProvider] }`: the providers a person can bring a key for (not NeoRouter) |
| GET | `/byok/:providerId` | | `{ provider: byokProvider }` |
| PUT | `/byok/:providerId` | `{ apiKey, baseUrl?, label? }` | `{ success: true, configured: true }`. The key is stored encrypted and never shown again. `400 API_KEY_REQUIRED`, `400 BASE_URL_REQUIRED` (custom endpoint, Ollama), `400 INVALID_URL`, `400 URL_NOT_ALLOWED` (the address is or resolves to loopback/private/link-local, unless it is the admin's own address for that provider), `404 UNKNOWN_PROVIDER` |
| DELETE | `/byok/:providerId` | | `{ success: true, configured: false }` |
| POST | `/byok/:providerId/test` | `{ apiKey?, baseUrl? }` (empty: the saved setup) | `{ success: true, ok: true, message }` or `{ success: true, ok: false, error }`; lists the provider's models, stores nothing. Same `400`s as PUT |

PUT, DELETE and test share a limit of 30 per 15 minutes (`429 RATE_LIMITED`).

`byokProvider`: `{ id, label, description, supportsApiKey, supportsBaseUrl,
requiresBaseUrl, defaultBaseUrl, isCustomEndpoint, configured, baseUrl,
customLabel }` (`baseUrl`/`customLabel` only when it is the person's own).

`model`: `{ id: "provider::model", modelId, provider, label, kind:
"chat"|"embedding"|"transcription", createdAt, supportsTools, supportsVision,
purpose, priceTier: "free"|"cheap"|"medium"|"expensive"|null, inputCostPerM,
available, providerStatus, providerStatusLabel, isByok, byokLabel,
adminGranted }`. NeoRouter's auto models are `neorouter::auto:<profile>`
(plain auto is `auto:default`); one id can appear once per kind.

## AI — `/api/ai` (session or phone)

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| GET | `/status` | | `{ available, enabled, optedIn, roles: { text, smart, vision, embedding, transcription }, budget: { used, limit } }`. Each role: `{ ready, model, source: "user"\|"neorouter"\|"auto"\|null, requestedUnavailable }`. `available`: the admin switch is on and some role has a model. While the switch is off no provider is asked and every role is `ready: false`. `budget`: this month's tokens and the account's limit (`null`: none) |
| PUT | `/folders/:id/excluded` | `{ excluded }` (boolean) | `{ node }` with `aiExcluded`. Owner only (another account's folder `404`); `400 NOT_A_FOLDER`, `400 ROOT_FOLDER`, `400 INVALID_VALUE`. Inherited by everything below the folder |

Errors AI features answer with: `403 AI_DISABLED` (admin switch off), `403
AI_NOT_OPTED_IN`, `429 AI_BUDGET_EXCEEDED`, `503 AI_MODELS_UNAVAILABLE` (no
model for the role), `502 AI_PROVIDER_ERROR`, `502 AI_BAD_JSON`, and for a
file `403 AI_VAULT` (vault content) or `403 AI_EXCLUDED` (inside an excluded
folder), `403 AI_FEATURE_OFF` (the person switched that feature off).

### Suggestions, jobs, rules and summaries

Every id here is one of the account's own: another account's file,
suggestion or rule, and a file only shared with the caller, answer `404`.
Feature switches (`ai_features`): `names` (rename suggestions), `folders`
(move suggestions), `tags`, `summaries`, `onUpload` (new files get a job by
themselves), `autoApplyHighConfidence` (high-tier suggestions are applied at
once, listed as `applied`), `newTags` (tags beyond the ones the account
already has), `newFolders` (moves into a folder that doesn't exist yet).

| Method | Path | Body / query | Answer |
| --- | --- | --- | --- |
| GET | `/suggestions` | `kind` (`rename`,`move`,`tags`), `tier` (`high`,`medium`,`low` or `all`; default high,medium), `status` (default `pending`), each comma-separated; `nodeId`, `limit` (50, max 200), `offset` | `{ items: [suggestion], total, nextOffset, counts }`, newest first |
| GET | `/suggestions/counts` | | `counts`: `{ pending: { rename, move, tags, total }, high, hiddenLow, proposals }` (pending high + medium per kind; low ones counted apart; open rule proposals) |
| POST | `/suggestions/:id/accept` | `{ value? }`: replaces the proposal: a name (rename), a folder id (move), a tag list (tags) | `{ suggestion }` with `status: "accepted"`. `409 AI_SUGGESTION_STALE` (the file changed; it is now `stale`), `409 AI_SUGGESTION_DECIDED`, `409 AI_TARGET_NOT_ALLOWED` (vault or excluded folder), `409 SHARE_BOUNDARY`, `400 NAME_*`, `400 INVALID_VALUE` |
| POST | `/suggestions/:id/reject` | | `{ suggestion }` with `status: "rejected"` (pending or stale ones) |
| POST | `/suggestions/accept-high` | `{ kind? }` (comma-separated) | `{ accepted, failed: [{ id, code }] }`: every pending high-tier one |
| POST | `/suggestions/bulk` | `{ ids: [suggestionId], action: "accept"\|"reject" }` (1–1000 ids) | `{ done, failed: [{ id, code }] }`: each one decided as by its own accept or reject route; a failure (`NOT_FOUND`, `AI_SUGGESTION_STALE`, `AI_SUGGESTION_DECIDED`, …) doesn't stop the rest. `400 INVALID_ACTION`, `400 INVALID_REQUEST` |
| POST | `/suggestions/:id/undo` | | `{ suggestion }` with `status: "undone"`: an `accepted` or `applied` one is put back (name, folder or tags) if the file is still as it left it, else `409 AI_UNDO_CONFLICT` (or `409 NAME_EXISTS` when the old name is taken); `409 AI_NOT_UNDOABLE` for other states |
| POST | `/jobs` | `{ nodeIds: [fileId] }` or `{ folderId }`, `kinds?: ["names","folders","tags"]` (default: every switched-on kind) | `202 { batchId, queued }`. `nodeIds` (1–1000 files): their names are suggested even when they already read well. `folderId`: a backfill of every file below it (at most 5000, else `413 TOO_MANY_FILES`) skipping vaults and excluded folders, renaming only meaningless names. `403 AI_VAULT` / `AI_EXCLUDED` for a listed file, `400 NOT_A_FILE`, `400 INVALID_KINDS`, `400 INVALID_REQUEST`, `403 AI_FEATURE_OFF`, `403 AI_DISABLED` / `AI_NOT_OPTED_IN` |
| GET | `/jobs` | `batchId?` | `{ pending, running, done, skipped, failed, paused, batches: [{ id, createdAt, total, pending, done, skipped, failed }] }` (the last 10 requests, or the one asked for; unknown `404`). `paused`: why pending jobs wait (`AI_DISABLED`, `AI_NOT_OPTED_IN`, `AI_BUDGET_EXCEEDED`) or null. Finished jobs are kept 7 days |
| GET | `/rules` | | `{ rules: [rule] }`, in the order they are tried |
| POST | `/rules` | `{ name, match: { type?, issuer?, extension?, namePattern? }, targetFolderId, enabled? }` | `201 { rule }`. At least one condition (`400 INVALID_RULE`); the target must be the account's folder outside vaults and excluded folders (`409 AI_TARGET_NOT_ALLOWED`); at most 200 rules |
| PATCH | `/rules/:id` | any field of POST | `{ rule }` |
| DELETE | `/rules/:id` | | `{ success: true }` |
| GET | `/rules/proposals` | | `{ proposals: [{ id, type, issuer, targetFolderId, targetPath, accepts, name }] }`: after 3 accepted moves of the same type and issuer into one folder, unless such a rule exists or it was dismissed |
| POST | `/rules/proposals/:id` | `{ name? }` | `201 { rule }` matching type and issuer |
| DELETE | `/rules/proposals/:id` | | `{ success: true }`: not offered again |
| POST | `/files/:id/summary` | `{ refresh? }` | `{ summary, model, etag, createdAt, cached }`. Kept per content (`etag`) until `refresh: true`. Smart role (images: vision). `422 AI_NO_CONTENT`, `400 NOT_A_FILE`, plus the AI errors above |

`POST /jobs` and `POST /files/:id/summary` share a limit of 300 per 15
minutes (`429 RATE_LIMITED`).

`suggestion`: `{ id, nodeId, kind, from, to, reason, confidence (0–1), tier,
status, source: "ai"|"rule", ruleId, model, createdAt, decidedAt, node }`.
`from`/`to`: a file name (`rename`), `{ id, path }` (`move`; a new folder
is `{ id: null, parentId, name, path }` and is created on accept), a tag
list (`tags`). `node`: the file now, with `path`, or null once deleted.
Tiers: `high` ≥ 0.85 (rules always), `medium` ≥ 0.6, `low` below (hidden by
default). Statuses: `pending`, `accepted`, `rejected`, `applied` (auto-apply),
`undone`, `stale` (the file got new content, or another name for a rename,
another folder for a move, or went to the trash).

`rule`: `{ id, name, match: { type, issuer, extension, namePattern },
targetFolderId, targetPath, enabled, createdAt }`. `type` and `issuer`
compare case-insensitively with what the extraction found, `extension`
without the dot, `namePattern` is a glob (`*`, `?`) over the whole name.

Applying goes through the normal file operations: the change journal and
`files:changed` as for any rename or move, `conflict: rename` for name
clashes. Whenever counts change the account's sockets get **`ai:suggestions`**
with `counts` as payload.

### Duplicates

Plain duplicate finding, no AI: it works whether AI is on or off, for the
signed-in account's own live files (no vault, no trash).

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| GET | `/duplicates` | | `{ groups: [group], pending }`, the most space to free first (at most 500 groups). `pending`: photos not compared yet; their hashes are made in the background after this request, so ask again while it is above 0 |
| POST | `/duplicates/dismiss` | `{ nodeIds: [fileId] }` (2–1000) | `{ success: true }`: "Not duplicates", a group whose files are all among these isn't listed again. Another account's file `404`, `400 INVALID_REQUEST` |

`group`: `{ kind: "exact"|"similar", similarity (percent), items: [{ node,
size, takenAt?, width?, height?, keep }] }`. `exact`: the same content (the
SHA-256 etag), any file type. `similar`: photos whose 64-bit perceptual hash
(dHash of a 9×8 grayscale thumbnail) differs in at most 3 bits (about 95 %);
exact copies appear there once. `node` carries `path`. `keep` marks the one
to keep: the oldest copy (exact) or the largest picture (similar). Removing
the others uses `DELETE /api/nodes/:id` (the trash), so it can be undone.

## NeoRouter — `/api/neorouter` (session or phone)

"Connect NeoRouter" with NeoRouter's device sign-in. `scope` is `server` (the
server's connection, admins only, else `403 ADMIN_REQUIRED`) or `user` (the
person's own, stored like a BYOK key). Roles: `chat`, `embeddings`,
`transcription`, `images`.

| Method | Path | Body | Answer |
| --- | --- | --- | --- |
| GET | `/` | | `{ roles, isAdmin, server, user }`. A connection: `{ connected, address, roles, offer: { roles: { chat: n, … }, notAllowed: { … } } \| null, reachable }`; non-admins get `server: { connected }` only |
| POST | `/connect` | `{ scope, address }` | `{ id, url, userCode }`: open `url` (NeoRouter's approval page) and compare `userCode`. `400 INVALID_SCOPE`, `400 INVALID_ADDRESS`, `400 NEOROUTER_UNREACHABLE`; a `user` address on a private network that isn't the server's own NeoRouter `400 URL_NOT_ALLOWED` |
| GET | `/connect/:id` | | `{ status: "pending"\|"completed"\|"failed", error }`; only to the account that started it, and a finished result only once (then `404`) |
| PUT | `/:scope/roles` | `{ roles: [...] }` | `{ roles }` (unknown roles dropped) |
| DELETE | `/:scope` | | `{ success: true }` |

`POST /connect`, `PUT` and `DELETE` share a limit of 30 per 15 minutes.

## Files

| Method | Path | Body / query | Notes |
| --- | --- | --- | --- |
| GET | `/folders/:id/children` | `sort, order, limit, cursor` (or `offset`) | Folders first. Returns `folder`, `path` (breadcrumbs), `items`, `total`, `nextCursor` |
| POST | `/folders` | `{ parentId, name, conflict? }` or `{ parentId, path: "a/b/c" }` | `path` creates missing folders and returns the deepest |
| GET | `/nodes/:id` | | Node plus breadcrumbs |
| PATCH | `/nodes/:id` | `{ name?, parentId?, conflict? }` | Rename and/or move |
| POST | `/nodes/:id/copy` | `{ parentId?, name?, conflict? }` | Defaults to "name (1)" beside the original. Copies share storage. A vault folder inside a copied folder is left out with its contents (the vault itself can't be copied: `409 VAULT_BOUNDARY`). More than `NEOCLOUD_MAX_COPY_NODES` (50 000) files and folders: `413 COPY_TOO_LARGE`, nothing copied |
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
`shared`. Nodes read outside the sync feeds also carry `aiExcluded: bool`
(the folder was excluded from AI by its owner, see
[AI](#ai--apiai-session-or-phone); it applies to everything below it too). They also carry
`aiTags` (the AI tags the owner accepted, `[]` when none) and `aiMeta` (what
the extraction found in the current content, null when nothing or the
content changed since: `{ date, dateSource: "content"|"exif"|"filename"|"modified",
issuer, subject, type, amount, currency, tags, language, summary, caption,
confidence: { date, issuer, subject, type, amount, tags }, overall, model }`).
Members of a shared folder see neither.

**Paging.** `limit` defaults to 500, at most 2000. While more follow,
`nextCursor` is a string to pass back as `cursor` (with the same `sort` and
`order`, else `400 INVALID_CURSOR`); it is `null` on the last page. Pages are
keyset-based: every sort (`name`, `size`, `modified`, `type`; `asc`/`desc`)
is a total order (ties by name, then id), so items added or removed meanwhile
never shift or repeat a page. `offset` still works without a cursor.

**Search.** Every word of `q` must appear in the name (case-insensitive),
or in the node's AI tags and extracted words (issuer, subject, type,
caption); such matches rank with the rest.
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

## Photos — `/api/photos` (session or phone)

The account's own photos and videos (`image/*` except SVG and icons,
`video/*`), newest first by when they were taken: EXIF `DateTimeOriginal`,
else the date a phone or camera wrote into the movie or the file name
(`IMG_20260312_101010.jpg`), else the modification time. `takenAt` is that
wall-clock time, `"YYYY-MM-DDTHH:MM:SS"` with no time zone; the first 10
characters are the day the photo belongs to. Vault files, trashed files and
files shared with the account are never listed.

| Method | Path | Query | Answer |
| --- | --- | --- | --- |
| GET | `/` | `cursor?`, `day?` (`YYYY-MM-DD`), `limit?` (1–500, default 200) | `{ items: [photo], nextCursor }` |
| GET | `/summary` | | `{ total, pending, days: [{ date, count }], months: [{ month, count }] }`, newest first |

- A photo: `{ id, takenAt, kind: "photo"|"video", width, height,
  durationSeconds, name, parentId, mime, size, etag, favorite }`. `width`,
  `height` (as displayed, EXIF rotation applied) and `durationSeconds` are
  `null` until the file was read, and for formats NeoCloud can't read.
- Pages continue from `nextCursor` (opaque; `null` on the last page) and are
  as fast at the end of a long timeline as at the start. `day` starts the
  page at the newest item of that day instead, for jumping there; it is
  ignored when `cursor` is set. `400 INVALID_CURSOR`, `INVALID_DAY`,
  `INVALID_LIMIT`.
- New uploads show at once with a date from the file name or modification
  time; a background task then reads each file once (EXIF, the movie header)
  for its real date and size and makes its `square` thumbnail. `pending`
  counts items not read yet: their place in the timeline may still move.
- Thumbnails: `GET /api/files/:id/thumbnail?size=square&v=<etag>`. Videos
  have no thumbnail.

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
| GET | `/zip` `?folderId=` | the shared folder (or a folder inside it, or the shared file) as ZIP; vault folders are left out. Counts as a download (a `HEAD` does not) |
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
   While one complete runs, another complete or a cancel of the same upload
   answers `409 UPLOAD_COMPLETING`; once it is done, `404 UPLOAD_NOT_FOUND`.
5. `DELETE /api/uploads/:id` cancels. An upload expires 24 h after its last part arrived (or after it was created, before any part).
   A part still arriving when its upload is cancelled answers
   `404 UPLOAD_NOT_FOUND` and is discarded.

**Already there?** — `POST /api/uploads/check` `{ parentId, sha256, size }`
→ `{ exists }`: whether a live file with that content (SHA-256, any case,
and size in bytes) is in the folder or any folder below it. Camera upload
asks before each photo, so photos it sent before are not sent again, even
after they were sorted into subfolders or its records were lost. `400
INVALID_HASH` / `INVALID_SIZE`; a folder of a share uses the owner's tree.

**Almost the same photo there?** — `POST /api/uploads/similar`
`{ hash }` or `{ hashes: [...] }` (up to 200; each the 64-bit dHash of the
photo as 16 hex characters, made like Review › Duplicates: oriented, white
behind transparency, grayscale, resized to 9×8 with Lanczos-3, each bit set when a pixel is
brighter than its right neighbour, rows top to bottom, most significant bit
first) → `{ matches: [{ hash, nodeId, similarity }] }`, one entry per asked
hash that has an own photo (not in a vault or the trash) at most 3 bits apart
(≥ 95 %); `similarity` in percent. Only photos already hashed can match;
asking starts hashing the rest in the background. Session or phone token
(`403 PHONE_REQUIRED` for desktop tokens). `400 INVALID_HASH` /
`TOO_MANY_HASHES`. Camera upload skips matched photos unless its "skip
similar" setting is off; against a server without this route (404) it uploads
normally.

`413 QUOTA_EXCEEDED` or `413 FILE_TOO_LARGE` when it does not fit.
`507 DISK_FULL` when the server's storage disk would drop below
`NEOCLOUD_MIN_FREE_DISK` (default 1 GiB): single uploads, parts, completing
a resumable upload and uploads into share links.

**Download** — `GET /api/files/:id/content?download=1&version=N`. Supports
`Range` (206/416), `If-None-Match` (304) and `HEAD`. HTML, SVG and other
active types are always sent as attachments; every response carries a
sandbox CSP. A file whose stored data is missing or fails authentication
answers `500 FILE_UNREADABLE` before any byte is sent; damage further into a
file can only cut the transfer short, never deliver altered bytes.

**Thumbnail** — `GET /api/files/:id/thumbnail?size=256|1024|square&v=<etag>`:
WebP of an image, cached after the first request. `256` and `1024` fit the
image into that many pixels; `square` is a 320 × 320 centre crop for photo
grids, made ahead of time for the Photos timeline. Answers carry the ETag
and `Cache-Control: private, max-age=86400`; when `v` is the file's current
`etag` the URL names one content, so it is `private, max-age=31536000,
immutable`. `415 PREVIEW_UNAVAILABLE` for other types, `404` for vault files
(also for the document preview).

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

## Imports — `/api/imports` (session or phone)

Imports from hosts that resolve to a private, loopback or link-local address
are admin-only (`403 IMPORT_HOST_BLOCKED`) unless `NEOCLOUD_IMPORT_ALLOW_PRIVATE=true`.

Copies a Nextcloud account or any WebDAV folder into NeoCloud, in the
background. The contract is [clients.md §7](clients.md#7-imports-nextcloud-and-other-webdav-servers).

| Method | Path | Body | Notes |
| --- | --- | --- | --- |
| POST | `/imports/test` | `{ source, url, username, password, sourcePath? }` | → `{ ok, files, bytes, error?, code? }`. Rate-limited (20 per 15 min per address). Writes nothing |
| POST | `/imports` | `{ source: "nextcloud"\|"webdav", url, username, password, sourcePath?: "/", targetFolderId?, includeFavorites?: true }` | `201 job`, queued at once. Rate-limited together with resume (20 per 15 min per address) |
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

Your data (`/export`, `/delete`): both ask for the password again
(`401 WRONG_PASSWORD`) and, with 2FA on, a TOTP or recovery code
(`401 TWO_FACTOR_REQUIRED` / `INVALID_CODE`). The export
(`kind: "neocloud-account"`, `format: 1`) holds `profile`, `storage`,
`settings`, `security` (2FA on/off, passkey labels and dates), `sessions`,
`devices`, `links`, `sharing` (`byMe`, `withMe`), `files` (every file and
folder, flat with `parentId`, files with their earlier `versions`),
`favorites`, `vaults`, `imports` and `audit` entries by or about the account.
File contents are not included (download the files or a ZIP), nor anything
that works as a credential: no password or code hashes, 2FA secrets, link
tokens or passwords, device token hashes, passkey keys, vault keys or import
passwords. `/delete` needs `confirmUsername` to repeat the username (any case;
`400 CONFIRMATION_MISMATCH`) and refuses the last active admin
(`409 LAST_ADMIN`). It removes the files and folders with their versions,
previews, blobs on disk and search index entries, share links, shares with
other accounts in both directions (those accounts get `shares:changed` and
`files:changed`), vaults, imports (running ones are stopped) with their stored
passwords, upload sessions and their parts, sessions, device tokens, WebDAV
app passwords, device logins, pairing and QR sign-in codes, passkeys and 2FA.
Rows go in one transaction; files on disk are removed after it commits.

## Admin — `/api/admin` (admin session or phone)

A browser session or the phone app's token (`android`/`ios`) of an admin
account; the admin flag is read on every request. Anything else, desktop
tokens included, gets `403 ADMIN_REQUIRED`.

| Method | Path | Body |
| --- | --- | --- |
| GET | `/health` · `/logs` · `/audit` · `/version` | `/health`: counts, disks, `database` (`ok`\|`damaged`\|`pending`, the last daily `quick_check`), `databaseCheckedAt`, `damagedBlobs` (weekly blob scrub), `blobScrubCycleAt`, `minFreeDiskBytes` |
| GET | `/users?q=` | `{ users: [{ id, username, displayName, email, isAdmin, disabled, disabledAt, usedBytes, quotaBytes, quotaMode, fileCount, aiMonthlyTokens, createdAt, lastLogin }] }`. `aiMonthlyTokens`: the account's own AI token budget (`null` follows the server default, `0` is no limit) |
| PUT | `/users/:id/quota` | `{ mode: "default"\|"unlimited"\|"custom", bytes? }` (`"50GB"` works) |
| PUT | `/users/:id/admin` | `{ isAdmin }`: a boolean; anything else `400 INVALID_VALUE` |
| POST | `/users/:id/password` | `{ password }` — signs the account out everywhere: browser sessions (and their sockets), device tokens, WebDAV app passwords, approved device logins and open pairing codes |
| POST | `/users/:id/usage/recalculate` | |
| PUT | `/users/:id/disabled` | `{ disabled }`: a boolean (else `400 INVALID_VALUE`) → the user. Disabling ends the account's sessions and device tokens at once (they stay ended after enabling); while disabled every sign-in (password, 2FA, passkey, QR sign-in, device login, pairing, device security key, WebDAV) answers `403 ACCOUNT_DISABLED` once the credential is right, any session or token still held answers `401`, and its share links answer `404`. `400 CANNOT_DISABLE_SELF`, `409 LAST_ADMIN` |
| DELETE | `/users/:id/sessions` | Sign out everywhere: ends every browser session (and socket) and revokes every device token and WebDAV app password → `{ success, sessions, devices }` (counts). For your own account the session or phone asking stays |
| DELETE | `/users/:id` | Not for admins; deletes the account like `POST /api/account/delete` |
| GET / PUT | `/config/storage` | `defaultQuotaBytes`, `maxUploadBytes`, `trashRetentionDays`, `versionRetention` |
| POST | `/storage/recalculate` | |
| GET | `/access` · PUT `/access/signup` | `{ allowSignup }` — off refuses every registration but the first account's |
| POST | `/keys/master` (5 / 15 min) | `{ password, code? }` → `{ masterKey }`; `code` (TOTP or recovery code) is required when the admin has 2FA on: `401 WRONG_PASSWORD`, `TWO_FACTOR_REQUIRED`, `INVALID_CODE` |
| GET | `/backups` | `{ targets, runs }`; secrets come back only as `passwordSet` etc. Each target carries `lastRun` and `lastVerification` (below, or null). A run has `bytesUploaded`/`blobsUploaded` so far and, once it has compared with the target, `bytesTotal`/`blobsTotal` it has to upload |
| POST | `/backups/targets` | `{ name, type: local\|sftp\|s3\|webdav, config, schedule: manual\|daily\|weekly, hour, weekday, retention }` |
| PUT / DELETE | `/backups/targets/:id` | an empty secret keeps the stored one |
| POST | `/backups/targets/:id/test` · `/backups/targets/:id/run` | run answers 202 and works in the background; `404 NOT_FOUND` for an unknown target, `409 BACKUP_RUNNING` while a run to it is going (in the server or `neocloud backup run`) |
| POST | `/backups/targets/:id/verify` | checks the newest snapshot → `{ verifiedAt, ok, snapshot, database, blobsChecked, missingBlobs, error }` |
| GET | `/backups/targets/:id/snapshots` | finished snapshots, newest first |
| GET | `/providers` | `{ providers: [{ key, label, type: "key"\|"url", configured, hint }] }`: AI provider env keys; `hint` is the masked key (`sk-a••••1234`) or the URL |
| PUT | `/providers` | `{ key, value }`: sets one (`""` clears it). `400 UNKNOWN_PROVIDER`, `400 INVALID_URL`. URLs may be on the LAN |
| GET | `/models` | `{ models: [model], disabledModels, autoEnableNewModels }`: the server's catalog (its own credentials); records newly seen models and, with auto-enable off, disables them |
| PUT | `/models/config` | `{ disabledModels: [id], autoEnableNewModels? }` → `{ ok, disabledModels, autoEnableNewModels }` |
| GET / PUT | `/users/:id/allowed-models` | PUT `{ modelIds: [id] \| null }`: models granted to the account although disabled for everyone; `400 UNKNOWN_MODEL` for an id not in the catalog. GET answers `{ modelIds, monthlyTokens }`, `monthlyTokens` being the account's budget override as in the user list (`null`: server default, `0`: no limit) |
| PUT | `/users/:id/ai-budget` | `{ monthlyTokens }`: a positive number, `0` (no limit) or `null` (server default) → `{ userId, monthlyTokens }` |
| GET / PUT | `/ai` | `{ enabled, monthlyTokens }`: AI on/off for the server (default off) and the default monthly token budget per account (`null`: none). Each field optional on PUT |
| GET | `/ai/usage` | This month: `{ since, users: [{ userId, username, tokens, calls, errors, limit }], features: [{ feature, tokens, calls }], models: [{ provider, model, tokens, calls, avgLatencyMs }] }` |

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
| GET / PUT | `/api/admin/config` | `{ fullTextSearch: bool }` | Admin session or phone. Off by default (`NEOCLOUD_FULL_TEXT_SEARCH` in `.env`). Off pauses the indexer and search ignores content; the index is kept |
| GET | `/api/admin/search-index` | | Admin session or phone. `{ enabled, indexed, pending, failed, bytes, available: { pdftotext, office } }`. `bytes`: encrypted texts plus FTS pages. `office` is always `true` (built-in zip reader) |
| POST | `/api/admin/search-index/rebuild` | | Admin session or phone. `202` with the status: the index is emptied and every file read again in the background |
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
