# Action usage and reference

Return to the [project overview](../README.md), see the [comparison with setup-go](COMPARISON.md), or read [architecture and resource behavior](ARCHITECTURE.md).

## Usage

Examples pin bundled preview `e6e119a8b0b524a6ac1606d660c48603c11753df`, which includes all six native executables. Always pin a **bundled distribution commit** supporting the inputs you use; source-only revisions cannot run directly as an action.

If the action repository is private, the workflow must have permission to access it. Where direct private-action sharing is unavailable, check out the bundled revision using a token with read access to that repository, then invoke its local path. The token is for acquisition, not a setup-go-better input:

```yaml
- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
- uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  with:
    repository: nicoheidtke/setup-go-better
    ref: e6e119a8b0b524a6ac1606d660c48603c11753df
    token: ${{ secrets.SETUP_GO_BETTER_TOKEN }}
    path: .github/private-actions/setup-go-better
    persist-credentials: false
- uses: ./.github/private-actions/setup-go-better
  with:
    go-version-file: go.mod
    cache: restore-save
```

```yaml
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: nicoheidtke/setup-go-better@e6e119a8b0b524a6ac1606d660c48603c11753df
        with:
          go-version: '1.26.x'
          cache: restore-save
          cache-scope: unit-tests
      - run: go test ./...
```

Exact versions, such as `1.26.8`, reuse an installed match without a network request. Minor selectors (`1.26` or `1.26.x`) prefer the highest installed matching patch. Set `check-latest: true` to resolve a minor selector against current official release metadata instead.

Alternatively, read the checked-out project's version file:

```yaml
- uses: nicoheidtke/setup-go-better@e6e119a8b0b524a6ac1606d660c48603c11753df
  with:
    go-version-file: go.mod
    cache: restore-save
```

Checkout must run first. `go-version-file` accepts a `go.mod` or `go.work` path, relative to `GITHUB_WORKSPACE` (the current directory for standalone use); absolute paths also work. Explicit `go-version` takes precedence and the file is not read in that case. At least one input is required; no version file is discovered implicitly.

A concrete `toolchain go1.26.8` directive selects that exact preferred compiler; otherwise the `go` directive is used. `toolchain default` falls back to the `go` directive. A patch directive such as `go 1.26.0` selects that exact patch, while `go 1.26` selects the minor with the installed-patch preference described above. A preferred toolchain older than the `go` minimum is rejected. The selected compiler must be Go 1.26 or newer; an older language minimum is accepted when paired with a supported preferred toolchain.

Go 1.x starting at 1.26 is supported. Prereleases, aliases such as `stable`, custom toolchain names, and arbitrary semver ranges are not accepted.

## Inputs

| Input | Default | Behavior |
| --- | --- | --- |
| `go-version` | Empty | Exact Go version or minor selector; overrides `go-version-file`. |
| `go-version-file` | Empty | Path to `go.mod` or `go.work`; used when `go-version` is empty. |
| `check-latest` | `false` | Check official metadata for the latest patch of a minor selector. |
| `cache` | `off` | `off`, `restore` (restore only), or `restore-save` (restore and automatic post-job save). |
| `cache-scope` | Empty | Explicit matrix or workload variant, in addition to repository/workflow/job identity. |
| `cache-chunk-concurrency` | `4` | Chunk compression/lookup/transfer workers, 1 through 16. Set to 1 for sequential chunk processing. |
| `cache-storage` | `archive` | Whole `archive` or content-defined `chunked` snapshots on GitHub cache. |
| `cache-overlap` | `true` | Prefetch cache data while the selected Go toolchain installs; merge after installation. |
| `cache-layout` | `split` | `split` module/build archives or a `combined` archive. |
| `cache-compression` | `gzip` | `gzip` or `none`. |
| `cache-compression-concurrency` | `4` | Whole-archive gzip workers, 1 through 16. Chunked storage uses its own compression workers. |
| `cache-restore-concurrency` | `4` | Validated-file publication workers, 1 through 16. Set 1 for sequential publication. |
| `cache-dependency-path` | Automatic scan | Newline-separated workspace-relative files or directories. Directories recursively discover Go manifests. |
| `cache-build-max-age` | `0` | Optional build-object age filter, for example `120h`. Zero disables it; modules are never age-filtered. |
| `cache-max-bytes` | `10737418240` | Per-archive limit for expanded tar and transferred archive bytes; 1 through 10 GiB. Includes tar headers and padding. |
| `cache-strict` | `false` | Fail on cache operational errors instead of warning and continuing. Invalid inputs always fail. |
| `cache-upload-concurrency` | `4` | Parallel upload workers, 1 through 32. |
| `cache-upload-chunk-bytes` | `33554432` | Upload block size in bytes, 1 through 4194304000. |
| `cache-download-concurrency` | `4` | Whole-archive download workers, 1 through 32. Set 1 for a single GET. |
| `cache-download-chunk-bytes` | `33554432` | Whole-archive download range size in bytes, 1 through 4194304000. |
| `cache-save-on-failure` | `false` | Save after failed jobs as well as successful jobs. Cancelled jobs never save. |
| `cache-report` | Empty | Optional restore JSON path. The post-job save report appends `.post.json`. |

## Cache behavior

For transfer pipelines, chunk layout, worker memory and staging details, see [architecture and resource behavior](ARCHITECTURE.md).

Caching uses GitHub Actions cache v2 and the runner's injected credentials. The selected Go executable supplies `GOCACHE` and `GOMODCACHE`, respecting your configured paths. Cache-off avoids cache path discovery, dependency scanning, transfer, and snapshot creation.

Snapshots include operating system, architecture, exact selected Go version, repository/workflow/job identity, explicit scope, archive layout/compression, and a dependency fingerprint. The fingerprint covers relative names and contents of `go.mod`, `go.sum`, `go.work`, and `go.work.sum` across discovered modules. Automatic discovery excludes `.git`, `.cache`, `vendor`, `node_modules`, `dist`, `bin`, and `.setup-go-better`. Explicit files may use other names.

Restore first looks for recent dependency-matching snapshots, then compatible snapshots in the same namespace so dependency changes can reuse existing objects. GitHub enforces branch visibility and default-branch fallback. Build/test/lint jobs have separate job identities; matrix variants within one job must include their differing dimensions in `cache-scope`:

```yaml
- uses: nicoheidtke/setup-go-better@e6e119a8b0b524a6ac1606d660c48603c11753df
  with:
    go-version: '${{ matrix.go }}'
    cache: restore-save
    cache-scope: 'test-race=${{ matrix.race }}'
    cache-dependency-path: |
      go.work
      services
      libraries
```

A save uses a fresh immutable key incorporating run and attempt identity plus a random invocation suffix. In split mode, restored module contents are compared with the local tree before packing: unchanged paths, file contents and permissions skip compression and upload even if timestamps changed. Same-size content changes are detected. Build archives remain fresh. Empty archives skip upload. Combined mode always considers the complete snapshot.

Downloads are validated in staging before local caches are modified. Existing local regular files win, and unrelated entries are retained. Links, unsafe paths, special files, duplicate entries, and case collisions are rejected. Corrupt downloads leave local data untouched; a merge I/O error may leave successfully added new files. The action targets trusted GitHub-hosted runner filesystems.

Build age filtering affects only recognized hash-named Go build objects in the archive. It never deletes local files. It is disabled by default because Go cache mtime behavior and suitable grace periods require workload measurements.

Cache errors warn by default. Unavailable runtime credentials, denied writes, and transport failures therefore allow the job to continue. There is no automatic network retry. Use `cache-strict: true` for explicit correctness checks. Tiny jobs can leave caching off or choose restore-only to control total transfer and CPU cost.

## Outputs and environment

| Output | Value |
| --- | --- |
| `go-version` | Selected exact version without a `go` prefix. |
| `goroot` | Absolute selected toolchain path. |
| `cache-hit` | `true` when all requested archives restore successfully; otherwise `false`. |
| `cache-key` | Comma-separated fresh immutable snapshot keys. |
| `cache-matched-key` | Comma-separated keys returned by compatible lookup. |
| `cache-restored-bytes` | Received archive payload bytes. |
| `cache-restore-ms` | Restore elapsed milliseconds, excluding cache path discovery and dependency hashing. |

Cache outputs are written when caching is enabled. The action sets `GOROOT` and adds its `bin` directory to subsequent steps' `PATH`. It leaves `GOTOOLCHAIN` unchanged. Set `GOTOOLCHAIN: local` in your job if Go must use the selected installation without automatic toolchain switching.

Chunked reports also record chunk count, reused chunk count and reused compressed bytes. Cache reports record preparation, lookup, transfer and archive processing times, transferred bytes, archive file/payload counts, matches, skips and errors. Post-job save runs after regular workflow steps, so an earlier upload-artifact step cannot collect a report that has not yet been written. Save reports also appear in post-step logs.

## Installation and platforms

Completed toolchains in the runner's tool cache and the action's installation directory are inspected first. Missing versions are resolved through official Go metadata, downloaded with SHA-256 and size verification, extracted in staging, then published with a directory rename. Interrupted downloads are never marked installed; existing installations are not overwritten.

| Operating system | Architectures |
| --- | --- |
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |
| Windows | amd64, arm64 |

## Standalone executable

```text
setup-go-better setup --go-version 1.26.8
setup-go-better setup --go-version 1.26.x --check-latest
setup-go-better setup --go-version-file go.mod
setup-go-better setup --go-version 1.26.8 --json result.json
```

Use `--json -` for a report on standard output; diagnostics go to standard error. Installer reports include the selected toolchain, installation source, phase timings, transferred bytes and peak temporary logical file payload.

Outside GitHub Actions, installation uses the operating system's temporary directory when `RUNNER_TEMP` is unset. The executable reports the path and does not modify its parent shell's environment. Automatic cache restore/save requires the GitHub action runtime.
