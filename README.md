# setup-go-better

A native Go toolchain installer for GitHub Actions, focused on fast setup, efficient downloads, and a small runtime footprint.

`setup-go-better` selects an existing Go installation whenever possible. When a download is necessary, it verifies an official Go archive and extracts it directly into a staging directory before making it available to subsequent steps.

The installer is written in Go and distributed as precompiled executables. The default action uses a small Bash launcher; it does not compile itself, download its own executable, or install npm packages during your job.

## Usage

Pin the action to a bundled distribution commit. Source-only commits do not include the native executables.

```yaml
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: nicoheidtke/setup-go-better@RELEASE_COMMIT
        with:
          go-version: '1.26.x'
      - run: go test ./...
```

Use an exact version for reproducible toolchain selection:

```yaml
- uses: nicoheidtke/setup-go-better@RELEASE_COMMIT
  with:
    go-version: '1.26.4'
```

An installed exact version is selected without a network request. Minor selectors prefer the highest matching installed patch. To check for the latest published patch instead:

```yaml
- uses: nicoheidtke/setup-go-better@RELEASE_COMMIT
  with:
    go-version: '1.26.x'
    check-latest: true
```

## Inputs

| Input | Required | Default | Behavior |
| --- | --- | --- | --- |
| `go-version` | Yes | — | An exact version such as `1.26.4`, or a minor selector such as `1.26` or `1.26.x`. |
| `check-latest` | No | `false` | For minor selectors, check official metadata for the latest matching stable patch. Exact versions remain exact. |

Go 1.x releases starting with 1.26 are accepted. Prereleases, aliases such as `stable`, version files, and arbitrary semver ranges are not accepted.

## Outputs and environment

| Output | Value |
| --- | --- |
| `go-version` | Selected exact version, without a `go` prefix. |
| `goroot` | Absolute path to the selected toolchain. |

The action sets `GOROOT` and adds its `bin` directory to subsequent steps' `PATH`. It leaves `GOTOOLCHAIN` unchanged. Go's own automatic toolchain selection can therefore choose a different version when a module requires it; set `GOTOOLCHAIN: local` in your workflow if you require the selected installation to be used without automatic switching.

```yaml
- uses: nicoheidtke/setup-go-better@RELEASE_COMMIT
  id: go
  with:
    go-version: '1.26.4'
- run: go version
```

## Installation behavior

1. Inspect completed toolchains in the runner's tool cache and the action's installation directory.
2. Reuse an installation matching the requested version, unless a minor selector requires a latest-patch check.
3. Resolve a missing version using official Go release metadata. Consult historical metadata only when necessary.
4. Download and verify the archive's SHA-256 and size. Extract into a temporary directory and publish the verified installation with a directory rename.

New installations live under `RUNNER_TEMP/setup-go-better/toolchains`. Existing runner toolchains are used in place. Linux and macOS archives are streamed through extraction; Windows ZIP archives are temporarily stored for random-access extraction.

An interrupted or invalid download is never marked as an installed toolchain. Existing installations are not overwritten.

This action installs the Go toolchain. Configure module and build caching separately in your workflow.

## Platform builds

Native executables are provided for:

| Operating system | Architectures |
| --- | --- |
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |
| Windows | amd64, arm64 |

The action targets GitHub-hosted runners and uses their Bash shell to launch the matching executable. It does not require a preinstalled Go compiler.

## Standalone executable

The same installer can be called directly:

```text
setup-go-better setup --go-version 1.26.4
setup-go-better setup --go-version 1.26.x --check-latest
setup-go-better setup --go-version 1.26.4 --json result.json
```

Use `--json -` to write the result to standard output. Diagnostic messages go to standard error. Reports include the selected toolchain, installation source, phase timings, transferred payload bytes, and peak temporary file payload size.

Outside GitHub Actions, the installer uses the operating system's temporary directory when `RUNNER_TEMP` is unset. It reports the installation path; it does not modify your parent shell's environment.

## Design

Toolchain reuse avoids unnecessary requests and writes. Streaming extraction avoids a second copy of the extracted tree. Version selection is deliberately limited to explicit versions and minor releases.

An alternative Node launcher invokes the same native executable using only Node's built-in APIs:

```yaml
- uses: nicoheidtke/setup-go-better/experimental/node@RELEASE_COMMIT
  with:
    go-version: '1.26.4'
```

Installation and version-selection behavior are identical between launchers. See [benchmark methodology](docs/BENCHMARKS.md) for how their runtime costs are compared.
