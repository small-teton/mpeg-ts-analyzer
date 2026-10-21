#!/usr/bin/env bash
set -euo pipefail

script_dir="$(dirname "$0")"
repo_dir="$(env -u GIT_DIR -u GIT_WORK_TREE -u GIT_PREFIX git -C "$script_dir" rev-parse --show-toplevel)"
image="mpeg-ts-analyzer-demo:go1.26.6-alpine3.24"
runtime="${DEMO_CONTAINER_RUNTIME:-}"

if [[ -z "$runtime" ]]; then
  candidates=(docker container)
  if [[ "$(uname -s)" == "Darwin" ]]; then
    candidates=(container docker)
  fi
  for candidate in "${candidates[@]}"; do
    if command -v "$candidate" >/dev/null 2>&1; then
      runtime="$candidate"
      break
    fi
  done
fi

if [[ "$runtime" != "docker" && "$runtime" != "container" ]]; then
  echo "set DEMO_CONTAINER_RUNTIME to 'docker' or 'container'" >&2
  exit 2
fi

if ! command -v "$runtime" >/dev/null 2>&1; then
  echo "required command not found: $runtime" >&2
  exit 1
fi

echo "Rendering with $runtime."

"$runtime" build \
  --file "${repo_dir}/docs/demo/Dockerfile" \
  --tag "$image" \
  "${repo_dir}/docs/demo"

"$runtime" run --rm \
  --memory 6g \
  --user "$(id -u):$(id -g)" \
  --volume "${repo_dir}:/workspace" \
  --workdir /workspace \
  "$image"
