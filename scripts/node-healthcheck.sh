#!/bin/sh
# Installed as `node`: compose files written for the Node.js image check health
# with `node -e "fetch(… /api/health …)"`. Only that call is answered.
case "${1:-} ${2:-}" in
  "-e "*/api/health*) exec neocloud healthcheck ;;
esac
echo "neocloud: this image has no Node.js; use 'neocloud healthcheck'." >&2
exit 127
