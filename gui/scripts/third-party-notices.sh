#!/bin/sh
# Writes the third-party notices shipped in the GUI packages: the Rust crates
# of the GUI (cargo-about, gui/about.toml) and the Go standard library and
# modules compiled into the bundled CLI for any released target.
#
# usage: third-party-notices.sh <output file> [<CLI directory>]
#
# The Go modules are read from the CLI binaries under <CLI directory> (as the
# GUI release workflow provides them, one per target). Without it, they are
# those of the CLI of this tree for each released target.
set -eu
out=$1
root=$(cd "$(dirname "$0")/../.." && pwd)

cargo about generate --locked \
  --manifest-path "$root/gui/Cargo.toml" \
  --config "$root/gui/about.toml" \
  "$root/gui/about.hbs" > "$out"

# The Go part is shared with the CLI release (.github/scripts/go-third-party-notices.sh).
if [ $# -ge 2 ]; then
  sources=$(find "$2" -type f -name 'mpeg-ts-analyzer*' ! -name '*.*' -o -type f -name 'mpeg-ts-analyzer*.exe')
  [ -n "$sources" ] || { echo "no CLI binaries in $2" >&2; exit 1; }
else
  sources="darwin/arm64 linux/amd64 windows/amd64"
fi
printf '\n\n' >> "$out"
# shellcheck disable=SC2086 # one source per word; the paths have no spaces
"$root/.github/scripts/go-third-party-notices.sh" \
  'Third-party software in the bundled mpeg-ts-analyzer CLI' $sources >> "$out"
