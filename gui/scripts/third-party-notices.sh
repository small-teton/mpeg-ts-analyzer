#!/bin/sh
# Writes the third-party notices shipped in the GUI packages: the Rust crates
# of the GUI (cargo-about, gui/about.toml) and the Go standard library and
# modules compiled into the bundled CLI.
#
# usage: third-party-notices.sh <bundled CLI binary> <output file>
set -eu
cli=$1
out=$2
root=$(cd "$(dirname "$0")/../.." && pwd)

cargo about generate --locked \
  --manifest-path "$root/gui/Cargo.toml" \
  --config "$root/gui/about.toml" \
  "$root/gui/about.hbs" > "$out"

# The modules the CLI binary was built with, as recorded in it.
deps=$(go version -m "$cli" | awk '$1 == "dep" { print $2 }')
{
  printf '\n\nThird-party software in the bundled mpeg-ts-analyzer CLI\n'
  printf '========================================================\n'
  printf '\n--------------------------------------------------------------------------------\n'
  printf 'Go standard library (%s)\n\n' "$(go version -m "$cli" | head -n 1 | awk '{ print $2 }')"
  cat "$(go env GOROOT)/LICENSE"
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
