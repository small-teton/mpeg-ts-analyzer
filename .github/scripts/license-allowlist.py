#!/usr/bin/env python3
"""The license allowlist of the release artifacts, kept in one place.

gui/about.toml (`accepted`) is the source of truth: cargo-about refuses to write
the GUI notices for a crate under any other license. This script derives the
other checks from it (the License check workflow runs both commands):

  license-allowlist.py go     prints the licenses go-licenses allows for the
                              CLI, comma-separated: `accepted` without the
                              licenses that gui/deny.toml grants only to
                              named crates (MPL-2.0 for option-ext).
  license-allowlist.py check  fails unless gui/deny.toml (cargo-deny) allows
                              exactly `accepted`: `[licenses] allow` plus the
                              licenses of `[[licenses.exceptions]]`, with no
                              license in both.
"""

import pathlib
import sys
import tomllib

ROOT = pathlib.Path(__file__).resolve().parents[2]


def load(name):
    with open(ROOT / "gui" / name, "rb") as f:
        return tomllib.load(f)


def main(argv):
    if len(argv) != 2 or argv[1] not in ("go", "check"):
        print("usage: license-allowlist.py go|check", file=sys.stderr)
        return 2
    accepted = load("about.toml")["accepted"]
    licenses = load("deny.toml")["licenses"]
    allow = licenses.get("allow", [])
    excepted = sorted({lic for exc in licenses.get("exceptions", []) for lic in exc["allow"]})

    if argv[1] == "go":
        print(",".join(lic for lic in accepted if lic not in excepted))
        return 0

    errors = []
    if len(set(accepted)) != len(accepted):
        errors.append("gui/about.toml: `accepted` lists a license twice")
    if both := sorted(set(allow) & set(excepted)):
        errors.append(f"gui/deny.toml: allowed globally and as an exception: {', '.join(both)}")
    if missing := sorted(set(accepted) - set(allow) - set(excepted)):
        errors.append(f"gui/deny.toml does not allow licenses of gui/about.toml: {', '.join(missing)}")
    if extra := sorted((set(allow) | set(excepted)) - set(accepted)):
        errors.append(f"gui/deny.toml allows licenses missing from gui/about.toml: {', '.join(extra)}")
    for error in errors:
        print(f"::error::{error}")
    if errors:
        return 1
    print(f"gui/deny.toml and gui/about.toml allow the same {len(accepted)} licenses.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
