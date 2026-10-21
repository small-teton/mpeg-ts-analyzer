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
visible output together with `render.sh`. It then checks that fingerprint and
the GIF checksum against [`manifest.sha256`](manifest.sha256).

The same check runs in GitHub Actions and in `.githooks/pre-push`. A change to
the displayed CLI output or animation script therefore fails until the demo is
regenerated. Avoiding pixel-for-pixel rendering in CI keeps the check stable
across operating systems, ImageMagick versions, and installed fonts.

## Regenerate the demo

Install Go and ImageMagick 7, then run:

```sh
make demo
```

The script supports Menlo on macOS and DejaVu Sans Mono on Linux. It rewrites
both `docs/demo.gif` and `docs/demo/manifest.sha256`. Inspect the complete
animation before committing both generated files with any related CLI or
render-script change.
