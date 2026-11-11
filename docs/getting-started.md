# Installation

NeoCloud runs as one Node.js process with a SQLite database. Everything that
changes at runtime (database, encrypted files, `.env` with the master key)
lives in one data folder.

## Docker

```bash
curl -fsSLO https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/compose.yaml
docker compose up -d
```

Data lives in the `neocloud-data` volume. The port is published on
`127.0.0.1:3344` for a reverse proxy on the same host; see
[operations](operations.md) for proxies, TLS and the other options.

Images: `ghcr.io/neolabs-systems/neocloud:<tag>` with `latest` (stable),
`beta`, `X.Y.Z` and `X.Y`. Add `-office` to any tag for the variant with
LibreOffice for Office previews (`latest-office`, `beta-office`, …).

## Script

Linux or macOS with Git and Node.js 20.19+:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/install.sh)
```

The installer clones the repository, runs `neocloud setup` and installs a
launchd (macOS) or systemd user (Linux) service. Data lives in
`~/.neocloud`.

## First steps

1. Open **http://localhost:3344** (or your server's address). The first
   account you create becomes the administrator.
2. Back up the master key: `neocloud keys export`
   (Docker: `docker compose exec neocloud neocloud keys export`). Every file
   is encrypted under it; without it neither the storage folder nor any
   backup can be read again.
3. Add a backup target under **Admin › Backups**.
4. Install the clients: NeoCloud Lite for Windows, macOS and Linux and the
   Android app from the [latest release](https://github.com/NeoLabs-Systems/NeoCloud/releases/latest).
   Pair a phone by scanning the QR code under **Settings › Devices**.

## CLI

```bash
neocloud status          # is it running, where does data live
neocloud logs -f         # follow the server log
neocloud doctor          # check the installation
neocloud admin grant ana # make an account admin
neocloud keys export     # print the master key for safekeeping
neocloud backup list     # backup targets and their last run
neocloud backup restore 1 2026-11-10T03-00-00Z   # restore a snapshot (server stopped)
neocloud update          # pull the latest version and restart
neocloud rollback        # undo the last update
```

`neocloud help` lists everything. Settings live in `~/.neocloud/.env`; see
[.env.example](../.env.example) for every option.
