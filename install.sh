#!/usr/bin/env bash
# NeoCloud installer: downloads the neocloud binary for this computer from the
# latest GitHub release and runs `neocloud install`. Existing data, including a
# Node.js installation's, is backed up and kept. The steps live in the shared
# NeoLabs installer; this only names the app.
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/install.sh)
# A specific release: NEOCLOUD_VERSION=v0.2.0 bash <(curl -fsSL ...)

set -euo pipefail

export APP_NAME=NeoCloud
export APP_CMD=neocloud
export APP_REPO=NeoLabs-Systems/NeoCloud
export APP_ENV=NEOCLOUD
export APP_DATA=data/neocloud.db
export APP_TARGETS="macos-x64 macos-arm64 linux-x64 linux-arm64"

SHARED="https://raw.githubusercontent.com/NeoLabs-Systems/neolabs-go/f16ae36573abfb21225dcc274d0ab98e46038773/scripts/install-cli.sh"
if ! INSTALLER="$(curl -fsSL "$SHARED")" || [[ -z "$INSTALLER" ]]; then
  echo "Could not download the installer. Check the network and try again." >&2
  exit 1
fi
exec bash -c "$INSTALLER" neocloud-install
