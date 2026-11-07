#!/bin/sh
# Signs the GUI's update packages for the in-app update and writes the update
# manifest that cargo-packager-updater reads (docs/gui/release-security.md,
# "Update signing").
#
# Usage: sign-gui-updates.sh <packages dir> <version> <tag> <public key> <out dir>
#
# <packages dir> holds the packages of the release:
#   mpeg-ts-analyzer-gui_<version>_macos_arm64.app.tar.gz
#   mpeg-ts-analyzer-gui_<version>_linux_x86_64.AppImage
#   mpeg-ts-analyzer-gui_<version>_windows_x86_64_setup.exe
# The key is read from CARGO_PACKAGER_SIGN_PRIVATE_KEY and its password from
# CARGO_PACKAGER_SIGN_PRIVATE_KEY_PASSWORD (both read by cargo packager).
# Every signature is verified against <public key> with minisign, so a private
# key that does not belong to it fails here. <out dir> receives the .sig files
# and mpeg-ts-analyzer-gui_<version>_update.json; the packages stay where they
# are.
set -eu

if [ $# -ne 5 ]; then
  echo "usage: $0 <packages dir> <version> <tag> <public key> <out dir>" >&2
  exit 2
fi
packages=$1 version=$2 tag=$3 public_key=$4 out=$5
: "${GITHUB_REPOSITORY:?}"

if [ -z "${CARGO_PACKAGER_SIGN_PRIVATE_KEY:-}" ]; then
  echo "::error::No signing key: set the secrets of the gui-release environment (docs/development/release.md)." >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# cargo packager stores the public key and the signatures as Base64 of the
# minisign text; minisign reads the decoded text.
base64 --decode "$public_key" > "$work/public.key"
grep -q '^untrusted comment: minisign public key' "$work/public.key" || {
  echo "::error::$public_key is not a cargo-packager (minisign) public key." >&2
  exit 1
}

base="mpeg-ts-analyzer-gui_${version}"
url="https://github.com/${GITHUB_REPOSITORY}/releases/download/${tag}"
mkdir -p "$out"
platforms='{}'

# <updater target> <format> <file name>
sign() {
  file="$packages/$3"
  [ -f "$file" ] || { echo "::error::$3 is missing" >&2; exit 1; }
  cp "$file" "$work/$3"
  cargo packager signer sign "$work/$3" >/dev/null
  base64 --decode "$work/$3.sig" > "$work/$3.minisig"
  # The trusted comment, which the signature covers, names the signed file.
  result=$(minisign -V -m "$work/$3" -x "$work/$3.minisig" -p "$work/public.key")
  case "$result" in
    *"file:$3"*) ;;
    *) echo "::error::The signature of $3 does not name it: $result" >&2; exit 1 ;;
  esac
  cp "$work/$3.sig" "$out/$3.sig"
  platforms=$(jq -c \
    --arg target "$1" --arg format "$2" \
    --arg url "$url/$3" --rawfile signature "$work/$3.sig" \
    '.[$target] = {url: $url, signature: $signature, format: $format}' <<EOF
$platforms
EOF
)
  echo "Signed and verified $3"
}

sign macos-aarch64 app "${base}_macos_arm64.app.tar.gz"
sign linux-x86_64 appimage "${base}_linux_x86_64.AppImage"
sign windows-x86_64 nsis "${base}_windows_x86_64_setup.exe"

jq -n \
  --arg version "$version" \
  --arg notes "https://github.com/${GITHUB_REPOSITORY}/releases/tag/${tag}" \
  --arg pub_date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson platforms "$platforms" \
  '{version: $version, notes: $notes, pub_date: $pub_date, platforms: $platforms}' \
  > "$out/${base}_update.json"
cat "$out/${base}_update.json"
