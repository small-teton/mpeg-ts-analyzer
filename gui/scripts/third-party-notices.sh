#!/bin/sh
# Writes the third-party notices shipped in the GUI packages: the Rust crates
# of the GUI (cargo-about, gui/about.toml) and the Go standard library and
# modules compiled into the bundled CLI for any released target.
#
# usage: third-party-notices.sh <output file>
set -eu
out=$1
root=$(cd "$(dirname "$0")/../.." && pwd)
bin=$(mktemp -d)
trap 'rm -rf "$bin"' EXIT

cargo about generate --locked \
  --manifest-path "$root/gui/Cargo.toml" \
  --config "$root/gui/about.toml" \
  "$root/gui/about.hbs" > "$out"

# The modules each released CLI binary is built with, as recorded in it. The
# set depends on the target: cobra uses mousetrap only on Windows.
for target in darwin/arm64 linux/amd64 windows/amd64; do
  CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} \
    go -C "$root" build -trimpath -o "$bin/${target%/*}-${target#*/}" .
done
deps=$(for cli in "$bin"/*; do go version -m "$cli"; done |
  awk '$1 == "dep" { print $2 }' | sort -u)
{
  printf '\n\nThird-party software in the bundled mpeg-ts-analyzer CLI\n'
  printf '========================================================\n'
  printf '\n--------------------------------------------------------------------------------\n'
  printf 'Go standard library (%s)\n\n' "$(go -C "$root" env GOVERSION)"
  cat "$(go -C "$root" env GOROOT)/LICENSE"
  for dep in $deps; do
    go -C "$root" mod download "$dep"
    dir=$(go -C "$root" list -m -f '{{.Dir}}' "$dep")
    version=$(go -C "$root" list -m -f '{{.Version}}' "$dep")
    found=
    for file in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      [ -f "$file" ] || continue
      found=1
      printf '\n--------------------------------------------------------------------------------\n'
      printf '%s %s (%s)\n\n' "$dep" "$version" "$(basename "$file")"
      cat "$file"
    done
    if [ -z "$found" ]; then
      echo "no license file found for $dep in $dir" >&2
      exit 1
    fi
  done
} >> "$out"
