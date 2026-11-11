# WebDAV

Every app that speaks WebDAV can open your NeoCloud files directly.

- **Address:** `https://cloud.example.com/dav/files/<username>/`
  (Nextcloud apps may also use `/remote.php/dav/files/<username>/` or
  `/remote.php/webdav/`).
- **Password:** an app password, not your login password. Create one under
  **Settings › Devices › Connect apps (WebDAV)** and copy it; it is shown
  once. Revoke it there any time.

| App | How |
| --- | --- |
| **macOS Finder** | *Go › Connect to Server* (⌘K), enter the address, sign in with user name and app password |
| **Windows Explorer** | *This PC › Map network drive*, folder = the address (Windows needs HTTPS for Basic sign-in) |
| **rclone** | `rclone config create neocloud webdav url=<address> vendor=owncloud user=<username> pass=$(rclone obscure <app password>)`. Use `vendor=owncloud`; `nextcloud` expects Nextcloud's own chunking and checksum endpoints |
| **Obsidian** | the *Remotely Save* plugin, service *WebDAV*, the address plus a vault folder |
| **Joplin** | *Synchronisation › WebDAV*, the address plus a folder such as `Joplin/` (create it first) |
| **KeePassXC** | open the database from the Finder or Windows drive above; KeePass2Android, Strongbox and KeeWeb open it over WebDAV directly |
| **Infuse** | *Add Files › Share › WebDAV*, your server, path `/dav/files/<username>/` |

Deleted files go to the trash and overwritten files keep their versions, as
in the web app. Vault folders stay hidden from WebDAV; their contents are
encrypted on your devices and only the NeoCloud apps can open them. Method
details are in the [API reference](api.md).
