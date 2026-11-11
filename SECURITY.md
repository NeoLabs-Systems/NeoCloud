# Security Policy

## Supported Versions

Security fixes go into the latest release. Update before reporting against an
older one.

## Report a Vulnerability Privately

Do not open a public issue for a suspected vulnerability. Use GitHub's
[private vulnerability reporting](https://github.com/NeoLabs-Systems/NeoCloud/security/advisories/new).

Include the affected version or commit, the component, reproduction steps or a
minimal proof of concept, and the impact you observed. Use test accounts and
redact logs; never include live credentials or real personal files.

## Security Scope

Especially useful:

- Reading, changing or deleting another account's files (IDOR).
- Reaching more through a share between accounts than it grants: files outside
  the shared folder, writing with read access, or finding out which accounts
  exist.
- Authentication, 2FA or device-token bypass.
- Getting plaintext out of the blob store without the master key, or
  tampering with a blob without detection.
- Quota bypass, or filling the server's disk past the limits.
- Script execution through a stored file served by NeoCloud.

## What the encryption covers

Files are encrypted at rest with keys derived from `NEOCLOUD_MASTER_KEY`. That
protects stolen disks and leaked backups. It does not protect against an
attacker who controls the running server or its `.env` file.

## What the full-text index reveals

Full-text search is off unless an admin turns it on, and each account can
opt out (its entries are removed). When it is on, the server reads the text
of the account's files (never vault files, never trashed ones) into the
database:

- The FTS5 table is contentless: it stores no copy of the text, no word
  positions (`detail=column`), and reading it returns nothing. But an index
  has to know its words: **anyone holding the database file, or a copy of
  it, can see which words occur in which file and how often, without the
  master key.** File names and folder structure are in the database in clear
  anyway; the index adds the vocabulary of the contents.
- The text kept for result snippets is deflated and encrypted with the
  account's data key (AES-256-GCM, bound to the file's id), like the blobs.
- Extraction happens in memory (a worker thread, or `pdftotext` reading
  stdin); no plaintext is written to disk for it.
- Database backups sent to backup targets are encrypted with the backup key,
  so the index leaves the server only in that form.

If the word list of your files must not sit in the database in readable
form, leave full-text search off or opt out; vault folders are never indexed.

## Sharing between accounts

Every file route that takes a node id from a member resolves it in one place
(`server/services/members/access.js`): a node someone else owns is reachable
only when a live, unexpired grant to the caller sits on it or on a folder
above it, with nothing up to the owner's root in the trash, and never when
it is in a vault. Anything else answers the same `404` as a missing node.
The check runs on every request, so removing a member or leaving a share
takes effect on the next request, including resumable uploads in progress.

- Members see breadcrumbs from the shared folder down only; the owner's
  folders above it, other files, vaults, versions, favorites, search, sync
  and trash stay hidden.
- A member's writes are stored as the owner's (key, quota, node limit); the
  shared folder itself can't be renamed, moved or deleted by a member, and
  moves can't cross its border. A copy across it re-encrypts every file
  under the receiving account and is charged to that account.
- Members can't re-share, create links or manage members (`404`).
- Recipients are named by exact email or username only; there is no account
  search or listing. Unknown names, the owner's own name and accounts that
  can't receive shares get one identical `404 RECIPIENT_NOT_FOUND`, and
  adding members is limited to 30 tries per account in 15 minutes. The owner
  sees a member's display name and what they typed, never the member's other
  identifier.
- Deleting the owner, the member or the shared node removes the share
  (foreign-key cascades).

## Imports and private networks

An import makes the server fetch a URL the user typed. To stop plain
accounts from probing the server's own network, every request and redirect
hop is checked against the addresses the host resolves to: loopback,
private, link-local, CGNAT and multicast ranges are refused with
`403 IMPORT_HOST_BLOCKED` unless the account is an admin or
`NEOCLOUD_IMPORT_ALLOW_PRIVATE=true`.

The address is checked again when the connection is made: the importer's
connections resolve the name once, refuse it if any address is private, and
connect to exactly the checked address, so a name that changes its answer
between the check and the connection (DNS rebinding) gets nowhere.
Credentials only follow redirects to the same origin. Folder listings are
capped at 16 MB and parsed in linear time, and a scan stops the job past
250,000 entries, 64 folder levels or 4,096-character paths.

## Office previews

LibreOffice converts Office documents to PDF from a decrypted copy in a
private temporary folder. It is not a full sandbox; what is done:

- The converter gets a minimal environment (`PATH`, `LANG`, and `HOME` and
  `TMPDIR` in the work folder); the master key and session secret never
  reach it.
- A fresh profile turns off link updates in Writer and Calc (which also
  stops external data and `WEBSERVICE` from refreshing), the loading of
  linked or remote graphics, macros (security level "very high") and active
  content, and the update check.
- It runs in its own process group; on timeout the whole group is killed
  before the render slot is freed.
- On Linux it runs without network access when `bwrap` or `unshare` can
  create a network namespace (`NEOCLOUD_PREVIEW_SANDBOX=auto`, the default;
  `off` skips it). If neither works, the server logs a warning and the
  converter keeps the server's network access.

It runs as the server's user and can read what that user can read. Set
`NEOCLOUD_OFFICE_PREVIEW=false` to turn Office previews off.

## Backup targets

Whoever can write a backup target cannot read it, and should not be able to
harm the server through it either. The database object is authenticated
(AES-GCM bound to the snapshot); a header with any segment size but the one
NeoCloud writes, or a stream that ends without an authenticated segment, is
refused. A decrypted snapshot must be a NeoCloud database (not empty, with a
schema version and a users table) before verify passes or restore uses it.
The manifest is plain JSON: it is read with a 64 MB cap, and every blob
entry must be `<user id>/<blob id>` resolving inside the blob folder, or
restore, verify and prune refuse the snapshot. Listings from WebDAV and SFTP
targets refuse names that are empty, `.`, `..` or contain a slash; WebDAV
listings are size-capped, depth- and entry-bounded, and redirects are not
followed.

## Sign-in, credentials and proxies

Failed sign-ins are counted per account, not per address: from the 5th wrong
password or code in 15 minutes each try waits twice as long, the 10th locks
sign-in for the rest of that window (never longer, since anyone who knows a
username can cause it), and a pending two-factor sign-in takes at most 5
codes. Replacing an enrolled second factor needs a current code. A password
change signs out every other session and revokes all device tokens and WebDAV
app passwords; an admin password reset does the same for every session.
Ending a session also closes its live socket.io connections. With an https
`PUBLIC_URL` and no `TRUST_PROXY`, only proxies on loopback or private
addresses may set `X-Forwarded-For`; keep port 3344 reachable from the proxy
only (see docs/operations.md).
