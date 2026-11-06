#!/bin/sh
# Writes the third-party notices shipped in the GUI packages: the Rust crates
# of the GUI (cargo-about, gui/about.toml) and the Go standard library and
# modules compiled into the bundled CLI for any released target.
#
# usage: third-party-notices.sh <output file> [<CLI directory>]
#
# The Go modules are read from the CLI binaries under <CLI directory> (as the
# GUI release workflow provides them, one per target). Without it, the CLI is
# built from this tree for each released target.
set -eu
out=$1
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cargo about generate --locked \
  --manifest-path "$root/gui/Cargo.toml" \
  --config "$root/gui/about.toml" \
  "$root/gui/about.hbs" > "$out"

if [ $# -ge 2 ]; then
  bin=$2
else
  bin=$work/bin
  for target in darwin/arm64 linux/amd64 windows/amd64; do
    CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} \
      go -C "$root" build -trimpath -o "$bin/${target%/*}-${target#*/}/mpeg-ts-analyzer" .
  done
fi
clis=$(find "$bin" -type f -name 'mpeg-ts-analyzer*' ! -name '*.*' -o -type f -name 'mpeg-ts-analyzer*.exe')
[ -n "$clis" ] || { echo "no CLI binaries in $bin" >&2; exit 1; }

# The Go version and modules each CLI binary is built with, as recorded in it.
# The set depends on the target: cobra uses mousetrap only on Windows.
info=$(for cli in $clis; do go version -m "$cli"; done)
goversions=$(printf '%s\n' "$info" | awk 'NR == 1 || /^[^\t]/ { print $2 }' | sort -u | tr '\n' ' ')
deps=$(printf '%s\n' "$info" | awk '$1 == "dep" { print $2 "@" $3 }' | sort -u)
mkdir "$work/mod"
printf 'module notices\n' > "$work/mod/go.mod"
{
  printf '\n\nThird-party software in the bundled mpeg-ts-analyzer CLI\n'
  printf '========================================================\n'
  printf '\n--------------------------------------------------------------------------------\n'
  printf 'Go standard library (%s)\n\n' "${goversions% }"
  cat "$(go -C "$root" env GOROOT)/LICENSE"
  for dep in $deps; do
    dir=$(go -C "$work/mod" mod download -json "$dep" | awk -F'"' '$2 == "Dir" { print $4 }')
    [ -n "$dir" ] || { echo "could not download $dep" >&2; exit 1; }
    found=
    for file in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
      [ -f "$file" ] || continue
      found=1
      printf '\n--------------------------------------------------------------------------------\n'
      printf '%s %s (%s)\n\n' "${dep%@*}" "${dep#*@}" "$(basename "$file")"
      cat "$file"
    done
    if [ -z "$found" ]; then
      echo "no license file found for $dep in $dir" >&2
      exit 1
    fi
  done
} >> "$out"
