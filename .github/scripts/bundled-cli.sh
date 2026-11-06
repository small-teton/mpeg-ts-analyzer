#!/usr/bin/env bash
# Provides the CLI that the GUI packages bundle: the published CLI release
# named in gui/BUNDLED_CLI_VERSION, never a CLI built from the GUI's commit.
#
#   bundled-cli.sh <out-dir> <goos/goarch>...
#
# For each target (darwin/arm64, linux/amd64, windows/amd64) it writes
# <out-dir>/<goos>-<goarch>/mpeg-ts-analyzer[.exe], and <out-dir>/VERSION with
# the CLI version. The release archives are checked against the release's
# checksums and its build provenance from the Release workflow, and the tag's
# VERSION file must name the same version.
#
# On a pull request (ALLOW_STAND_IN=true) whose CLI release is not published
# yet but whose VERSION already names it, a CLI built from this commit stands
# in, so the packaging can be checked before the CLI is released. A release
# never uses a stand-in.
#
# Needs GH_TOKEN and GITHUB_REPOSITORY, and gh, git, go (for a stand-in) and
# tar or unzip.
set -euo pipefail

fail() {
  echo "::error::$*" >&2
  exit 1
}

root=$(cd "$(dirname "$0")/../.." && pwd)
state="$root/.github/scripts/release-state.sh"
out=$1
shift
version=$("$state" version "$root/gui/BUNDLED_CLI_VERSION")
tag="v$version"
repo=${GITHUB_REPOSITORY:?}
mkdir -p "$out"

archive() {
  case $1 in
    darwin/arm64) echo mpeg-ts-analyzer_Darwin_arm64.tar.gz ;;
    linux/amd64) echo mpeg-ts-analyzer_Linux_x86_64.tar.gz ;;
    windows/amd64) echo mpeg-ts-analyzer_Windows_x86_64.zip ;;
    *) fail "no released CLI archive for $1" ;;
  esac
}

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi |
    awk '{ print $1 }'
}

published=$(gh release view "$tag" --repo "$repo" --json isDraft --jq '.isDraft | not' 2>/dev/null || echo false)

if [ "$published" = true ]; then
  git -C "$root" fetch --quiet origin "+refs/tags/$tag:refs/tags/$tag"
  tag_version=$(git -C "$root" show "$tag:VERSION")
  [ "$tag_version" = "$version" ] || fail "$tag has VERSION '$tag_version', not $version"
  downloads=$(mktemp -d)
  checksums="mpeg-ts-analyzer_${version}_checksums.txt"
  patterns=(--pattern "$checksums")
  for target in "$@"; do
    patterns+=(--pattern "$(archive "$target")")
  done
  gh release download "$tag" --repo "$repo" --dir "$downloads" "${patterns[@]}"
  for target in "$@"; do
    name=$(archive "$target")
    expected=$(awk -v name="$name" '$2 == name { print $1 }' "$downloads/$checksums")
    [ -n "$expected" ] || fail "$name is not listed in $checksums"
    [ "$(sha256 "$downloads/$name")" = "$expected" ] || fail "$name does not match $checksums"
    gh attestation verify "$downloads/$name" --repo "$repo" \
      --signer-workflow "$repo/.github/workflows/release.yml" >/dev/null ||
      fail "$name has no build provenance from the Release workflow"
    dir="$out/${target%/*}-${target#*/}"
    mkdir -p "$dir"
    case $name in
      *.zip)
        if command -v unzip >/dev/null; then
          unzip -q -o "$downloads/$name" mpeg-ts-analyzer.exe -d "$dir"
        else
          7z x -y -o"$dir" "$downloads/$name" mpeg-ts-analyzer.exe >/dev/null
        fi ;;
      *) tar -xzf "$downloads/$name" -C "$dir" mpeg-ts-analyzer ;;
    esac
    echo "$target: $name from $tag, checksum and provenance verified"
  done
elif [ "${ALLOW_STAND_IN:-false}" = true ] &&
  [ "$("$state" version "$root/VERSION")" = "$version" ]; then
  echo "::warning::CLI $tag is not published yet; the packages bundle a CLI built from this commit. A release requires $tag."
  for target in "$@"; do
    dir="$out/${target%/*}-${target#*/}"
    mkdir -p "$dir"
    # Built like the CLI release (GoReleaser): static, trimmed, stripped.
    CGO_ENABLED=0 GOOS=${target%/*} GOARCH=${target#*/} \
      go -C "$root" build -trimpath -ldflags "-s -w" -o "$dir/mpeg-ts-analyzer$([ "${target%/*}" = windows ] && echo .exe)" .
    echo "$target: built from this commit (stand-in for $tag)"
  done
else
  fail "CLI $tag is not published. Release it first, or set gui/BUNDLED_CLI_VERSION to a published CLI version."
fi
echo "$version" > "$out/VERSION"
