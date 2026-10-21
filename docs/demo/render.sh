#!/usr/bin/env bash
set -euo pipefail

# Renderer invoked inside the generation container.

script_dir="$(dirname "$0")"
repo_dir="$(realpath "${script_dir}/../..")"
output="${repo_dir}/docs/demo.gif"
manifest="${repo_dir}/docs/demo/manifest.sha256"
sample="${repo_dir}/sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/mpeg-ts-analyzer-demo.XXXXXX")"
mode="${1:-render}"

if [[ "$mode" != "render" && "$mode" != "--check" ]]; then
  echo "usage: $0 [--check]" >&2
  exit 2
fi

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

for command in go; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "required command not found: $command" >&2
    exit 1
  fi
done

export XDG_CACHE_HOME="${work_dir}/cache"
mkdir -p "$XDG_CACHE_HOME" "${work_dir}/frames"

if [[ "$mode" == "render" ]]; then
  if ! command -v magick >/dev/null 2>&1; then
    echo "required command not found: magick" >&2
    exit 1
  fi
  if [[ -r /System/Library/Fonts/Menlo.ttc ]]; then
    font_regular="/System/Library/Fonts/Menlo.ttc"
    font_bold="$font_regular"
  elif [[ -r /usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf ]]; then
    font_regular="/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"
    font_bold="/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf"
  elif [[ -r /usr/share/fonts/TTF/DejaVuSansMono.ttf ]]; then
    font_regular="/usr/share/fonts/TTF/DejaVuSansMono.ttf"
    font_bold="/usr/share/fonts/TTF/DejaVuSansMono-Bold.ttf"
  elif [[ -r /usr/share/fonts/dejavu/DejaVuSansMono.ttf ]]; then
    font_regular="/usr/share/fonts/dejavu/DejaVuSansMono.ttf"
    font_bold="/usr/share/fonts/dejavu/DejaVuSansMono-Bold.ttf"
  else
    echo "a Menlo or DejaVu Sans Mono font file is required" >&2
    exit 1
  fi
fi

binary="${work_dir}/mpeg-ts-analyzer"
env GOCACHE="${work_dir}/go-cache" go build -o "$binary" "$repo_dir"

header_full="$("$binary" "$sample" --dump-ts-header --limit 65536 | sed "s|$sample|sample.ts|")"
header_output="$(printf '%s\n' "$header_full" | sed -n '1,12p')"

adaptation_full="$("$binary" "$sample" --dump-adaptation-field --limit 65536 | sed "s|$sample|sample.ts|")"
adaptation_output="$(printf '%s\n' "$adaptation_full" | sed -n '1,17p')"

psi_output="$("$binary" "$sample" --dump-psi | sed "s|$sample|sample.ts|" | fold -s -w 118)"

timestamp_full="$("$binary" "$sample" --dump-timestamp | sed "s|$sample|sample.ts|")"
timestamp_samples="$(printf '%s\n' "$timestamp_full" | awk '/^0x/ { print; count++; if (count == 9) exit }')"
timestamp_summary="$(printf '%s\n' "$timestamp_full" | tail -n 6)"
timestamp_output="${timestamp_samples}
...
${timestamp_summary}"

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

{
  printf 'demo-source-v2\n'
  printf 'render_script_sha256=%s\n' "$(sha256_file "${repo_dir}/docs/demo/render.sh")"
  printf 'container_script_sha256=%s\n' "$(sha256_file "${repo_dir}/docs/demo/render-container.sh")"
  printf 'dockerfile_sha256=%s\n' "$(sha256_file "${repo_dir}/docs/demo/Dockerfile")"
  printf '\n[ts-header]\n%s\n' "$header_output"
  printf '\n[adaptation-field]\n%s\n' "$adaptation_output"
  printf '\n[psi]\n%s\n' "$psi_output"
  printf '\n[timestamp]\n%s\n' "$timestamp_output"
} >"${work_dir}/source.txt"
source_hash="$(sha256_file "${work_dir}/source.txt")"

if [[ "$mode" == "--check" ]]; then
  if [[ ! -f "$output" || ! -f "$manifest" ]]; then
    echo "README demo is missing; run 'make demo' and commit the generated files." >&2
    echo "See docs/demo/README.md for regeneration and troubleshooting instructions." >&2
    exit 1
  fi
  recorded_source_hash="$(awk '$1 == "source_sha256" { print $2 }' "$manifest")"
  recorded_gif_hash="$(awk '$1 == "gif_sha256" { print $2 }' "$manifest")"
  actual_gif_hash="$(sha256_file "$output")"
  if [[ "$source_hash" != "$recorded_source_hash" ]]; then
    echo "README demo is stale: its CLI output, render scripts, or container definition changed." >&2
    echo "Run 'make demo', inspect docs/demo.gif, and commit it with docs/demo/manifest.sha256." >&2
    echo "See docs/demo/README.md for regeneration and troubleshooting instructions." >&2
    exit 1
  fi
  if [[ "$actual_gif_hash" != "$recorded_gif_hash" ]]; then
    echo "README demo checksum does not match docs/demo/manifest.sha256." >&2
    echo "Run 'make demo', inspect the result, and commit both generated files." >&2
    echo "See docs/demo/README.md for regeneration and troubleshooting instructions." >&2
    exit 1
  fi
  if ! grep -Fq '](docs/demo.gif)' "${repo_dir}/README.md"; then
    echo "README.md does not reference docs/demo.gif." >&2
    echo "See docs/demo/README.md for the expected demo layout and verification steps." >&2
    exit 1
  fi
  echo "README demo is current."
  exit 0
fi

frame_number=0
frames=()
delays=()
scene_number=0
scene_gifs=()
magick_limits=(
  -define "registry:temporary-path=${work_dir}"
  -limit memory 256MiB
  -limit map 512MiB
  -limit disk 4GiB
)

render_frame() {
  local content="$1"
  local delay="$2"
  local frame
  frame="$(printf '%s/frame-%03d.png' "${work_dir}/frames" "$frame_number")"
  printf '%s' "$content" >"${work_dir}/frame.txt"

  magick -size 1440x760 "xc:#0d1117" \
    -fill "#161b22" -stroke "#30363d" -strokewidth 2 \
    -draw "roundrectangle 24,24 1416,736 18,18" \
    -fill "#ff5f56" -stroke none -draw "circle 55,55 62,55" \
    -fill "#ffbd2e" -draw "circle 79,55 86,55" \
    -fill "#27c93f" -draw "circle 103,55 110,55" \
    -fill "#8b949e" -font "$font_bold" -pointsize 18 \
    -gravity north -annotate +0+43 "mpeg-ts-analyzer" \
    -fill "#c9d1d9" -font "$font_regular" -pointsize 18 \
    -gravity northwest -interline-spacing 8 \
    -annotate +54+94 "@${work_dir}/frame.txt" \
    "$frame"

  frames+=("$frame")
  delays+=("$delay")
  frame_number=$((frame_number + 1))
}

animate_command() {
  local command="$1"
  local prefix="$2"
  local length=${#command}
  local visible=0
  while ((visible < length)); do
    visible=$((visible + 7))
    if ((visible > length)); then
      visible=$length
    fi
    render_frame "${prefix}\$ ${command:0:visible}█" 7
  done
  render_frame "${prefix}\$ ${command}" 35
}

play_scene() {
  local command="$1"
  local scene_output="$2"
  local line_delay="$3"
  local hold_delay="$4"
  local transcript
  local visible_transcript

  animate_command "$command" ""
  transcript="\$ ${command}"
  while IFS= read -r line; do
    transcript+=$'\n'"$line"
    visible_transcript="$(printf '%s\n' "$transcript" | tail -n 18)"
    render_frame "$visible_transcript" "$line_delay"
  done <<<"$scene_output"
  render_frame "$visible_transcript" "$hold_delay"
}

encode_scene() {
  local scene_gif
  local scene_args=()
  local index

  scene_gif="$(printf '%s/scene-%02d.gif' "$work_dir" "$scene_number")"
  for index in "${!frames[@]}"; do
    scene_args+=(-delay "${delays[$index]}" "${frames[$index]}")
  done
  magick "${magick_limits[@]}" "${scene_args[@]}" -loop 0 -layers Optimize "$scene_gif"
  scene_gifs+=("$scene_gif")
  frames=()
  delays=()
  scene_number=$((scene_number + 1))
}

play_scene "mpeg-ts-analyzer sample.ts --dump-ts-header --limit 65536" "$header_output" 7 80
encode_scene
play_scene "mpeg-ts-analyzer sample.ts --dump-adaptation-field --limit 65536" "$adaptation_output" 6 80
encode_scene
play_scene "mpeg-ts-analyzer sample.ts --dump-psi" "$psi_output" 4 100
encode_scene
play_scene "mpeg-ts-analyzer sample.ts --dump-timestamp" "$timestamp_output" 8 240
encode_scene

magick "${magick_limits[@]}" "${scene_gifs[@]}" -loop 0 "$output"
gif_hash="$(sha256_file "$output")"
{
  printf 'source_sha256 %s\n' "$source_hash"
  printf 'gif_sha256 %s\n' "$gif_hash"
} >"$manifest"
echo "wrote $output"
echo "wrote $manifest"
