# Directory structure

A map of what each directory and top-level file of this repository is for, who
uses it, and where to read more. The division of the Go code and where a new
check belongs are in [Architecture](docs/development/architecture.md).

## Command-line analyzer (Go)

| Path | What it is | Read more |
| --- | --- | --- |
| `main.go` | Entry point of the `mpeg-ts-analyzer` binary; calls `cmd`. | [Architecture](docs/development/architecture.md#packages) |
| `cmd/` | Command-line parsing (Cobra), flag validation and the exit status. | [CLI usage](docs/cli/usage.md) |
| `options/` | Options shared by the command and the parser. | [Architecture](docs/development/architecture.md#packages) |
| `bitbuffer/` | Bit-level reading of byte buffers. 100.0% coverage required. | [Architecture](docs/development/architecture.md#packages) |
| `tsparser/` | Packet-size detection, framing, synchronization, TS/PSI/PES decoding, the single analysis pass, and the report types with their text and JSON rendering. Also holds the fixture tests. 100.0% coverage required. | [Architecture](docs/development/architecture.md) |
| `internal/analysis/` | Check policy: bounded aggregation, applicability, thresholds and statuses. Imports only the standard library. 100.0% coverage required. | [Architecture](docs/development/architecture.md#who-decides-what) |
| `go.mod`, `go.sum` | The Go module `github.com/small-teton/mpeg-ts-analyzer/v2`. | |

## Desktop GUI (Rust)

| Path | What it is | Read more |
| --- | --- | --- |
| `gui/` | The eframe/egui desktop application. It runs the CLI as a subprocess and contains no analysis logic. `src/` the application, `tests/fixtures/` CLI outputs and signed update data for its tests, `assets/` fonts and icons, `scripts/` signing, notices and memory-profile scripts, `BUNDLED_CLI_VERSION` the CLI release it bundles, `updater.pub` the update signing key. | [GUI design](docs/gui/design.md), [GUI CLI interface](docs/gui/cli-interface.md), [Development](docs/development/index.md#desktop-gui) |
| `rust-toolchain.toml` | The pinned Rust toolchain for `gui/`. | [Development](docs/development/index.md#desktop-gui) |

## Test data and tools

| Path | What it is | Read more |
| --- | --- | --- |
| `sample_data/` | Small, healthy streams (188-byte TS and 192-byte M2TS) for trying the analyzer, the README demo and tests. | [`sample_data/README.md`](sample_data/README.md), [Sample data](docs/development/sample-data.md) |
| `sample_data/diagnostics/` | Intentionally faulty or edge-case streams (188, 192 and 204-byte packets), written by the generators in `tools/` and checked against them by tests. | [`sample_data/diagnostics/README.md`](sample_data/diagnostics/README.md) |
| `tools/` | Go programs for development: the deterministic fixture generators (`generate_*`) and the documentation-policy check (`docs_policy`). | [`tools/README.md`](tools/README.md) |

## Documentation

| Path | What it is | Read more |
| --- | --- | --- |
| `README.md` | The landing page: what the analyzer checks, GUI images, installation and a quick start. | |
| `docs/` | The user and contributor documentation, published as a site: `getting-started/`, `cli/`, `gui/`, `reference/` and `development/` pages, and `index.md`, the documentation home. | [Documentation home](docs/index.md) |
| `docs/json-schema-*.json` | JSON Schemas of the machine-readable outputs, validated by tests. | [Machine-readable output](docs/reference/json-output.md) |
| `docs/demo/`, `docs/demo.gif` | The container-based renderer of the README demo animation, its manifest, and the animation. | [README demo](docs/demo/README.md) |
| `docs/images/` | GUI screenshots shown by the README, rendered from fixtures. | [Development](docs/development/index.md#readme-screenshots) |
| `mkdocs.yml` | Configuration and navigation of the documentation site. | [Documentation site](docs/development/index.md#documentation-site) |
| `DIRECTORY_STRUCTURE.md` | This map. | |
| `OPTIONS.md`, `FUZZING.md`, `docs/DEVELOPMENT.md` | Pointers kept for old links to the pages that replaced them. | |
| `AGENTS.md`, `CLAUDE.md` | Instructions for AI coding agents: validation, coverage, documentation, commit and PR rules. `CLAUDE.md` points to `AGENTS.md`. | [`AGENTS.md`](AGENTS.md) |
| `SECURITY.md`, `LICENSE` | The security policy and the license. | |

## Build, development environment and CI

| Path | What it is | Read more |
| --- | --- | --- |
| `Makefile` | Build, test, coverage, documentation-policy, demo and GUI targets. | [Test and coverage](docs/development/index.md#test-and-coverage) |
| `VERSION` | The CLI version (`X.Y.Z`), injected at build time. | [Release](docs/development/release.md) |
| `mise.toml` | Pinned Go version, and the Rust version through `rust-toolchain.toml`, for `mise install`. | [Development](docs/development/index.md#desktop-gui) |
| `docker/`, `compose.yaml`, `.dockerignore` | The development container image (Go, FFmpeg, TSDuck) and its Compose service. | [Containerized development environment](docs/development/index.md#containerized-development-environment) |
| `.devcontainer/` | Dev Container definition that uses the Compose service. | [Containerized development environment](docs/development/index.md#containerized-development-environment) |
| `.githooks/` | The `pre-push` hook (`make setup` enables it): build, lint, tests, coverage, demo and GUI checks. | [Test and coverage](docs/development/index.md#test-and-coverage) |
| `.golangci.yml` | golangci-lint configuration. | |
| `.goreleaser.yaml` | GoReleaser configuration of the CLI release builds. | [Release](docs/development/release.md) |
| `.github/workflows/` | CI (Go, GUI, CodeQL, dependency review, documentation policy and site) and the CLI and GUI release and version-bump workflows. | [Dependency and supply-chain checks](docs/development/index.md#dependency-and-supply-chain-checks), [Release](docs/development/release.md) |
| `.github/scripts/`, `.github/gui-release-notes.md` | Helper scripts and the release-notes template of the release workflows. | [Release](docs/development/release.md) |
| `.github/tools/` | A separate Go module that pins CI tools (golangci-lint, govulncheck), run with `go tool -modfile`. | |
| `.github/docs-site/` | The hash-pinned MkDocs toolchain of the documentation site. | [Documentation site](docs/development/index.md#documentation-site) |
| `.github/ISSUE_TEMPLATE/`, `.github/pull_request_template.md`, `.github/CODEOWNERS` | Issue forms, the pull-request template and code owners. | [`AGENTS.md`](AGENTS.md#contribution-workflow) |
| `renovate.json` | Renovate configuration for dependency updates. | [Dependency and supply-chain checks](docs/development/index.md#dependency-and-supply-chain-checks) |
| `.gitattributes`, `.gitignore` | Git attributes (binary update fixtures) and ignored paths. | |

## Not committed

`out/` (build and coverage output), `dist/` (release builds), `gui/target/`
(Rust build output), `site/` and `.venv-docs/` (local documentation-site
builds), and `local/` (private, machine-specific agent rules and helpers) are
ignored by Git.
