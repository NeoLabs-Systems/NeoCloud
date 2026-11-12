#!/usr/bin/env bash
# Renders the app icons committed next to this script from the logo
# (flutter_app/assets/branding/logo.svg). Run it on a Mac with Google
# Chrome when the logo changes; the packaging scripts use the results.
#   macos-1024.png   the tile on Apple's icon grid (824 px inside 1024)
#   neocloud-512.png the tile edge to edge (Linux, Windows)
#   neocloud.ico     16 to 256 px, PNG-compressed entries (Windows)
# The PNG and the .ico are also copied to internal/shellfolder, which
# embeds them as the icon of the folders the app creates.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
logo="$here/../../../../flutter_app/assets/branding/logo.svg"
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# render <canvas px> <tile px> <out.png>
render() {
  local offset=$(( ($1 - $2) / 2 ))
  cat > "$work/page.html" <<HTML
<!doctype html><html><body style="margin:0;background:transparent">
<img src="file://$logo" style="position:absolute;left:${offset}px;top:${offset}px;width:$2px;height:$2px">
</body></html>
HTML
  "$chrome" --headless --disable-gpu --hide-scrollbars --allow-file-access-from-files \
    --default-background-color=00000000 --window-size="$1,$1" \
    --user-data-dir="$work/profile" --screenshot="$3" "file://$work/page.html" 2>/dev/null
}

render 1024 824 "$here/macos-1024.png"
render 512 512 "$here/neocloud-512.png"
for size in 16 24 32 48 64 128 256; do
  sips -z "$size" "$size" "$here/neocloud-512.png" --out "$work/$size.png" >/dev/null
done
python3 - "$work" "$here/neocloud.ico" <<'PY'
import struct, sys
work, out = sys.argv[1], sys.argv[2]
sizes = [16, 24, 32, 48, 64, 128, 256]
images = [open(f"{work}/{s}.png", "rb").read() for s in sizes]
header = struct.pack("<HHH", 0, 1, len(sizes))
offset = 6 + 16 * len(sizes)
entries = b""
for size, data in zip(sizes, images):
    entries += struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(data), offset)
    offset += len(data)
open(out, "wb").write(header + entries + b"".join(images))
PY
cp "$here/neocloud-512.png" "$here/../../internal/shellfolder/neocloud.png"
cp "$here/neocloud.ico" "$here/../../internal/shellfolder/neocloud.ico"
echo "rendered into $here"
