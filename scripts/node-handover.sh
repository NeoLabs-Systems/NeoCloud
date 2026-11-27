#!/bin/sh
# Hands a Node.js installation of NeoCloud over to the Go binary.
#
# NeoCloud's Node.js updater (`neocloud update [--tag v]`) checks out the new
# release and runs `npm ci` in it. package.json in this tree exists only so that
# npm runs this script; .npmrc makes that a dry run, so the old node_modules
# stay intact. The updater cannot run this release, so this script:
#   1. copies install.sh and itself out of the checkout (the rollback resets it),
#   2. starts the handover in the background, waiting for the updater to exit,
#   3. fails the npm ci, so the updater rolls back at once and keeps the
#      Node.js version running meanwhile.
# The handover then runs install.sh, which installs the Go binary and runs
# `neocloud install`: it downloads the release, stops the Node.js server, backs
# up the database, points the launchd/systemd service at the binary and restores
# everything if the new server does not answer.
set -u

if [ "${1:-}" = "--after" ]; then
  updater="$2"; installer="$3"; channel="$4"; version="$5"
  while kill -0 "$updater" 2>/dev/null; do sleep 2; done
  sleep 3
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) handing over to the Go binary (channel ${channel}${version:+, release ${version}})"
  # install.sh replaces the Node.js `neocloud` command before the new server has
  # proven itself, so keep what was there.
  launcher="${NEOCLOUD_BIN_DIR:-$HOME/.local/bin}/neocloud"
  saved=""
  if [ -f "$launcher" ] && [ ! -L "$launcher" ]; then
    saved="${installer}.launcher"
    cp -p "$launcher" "$saved"
  fi
  link="$(command -v neocloud 2>/dev/null || true)"
  target=""
  if [ -n "$link" ] && [ -L "$link" ]; then target="$(readlink "$link")"; fi
  NEOCLOUD_RELEASE_CHANNEL="$channel" NEOCLOUD_VERSION="$version" bash "$installer" </dev/null
  status=$?
  if [ "$status" -ne 0 ]; then
    if [ -n "$saved" ]; then cp -p "$saved" "$launcher"; fi
    if [ -n "$target" ] && [ "$(readlink "$link" 2>/dev/null)" != "$target" ]; then
      rm -f "$link"
      ln -s "$target" "$link"
    fi
    echo "The switch did not finish; NeoCloud keeps running on Node.js. Run install.sh to try again."
  fi
  rm -f "$installer" "${installer}.launcher" "$0"
  rmdir "$(dirname "$0")" 2>/dev/null
  exit "$status"
fi

# The updater is the Node.js CLI among this script's ancestors (npm and its
# shells sit in between).
updater=""
pid=$$
for _ in 1 2 3 4 5 6 7 8; do
  pid="$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')"
  [ -n "$pid" ] && [ "$pid" != "1" ] || break
  case "$(ps -o command= -p "$pid" 2>/dev/null)" in
    # Run as bin/neocloud.js or through the npm link (".../bin/neocloud update").
    *bin/neocloud.js*|*/neocloud\ update*) updater="$pid"; break ;;
  esac
done
if [ -z "$updater" ]; then
  echo "NeoCloud no longer uses npm. Install or update it with install.sh: https://github.com/NeoLabs-Systems/NeoCloud" >&2
  exit 1
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
home="${NEOCLOUD_HOME:-$HOME/.neocloud}"
env_file="${NEOCLOUD_ENV_FILE:-$home/.env}"

# The Node.js CLI exported every .env value, but its launchd service never read
# the directories from that file: the new install must not either.
if [ "$(uname -s)" = "Darwin" ] && [ -f "$HOME/Library/LaunchAgents/com.neocloud.plist" ]; then
  for key in NEOCLOUD_DATA_DIR NEOCLOUD_STORAGE_DIR NEOCLOUD_ENV_FILE; do
    if grep -q "^${key}=" "$env_file" 2>/dev/null; then unset "$key"; fi
  done
fi
logs="${NEOCLOUD_DATA_DIR:-$home/data}/logs"
mkdir -p "$logs"

# The channel the Node.js install followed: its setting, else its branch, else
# the release tag it was pinned to.
version=""
channel="$(sed -n 's/^NEOCLOUD_RELEASE_CHANNEL=//p' "$env_file" 2>/dev/null | head -n 1 | tr -d ' \r' | tr '[:upper:]' '[:lower:]')"
branch="$(git -C "$root" symbolic-ref -q --short HEAD 2>/dev/null || true)"
if [ -z "$branch" ]; then
  version="$(git -C "$root" describe --tags --exact-match 2>/dev/null || true)"
fi
if [ -z "$channel" ]; then
  case "${branch}:${version}" in
    main:*) channel="stable" ;;
    :*-beta*) channel="beta" ;;
    :v*) channel="stable" ;;
    *) channel="beta" ;;
  esac
fi
case "$channel" in beta|preview|prerelease|pre-release) channel="beta" ;; *) channel="stable" ;; esac

work="$(mktemp -d "${TMPDIR:-/tmp}/neocloud-handover.XXXXXX")"
cp "$root/install.sh" "$work/install.sh"
cp "$0" "$work/node-handover.sh"
# The old checkout must not be served by the new server.
unset NEOCLOUD_APP_DIR
nohup sh "$work/node-handover.sh" --after "$updater" "$work/install.sh" "$channel" "$version" >>"$logs/handover.log" 2>&1 &

echo "NeoCloud now runs as a single program instead of Node.js. This update rolls back first and keeps NeoCloud running; the switch then finishes by itself in the background (progress: $logs/handover.log)." >&2
exit 1
