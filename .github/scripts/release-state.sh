#!/usr/bin/env bash
# Checks shared by the release workflows. Each command fails with a GitHub
# Actions error annotation.
#
#   release-state.sh semver <value>
#       Fails unless the value is a bare X.Y.Z version.
#   release-state.sh version <file>
#       Prints the version in a VERSION file: a bare X.Y.Z, optionally
#       followed by one newline. Anything else, including blank lines, fails.
#   release-state.sh on-master <commit>
#       Fails unless the commit is on origin/master (it is fetched first).
#   release-state.sh tag <tag> <commit>
#       Prints what releasing <commit> as <tag> means:
#         new        no tag yet: build, then create the tag at <commit>
#         resume     the tag points at <commit> and no published release
#                    exists: build and publish again (a previous run stopped)
#         published  a published release exists for the tag at <commit>:
#                    nothing to do
#       Fails if the tag points at another commit, or a published release
#       exists without the tag. Tags and releases are never changed here.
#       Needs GH_TOKEN for the release lookup.
set -euo pipefail

fail() {
  echo "::error::$*" >&2
  exit 1
}

semver() {
  [[ "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
    fail "'$1' is not a bare X.Y.Z version"
}

read_version() {
  local raw
  # A sentinel keeps the trailing newlines that command substitution drops.
  raw=$(cat "$1" && printf x) || fail "cannot read $1"
  raw=${raw%x}
  raw=${raw%$'\n'}
  semver "$raw"
  # Bash drops NUL bytes when reading, so compare the file itself with the
  # two permitted forms.
  cmp -s "$1" <(printf '%s' "$raw") || cmp -s "$1" <(printf '%s\n' "$raw") ||
    fail "$1 must contain only a bare X.Y.Z version and at most one final newline"
  printf '%s\n' "$raw"
}

on_master() {
  git fetch --quiet origin master
  git merge-base --is-ancestor "$1" origin/master ||
    fail "commit $1 is not on master"
}

# The commit a remote tag points at, peeled for an annotated tag; empty when
# the tag does not exist.
remote_tag_commit() {
  local refs
  refs=$(git ls-remote --tags origin "refs/tags/$1" "refs/tags/$1^{}")
  awk -v peeled="refs/tags/$1^{}" '$2 == peeled { print $1; found = 1 } END { if (!found) exit 1 }' <<<"$refs" ||
    awk '{ print $1; exit }' <<<"$refs"
}

# Prints draft, published or none.
release_status() {
  local out
  if out=$(gh release view "$1" --json isDraft --jq .isDraft 2>&1); then
    [ "$out" = true ] && echo draft || echo published
  elif [[ "$out" == *"release not found"* ]]; then
    echo none
  else
    fail "cannot look up release $1: $out"
  fi
}

tag_state() {
  local tag=$1 commit=$2 tagged release
  tagged=$(remote_tag_commit "$tag")
  release=$(release_status "$tag")
  if [ -z "$tagged" ]; then
    [ "$release" != published ] || fail "release $tag is published but the tag does not exist"
    echo new
  elif [ "$tagged" != "$commit" ]; then
    fail "tag $tag already points at $tagged, not $commit; it is not moved"
  elif [ "$release" = published ]; then
    echo published
  else
    echo resume
  fi
}

case "${1:-}" in
  semver) semver "$2" ;;
  version) read_version "$2" ;;
  on-master) on_master "$2" ;;
  tag) tag_state "$2" "$3" ;;
  *) fail "usage: $0 semver <value> | version <file> | on-master <commit> | tag <tag> <commit>" ;;
esac
