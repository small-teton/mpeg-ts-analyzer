#!/bin/sh
# Measures the GUI's heap in the scenarios of docs/gui/memory-profile.md:
# generates the inputs once (ffmpeg), builds the CLI from this tree and runs
# the ignored memory_profile test alone, so the process-wide heap counters see
# only its work.
#
# usage: memory-profile.sh [<input directory>]
#
# The inputs (about 1.2 GB) are kept in <input directory>, by default
# $TMPDIR/mtsa-memory-profile, and reused by later runs. They are never part
# of the repository.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
dir=${1:-${TMPDIR:-/tmp}/mtsa-memory-profile}
mkdir -p "$dir"

# One program at a constant 2 Mbit/s for 4400 s (about 1.1 GB): the default
# 64 MiB range covers about 270 s of it, the large range the whole file.
if [ ! -s "$dir/single.ts" ]; then
  ffmpeg -nostdin -loglevel error -y \
    -f lavfi -i testsrc2=size=320x240:rate=25 \
    -f lavfi -i sine=frequency=1000:sample_rate=48000 \
    -t 4400 -map 0:v -map 1:a \
    -c:v mpeg2video -b:v 1500k -minrate 1500k -maxrate 1500k -bufsize 1000k \
    -c:a mp2 -b:a 128k \
    -f mpegts -muxrate 2000000 "$dir/single.ts.part"
  mv "$dir/single.ts.part" "$dir/single.ts"
fi

# Two programs, each with video and audio, at 2 Mbit/s in all for 400 s
# (about 100 MB), more than the default range.
if [ ! -s "$dir/multi.ts" ]; then
  ffmpeg -nostdin -loglevel error -y \
    -f lavfi -i testsrc2=size=320x240:rate=25 \
    -f lavfi -i sine=frequency=1000:sample_rate=48000 \
    -f lavfi -i testsrc=size=320x240:rate=25 \
    -f lavfi -i sine=frequency=500:sample_rate=48000 \
    -t 400 -map 0:v -map 1:a -map 2:v -map 3:a \
    -c:v mpeg2video -b:v 700k -minrate 700k -maxrate 700k -bufsize 500k \
    -c:a mp2 -b:a 96k \
    -program title=A:st=0:st=1 -program title=B:st=2:st=3 \
    -f mpegts -muxrate 2000000 "$dir/multi.ts.part"
  mv "$dir/multi.ts.part" "$dir/multi.ts"
fi

go -C "$root" build -o "$root/out/gui/mpeg-ts-analyzer" .
MPEG_TS_ANALYZER_CLI="$root/out/gui/mpeg-ts-analyzer" \
MTSA_PROFILE_SINGLE="$dir/single.ts" \
MTSA_PROFILE_MULTI="$dir/multi.ts" \
  cargo test --manifest-path "$root/gui/Cargo.toml" --locked --release \
  memory_profile -- --ignored --nocapture --test-threads=1
