#!/bin/sh
# Writes the third-party notices of the mpeg-ts-analyzer CLI to standard
# output: the Go standard library, with the Go version, and the license files
# of every module compiled into the CLI for the given sources. Used for the CLI
# release archives and packages (.goreleaser.yaml) and for the bundled CLI in
# the GUI packages (gui/scripts/third-party-notices.sh).
#
# usage: go-third-party-notices.sh <title> <source>...
#
# Each source is one of:
#   <file>           a CLI binary; its Go version and modules are read from the
#                    build information recorded in it (go version -m).
#   <goos>/<goarch>  the CLI of this tree for that target (go list -deps).
#   <goos>           the CLI of this tree for every port of that GOOS in
#                    `go tool dist list`, a superset of any GOARCH released.
#
# The module set depends on the target: cobra uses mousetrap only on Windows.
# A module without a LICENSE*, LICENCE*, COPYING* or NOTICE* file, or one
# replaced in go.mod, is an error. The output is sorted and has no timestamps,
# so the same sources and Go toolchain give the same bytes.
set -eu
export LC_ALL=C

[ $# -ge 2 ] || { echo "usage: $0 <title> <source>..." >&2; exit 2; }
title=$1
shift
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# One line per source and module: "go <version>" or "dep <path>@<version>".
list_target() {
  CGO_ENABLED=0 GOOS=${1%/*} GOARCH=${1#*/} go -C "$root" list -deps \
    -f '{{with .Module}}{{if not .Main}}dep {{.Path}}@{{.Version}}{{with .Replace}} replaced{{end}}{{end}}{{end}}' . ||
    { echo "go list failed for $1" >&2; return 1; }
  printf 'go %s\n' "$(go -C "$root" env GOVERSION)"
}
for source in "$@"; do
  if [ -f "$source" ]; then
    go version -m "$source" > "$work/buildinfo" ||
      { echo "no Go build information in $source" >&2; exit 1; }
    awk 'NR == 1 { print "go", $2 } $1 == "dep" { print "dep", $2 "@" $3 } $1 == "=>" { print "dep", $2, "replaced" }' \
      "$work/buildinfo"
  else
    case $source in
      */*) list_target "$source" ;;
      *)
        ports=$(go tool dist list | awk -F/ -v goos="$source" '$1 == goos')
        [ -n "$ports" ] || { echo "no Go ports for GOOS $source" >&2; exit 1; }
        for port in $ports; do list_target "$port"; done ;;
    esac
  fi
done > "$work/info"

if grep -q ' replaced$' "$work/info"; then
  echo "replaced modules are not supported:" >&2
  grep ' replaced$' "$work/info" >&2
  exit 1
fi
goversions=$(awk '$1 == "go" { print $2 }' "$work/info" | sort -u | tr '\n' ' ')
deps=$(awk '$1 == "dep" { print $2 }' "$work/info" | sort -u)
[ -n "$goversions" ] || { echo "no Go version found" >&2; exit 1; }

# The modules are downloaded in an empty module, so a binary built from
# another commit gets the license files of the versions recorded in it.
mkdir "$work/mod"
printf 'module notices\n' > "$work/mod/go.mod"
printf '%s\n' "$title"
printf '%s\n' "$title" | sed 's/./=/g'
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
