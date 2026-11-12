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
   - `GET /api/account/devices/authorize/:userCode` (session or phone app) → `{ name, platform, createdAt, expiresAt }`
   - `POST /api/account/devices/authorize/:userCode` (session or phone app) `{ approve: true|false }` → `{ success: true }`
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
  `*.tmp`, `*.ncpart` (unless the pair turns `use_default_ignores` off).
  Pairs add their own patterns, and `.cloudignore` files add more (see
  "Ignoring files" below). Always ignored on both sides, not a
  pattern and not removable: a `.neocloud` file or folder in any case
  (`.NeoCloud`, …), macOS folder-icon files (`Icon\r`) anywhere and the
  `desktop.ini` at the pair's root (the folder icon on Windows).
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
  more than `delete_guard_percent` (50) % of the pair's tracked files or
  more than `delete_guard_files` (500), or when the
  server folder is suddenly empty while files are tracked (`remoteEmpty`,
  any count), no deletion runs: the pair waits with
  `needsConfirmation { remoteDeletes, localDeletes, reason: massDelete|remoteEmpty, digest }`
  (`digest`: SHA-256 over the held deletions, side and path).
  Everything else in the pass (uploads, downloads, renames) still runs, and
  folders whose deletion waits are not recreated. The user approves with
  `neocloud-lite pair confirm-deletes <id>` or on the lite app window.
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
  taken). Day folders older than `trash_retention_days` (30; 0 keeps them)
  are pruned at the start of a pass.
  A folder deleted on the server moves as a whole, ignored files included,
  when everything in it may go (unchanged tracked files, ignored files);
  one that only holds empty folders and OS junk (`.DS_Store`, `Thumbs.db`,
  `desktop.ini`, `Icon\r`) is removed, and those are the only files ever removed
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
| `tooLarge` | a file over the pair's `max_file_size_mb` |
| `excluded` | an excluded folder kept locally because it holds files that are not in NeoCloud |

The lite client lists them in `status --json` (`pairs[].report.notSynced`)
and in its app window.

### Folder settings (lite)

Each `[[pairs]]` entry in `config.toml` can carry these (TOML names; the
app window shows them per folder, the rarer ones under "Advanced"). An
unset value means the default, so older config files keep their behaviour.

| Setting | Default | Does |
| --- | --- | --- |
| `mode` | `mirror` | Full local copies. `on_demand` is not available yet; a hand-edited one loads as `mirror` and the API refuses it. |
| `exclude_paths` | `[]` | Selective sync: folders (relative to the pair's remote folder) that are not synced. A local copy this client synced moves to the local trash; one holding anything not in NeoCloud stays (`excluded`), as does every local folder of an upload-only pair. The cloud copy is never touched. |
| `new_folders` | `ask` for the whole cloud, else `sync` | A folder that appears at the top of the remote folder after the first sync: `sync`, `ask` (held until the user answers; "Don't sync" adds it to `exclude_paths`), or `skip` (excluded at once). |
| `big_folder_prompt_mb` | global (500) | A new top-level folder bigger than this asks even with `sync`; 0 turns it off. |
| `propagate_deletes` | `true` | `false` never deletes on the receiving side: with `up` a backup, with `down` an archive, with `both` a deleted file comes back from the other side. |
| `use_default_ignores` | `true` | The built-in ignore list above. |
| `include_hidden` | `true` | `false` skips names starting with `.` on both sides and, on Windows, local files marked hidden or system (left alone, never deleted). |
| `max_file_size_mb` | 0 (any) | Bigger files are not synced (`tooLarge`). |
| `symlinks` | `skip` | Links are never followed (`symlink`). |
| `conflict` | `keep_both` | Both sides changed a file (two-way pairs): `keep_both` (conflict copy), `prefer_local`, `prefer_remote` or `newer` (modification times). The losing version goes to the local trash. |
| `trash_retention_days` | 30 | 0 keeps the local trash forever. |
| `delete_guard_files` / `delete_guard_percent` | global (500 / 50) | The mass-delete guard. |
| `respect_network_rules` | `true` | `false` keeps the pair syncing on metered networks and in battery saver. |

App-wide, at the top of `config.toml`: `big_folder_prompt_mb` (500),
`delete_guard_files` (500), `delete_guard_percent` (50),
`pause_on_metered` (`true`; Windows via the connection cost, Linux via
NetworkManager; not detected on macOS), `pause_on_battery_saver` (`true`;
Windows battery saver, macOS Low Power Mode, Linux power-profiles-daemon's
power-saver), `notifications` (`errors`: failed runs, held deletions,
conflicts, new folders waiting; `all` adds a line per run that changed
files; `off`), `file_manager_sidebar` (`true`) and `proxy` (`system`:
`HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY`, else the macOS or Windows proxy
settings; `none`; `manual` with `proxy_url`, `http://`, `https://` or
`socks5://`). Loopback servers never go through a proxy. With `system`,
hosts on the Windows "don't use the proxy for" list go direct, and a system
proxy that can't be reached (checked every two minutes) is skipped, so a
work proxy left on at home doesn't block syncing. The sign-in screen has the
same choice under "Connection settings".

#### Ignoring files (`.cloudignore`)

A `.cloudignore` works like a `.gitignore`, with git's syntax
(https://git-scm.com/docs/gitignore). One can sit in any folder of a pair,
the root included; its patterns are relative to that folder.

- Order: the default list and the pair's `ignore` patterns come first (the
  analogue of `.git/info/exclude`), then the root's `.cloudignore`, then
  those of deeper folders. The last pattern that matches decides, so a
  deeper file overrides a shallower one and `!pattern` re-includes.
- Syntax: blank lines and `#` comments are skipped; `\#` and `\!` start a
  pattern with those characters; trailing spaces are dropped unless quoted
  with `\`. A pattern without a slash matches a name at any depth; a
  leading or middle slash anchors it to the file's folder; a trailing slash
  matches folders only. `*`, `?` and `[...]` (ranges, `[!...]`, `[^...]`,
  `[:alpha:]`-style classes) never match a slash; `**/` at the start,
  `/**` at the end and `/**/` in the middle match across folders.
  Matching is case-sensitive. A UTF-8 BOM and CRLF line ends are fine.
- As in git, a path inside an ignored folder can't be re-included, and the
  `.cloudignore` of an ignored folder is not read.
- The `.cloudignore` files sync like any other file, so every device uses
  the same rules, and a conflict on one is handled like any file. Patterns
  never ignore a `.cloudignore` itself, and `include_hidden = false` doesn't
  hide it either (on Windows, nor does the hidden attribute); ignoring its
  folder does.
- Both sides use the same rules: for each `.cloudignore` a pass applies the
  version it is about to leave on both sides (the local one when it wins or
  both agree, the cloud's when that wins, none when it is being deleted).
  A cloud version that wins is fetched before anything else is decided, so
  a `.cloudignore` that only exists in the cloud (a cloud-first pair, an
  edit on another device) applies before the files it ignores would be
  downloaded. If it can't be fetched the pass stops without acting.
- When a change ignores files that were synced, they stop syncing and both
  copies stay as they are: nothing is deleted on either side, and the
  pass forgets their state rows. When they are un-ignored, both sides merge
  like a first sync: equal files are recorded, one-sided ones copied, and
  only files that really differ become conflict copies. Nothing is deleted
  either way.

Paths a `.cloudignore` keeps out are listed per pair, at most 200, without
the paths inside an ignored folder (`pairs[].report.ignored`, `{ path,
reason: "ignored", detail }`, detail like `Ignored by .cloudignore (line 3:
*.log)`). They are not part of `notSynced`. The folder editor's "Ignore
patterns" section opens the root `.cloudignore` in the text editor, creating
it from a short commented template when there is none.
`neocloud-lite check-ignore <path>...` prints, like `git check-ignore -v`,
whether each path is ignored and by which rule (`<source>:<line>:<pattern>`,
the source being a `.cloudignore`, `folder settings` or `default
patterns`); it reads the `.cloudignore` files on this computer.

Files a pass could not sync are listed per pair
(`pairs[].report.failed`: `{ path, detail, both }`) next to `notSynced`
and the new folders waiting (`newFolders`: `{ path, size }`). The app
window's issues view retries one, or, when it exists on both sides, keeps
this computer's version, the cloud's or both.

### Scheduling

The journal is polled every `poll_seconds` (15) and a pass runs only for
pairs it touches; fsnotify events (1.5 s settle) and the `rescan_seconds`
timer (5 min) cover local changes.

### App window (lite)

`neocloud-lite run` serves its UI on `127.0.0.1` at a random port, which
every local user can reach, so every request needs a session. "Open
NeoCloud" in the tray mints a one-time launch URL (`/launch?code=…`, valid
for 2 minutes; `run --headless` prints one that stays valid until used);
the code works once and is exchanged for an `HttpOnly`, `SameSite=Strict`
session cookie, and no lasting secret ever appears on a command line. API
calls also need an `X-Requested-With` header and are refused from another
site (`Sec-Fetch-Site`) or another `Host`. The page and its files
(`internal/settingsui/ui`, embedded) run under a CSP that allows only this
server's own scripts, styles, images and fonts (plus the QR code's `data:`
URI); nothing is loaded from the network. The page wears the NeoLabs theme
of the Flutter app (NeoRouter's tokens on NeoCloud's palette, light and dark
from the system, Geist and Geist Mono embedded). `/launch?code=…&view=settings`
opens it on Settings (the tray's "Settings…").

The URL opens as a small app window: the first Chromium browser found
(Chrome, Chromium, Edge, Brave; in `/Applications` and `~/Applications` on
macOS, under `%LOCALAPPDATA%` / `%ProgramFiles%` on Windows, on `PATH` on
Linux) is started with `--app=<launch URL> --window-size=440,680` and its own
profile in `<config dir>/app-window`, never the user's browsing profile.
Without one the default browser opens the URL. A click within 3 s of the
last one is ignored; a newer window supersedes the older one, which closes
itself. The window opens by itself when the client starts signed out.

Screens: welcome (server address, optional username, "Continue" for the
device-code flow and "Sign in with security key", §10), approval (the code, a QR code and a
button that opens the approval page in the user's own browser), home
(account, storage, overall status as a dot plus a word, "needs attention"
cards for held-back deletions, errors and not-synced items, the folder
list with per-folder actions, the last 20 changes), the add-folder wizard
(first a choice: "A folder from this computer" or "Files from NeoCloud";
see below), folder editing (direction, the folder tree for selective sync,
new folders, and the folder settings above under "Advanced"), the issues
view and settings (start at login, pause, sidebar entry, notifications,
network rules, proxy, bandwidth, guard and big-folder defaults, security
key lock, account, version, logs). The page polls `/api/state` every 2 s. Recent activity is
kept in memory only; the log file has the full history.

The two wizard flows:

- **A folder from this computer**: an existing local folder (native folder
  dialog: `osascript` on macOS, a PowerShell `FolderBrowserDialog` on
  Windows, `zenity`/`kdialog`/`yad` on Linux, a text field otherwise), then
  a server folder browser that can create folders, then the direction and
  optional ignore patterns. The local folder must exist; it is never
  created or restyled.
- **Files from NeoCloud** (cloud-first): a server folder, or the root
  ("Sync everything"), then the local folder the app creates for it and,
  under "Choose folders", which folders inside to sync and what to do with
  new ones:
  `~/NeoCloud` for the whole cloud, `~/NeoCloud/<folder name>` for one
  folder (`~/NeoCloud <name>` when `~/NeoCloud` is a pair itself). A taken
  name gets ` 2`, ` 3`…; the proposal never overlaps a pair. "Change
  location" picks another parent folder. Then the direction (both ways by
  default). The first pass merges into the empty folder: it downloads
  everything and deletes nothing (no state rows, so nothing counts as
  deleted and the mass-delete guard has nothing to count). The CLI does the
  same with `pair add <local> <remote> --create` (the folder must not
  exist yet).

Folders the app created (`created = true` on the pair) get the NeoCloud
look, best effort (failures are logged, the pair works anyway): the logo as
folder icon (macOS: `NSWorkspace setIcon`, which writes `Icon\r` and the
folder's FinderInfo; Windows: a hidden system `desktop.ini` pointing at
`neocloud.ico` in the config folder, folder marked read-only; Linux: `gio
set metadata::custom-icon`), also on a `NeoCloud` folder made to hold it.
The NeoCloud folder of the first such pair is added to the Finder sidebar
Favorites (`LSSharedFileList`), the Explorer navigation pane (a per-user
delegate folder, CLSID `{9F4DC68E-FECA-4260-9BF2-A1C29417C317}`, removed by
the uninstaller too) or the GTK bookmarks, and taken out again when no
created pair is left or `file_manager_sidebar` is turned off. The client remembers
what it added (`pinned` in the config), so an entry the user removes by
hand is not added back.

Local API (all need the session and the header):

| Method | Path | Does |
| --- | --- | --- |
| GET | `/api/state` | Config, engine status, login, key lock, activity, version |
| POST · DELETE | `/api/login` | Start a device-code sign-in · stop waiting for it |
| POST | `/api/logout` | Sign out |
| GET | `/api/account` | `{ used, limit }` from `/api/devices/me`, cached 30 s |
| POST | `/api/pairs` · PUT/DELETE `/api/pairs/:id` | Add (`create: true` makes the local folder) · edit (direction, ignore, paused; `settings`, the folder settings in camelCase, replaces them all) · remove a folder |
| POST | `/api/pairs/:id/new-folders` | `{ path, sync }`: answer a new-folder prompt |
| POST | `/api/pairs/:id/resolve` | `{ path, choice }`: retry a failed file (`""`), or keep `local`, `remote` or `both` |
| GET | `/api/local/suggest?remote=&parent=` | `{ path }`: the folder the cloud-first flow would create for `remote` inside `parent` (default location when empty) |
| POST | `/api/pairs/:id/sync` · `/open` · `/confirm-deletes` | Sync one folder · open it in the file manager · approve held-back deletions |
| POST | `/api/pairs/:id/cloudignore` | Open the folder's root `.cloudignore` in the text editor, creating it first when missing (`/api/state` has `cloudIgnore`: pair id → whether it exists) |
| POST | `/api/sync` | Sync everything |
| POST | `/api/pick-folder` (`?for=parent`) | Native folder dialog → `{ path }`, `{ cancelled }` or `{ unsupported }` |
| GET | `/api/remote/folders?id=` or `?path=` | Server folders inside `id` or `path` (root by default), vaults left out |
| POST | `/api/remote/folders` | `{ parentId, name }`: create (or reuse) a server folder |
| POST | `/api/open/approval` · `/web` · `/logs` | Open the pending approval page, the web app or the log file |
| PUT | `/api/settings` | Pause, autostart, bandwidth, device name; `global` replaces the app-wide settings (`/api/state` shows them with defaults filled in) |
| POST · DELETE | `/api/key-lock` | Turn the security key lock on · off |

The tray menu, in the app window's words: the status line, Open NeoCloud,
Open web app, Sync now, Pause syncing / Resume syncing, "Open <folder>" for
up to five synced folders, Settings… and Quit NeoCloud. Signed out, Open web
app, Sync now and Pause are greyed out. Its icon (the logo's glyph, a template image
in the macOS menu bar) carries a badge for syncing, paused, locked and
needs attention.

### Security key lock (lite)

Optional, macOS and Linux only: with it on, the lite client syncs only
while one FIDO2 security key is plugged in and locks when it is removed.
Turn it on in the app
window or with `neocloud-lite key enroll` (`key status`, `key remove`); quit
the tray app before using the CLI for it, because the running app would
write back its older copy of the config.

- **Enroll.** Exactly one key plugged in, one touch: a CTAP2
  `makeCredential` for a non-resident ES256 credential with `hmac-secret`
  (rpId `neocloud-lite.local`; nothing is stored on the key), then a
  `getAssertion` with a random 32-byte salt. The device token is sealed
  with AES-256-GCM under HKDF-SHA-256 of the hmac-secret output (the server
  URL is the additional data). `config.toml` keeps `[key_lock]`
  (`rp_id`, `credential_id`, `salt`, `sealed_token`) and no plaintext
  `token`.
- **PIN.** A key with a PIN usually needs it to create the credential: the
  app window and the CLI ask for it (clientPIN `getPinToken`, PIN
  protocol 1); it is used once and never stored. Unlocking never sends a
  PIN, so the key uses its "without UV" secret and unlocks without one. A
  key set to `alwaysUv` cannot unlock automatically and is refused.
- **Unlock.** Every 2 s the client lists FIDO HID devices. A new one is
  asked for the hmac-secret output without a touch (`up=false`); a key that
  refuses gets the tray and app window showing "touch your security key
  to unlock" and one `up=true` try (30 s). The token is unsealed into memory
  only and sync resumes. A key without the credential counts as absent and
  is not asked again until it is unplugged; so is one that was not touched
  in time.
- **Lock.** When the key that unlocked disappears, the token is dropped
  from memory, a run in progress is cancelled and the engine reports
  `locked` ("Locked" in the tray) until the key returns. Go strings cannot
  be zeroed, so the dropped token stays in freed memory until it is
  reused. One-shot commands (`sync --once`) unlock with the key themselves,
  `status` does not touch it.
- **Turn off** (`key remove`, or the app window) needs the key: the
  token is unsealed once more and written back in the clear. Signing out
  or signing in again also turns the lock off (it sealed the old token).

Per OS:

| OS | Transport | Status |
| --- | --- | --- |
| Linux | `/dev/hidraw*`, found by the FIDO usage page `0xF1D0` in the sysfs report descriptor (pure Go) | Supported. The user needs access to the hidraw node: systemd 244+ tags security keys `uaccess` itself (`60-fido-id.rules`); on older systems install libfido2's [`70-u2f.rules`](https://github.com/Yubico/libfido2/blob/main/udev/70-u2f.rules) into `/etc/udev/rules.d/` and replug the key. |
| macOS | IOKit HID (cgo, as the tray) | Supported. |
| Windows | — | Left out: since Windows 10 1903 only administrators may open FIDO HID devices, and webauthn.dll has no `up=false` (every assertion shows the Windows Security dialog and needs a touch), so it cannot unlock silently while the key is plugged in. `/api/state` reports `keyLock.supported: false`, the app window has no security key section, `neocloud-lite key …` answers that the lock is not available on this platform, and a `[key_lock]` in a config copied from another computer is ignored at start (logged; the client is signed out until it signs in again, which drops the section from the file). |

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
scans it and approves. The Go lite client shows it in its app window and
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

- `POST /api/imports` (session or phone app) `{ source: "nextcloud"|"webdav", url, username, password, sourcePath?: "/", targetFolderId?, includeFavorites?: true }`
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

## 10. Security key sign-in

A native client can sign in with one of the account's passkeys or security
keys instead of the device-code flow. The keys are registered in the web
app (WebAuthn, RP ID = the server's host), so the client is its own
WebAuthn client and builds `clientDataJSON` exactly as a browser on the
server's origin would.

1. `POST /api/devices/webauthn/options` (no auth, rate-limited) `{ username? }`
   → `{ challengeId, origin, options }`. Without a username `allowCredentials`
   is empty and the key picks a discoverable credential; with one, it lists
   that account's keys (an unknown name gets an empty list too).
   `origin` and `options.rpId` come from `PUBLIC_URL` (else the address the
   request came in on), never from an `Origin` header. The challenge is kept
   on the server under `challengeId` for 5 minutes.
2. The client checks that `origin` is the address the user typed and that
   `rpId` is its host or a parent domain (the browser rule), builds
   `{"type":"webauthn.get","challenge":<options.challenge>,"origin":<origin>,"crossOrigin":false}`
   and has the key sign its SHA-256 (`getAssertion`, user presence always;
   user verification for `preferred`/`required` when the key can).
3. `POST /api/devices/webauthn/verify` `{ challengeId, response, name, platform }`,
   `response` being the browser's `PublicKeyCredential` JSON (base64url
   fields). The challenge is spent by this call whatever its outcome. As in
   the browser sign-in, a key that verified the user stands in for the
   second factor; a presence-only key on an account with 2FA answers
   `{ requiresTwoFactor: true, challengeId }`, and
   `POST /api/devices/webauthn/2fa` `{ challengeId, code }` finishes it.
4. The answer is the token response of §1; the client stores it as after
   the device-code flow.

**Lite client** (`internal/securitykey`): on macOS (IOKit) and Linux
(hidraw) it talks CTAP2 to the key itself (`internal/fido`): `getInfo`,
then a PIN token (`clientPIN` protocol 1) when the key has a PIN, or the
`uv` option for a key with a fingerprint reader, then `getAssertion` and
`getNextAssertion` for further discoverable credentials (the first, newest
one signs in; a typed username narrows it to that account). Exactly one key
may be plugged in; the app waits up to 20 s for one. On Windows, where only
administrators may open FIDO HID devices, `webauthn.dll`
(`WebAuthNAuthenticatorGetAssertion`, API version 1 or later, Windows 10
1903+) runs the ceremony and shows the Windows Security dialog, which asks
for the PIN itself; the client still supplies its own `clientDataJSON`.

App window: under "Continue" the welcome screen has an optional username
and "Sign in with security key". The page posts `{ server, username, pin }`
to `POST /api/login/key`, which waits for the touch ("Touch your security
key", with Cancel) and answers `{ success: true }`, `{ next: "pin" }` (the
key has a PIN: a PIN field appears and the next try sends it; a wrong PIN
answers `{ next: "pin", error }`) or `{ next: "code" }` (the authenticator
code, sent to `POST /api/login/key/code { code }`). Errors are plain words:
no key found, more than one key, key not registered on this account,
cancelled, not touched in time. The PIN is used for that one request and
never stored.
