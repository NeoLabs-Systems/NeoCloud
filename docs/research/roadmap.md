# NeoCloud — research findings and roadmap (Nov 2026)

Eight parallel research tracks: competitors, self-hosted pain points, web
file-browser UX, sync reliability, mobile, sharing/security, server
operations, search/organization. **200 web searches** (the per-session tool
cap) plus ~50 primary pages read in full (Dropbox Nucleus/CanopyCheck/Trinity,
rclone bisync, Syncthing, Microsoft Cloud Files, SQLite WAL/pragma docs,
OWASP password storage, Cryptomator, Proton, Bitwarden, Immich, Ente,
Android background/FGS docs, NN/g, WCAG 2.2…). Each track also audited the
NeoCloud code it covered.

## What wins and loses users (consensus across tracks)

1. **Losing files is the #1 reason people leave** (Google Drive 84.x
   rollback, Nextcloud clients wiping 53–300 GB after a reinstall or a
   selective-sync change, Syncthing mass deletes from an unmounted folder).
   A sync client must be *provably* unable to destroy data.
2. **Boring, reversible upgrades.** Nextcloud's broken updates, disabled apps
   and "restore from backup" fixes are the second most cited reason to leave.
3. **Fast and light.** Nextcloud folders take 5–30 s to load; Seafile and
   OpenCloud win on speed. Budget: listing < 200 ms, first paint < 1 s.
4. **Status that explains itself.** "Waiting to upload" with no reason
   (iCloud, Google Photos) destroys trust. Every stall needs a reason + action.
5. **Undo beats "Are you sure?"** Trash/move/rename should be instant with an
   Undo toast; confirmation only for irreversible actions.
6. **Encryption you can't lose yourself in.** Recovery kits (Proton,
   Cryptomator, Bitwarden), and E2E that never blocks login/sync/upgrades.
7. **Mobile photo backup is everyone's weak spot.** Trust needs: nothing
   skipped silently, numbers that add up, hash-idempotent re-runs, one bad file
   never blocks the rest, honest iOS/Android background limits.
8. **Stay focused.** Bloat (Talk/Mail/AI) while sync rots drives people to
   focused tools. No plugin ecosystem, no external search engine, no cloud AI.

## Defects found in NeoCloud (fixed in this wave unless noted)

| Area | Finding |
|---|---|
| Sync (both) | No mass-delete guard / no root marker: an unmounted or emptied folder trashes everything remotely. |
| Sync (both) | Remote-driven local deletes are permanent (no local trash). |
| Sync (Dart) | 0-byte same-name placeholders can be edited and uploaded over the real file. |
| Sync (Dart) | No base etag on upload; no re-stat before local delete; full local walk every 15 s. |
| Sync (both) | No fsync before rename; no NFC/case normalization; downloads restart from zero. |
| Server | Backup reads blob list *before* the DB snapshot → restored DB can reference missing blobs. |
| Server | Admin health runs a full `integrity_check` synchronously on every request. |
| Server | No fsync on blob writes; no downgrade guard; no pre-update snapshot/rollback; doctor can migrate a live DB. |
| Server | WAL unbounded; keepAliveTimeout 5 s (random proxy 502s); 5 s forced shutdown kills uploads; health check never touches DB. |
| Shares | Upload links expose the whole folder; per-link guessing unthrottled; 1-char passwords; every dialog open mints a new link; links not editable. |
| Vaults | PBKDF2 310k (OWASP 2023+: 600k); 8-char passphrase, no meter, no recovery key. |
| Web UI | No list virtualization; no undo; sequential non-atomic batch ops; no keyboard focus model; zero Semantics; muted text fails WCAG AA (4.21:1 dark, 3.51:1 light); upload queue has no pause/ETA/beforeunload. |
| Mobile | Date watermark skips late-arriving photos; first failure stops the whole backup; reinstall re-uploads as duplicates; limited photo access treated as success; drawer instead of bottom nav. |
| Privacy | Landing and share pages load Google Fonts (visitor IPs to Google; LG München 2022). |

## Roadmap

**Wave A — now (data safety, stability, the obvious UX wins)**
- Sync engines: mass-delete guard (>50 % or >500 deletes → pause + ask),
  `.neocloud/pair-id` root marker, local trash for remote-driven deletes,
  re-stat before delete, base etag on every upload, fsync-then-rename,
  NFC + case-collision detection, Range-resumed downloads, placeholder
  edits become conflict copies, local rescans split from remote polling,
  invariant/fuzz tests.
- Server: snapshot-first backups, safe restore, cached `quick_check`, fsync
  blobs, downgrade guard, pre-update snapshot + rollback, WAL limits and
  checkpoints, proxy-safe timeouts, graceful shutdown, real health check,
  scheduled-backup retry, disk-full 507, default local DB snapshots.
- Server features: share modes (view / view+upload / upload-only), editable
  links, per-link throttling, download limits; vault KDF floor 600k; search
  filters + ranking + paths; favorites; opened-at tracking for Quick Access;
  trash expiry; storage breakdown + duplicates; folder paging; self-hosted
  fonts; share page names the sharer.
- Web: undo toasts, optimistic parallel batch ops, sliver virtualization,
  delayed skeletons, keyboard focus model + type-ahead + `/` + Ctrl/⌘+K
  command palette + `?` sheet, Semantics, AA contrast, upload ETA/pause/
  resume/beforeunload, richer conflict dialog, search chips, favorites,
  storage page, share dialog rework, vault recovery key + strength meter.
- Mobile/desktop: hash-idempotent camera upload with per-file failures and
  specific statuses, run on resume, limited-access warning, "new only vs
  everything"; bottom navigation on phones; desktop conflicts list,
  "not synced" panel, mass-delete confirmation.

**Wave B — next**
Point-in-time folder restore from the change journal, per-file activity,
version pinning + age-based thinning, photo timeline, FTS5 full-text search
(opt-in, never vaults), WebDAV endpoint, QR pairing for phones, Nextcloud/
Seafile importer, notification digest, iOS background URLSession uploads,
Android foreground worker + MediaStore trigger, OS-level files on demand
(Cloud Files API / File Provider) behind a beta flag.

**Deliberately not built**
Shared/system tags, Elasticsearch/Solr, ML "priority" predictions, cloud AI
over file contents, an email per file change, a third-party plugin store.

## API contract for wave A

All routes accept session or device token unless marked.

- `GET /api/search?q=&type=image|video|audio|document|pdf|archive|folder&after=ISO&before=ISO&minSize=&maxSize=&parentId=&limit=`
  → `{ items: [node + { path }] }`. Ranking: exact name, prefix, word prefix,
  then most recently modified. `q` may be empty when a filter is set.
- `GET /api/folders/:id/children?limit=&cursor=` → existing shape +
  `nextCursor` (null when done). Default limit 500, max 2000. Ordering stays
  folders first then by the requested sort.
- Favorites: `PUT /api/nodes/:id/favorite`, `DELETE /api/nodes/:id/favorite`,
  `GET /api/favorites` → `{ items }`. Nodes carry `favorite: bool`.
- Opened tracking: `POST /api/nodes/:id/opened` (204). `GET /api/recent?by=opened|modified`
  (default `opened`, falls back to modified for never-opened files).
- Trash: `GET /api/trash` items carry `expiresAt`.
- Storage: `GET /api/storage/breakdown` → `{ total, files, trash, versions, byType: [{ type, bytes, count }], byFolder: [{ id, name, bytes }], largest: [node + path] }`;
  `GET /api/storage/duplicates` → `{ groups: [{ sha256, size, nodes: [node + path] }], reclaimable }`.
- Shares: `POST /api/shares` body gains `mode: "view" | "view_upload" | "upload_only"` (replaces
  `allowUpload`, which stays accepted as `view_upload`), `maxDownloads?`,
  `note?` (≤ 500 chars); passwords ≥ 8 chars. `PATCH /api/shares/:id`
  `{ expiresAt?, password? (null clears), mode?, maxDownloads?, note? }` keeps
  the token. `GET /api/shares?nodeId=` filters to one node's active links.
  Public meta adds `sharedBy`, `host`, `note`, `mode`; upload-only returns no
  `items`. Per-token unlock throttle: exponential delay after 5 failures,
  `429 SHARE_LOCKED` after 10 in 30 min. Download limit reached → `410 SHARE_EXHAUSTED`.
- Vaults: `POST /api/vaults` and `PUT /api/vaults/:id/key` require
  `iterations ≥ 600000` (existing vaults keep working; clients re-wrap on unlock).
- Errors: disk below `NEOCLOUD_MIN_FREE_DISK` (default 1 GiB) → uploads get `507 DISK_FULL`.
- Health: `GET /api/health` → `{ status: "ok" | "degraded", db: "ok", storage: "ok" }`, 503 when not ok.
