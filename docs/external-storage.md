# Cloud storage (external storage)

Each account can attach **its own** cloud storage and browse it as a folder
next to its files: S3-compatible buckets (AWS, Backblaze B2, Wasabi, MinIO),
WebDAV (another Nextcloud, a NAS, Hetzner Storage Box) and SFTP. There are no
admin-defined mounts and no server paths or local disks.

## What it is, and is not

- **Pass-through.** Files stay at the provider. Browsing lists the folder
  there, opening streams the bytes through the server, uploads stream the
  other way. Nothing is copied into NeoCloud's blob store unless the person
  copies a file (Copy to NeoCloud / Copy to cloud storage).
- **Invisible to everything that reads the tree.** Mounts are not nodes: sync
  clients (`clients/lite`), the photos indexer, AI, full-text search, shares,
  versions, trash and backups never see external files. The server's backup
  includes only the (encrypted) connection settings.
- **Version 1** supports: browse, open/download, upload, new folder, rename
  and move, delete (at the provider, not recoverable here), copy of single
  files in both directions, and a read-only switch per connection. No
  previews through the preview pipeline yet (images open inline), no folder
  copy, no sharing of external files.

## Security model

- **Credentials** are one JSON document per connection, sealed with the
  account's own data key (AES-256-GCM, bound to the account and the
  connection id). The API only ever returns whether a secret is set.
- **Network.** Hosts are typed by users, so the server refuses loopback,
  private, link-local and cloud-metadata addresses by default. The name is
  resolved once, every address is checked and the socket connects to exactly
  those addresses (no DNS rebinding); IP literals are checked up front. For a
  Nextcloud or MinIO on the home network set
  `NEOCLOUD_EXTERNAL_ALLOW_PRIVATE=true` (admin accounts may always connect to
  private addresses). Metadata addresses (169.254.0.0/16, 100.100.100.200)
  stay refused either way.
- **Redirects** from a WebDAV server are never followed (the password would
  travel with them); the user is told where the server wants to go.
- **SFTP** remembers the host key (SHA-256) from the first successful test and
  requires it afterwards; symbolic links are not listed.
- **Paths** are normalised on the server; `..` and control characters are
  refused, and each provider confines everything below the chosen root
  (S3 prefix, WebDAV URL, SFTP folder).
- **Limits.** Ten connections per account, listings capped at 5000 entries,
  folder moves and deletes on S3 at 5000 objects, eight simultaneous provider
  operations per account, upload size cap
  (`NEOCLOUD_EXTERNAL_MAX_UPLOAD_BYTES`), rate limits on testing and on
  operations, timeouts on connecting and on stalled transfers.
- Files from a provider are always served sandboxed (`Content-Security-Policy:
  sandbox`, `nosniff`) and as attachments unless they are raster images,
  audio or video.

## Adding a provider

A provider is one module in `server/services/external_storage/adapters/` that
exports `type`, `fields`, optional `validate`, `hostOf` and `create`, and is
listed in `adapters/index.js` (the adapter interface is described there). The
settings form on the clients is the only other place to add.
