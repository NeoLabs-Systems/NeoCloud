#!/usr/bin/env bash
# NeoCloud installer
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/NeoLabs-Systems/NeoCloud/main/install.sh)
# A specific release: NEOCLOUD_VERSION=v0.2.0 bash <(curl -fsSL ...)

set -euo pipefail

if [[ -t 1 ]]; then
  BOLD='\033[1m'; RESET='\033[0m'; RED='\033[1;31m'; GRN='\033[1;32m'; YEL='\033[1;33m'; DIM='\033[2m'
else
  BOLD=''; RESET=''; RED=''; GRN=''; YEL=''; DIM=''
fi

ok()   { echo -e "  ${GRN}✓${RESET}  $*"; }
info() { echo -e "  ${YEL}→${RESET}  $*"; }
err()  { echo -e "  ${RED}✗${RESET}  $*" >&2; }

echo -e "${YEL}${BOLD}NeoCloud installer${RESET}"
echo

MISSING=()
command -v git  &>/dev/null && ok "git $(git --version | awk '{print $3}')" || MISSING+=("git")
command -v node &>/dev/null && ok "Node.js $(node --version)"                || MISSING+=("node 20.19+ (https://nodejs.org)")
command -v npm  &>/dev/null && ok "npm $(npm --version)"                     || MISSING+=("npm")
if [[ ${#MISSING[@]} -gt 0 ]]; then
  err "Missing requirements:"
  for m in "${MISSING[@]}"; do echo "     • $m"; done
  exit 1
fi

INSTALL_DIR="${NEOCLOUD_INSTALL_DIR:-$HOME/NeoCloud}"
BRANCH="${NEOCLOUD_BRANCH:-main}"
# A release tag (v0.2.0) pins the install to it; `neocloud update --tag <next>` moves it.
VERSION="${NEOCLOUD_VERSION:-}"
REPO="https://github.com/NeoLabs-Systems/NeoCloud.git"

if [[ -d "$INSTALL_DIR/.git" ]]; then
  ORIGIN_URL="$(git -C "$INSTALL_DIR" remote get-url origin 2>/dev/null || true)"
  if [[ ! "$ORIGIN_URL" =~ NeoLabs-Systems/NeoCloud ]]; then
    err "${INSTALL_DIR} is a git repository but not a NeoCloud checkout (origin: ${ORIGIN_URL:-none})."
    exit 1
  fi
  info "Updating ${INSTALL_DIR}..."
  if [[ -n "$VERSION" ]]; then
    git -C "$INSTALL_DIR" fetch --tags origin
    git -C "$INSTALL_DIR" checkout --detach "$VERSION"
  else
    git -C "$INSTALL_DIR" fetch origin "$BRANCH"
    git -C "$INSTALL_DIR" checkout "$BRANCH"
    git -C "$INSTALL_DIR" pull --ff-only origin "$BRANCH"
  fi
else
  info "Cloning ${VERSION:-$BRANCH} into ${INSTALL_DIR}..."
  git clone --branch "${VERSION:-$BRANCH}" "$REPO" "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"
info "Installing dependencies..."
# Exactly what package-lock.json pins, so every install runs the tested tree.
npm ci --omit=dev --no-audit --no-fund
npm link >/dev/null 2>&1 || info "npm link failed; run the CLI as: node ${INSTALL_DIR}/bin/neocloud.js"

node bin/neocloud.js setup
node bin/neocloud.js start

echo
ok "Done. Open ${BOLD}http://localhost:3344${RESET} and create the first account ${DIM}(it becomes the admin)${RESET}."
