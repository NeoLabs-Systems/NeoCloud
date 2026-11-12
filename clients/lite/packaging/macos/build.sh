#!/usr/bin/env bash
# Builds NeoCloud.app (universal, menu bar only) and
# NeoCloud-<tag>-macos.dmg into <outdir>. Needs a Mac (cgo for the tray).
# The app is signed ad hoc only: until it is signed with a Developer ID
# and notarised, first open it with right-click › Open.
#   packaging/macos/build.sh v1.2.3 dist
source "$(dirname "$0")/../common.sh"
cd "$LITE"
for arch in arm64 amd64; do
  CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" \
    -o "$WORK/neocloud-$arch" ./cmd/neocloud-lite
done

app="$OUT/NeoCloud.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
lipo -create -output "$app/Contents/MacOS/NeoCloud" "$WORK/neocloud-arm64" "$WORK/neocloud-amd64"
chmod 755 "$app/Contents/MacOS/NeoCloud"
sed -e "s/@NUMVERSION@/$NUMVERSION/g" -e "s/@TAG@/$TAG/g" packaging/macos/Info.plist.in > "$app/Contents/Info.plist"
plutil -lint "$app/Contents/Info.plist" >/dev/null

iconset="$WORK/NeoCloud.iconset"
mkdir "$iconset"
for size in 16 32 128 256 512; do
  sips -z $size $size packaging/icons/macos-1024.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  sips -z $((size * 2)) $((size * 2)) packaging/icons/macos-1024.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns -o "$app/Contents/Resources/NeoCloud.icns" "$iconset"

codesign --force --sign - --timestamp=none "$app"
codesign --verify --strict "$app"

stage="$WORK/dmg"
mkdir "$stage"
cp -R "$app" "$stage/"
ln -s /Applications "$stage/Applications"
dmg="$OUT/NeoCloud-$TAG-macos.dmg"
rm -f "$dmg"
hdiutil create -volname NeoCloud -srcfolder "$stage" -fs HFS+ -format UDZO -ov "$dmg" >/dev/null
echo "$dmg"
