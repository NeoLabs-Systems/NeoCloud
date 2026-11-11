<h1 align="center">NeoCloud</h1>

<p align="center"><strong>Your files. Your server. Encrypted.</strong></p>

<p align="center">
  Self-hosted file storage for the whole household or team.<br>
  Lightweight, fast, and part of the NeoLabs family.
</p>

<p align="center">
  <a href="LICENSE"><img alt="AGPL-3.0 license" src="https://img.shields.io/badge/license-AGPL--3.0-a855f7?style=flat-square"></a>
  <a href="https://nodejs.org"><img alt="Node.js 20.19 or newer" src="https://img.shields.io/badge/node-20.19%2B-5fa04e?style=flat-square&logo=nodedotjs&logoColor=white"></a>
  <a href="compose.yaml"><img alt="Docker" src="https://img.shields.io/badge/docker-compose-2496ed?style=flat-square&logo=docker&logoColor=white"></a>
  <img alt="Status: early beta" src="https://img.shields.io/badge/status-early%20beta-e3b655?style=flat-square">
</p>

<p align="center">
  <a href="https://discord.gg/f59rg2RwUT"><img src="https://img.shields.io/badge/Join%20NeoLabs-Discord-5865F2?style=for-the-badge&amp;logo=discord&amp;logoColor=white" alt="Join the NeoLabs Discord"></a>
</p>

<table>
  <tr>
    <td width="33%" valign="top">
      <h3>🔐 Encrypted at rest</h3>
      Every file is AES-256-GCM encrypted on disk, with its own key per account.
    </td>
    <td width="33%" valign="top">
      <h3>🪶 Lightweight</h3>
      One Node process and one SQLite file. No PHP, no app store, no update treadmill.
    </td>
    <td width="33%" valign="top">
      <h3>👥 Multi-user</h3>
      Accounts, quotas per user, an admin page, 2FA. From day one.
    </td>
  </tr>
</table>

---

## 🚀 Install

| | | |
| --- | --- | --- |
| 🐳 **Docker** | Home servers, NAS boxes and VPS. | [compose.yaml](compose.yaml) |
| 📜 **Script** | Linux or macOS with Git and Node.js 20.19+. | [install.sh](install.sh) |

**Docker**

```bash
curl -fsSLO https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/compose.yaml
docker compose up -d
```

**Script**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/install.sh)
```

Then open **http://localhost:3344** (or your server's address). The first account you create becomes the administrator.

> [!IMPORTANT]
> Back up your master key right after installing: `neocloud keys export` (Docker: `docker compose exec neocloud neocloud keys export`). Every file is encrypted under it. Without it, neither your storage folder nor any backup of it can be read again.

## ✨ What it does

<table>
  <tr>
    <td width="33%" valign="top"><b>📁 Files & folders</b><br>Upload by drag & drop, whole folders included. Rename, move, copy, search, recent files.</td>
    <td width="33%" valign="top"><b>⏯️ Big files, any connection</b><br>Resumable uploads in 8 MB parts, checksummed. Downloads support seeking for video and PDFs.</td>
    <td width="33%" valign="top"><b>🕓 Versions & trash</b><br>Replacing a file keeps earlier versions. Deleted items wait in the trash before they go.</td>
  </tr>
  <tr>
    <td valign="top"><b>💾 Backups</b><br>Incremental and encrypted, to a NAS, SFTP, S3 or WebDAV. Daily or weekly, with retention.</td>
    <td valign="top"><b>🔄 Built for sync</b><br>A change journal and device tokens for the Android and iOS app and the desktop lite client.</td>
    <td valign="top"><b>👁️ Built-in viewers</b><br>Photos, PDF, Office, text, video and audio open right in the app.</td>
  </tr>
  <tr>
    <td valign="top"><b>📊 Quotas</b><br>A default for everyone, more or less for single accounts, or unlimited.</td>
    <td valign="top"><b>🌍 Deutsch & English</b><br>The whole app in both languages, light and dark.</td>
    <td valign="top"><b>🛡️ Hardened</b><br>Per-account isolation, CSRF and origin checks, rate limits. Downloads carry a sandbox CSP; Office previews run LibreOffice without the server's secrets, with macros and external links off, and without network access where the host supports it (bwrap or unshare on Linux).</td>
  </tr>
</table>

## 📦 Move from Nextcloud

NeoCloud copies your Nextcloud files over by itself, folders, modification
dates and favorites included.

1. In Nextcloud, open **Settings › Security**, enter a name like
   "NeoCloud import" under *Devices & sessions* and click **Create new app
   password**. Copy the password it shows. (Use an app password, not your
   login password; it also works with two-factor sign-in, and you can revoke
   it there when the move is done.)
2. Start the import with your Nextcloud address (`https://cloud.example.com`),
   your user name and that app password (API: `POST /api/imports`, see
   [docs/api.md](docs/api.md#imports--apiimports-session-only)).
3. Files land in a folder `Nextcloud`. The import runs in the background,
   survives restarts and can be paused and resumed. Running it again later
   only fetches what is new or changed.

The password is stored encrypted while the import runs and deleted when it
ends. Share links, shares with other users, versions, trash, comments and tags
stay in Nextcloud; only your current files and folders move. Any other WebDAV
server works too (`source: "webdav"` with the folder URL).

## 🔌 Connect apps via WebDAV

Every app that speaks WebDAV can open your NeoCloud files directly.

- **Address:** `https://cloud.example.com/dav/files/<username>/`
  (Nextcloud apps may also use `/remote.php/dav/files/<username>/` or
  `/remote.php/webdav/`).
- **Password:** an app password, not your login password. Create a device
  of type *WebDAV* under **Settings › Devices** (API:
  `POST /api/account/devices { "name": "Laptop Finder", "platform": "webdav" }`)
  and copy the token it shows once. Revoke it there any time.

| App | How |
| --- | --- |
| **macOS Finder** | *Go › Connect to Server* (⌘K), enter the address, sign in with user name and app password |
| **Windows Explorer** | *This PC › Map network drive*, folder = the address (Windows needs HTTPS for Basic sign-in) |
| **rclone** | `rclone config create neocloud webdav url=<address> vendor=owncloud user=<username> pass=$(rclone obscure <app password>)`, then `rclone sync ~/Photos neocloud:Photos`. Use `vendor=owncloud` (keeps modification times); `nextcloud` expects Nextcloud's own chunking and checksum endpoints |
| **Obsidian** | the *Remotely Save* plugin, service *WebDAV*, the address plus a vault folder |
| **Joplin** | *Synchronisation › WebDAV*, the address plus a folder such as `Joplin/` (create it first) |
| **KeePassXC** | open the database from the Finder/Windows drive above; KeePass2Android, Strongbox and KeeWeb open it over WebDAV directly |
| **Infuse** | *Add Files › Share › WebDAV*, your server, path `/dav/files/<username>/` |

Deleted files go to the trash and overwritten files keep their versions, as
in the web app. Vault folders stay hidden from WebDAV; their contents are
encrypted on your devices and only the NeoCloud apps can open them.

## 🗺️ Roadmap

| Phase | What |
| --- | --- |
| **v1** | Server, encryption, quotas, uploads, versions, trash, admin, CLI |
| **v1** | Web app under `/app` with the NeoLabs login, sidebar, settings and admin. Viewers for images, PDF, Office, text, audio and video |
| **v1** | Encrypted, incremental backups to a folder or NAS, SFTP, S3-compatible storage and WebDAV, with schedules and retention |
| **Phase 2** | Android and iOS (the Flutter app): browse, view, automatic photo upload, share to NeoCloud (Android) |
| **Phase 3** | NeoCloud Lite, the desktop client: a single small tray executable for sync (Windows, macOS, Linux) |
| **Phase 4** | End-to-end encrypted vault folders, sharing links, editor |

## ⌨️ CLI

```bash
neocloud status          # is it running, where does data live
neocloud logs -f         # follow the server log
neocloud doctor          # check the installation
neocloud admin grant ana # make an account admin
neocloud keys export     # print the master key for safekeeping
neocloud backup list     # backup targets and their last run
neocloud backup restore 1 2026-11-10T03-00-00Z   # restore a snapshot (server stopped)
neocloud update          # pull the latest version and restart
```

`neocloud help` lists everything. Settings live in `~/.neocloud/.env`; see [.env.example](.env.example) for every option.

## 🔐 How the encryption works

```
NEOCLOUD_MASTER_KEY (in .env, generated once)
 └─ wraps a random data key per account
     └─ derives a key per file blob
         └─ AES-256-GCM in 64 KB segments, each one authenticated
```

Segments are bound to their file, their position and whether they are the last one, so a blob can't be altered, reordered, swapped or cut short without detection. Any byte range decrypts on its own, which is what lets videos seek.

Backups copy the encrypted blobs as they are and encrypt the database snapshot too, so a backup target never sees a file name or a byte of content.

This protects against a stolen disk, a leaked backup or a nosy storage provider. It does not protect against someone who controls the running server; that is what the planned end-to-end vault folders are for.

## 🧑‍💻 Development

```bash
npm install
npm run dev     # server with --watch on port 3344
npm test        # unit, integration and security tests
npm run check   # syntax check every server file
```

The Flutter app lives in [`flutter_app/`](flutter_app) and targets the web, Android and iOS; the desktop is served by the lite client below. For local work, run it against the dev server:

```bash
cd flutter_app
flutter run -d chrome --dart-define=NEOCLOUD_BACKEND_URL=http://localhost:3344
```

Production serves the web build under `/app` from `server/public` (`npm run flutter:build:web`).

### Desktop client (NeoCloud Lite)

[`clients/lite/`](clients/lite) is the desktop client for Windows, macOS and Linux, a Go tray executable (Go 1.26+, cgo for the tray on macOS):

```bash
cd clients/lite
go build -o neocloud-lite ./cmd/neocloud-lite
./neocloud-lite login --server http://localhost:3344
./neocloud-lite pair add ~/NeoCloud /Documents --direction both
./neocloud-lite run              # tray icon; `run --headless` for servers
go test ./... && go test -tags e2e ./e2e/...   # e2e spawns a real server
```

Builds per target (cgo is needed only for the macOS tray, so macOS builds on a Mac):

```bash
CGO_ENABLED=1 GOOS=darwin  GOARCH=arm64 go build ./cmd/neocloud-lite   # also amd64; needs Xcode CLT
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/neocloud-lite
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build ./cmd/neocloud-lite   # also arm64
```

The settings page opens through a one-time link: the tray's *Settings…* mints one valid for 2 minutes, `run --headless` prints one that stays valid until it is used.

Read [GUIDELINES.md](GUIDELINES.md) before contributing. The [API reference](docs/api.md) lists every endpoint.

## 🧪 Project status

- **Early beta.** Maintained mainly by one person. Expect breaking changes before 1.0.
- **Help.** Issues for bugs, [SECURITY.md](SECURITY.md) for vulnerabilities.

## 📄 License

[AGPL-3.0-only](LICENSE).

<p align="center"><sub>Made with ❤️ by <a href="https://github.com/neooriginal">Neo</a> · <a href="https://github.com/NeoLabs-Systems">NeoLabs Systems</a></sub></p>
