#!/bin/sh
# cargo-packager's before-each-package-command. Before the .dmg is made from
# the .app, signs the .app ad hoc ("-"): the main executable gets a signature
# that seals Info.plist and the resources. Without it only the executables
# carry their linkers' signatures, and macOS reports the downloaded app as
# "damaged" instead of offering "Open Anyway".
#
# Not --deep, and not cargo-packager's own signing (macos.signing-identity),
# which re-signs every Mach-O file: the bundled CLI in Contents/Resources stays
# byte for byte the published release binary, already signed by its linker,
# and the bundle seal records its hash.
set -eu
[ "${CARGO_PACKAGER_FORMAT:-}" = dmg ] || exit 0
app=target/release/mpeg-ts-analyzer.app
codesign --force --sign - "$app"
codesign --verify --strict --verbose=2 "$app"
