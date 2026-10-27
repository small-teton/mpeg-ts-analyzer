# Development

Run the commands in this guide from the repository root unless noted otherwise.

## Containerized development environment

The development image provides the Go toolchain, FFmpeg (`ffprobe`), and TSDuck
(`tsanalyze`, `tsbitrate`) on both amd64 and arm64 hosts. The source tree is
bind-mounted into the container, while Go module and build caches are kept in
named volumes.

### Docker Compose

Build the image and open a shell:

```bash
docker compose build dev
docker compose run --rm dev
```

Then run the normal development commands from inside the container:

```bash
make build
make test
make coverage
.githooks/pre-push
```

To run a single command without opening a shell:

```bash
docker compose run --rm dev make coverage
```

Docker Compose v2 is required. VS Code users can instead install the Dev
Containers extension, open the repository, and select **Reopen in Container**.

### Apple container

[Apple container](https://github.com/apple/container) users on Apple silicon
and macOS 26 can build the same OCI image. Start its system service once, then
build the image and open a shell:

```bash
container system start
container build --file docker/Dockerfile --tag mpeg-ts-analyzer-dev .
container run --rm -it \
  --volume "$PWD:/workspace" \
  --volume mpeg-ts-analyzer-go-mod:/go/pkg/mod \
  --volume mpeg-ts-analyzer-go-build:/root/.cache/go-build \
  --workdir /workspace \
  mpeg-ts-analyzer-dev
```

The two named cache volumes are created automatically on first use. To run one
command without opening a shell, append it after the image name, for example:

```bash
container run --rm \
  --volume "$PWD:/workspace" \
  --volume mpeg-ts-analyzer-go-mod:/go/pkg/mod \
  --volume mpeg-ts-analyzer-go-build:/root/.cache/go-build \
  --workdir /workspace \
  mpeg-ts-analyzer-dev make coverage
```

Apple container does not consume `compose.yaml`; the commands above mirror its
source and cache mounts directly.

## Test and coverage

```bash
make setup      # configure git hooks (run once after clone)
make build      # build binary
make test       # run all tests
make coverage   # run tests with coverage report
make check-demo # verify that the README GIF matches the current CLI output
make check-docs-policy # check interface/documentation changes against origin/master
make demo       # regenerate in Docker or Apple container
make install    # install to $GOPATH/bin
make uninstall  # remove from $GOPATH/bin
make clean      # remove build/coverage artifacts
```

The version string is managed in the [`VERSION`](../VERSION) file and injected
at build time.

Coverage is measured for `bitbuffer` and `tsparser` packages only. The CLI entry
point (`cmd`, `main.go`) is excluded from coverage targets. Both packages should
maintain 100% coverage.

A pre-push hook (`make setup` to enable) runs the build, test, coverage, and
README demo checks before every push. Push is rejected if coverage drops below
100% or the committed GIF is stale.

Pull requests also run a deterministic documentation-policy check. It verifies
the PR template sections, CLI flag coverage in [`OPTIONS.md`](../OPTIONS.md),
local Markdown links, and required README, OPTIONS, schema, and demo maintenance
when a public interface changes. Definite omissions fail the check. Possible
text-output changes that need human judgment remain non-blocking and are posted
as a single updatable PR warning comment as well as to the Actions summary. The
check uses no external AI or API key. Run it locally against another base with,
for example, `make check-docs-policy DOCS_POLICY_BASE=origin/master`.

## README demo

The animated terminal demo near the top of the [README](../README.md) is
generated from the current binary and the bundled 188-byte sample:

```sh
make check-demo # check the recorded CLI output fingerprint and GIF checksum
make demo       # regenerate using Docker or Apple container
```

Allow about three minutes for regeneration on a recent Apple-silicon Mac; the
first run can take longer while the container image and packages are downloaded.
Runtime varies with the host and container engine.

The canonical regeneration path supports Docker and
[Apple container](https://github.com/apple/container). On macOS it prefers
Apple container and falls back to Docker; elsewhere it prefers Docker. Set
`DEMO_CONTAINER_RUNTIME=container` or `DEMO_CONTAINER_RUNTIME=docker` to select
one explicitly. Both build the same image, which pins the Go/Alpine base by
digest and installs ImageMagick 7 and DejaVu Sans Mono, so contributors do not
need those rendering dependencies on the host. Apple container requires Apple
silicon and macOS 26; run `container system start` once after installing it. If
a full-tunnel VPN causes DNS timeouts during an Apple container build, pause it
for the generation and reconnect it afterward; see
[apple/container#1881](https://github.com/apple/container/issues/1881).
The lightweight check requires only Go, runs in GitHub Actions and the pre-push
hook, and fails when displayed CLI output, rendering code, or the container
definition changes without a matching regenerated GIF. Inspect the complete
animation before committing it. See [demo/README.md](demo/README.md) for
generation and verification details.
