> [!WARNING]
> **Work in progress.** NeoCloud is under active development and not ready to be used yet. Don't trust it with files you can't lose.

<h1 align="center">NeoCloud</h1>

<p align="center"><strong>Your files. Your server. Encrypted.</strong></p>

<p align="center">
  Self-hosted file storage for the whole household or team.<br>
  One process, one database, every file encrypted.
</p>

<p align="center">
  <a href="https://github.com/NeoLabs-Systems/NeoCloud/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/NeoLabs-Systems/NeoCloud?style=flat-square&label=release&color=111827&include_prereleases"></a>
  <a href="LICENSE"><img alt="AGPL-3.0 license" src="https://img.shields.io/badge/license-AGPL--3.0-a855f7?style=flat-square"></a>
  <a href="docs/getting-started.md#docker"><img alt="Docker image" src="https://img.shields.io/badge/docker-ghcr.io-2496ed?style=flat-square&logo=docker&logoColor=white"></a>
  <a href="https://nodejs.org"><img alt="Node.js 22 or newer" src="https://img.shields.io/badge/node-22%2B-5fa04e?style=flat-square&logo=nodedotjs&logoColor=white"></a>
</p>

<p align="center">
  <a href="https://github.com/NeoLabs-Systems/NeoCloud/releases/latest"><img alt="macOS" src="https://img.shields.io/badge/macOS-Lite-000000?style=for-the-badge&logo=apple&logoColor=white"></a>
  <a href="https://github.com/NeoLabs-Systems/NeoCloud/releases/latest"><img alt="Windows" src="https://img.shields.io/badge/Windows-Lite-0078d4?style=for-the-badge&logo=windows&logoColor=white"></a>
  <a href="https://github.com/NeoLabs-Systems/NeoCloud/releases/latest"><img alt="Linux" src="https://img.shields.io/badge/Linux-Lite-fcc624?style=for-the-badge&logo=linux&logoColor=black"></a>
  <a href="https://github.com/NeoLabs-Systems/NeoCloud/releases/latest"><img alt="Android" src="https://img.shields.io/badge/Android-APK-3ddc84?style=for-the-badge&logo=android&logoColor=white"></a>
  <img alt="iOS coming soon" src="https://img.shields.io/badge/iOS-coming_soon-6b7280?style=for-the-badge&logo=apple&logoColor=white">
</p>

<p align="center">
  <a href="https://discord.gg/f59rg2RwUT"><img src="https://img.shields.io/badge/Join%20NeoLabs-Discord-5865F2?style=for-the-badge&amp;logo=discord&amp;logoColor=white" alt="Join the NeoLabs Discord"></a>
</p>

<table>
  <tr>
    <td width="33%" valign="top">
      <h3>🔐 Encrypted</h3>
      Every file encrypted at rest.<br>
      End-to-end vault folders on top.
    </td>
    <td width="33%" valign="top">
      <h3>🪶 Lightweight</h3>
      One Node process, one SQLite file.<br>
      No PHP, no plugin store.
    </td>
    <td width="33%" valign="top">
      <h3>👥 Multi-user</h3>
      Accounts, quotas, 2FA, passkeys and an admin page.
    </td>
  </tr>
</table>

---

## 🚀 Install

| | | |
| --- | --- | --- |
| 🐳 **Docker** | Home servers, NAS boxes and VPS. | [Guide](docs/getting-started.md#docker) |
| 📜 **Script** | Linux or macOS with Git and Node.js 22+. | [Guide](docs/getting-started.md#script) |

**Docker**

```bash
curl -fsSLO https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/compose.yaml
docker compose up -d
```

**Script**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/install.sh)
```

Then open **http://localhost:3344** (or your server's address). The first account you create becomes the administrator. Back up the master key right away with `neocloud keys export`: without it no file or backup can be read again.

## ✨ What it does

<table>
  <tr>
    <td width="33%" valign="top"><b>📁 Files</b><br>Drag & drop, whole folders, resumable uploads, search inside documents.</td>
    <td width="33%" valign="top"><b>🕓 Versions & trash</b><br>Earlier versions kept, deletes recoverable, undo everywhere.</td>
    <td width="33%" valign="top"><b>👁️ Viewers</b><br>Photos, PDF, Office, text, video and audio in the browser.</td>
  </tr>
  <tr>
    <td valign="top"><b>🔄 Sync</b><br>NeoCloud Lite tray client for Windows, macOS and Linux.</td>
    <td valign="top"><b>📱 Mobile</b><br>Android app with photo backup and share-to-NeoCloud. Pair by QR code.</td>
    <td valign="top"><b>🔗 Sharing</b><br>Links with expiry, password, download limits and upload-only requests.</td>
  </tr>
  <tr>
    <td valign="top"><b>💾 Backups</b><br>Encrypted and incremental to a NAS, SFTP, S3 or WebDAV.</td>
    <td valign="top"><b>📂 WebDAV</b><br>Finder, Explorer, rclone, Obsidian and KeePassXC with app passwords.</td>
    <td valign="top"><b>📦 From Nextcloud</b><br>One import copies files, dates and favorites over.</td>
  </tr>
</table>

## 📚 Docs

| Start | Use | Run |
| --- | --- | --- |
| [Installation](docs/getting-started.md) | [Clients](docs/clients.md) | [Operations](docs/operations.md) |
| [Move from Nextcloud](docs/nextcloud-import.md) | [WebDAV](docs/webdav.md) | [Encryption](docs/encryption.md) |
| [Development](docs/development.md) | [API reference](docs/api.md) | [Security](SECURITY.md) |

## 🧪 Project status

- **Beta.** Maintained mainly by one person. Expect breaking changes before 1.0.
- **Help.** [Issues](https://github.com/NeoLabs-Systems/NeoCloud/issues) for bugs, [SECURITY.md](SECURITY.md) for vulnerabilities.

## 📄 License

[AGPL-3.0-only](LICENSE).

<p align="center"><sub>Made with ❤️ by <a href="https://github.com/neooriginal">Neo</a> · <a href="https://github.com/NeoLabs-Systems">NeoLabs Systems</a></sub></p>
