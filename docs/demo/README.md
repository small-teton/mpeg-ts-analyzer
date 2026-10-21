# README demo

The animated terminal demo in [`docs/demo.gif`](../demo.gif) is generated from
the bundled 188-byte sample and the current `mpeg-ts-analyzer` binary. Its four
scenes show TS headers, adaptation fields/PCR, PAT/PMT, and PCR/PTS/DTS output
followed by the final compliance result.

## Check whether the demo is current

```sh
make check-demo
```

This check requires only Go and does not render the GIF. It builds the current
binary, runs every command represented in the animation, and fingerprints the
visible output together with both render scripts and the Dockerfile. It then
checks that fingerprint and the GIF checksum against
[`manifest.sha256`](manifest.sha256).

The same check runs in GitHub Actions and in `.githooks/pre-push`. A change to
the displayed CLI output or animation script therefore fails until the demo is
regenerated. Avoiding pixel-for-pixel rendering in CI keeps the check stable
across operating systems, ImageMagick versions, and installed fonts.

## Regenerate in a container

Install Docker or [Apple container](https://github.com/apple/container), then
run:

```sh
make demo
```

On macOS the runner prefers Apple container and falls back to Docker. On other
platforms it prefers Docker and falls back to Apple container. Select one
explicitly when needed:

```sh
DEMO_CONTAINER_RUNTIME=container make demo
DEMO_CONTAINER_RUNTIME=docker make demo
```

Apple container requires Apple silicon and macOS 26. Start its services once
after installation with `container system start`.

Both paths build [`Dockerfile`](Dockerfile), which pins the Go/Alpine base image
by digest and installs ImageMagick 7 and DejaVu Sans Mono. Rendering as the
current user keeps the generated files writable on the host. The runner uses a
1 GB container limit. Each scene is optimized separately so ImageMagick can
release its pixel cache before the next scene; bounded cache data spills to the
container's temporary disk when needed. The four optimized scenes are then
joined without another whole-animation optimization pass. The command rewrites
both `docs/demo.gif` and `docs/demo/manifest.sha256`.

The base filesystem is digest-pinned, while the Alpine packages are
intentionally not version-pinned. A later image build can therefore resolve a
newer ImageMagick or font package and produce a different GIF even when the
source fingerprint is unchanged. After rebuilding, inspect the animation and
commit the updated GIF and manifest together. When the Go version in `go.mod`
changes, update the matching Go image tag and digest in the Dockerfile.

Some full-tunnel VPN clients currently break Apple container's outbound vmnet
NAT. If `apk add` reports DNS timeouts while the host is online, pause the VPN,
run `make demo`, and reconnect it afterward. This is tracked in
[apple/container#1881](https://github.com/apple/container/issues/1881).

Inspect the complete animation before committing both generated files with any
related CLI, render-script, or container change.

## Why the container uses 1 GB

The animation contains 131 1440x760 frames. One uncompressed RGBA copy of the
complete sequence is about 547 MiB, but ImageMagick's `-layers Optimize` may
hold multiple pixel caches while comparing frames. Optimizing the entire
animation in one invocation was killed by Apple container with both 1 GB and
3 GB limits. Raising the limit to 6 GB completed successfully, but that was a
diagnostic workaround rather than a reasonable requirement for this asset.

The renderer instead optimizes each of the four scenes in a separate
ImageMagick process. Exiting that process releases its pixel caches before the
next scene begins. The final step only joins the four already optimized GIFs;
it does not optimize all 131 full-size frames again. With this layout, Apple
container 1.3.0 completed generation with a 1 GB container limit in about 156
seconds.

ImageMagick is additionally limited to 256 MiB of heap pixel cache and 512 MiB
of memory-mapped pixel cache. Excess cache data spills into the renderer's
temporary directory, with a 4 GiB safety ceiling. That disk value is a maximum,
not preallocated space. If future changes materially increase the dimensions or
frames per scene, prefer splitting the scenes further or reducing retained
frames before raising the container memory limit.
