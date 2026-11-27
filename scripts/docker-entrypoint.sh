#!/bin/sh
# Arguments are neocloud commands (`serve` by default). The forms the Node.js
# image took keep working: `neocloud <command>` and `node server/index.js`.
set -eu

[ "$#" -gt 0 ] || set -- serve
case "$1" in
  sh|bash|/bin/sh|/bin/bash) exec "$@" ;;
  neocloud) shift; [ "$#" -gt 0 ] || set -- help ;;
  node) set -- serve ;;
esac

home="${NEOCLOUD_HOME:-/data}"
if ! touch "$home/.write-test" 2>/dev/null; then
  echo "neocloud: $home is not writable by uid $(id -u)." >&2
  echo "neocloud: fix the volume ownership, e.g." >&2
  echo "neocloud:   docker run --rm -v neocloud_neocloud-data:/data alpine chown -R 1000:1000 /data" >&2
  exit 1
fi
rm -f "$home/.write-test"

exec neocloud "$@"
