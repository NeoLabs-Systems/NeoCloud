# Sourced by the packaging scripts. Sets LITE (clients/lite), TAG,
# NUMVERSION (1.2.3 from v1.2.3-beta.4) and LDFLAGS.
set -euo pipefail
LITE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:?usage: build.sh <tag> <outdir> [arch]}"
NUMVERSION="${TAG#v}"
NUMVERSION="${NUMVERSION%%-*}"
[[ "$NUMVERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || NUMVERSION="0.0.0"
LDFLAGS="-s -w -X github.com/neolabs/neocloud/clients/lite/internal/settingsui.buildVersion=$TAG"
mkdir -p "${2:?usage: build.sh <tag> <outdir> [arch]}"
OUT="$(cd "$2" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
