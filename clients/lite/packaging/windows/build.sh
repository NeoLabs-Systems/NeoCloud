#!/usr/bin/env bash
# Builds NeoCloud.exe (tray only, no console, icon and version info) and
# the per-user installer NeoCloud-Setup-<tag>-windows-<x64|arm64>.exe into
# <outdir>. Cross-compiles with cgo off; needs makensis (apt install nsis,
# brew install makensis).
#   packaging/windows/build.sh v1.2.3 dist amd64
source "$(dirname "$0")/../common.sh"
arch="${3:-amd64}"
case "$arch" in
  amd64) label=x64 ;;
  arm64) label=arm64 ;;
  *) echo "unknown arch $arch" >&2; exit 2 ;;
esac
cd "$LITE"

# Icon, version info and manifest as a .syso next to main, linked in by go build.
syso="cmd/neocloud-lite/rsrc_windows_$arch.syso"
trap 'rm -rf "$WORK" "$syso"' EXIT
go run github.com/tc-hib/go-winres@v0.3.3 simply --arch "$arch" --out "$WORK/rsrc" \
  --icon packaging/icons/neocloud.ico --manifest gui \
  --product-name NeoCloud --file-description NeoCloud --original-filename NeoCloud.exe \
  --product-version "$TAG" --file-version "$NUMVERSION.0" --copyright "NeoLabs Systems"
cp "$WORK/rsrc_windows_$arch.syso" "$syso"

CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS -H=windowsgui" \
  -o "$WORK/NeoCloud.exe" ./cmd/neocloud-lite

setup="$OUT/NeoCloud-Setup-$TAG-windows-$label.exe"
makensis -V2 -DTAG="$TAG" -DNUMVERSION="$NUMVERSION" -DEXE="$WORK/NeoCloud.exe" \
  -DICON="$LITE/packaging/icons/neocloud.ico" -DOUTFILE="$setup" packaging/windows/installer.nsi
echo "$setup"
