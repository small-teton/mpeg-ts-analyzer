# Development

Run the commands in this guide from the repository root unless noted otherwise.
[Directory structure](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/DIRECTORY_STRUCTURE.md)
maps what each directory and top-level file of the repository is for, and
[Architecture](architecture.md) how the Go code is divided.

## Containerized development environment

The development image provides the Go toolchain, FFmpeg (`ffprobe`), and TSDuck
(`tsanalyze`, `tsbitrate`) on both amd64 and arm64 hosts. The source tree is
bind-mounted into the container, while Go module and build caches are kept in
named volumes.

### How the container workspace works

The container is a disposable Linux environment around the repository on your
host. It does not contain a separate copy of the source code.

| Host resource | Container path | Purpose and lifetime |
| --- | --- | --- |
| Repository root | `/workspace` | Read-write source mount. Changes made on either side are immediately visible on the other. |
| Go module cache volume | `/go/pkg/mod` | Downloaded modules. Preserved after the container exits. |
| Go build cache volume | `/go/cache/go-build` | Compiled package cache. Preserved after the container exits. |
| Container writable layer | Paths outside the mounts | Files created or changed at runtime are removed with the container. Tools remain available because they are stored in the image. |

Commands run from `/workspace`, so generated repository files such as `out/`
also appear in the host checkout. The `--rm` option removes the temporary
container after its shell or command exits; it does not remove the source tree
or named cache volumes.

### Docker Compose

Docker with Compose v2 is required. Build the image, then start a temporary
container and open its Bash shell:

```bash
DEV_UID="$(id -u)" docker compose build dev
docker compose run --rm dev
```

The first command downloads and installs the development tools into a reusable
local image. It also gives the image's `developer` user the same user ID as the
host account, so files created in the bind-mounted checkout retain the correct
owner on Linux. Run it again after `docker/Dockerfile` changes. The second
command uses that image to start the shell.

The prompt changes to something similar to `developer@abc123:/workspace$`.
Commands entered at that prompt run inside the container:

```bash
pwd              # /workspace
ls               # the repository files from the host
make build
make test
make coverage
.githooks/pre-push
```

Run `exit` or press Control-D to leave the shell. The source files and Go caches
remain available for the next container.

To run a single command without opening a shell:

```bash
docker compose run --rm dev make coverage
```

VS Code users can instead install the Dev Containers extension, open the
repository, and select **Reopen in Container**. After it opens, select
**Terminal → New Terminal**; that terminal starts in `/workspace`, and the same
`make` commands can be used there. Files edited in VS Code are the bind-mounted
files in the host checkout.

### Apple container

[Apple container](https://github.com/apple/container) users on Apple silicon
and macOS 26 can build the same OCI image. Start its system service once, then
build the image and open a shell:

```bash
container system start
container build \
  --build-arg USER_UID="$(id -u)" \
  --file docker/Dockerfile \
  --tag mpeg-ts-analyzer-dev .
container run --rm -it \
  --volume "$PWD:/workspace" \
  --volume mpeg-ts-analyzer-go-mod:/go/pkg/mod \
  --volume mpeg-ts-analyzer-go-build:/go/cache/go-build \
  --workdir /workspace \
  mpeg-ts-analyzer-dev
```

This opens the same kind of shell in `/workspace`; use `exit` or Control-D to
leave it. The two named cache volumes are created automatically on first use.
To run one command without opening a shell, append it after the image name, for
example:

```bash
container run --rm \
  --volume "$PWD:/workspace" \
  --volume mpeg-ts-analyzer-go-mod:/go/pkg/mod \
  --volume mpeg-ts-analyzer-go-build:/go/cache/go-build \
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
make demo       # regenerate in Docker or Apple container (run on the host)
make install    # install to $GOPATH/bin (run on the host)
make uninstall  # remove from $GOPATH/bin (run on the host)
make clean      # remove build/coverage artifacts
```

The version string is managed in the [`VERSION`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/VERSION) file and injected
at build time.

Coverage is measured for the `bitbuffer`, `tsparser` and `internal/analysis`
packages only. The CLI entry point (`cmd`, `main.go`) is excluded from coverage
targets. Each of the three packages must maintain 100% coverage through its own
tests. [Architecture](architecture.md) describes what belongs in each package.

The JSON outputs are validated against the checked-in schemas
(`docs/json-schema-v2.json`, `docs/json-schema-analysis-v2.json` and
`docs/json-schema-packets-v1.json`) by `tsparser/json_schema_test.go`: every
output kind and lifecycle state is written through the CLI's own JSON output
functions and checked with a JSON Schema Draft 2020-12 validator
([`santhosh-tekuri/jsonschema`](https://github.com/santhosh-tekuri/jsonschema),
Apache-2.0). The cross-file `$ref`s resolve to the local files under the
published `$id`s (`site_url` in `mkdocs.yml` plus the file name), with no
network access. Only tests import the validator, so it is not linked into the
CLI. When an output gains a state or an optional field, add a case there.

A pre-push hook (`make setup` to enable) runs the build, test, coverage, and
README demo checks before every push. Push is rejected if coverage drops below
100% or the committed GIF is stale.

Pull requests also run a deterministic documentation-policy check. It verifies
the PR template sections, CLI flag coverage in
[`docs/cli/usage.md`](../cli/usage.md), local Markdown links, and required
updates of the CLI and JSON pages, the schema, and the demo when a public
interface changes. Definite omissions fail the check. Possible
text-output changes that need human judgment remain non-blocking and are posted
as a single updatable PR warning comment as well as to the Actions summary. The
check uses no external AI or API key. Run it locally against another base with,
for example, `make check-docs-policy DOCS_POLICY_BASE=origin/master`.

## Desktop GUI

The GUI in [`gui/`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/gui) is a Rust application (eframe/egui) that runs the
CLI as a subprocess. Design notes: [`docs/gui/design.md`](../gui/design.md) and
[`docs/gui/cli-interface.md`](../gui/cli-interface.md).

Develop the GUI on the host, not in the development container: OpenGL/wgpu
windows do not work through the container. Run `mise install` at the repository
root to install the pinned Go and Rust toolchains. Mise reads the root
`rust-toolchain.toml` and uses [rustup](https://rustup.rs/) to install its
minimal profile plus clippy and rustfmt. Developers who do not use mise can
install rustup directly; its proxies select the same file automatically from
the repository root. On Linux, the usual desktop development packages
(X11/Wayland, xkbcommon) are needed to run it.

```bash
make gui-run    # build the CLI into out/gui/ and run the GUI against it
make gui-run-sample # the same, opening the standard 188-byte sample and analyzing the whole file
make gui-test   # cargo test
make gui-lint   # rustfmt check and clippy -D warnings
make gui-package # package for this OS into gui/target/release/ (needs cargo-packager)
```

To open and immediately analyze the same range while iterating on the UI:

```bash
make gui-run GUI_ARGS='sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --offset 0 --limit 67108864'
```

`make gui-run-sample` opens the standard 188-byte sample and analyzes the
whole file (`--limit 0`); set `SAMPLE` to open another file:

```bash
make gui-run-sample SAMPLE=sample_data/diagnostics/sample_204byte_all_findings.ts
```

`make gui-package` builds the CLI from this tree into `gui/target/bundled-cli/`
(a release bundles the published CLI release instead), writes
`gui/target/notices/THIRD-PARTY-NOTICES.txt` and runs
[cargo-packager](https://github.com/crabnebula-dev/cargo-packager) with the
configuration in `gui/Cargo.toml` (`[package.metadata.packager]`). Install the
versions pinned in `.github/workflows/gui-release.yml`
(`cargo install cargo-packager --version <version> --locked` and
`cargo install cargo-about --version <version> --locked --features cli`). The
notices cover the Rust crates of the GUI (cargo-about with `gui/about.toml` and
`gui/about.hbs`; a crate under a license missing from `accepted` fails the
build) and the Go standard library and the modules of the CLI for each released
target, written by `.github/scripts/go-third-party-notices.sh`, which also
writes the notices of the CLI release archives (see
[Release](release.md)). To check a
package, run its GUI executable with `--check-cli`: it prints which CLI it found
and from where, without opening a window, and exits non-zero when none works.
The icons in `gui/assets/icons/` are generated from `icon.svg` by
`gui/assets/icons/generate.sh` (macOS, ImageMagick).

`make gui-test` also runs the UI tests in `gui/src/app/ui_tests.rs`. They use
[egui_kittest](https://docs.rs/egui_kittest) (a dev-dependency kept at the
egui version) to drive the window through its accessibility tree, without a
native window, file dialog or CLI process. They find widgets by role and
accessible name, not by screen position. The state they need, such as a checked CLI,
an opened sample file or an analysis, is set directly from the fixtures in
`gui/tests/fixtures/` (CLI output for `sample_data`, regenerated with the CLI
when its output changes). egui_kittest's accessibility check is on, so every
input widget a test reaches needs an accessible name: tie a field to its label
with `labelled_by`, and give an icon-only button a name with `widget_info`. Run
only these tests with `cargo test --manifest-path gui/Cargo.toml ui_tests`.

### GUI memory profile

`gui/scripts/memory-profile.sh` measures the GUI's peak and retained heap for
a default and a large range, two programs, page navigation, a cancelled
analysis and a replacement file (needs ffmpeg; the inputs are generated
outside the repository). Run it before and after a change to what an analysis
keeps, and update the results in [GUI memory profile](../gui/memory-profile.md).

### README screenshots

The GUI images of the README and the GUI guide are rendered, not captured:

- `docs/images/gui-tabs.png`: Overview, Timeline, Bitrate and Packets in a
  2×2 grid.
- `docs/images/gui-byte-inspector.png` and `docs/images/gui-bit-inspector.png`:
  the Byte inspector and the Bit inspector, each on its own, with the 13-bit
  PID of a PMT packet selected. They are drawn by the same functions as the
  Packets screen.
- `docs/images/gui-wire-layout.png`: the detailed inspection page of that PMT
  packet, with its PID selected.
- `docs/images/gui-star-reminder.png`: Packets with the star reminder above
  the status bar. The other images are rendered without it.

```bash
make gui-screenshots        # render the images into docs/images/
make check-gui-screenshots  # render them again and compare with docs/images/
```

The generator is `gui/src/app/screenshots.rs`, a test behind the cargo feature
`screenshots`. The feature enables egui_kittest's wgpu renderer and image
output, so ordinary builds and tests do not depend on them. It renders the app
with the same fixtures as the UI tests, at a fixed size, pixels per point and
dark theme. It uses only the bundled fonts, never the OS Japanese font. The
input is shown as the relative path the fixture names, so the images contain
no local paths, times or cursor.

macOS is the canonical renderer:

- Regenerate the images there and commit them with the change that alters the
  UI.
- The `screenshots` job of the `GUI` workflow runs the check on `macos-latest`
  whenever GUI files, `docs/images/gui-*` or the README change. On a mismatch
  it fails and uploads `*.new.png` and `*.diff.png` as the `gui-screenshots`
  artifact. It never commits images.
- The pre-push hook also runs the check on macOS. Locally the same files are
  in `gui/target/screenshots/`.

GUI releases are made by the `GUI release` workflow and bundle the published
CLI release named in `gui/BUNDLED_CLI_VERSION`; see
[GUI release](release.md#gui-release). Pull requests that change the packaging
build and check the packages on all three OSes without publishing them.

The GUI finds the CLI through the `MPEG_TS_ANALYZER_CLI` environment variable,
then the path in Settings, a CLI bundled next to the GUI, and `mpeg-ts-analyzer`
on `PATH` ([details](../gui/design.md#cli-discovery-order)). `make gui-run` sets
the variable.

Editors using rust-analyzer need the GUI manifest declared, because
`Cargo.toml` is not at the repository root; in VS Code:

```json
{ "rust-analyzer.linkedProjects": ["gui/Cargo.toml"] }
```

The pre-push hook runs `make gui-lint` and `make gui-test` when `gui/`, the
`Makefile` or the Go/Rust toolchain configuration changed. On macOS it also
runs `make check-gui-screenshots` when those files, `docs/images/` or the README
changed. `make gui-lint` runs clippy with and without the `screenshots`
feature.
In CI, the `GUI` workflow runs for every pull request and reports one aggregate
check, `gui-ci`. Its lint and test jobs run only when `gui/`, the `Makefile`,
`mise.toml`, `rust-toolchain.toml`, `renovate.json` or the workflow changed;
otherwise they are skipped and `gui-ci` passes. The `Go` workflow combines
formatting, lint, build, tests, coverage and the README demo check in one
`go-ci` job. Go jobs are skipped for changes that only affect the GUI, Markdown
files other than `README.md`, or `docs/images/`. `README.md` (whose link to
`docs/demo.gif` the demo check requires), the JSON schemas and the demo sources
under `docs/` still run them. The
development container is built only when its inputs
(`compose.yaml`, `docker/`, `Makefile`, toolchain/module files or the Go
workflow) change, plus the weekly scheduled check; ordinary Go source changes
use the host CI toolchain and do not repeat coverage in Docker.

CodeQL uses the changed paths to analyze only Go or only Rust on pushes to
`master`, and only Go on pull requests: the Rust analysis takes several minutes
and is not a required check, so Rust code is analyzed after the merge. Its
weekly scheduled run still analyzes both languages. The weekly
`govulncheck` and `cargo-audit` runs cover new advisories against unchanged lock
files, so those expensive checks do not need to be repeated for unrelated
pushes to `master`. `cargo-audit` otherwise runs only when `gui/Cargo.lock` or
the `GUI` workflow changed, since the advisories concern the locked
dependencies, not the GUI's own code.

Pull requests restore `cargo-audit`, `cargo-about` and `cargo-packager` from a
cache keyed by their pinned version instead of compiling them each time.
Release runs never use the cache and build the tools from source.

The `GUI` workflow's lint, test and screenshot jobs restore the compiled Rust
dependencies with `Swatinem/rust-cache`, one cache per job and operating
system. Only pushes to `master` save it, so pull requests reuse `master`'s
cache without evicting it. `GUI release` does not use this cache.

The development-container job builds the image with Buildx and keeps its layers
in the GitHub Actions cache, so a run whose `docker/Dockerfile` is unchanged
reuses the installed packages and the TSDuck download. As with the Rust cache,
only pushes to `master` and the weekly run save it; the weekly run builds
without the cache so it still checks current package sources, and the image is
loaded under the name Compose uses, so the checks still run through
`docker compose run`. The Dockerfile retries the apt and TSDuck downloads
(`curl --retry-all-errors` also retries HTTP 5xx responses). Downloads made
inside third-party actions, such as the CodeQL bundle or the GoReleaser binary,
use those actions' own retries.

A newer push to a pull request cancels the superseded `Go`, `GUI`, `CodeQL`,
`Dependency review` and `Documentation policy` runs; the newer run reports the
required checks for the new head commit. Pushes to `master`, scheduled runs and
release runs are never cancelled this way.

Release publication is kept out of ordinary CI:

- `Release` publishes the CLI release when manually dispatched on `master`, or
  for a user-pushed `vX.Y.Z` tag that matches `VERSION`. It creates the tag
  only after a complete build has passed; see [Release](release.md).
  Pull requests that change its inputs run its checks and build as a dry run.
- `GUI release` publishes only when manually dispatched on `master` or for a
  user-pushed `gui-vX.Y.Z` tag, and creates the tag only after every package
  passed; see [GUI release](release.md#gui-release). On pull requests that
  change the packaging it builds and installs the packages and signs the
  update packages with a throwaway key as a dry run, and its signing with the
  release key and its publication job are skipped. The packaging means the icons, the notices
  configuration and scripts, `cli.rs`, `main.rs`, the toolchain, the workflow,
  `gui/BUNDLED_CLI_VERSION`, `VERSION`, the update signing key
  (`gui/updater.pub`), the release scripts and notes, and any
  change to `gui/Cargo.toml` other than
  dev-dependencies and the optional dependencies and features that the default
  features do not enable. Those, and `gui/Cargo.lock` changes, only check the
  third-party notices.
- `Bump Version` (CLI) and `GUI Bump Version` run only when manually
  dispatched.

## Dependency and supply-chain checks

| Check | Where | What it catches |
|---|---|---|
| Renovate | `renovate.json` | Weekly grouped updates (Go, Rust, GitHub Actions, CI tools), weekly `Cargo.lock` maintenance, and immediate security pull requests |
| Dependabot alerts | repository settings | Advisories for the dependency graph (`go.mod`, `gui/Cargo.lock`, Actions); Renovate reads them, and also OSV, to open security pull requests |
| `govulncheck` | `Go` workflow | Go advisories that reach the CLI's code, on Go changes and weekly |
| `cargo-audit` | `GUI` workflow | RustSec advisories in `gui/Cargo.lock`, when it changes and weekly |
| Dependency review | `Dependency review` workflow | A pull request that adds a dependency version with a known advisory |
| License check | `License check` workflow | A Go module of the CLI (go-licenses) or a Rust crate of the GUI (cargo-deny) under a license outside the allowlist |
| CodeQL | `CodeQL` workflow | Code-level issues in changed Go code (pull requests and `master`) and Rust code (`master`), with a weekly full scan |

Renovate waits three days before proposing a newly published crate
(`security:minimumReleaseAgeCrate`), so a release that crates.io pulls in its
first days is not adopted. Security fixes skip that delay, the weekly schedule
and major-update approval, and are never automerged: review them like any other
pull request once CI passes.

A scheduled `govulncheck` or `cargo-audit` failure opens an issue labeled
`security` (one per tool; later failures comment on it), because nothing else
would report an advisory against unchanged dependencies.

`cargo-audit` fails only for vulnerabilities. Unmaintained, unsound or yanked
crates appear as warnings in the job log; most come in through egui and `rfd`
and are resolved by updating those crates.

The `License check` workflow runs only when `go.mod`, `go.sum`, Go sources,
`.goreleaser.yaml`, `gui/Cargo.toml`, `gui/Cargo.lock`, the allowlist files or
the check itself change (on pull requests, including Renovate's, and on pushes
to `master`), or when dispatched manually; each of its Go and Rust jobs runs
only when its own inputs changed. The Go job also follows Go sources and
`.goreleaser.yaml` because the CLI's import graph, not `go.mod` alone, decides
which modules ship: `go.mod` also lists test-only modules, so importing one of
them from the CLI changes the release without touching `go.mod` or `go.sum`. It is not a required check, so unrelated pull requests never
wait for it. The allowlist is `accepted` in `gui/about.toml`, which also gates
the GUI's third-party notices:

- The Go job runs `go-licenses check` on the CLI for linux, windows and darwin
  (the module set differs by GOOS: cobra uses mousetrap only on Windows), with
  `accepted` without MPL-2.0, as printed by
  `.github/scripts/license-allowlist.py go`. The module itself is ignored.
  An unknown license fails the check.
- The Rust job runs `cargo deny check licenses` with `gui/deny.toml`, over the
  three released GUI targets without dev-dependencies. Its `allow` list plus
  its per-crate `exceptions` (MPL-2.0 for option-ext only) must equal
  `accepted`; `.github/scripts/license-allowlist.py check` fails otherwise.
  Only the `licenses` check is enabled.

To accept a new license, review it first (it must permit binary distribution
with the notices that `THIRD-PARTY-NOTICES.txt` provides), then add it to
`accepted` in `gui/about.toml` and to `allow` in `gui/deny.toml`, or, for a
license accepted for one crate only, to an `exceptions` entry of that crate
with a comment that explains why.

GitHub Actions are pinned to full commit SHAs and tools to exact versions
(`cargo-audit`, `cargo-deny` and `go-licenses` through `# renovate:`
comments), all updated by Renovate.
Release-time measures for the GUI (checksums, SBOM and build provenance) and the
remaining code-signing work are documented in
[`docs/gui/release-security.md`](../gui/release-security.md).

## Documentation

The Markdown in this repository is the documentation, reviewed with the code it
describes:

- `README.md` is the landing page: what the analyzer does, the GUI images,
  installation and a quick start.
- [`docs/index.md`](../index.md) is the documentation home and lists every
  page.
- The user guides are under `docs/getting-started/`, `docs/cli/`, `docs/gui/`
  (`overview.md`) and `docs/reference/`. Contributor pages are under
  `docs/development/`, next to the GUI design notes in `docs/gui/`.

Each topic has one page; link to it instead of repeating it. Flags, the help
text, verdicts and exit codes belong in [`docs/cli/usage.md`](../cli/usage.md),
and the JSON outputs in
[`docs/reference/json-output.md`](../reference/json-output.md); the
documentation-policy check enforces both. `OPTIONS.md`, `FUZZING.md` and
`docs/DEVELOPMENT.md` remain only as pointers to the pages that replaced them.
Outside `docs/`,
[`DIRECTORY_STRUCTURE.md`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/DIRECTORY_STRUCTURE.md)
maps the repository and
[`tools/README.md`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/tools/README.md)
lists the fixture generators; the samples they write are described in
[Sample data](sample-data.md).

### Documentation site

The same pages are published as a site on GitHub Pages,
<https://small-teton.github.io/mpeg-ts-analyzer/>, with navigation and search.
The Markdown stays the only source: the site adds nothing that GitHub does not
show.

- [`mkdocs.yml`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/mkdocs.yml)
  configures the site with [MkDocs](https://www.mkdocs.org/) and
  [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/). Its
  `nav` lists every published page, so a new page under `docs/` must be added
  there too. `docs/DEVELOPMENT.md` and the demo's render scripts are not
  published.
- The build is strict: an unlisted page, a missing page or a broken link or
  anchor fails it. Heading anchors follow GitHub's rules, so an in-page link
  works on both GitHub and the site. Link to files outside `docs/` (the
  README, `gui/`, `VERSION`) with their GitHub URLs, since the site does not
  contain them.
- Show images with Markdown image syntax, not an HTML
  `<img>` tag. MkDocs rewrites Markdown paths for the site's page URLs but
  leaves paths in HTML as written, and the strict build does not check them.
- `.github/workflows/docs-pages.yml` builds the site on pull requests that
  change `docs/`, `mkdocs.yml` or the toolchain, and publishes it from
  `master`.
- The toolchain is pinned with hashes in `.github/docs-site/requirements.txt`,
  compiled with `pip-compile --generate-hashes` from
  `.github/docs-site/requirements.in`. Renovate updates both.

To build and preview the site locally:

```bash
python3 -m venv .venv-docs
.venv-docs/bin/pip install --require-hashes --no-deps -r .github/docs-site/requirements.txt
.venv-docs/bin/mkdocs build --strict    # into site/
.venv-docs/bin/mkdocs serve             # http://127.0.0.1:8000/
```

Material for MkDocs is in maintenance mode: its maintainers announced that it
reaches end of life on May 5, 2027, and that its successor is
[Zensical](https://zensical.org/), which reads `mkdocs.yml`. Zensical was in
beta when this site was set up. The pages use only plain Markdown, tables,
admonitions and heading anchors, so moving to Zensical should need only the
toolchain and the workflow; plan the move before the end of life.

## README demo

The animated terminal demo near the top of the [README](https://github.com/small-teton/mpeg-ts-analyzer#readme) is
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
animation before committing it. See [demo/README.md](../demo/README.md) for
generation and verification details.
