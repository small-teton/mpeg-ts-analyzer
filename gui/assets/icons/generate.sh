#!/bin/sh
# Regenerates the GUI icons from icon.svg (macOS: QuickLook renders the SVG,
# sips resizes it, iconutil writes the macOS .icns and ImageMagick the
# Windows .ico). Run from anywhere.
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
qlmanage -t -s 1024 -o "$tmp" "$dir/icon.svg" >/dev/null
for size in 32 64 128 256 512 1024; do
  sips -z "$size" "$size" "$tmp/icon.svg.png" --out "$dir/${size}x${size}.png" >/dev/null
done
set=$tmp/icon.iconset
mkdir "$set"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$tmp/icon.svg.png" --out "$set/icon_${size}x${size}.png" >/dev/null
  double=$((size * 2))
  sips -z "$double" "$double" "$tmp/icon.svg.png" --out "$set/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns -o "$dir/icon.icns" "$set"
magick "$dir/256x256.png" -define icon:auto-resize=256,64,48,32,24,16 "$dir/icon.ico"
