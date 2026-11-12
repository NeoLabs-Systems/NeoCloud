#!/usr/bin/env bash
# Builds NeoCloud-<tag>-linux-x86_64.AppImage into <outdir> (best effort).
# The app adds its own XDG autostart entry, pointing at the AppImage file.
# Uses appimagetool from PATH, else downloads the pinned release.
#   packaging/linux/build.sh v1.2.3 dist
source "$(dirname "$0")/../common.sh"
cd "$LITE"
appdir="$WORK/NeoCloud.AppDir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/applications" "$appdir/usr/share/icons/hicolor/512x512/apps"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" \
  -o "$appdir/usr/bin/neocloud" ./cmd/neocloud-lite
cp packaging/linux/neocloud.desktop "$appdir/neocloud.desktop"
cp packaging/linux/neocloud.desktop "$appdir/usr/share/applications/"
cp packaging/icons/neocloud-512.png "$appdir/neocloud.png"
cp packaging/icons/neocloud-512.png "$appdir/usr/share/icons/hicolor/512x512/apps/neocloud.png"
ln -s usr/bin/neocloud "$appdir/AppRun"

tool="$(command -v appimagetool || true)"
if [ -z "$tool" ]; then
  tool="$WORK/appimagetool"
  curl -fsSL -o "$tool" https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-x86_64.AppImage
  chmod +x "$tool"
fi
image="$OUT/NeoCloud-$TAG-linux-x86_64.AppImage"
ARCH=x86_64 VERSION="$TAG" APPIMAGE_EXTRACT_AND_RUN=1 "$tool" --no-appstream "$appdir" "$image"
echo "$image"
