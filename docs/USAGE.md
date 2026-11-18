# Action usage and reference

Return to the [project overview](../README.md), see the [comparison with setup-go](COMPARISON.md), or read [architecture and resource behavior](ARCHITECTURE.md). For permissions and cache trust, see the [security model](SECURITY_MODEL.md). For misses, save outcomes and startup errors, see [troubleshooting](TROUBLESHOOTING.md).

## Usage

Examples use the `v2` major version tag. It follows compatible releases. For a fixed revision, use the full distribution SHA from the [release notes](https://github.com/nicoheidtke/ready-setup-go/releases); source commits do not contain the packaged action.

Version 2 introduces the baseline save policies described below. Existing v1 pins retain their previous behavior. See the [short migration guide](https://github.com/nicoheidtke/ready-setup-go/blob/fe5d75d54cb3e62a1e95656e3f511cc30fbbfe89/docs/RELEASES.md#baseline-cache-policy-migration).

```yaml
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: nicoheidtke/ready-setup-go@v2
        with:
          go-version: '1.26.x'
          cache: restore-save
          cache-scope: unit-tests
      - run: go test ./...
```

Exact versions, such as `1.26.8`, reuse an installed match without a network request. Minor selectors (`1.26` or `1.26.x`) prefer the highest installed matching patch. Set `check-latest: true` to resolve a minor selector against current official release metadata instead.

Alternatively, read the checked-out project's version file:

```yaml
- uses: nicoheidtke/ready-setup-go@v2
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
| `summary` | `true` | Append concise toolchain and cache results to the GitHub job summary. Set `false` to disable main and post-job summaries. |
| `cache` | `off` | `off`, `restore` (restore only), or `restore-save` (restore and automatic post-job save). |
| `cache-save-policy` | `on-miss` | Save a missing dependency baseline, or `always` to refresh eligible snapshots after hits. |
| `cache-save-branches` | `default` | Save only on the repository default branch, or `all` to allow writers in other branch/PR scopes. Applies to modules and builds. |
| `cache-scope` | Empty | Explicit matrix or workload variant, in addition to repository/workflow/job identity. |
| `cache-module-scope` | Empty | Opt-in repository module sharing group across workflows, jobs, scopes, and Go versions. Requires `split`; all eligible writers must be trusted. |
| `cache-gzip-level` | `default` | Gzip level `1` through `9`, or the existing standard-library default. Lower levels favor compression speed over encoded size. Ignored for other codecs. |
| `cache-chunk-concurrency` | `4` | Chunk compression/lookup/transfer workers, 1 through 16. Set to 1 for sequential chunk processing. |
| `cache-storage` | `archive` | Whole `archive` or content-defined `chunked` snapshots on GitHub cache. |
| `cache-overlap` | `true` | Prefetch cache data while the selected Go toolchain installs; merge after installation. |
| `cache-layout` | `split` | `split` module/build archives or a `combined` archive. |
| `cache-compression` | `gzip` | `gzip`, `zstd`, or `none`. The codec is bundled; no runner compression tool is required. |
| `cache-compression-concurrency` | `4` | Whole-archive compression workers, 1 through 16. Chunked storage uses its own compression workers. |
| `cache-restore-concurrency` | `4` | Validated-file publication workers, 1 through 16. Set 1 for sequential publication. |
| `cache-dependency-path` | Automatic scan | Newline-separated workspace-relative files or directories. Directories recursively discover Go manifests. |
| `cache-build-save` | `true` | In split layout, set `false` to skip build packing and upload, including on a miss. Module saves and all restores are unaffected. Use a workflow expression for save frequency. |
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

Packaged distributions support zstd with both archive and chunked storage. Set `cache-compression: zstd` with either archive or chunked storage when using a distribution that supports it. Gzip remains the default. Compression choices use separate cache identities, so switching codecs starts with a cold cache. The bundled codec needs no `zstd` or `tar` executable on the runner.

Packaged distributions also support `cache-module-scope`, `cache-build-save`, and `cache-gzip-level`. Gzip levels retain the existing archive namespace and decoder compatibility; encoded chunk hashes can differ by level, reducing chunk reuse when levels change. Existing defaults and keys remain unchanged.

For transfer pipelines, chunk layout, worker memory and staging details, see [architecture and resource behavior](ARCHITECTURE.md).

Caching uses GitHub's cache service v2 API directly from Go, with the runner's injected credentials; it needs no PAT or extra `GITHUB_TOKEN` permission. The service API version is separate from the `actions/cache` action's release version (for example, `actions/cache@v6`); this action does not invoke `actions/cache`. The selected Go executable supplies `GOCACHE` and `GOMODCACHE`, respecting your configured paths. Cache-off avoids cache path discovery, dependency scanning, transfer, and snapshot creation. The action's `cache` input requests operations within the runner's allowed access; GitHub's workflow/job `cache-mode` independently limits its scoped token. See [policy and trust boundaries](SECURITY_MODEL.md#action-policy-and-runner-policy), including why restore-only still trusts cached build inputs.

Snapshots include operating system, architecture, exact selected Go version, repository/workflow/job identity, explicit scope, archive layout/compression, and a dependency fingerprint. The fingerprint covers relative names and contents of `go.mod`, `go.sum`, `go.work`, and `go.work.sum` across discovered modules. Automatic discovery excludes `.git`, `.cache`, `vendor`, `node_modules`, `dist`, `bin`, and `.ready-setup-go`. Explicit files may use other names. Source revisions with [dependency resource limits](ARCHITECTURE.md#cache-lifecycle-and-transfer) reject oversized selections during cache preparation; narrow `cache-dependency-path` to the relevant manifests when needed. Accepted selections keep their existing fingerprints.

Restore first looks for recent dependency-matching snapshots, then compatible snapshots in the same namespace so dependency changes can reuse existing objects. GitHub enforces branch visibility and default-branch fallback. Build/test/lint jobs have separate job identities; matrix variants within one job must include their differing dimensions in `cache-scope`:

```yaml
- uses: nicoheidtke/ready-setup-go@v2
  with:
    go-version: '${{ matrix.go }}'
    cache: restore-save
    cache-scope: 'test-race=${{ matrix.race }}'
    cache-dependency-path: |
      go.work
      services
      libraries
```

Set the same nonempty `cache-module-scope` in trusted jobs to share module snapshots across workflow/job identities, `cache-scope` variants, and selected Go versions. This opt-in uses a separate module namespace; operating system, architecture, storage, compression, and dependency matching remain partitioned. Build snapshots retain their full existing isolation. Returning to an empty module scope restores the original namespace. Scope names are grouping labels, not access controls; GitHub branch/token visibility still applies. Module caches are build inputs, including executable toolchain downloads: only group writers whose contents every reader trusts.

A save uses a fresh immutable key incorporating run and attempt identity plus a random invocation suffix. Matching and compatible lookup prefixes remain stable, so existing snapshots can still restore. An exact dependency match means the current fingerprint matched and the complete snapshot restored successfully; it does not require equality with the new invocation's save key. Empty archives skip upload.

### Choosing a save policy

With `cache: restore-save`, the defaults maintain a dependency baseline on the repository's default branch. Every branch tries a dependency match first, then compatible fallback. A successful default-branch job saves nonempty snapshots that did not restore for the current dependency fingerprint. Successfully restored matches skip save-time directory scans, fingerprinting, compression and upload. Split module/build snapshots decide independently; combined snapshots follow the same rule.

Dependency changes can restore the previous baseline and seed a new one. A source-only edit does not refresh the baseline: Go rebuilds results that no longer match, and that new work is not retained remotely. The module cache (`GOMODCACHE`) holds downloaded source and metadata; the build cache (`GOCACHE`) mixes compiled dependencies, application packages and cached test results. This policy deliberately trades repeated application work for fewer uploads. It does not compare build contents or automatically refresh by age. Eviction, deletion or compatibility changes can also require seeding again; repeatedly accessed entries need not expire.

Default-branch eligibility requires `GITHUB_REF` to match `repository.default_branch` in the runner's `GITHUB_EVENT_PATH`, with matching repository identity. Missing, invalid or larger-than-1-MiB event metadata skips saves with an explanation, even in strict mode. Feature branches, PR merge refs and tags restore without saving by default. Post rechecks branch and runner permissions. Standalone callers without runner event metadata can explicitly select `cache-save-branches: all`.

| Goal | Configuration | Tradeoff |
| --- | --- | --- |
| Default-branch baseline | `cache: restore-save` | Warm dependency matches avoid saves; source-only results are not refreshed. |
| Long-lived branch baselines | Also set `cache-save-branches: all` | Branch/PR writers seed changed dependency baselines in their GitHub ref scopes. |
| Retain recent compiled results | Also set `cache-save-policy: always` | Every eligible nonempty build snapshot is packed and saved after hits. |
| Consume existing caches without any saves | `cache: restore` | Neither module nor build caches are seeded or refreshed. |

Writer eligibility and refresh frequency are independent. For example, permit branch-local baselines and refresh builds every fifth workflow run:

```yaml
cache: restore-save
cache-save-branches: all
cache-save-policy: "${{ (endsWith(github.run_number, '0') || endsWith(github.run_number, '5')) && 'always' || 'on-miss' }}"
```

This is a workflow-run-number policy, not an elapsed-time interval or guaranteed refresh cadence. Failed, skipped or cancelled eligible runs can leave a longer gap. Set `always` directly for every eligible run. In always mode, split modules retain the full-content comparison: unchanged paths, permissions and contents skip packing/upload even if timestamps change; changed contents, including same-size changes, can save. Combined mode considers the complete snapshot.

GitHub controls branch visibility and default-branch fallback; branch names need not enter cache keys. A PR-created cache is scoped to its merge ref. The action's workflow/job and matrix isolation still applies: each consuming job/scope needs an eligible writer in that namespace. Saving only in a build job does not seed a separate test job. Shared module groups should be seeded with all needed module contents, or separated when their workloads differ; the first matching baseline is retained under on-miss even if a later job downloads more modules.

`cache-build-save: false` remains an additional split-layout opt-out. It skips build packing/upload even on a miss, so it prevents seeding as well as refresh. It does not change module policy. For example, disable build snapshots while retaining eligible module saves:

```yaml
cache: restore-save
cache-build-save: false
```

False with combined layout is rejected because modules and builds share one archive. To deliberately refresh without discarding compatible history, select `always` for the desired invocation. Avoid putting commit identity in `cache-scope`: that prevents reuse. Compare complete workflow time, repeated compilation and storage before choosing a different policy; fewer uploads alone do not establish a speedup.

Downloads are validated in staging before local caches are modified. Existing local regular files win, and unrelated entries are retained. Links, unsafe paths, special files, duplicate entries, and case collisions are rejected. Corrupt downloads leave local data untouched; a merge I/O error may leave successfully added new files. The action targets trusted GitHub-hosted runner filesystems.

Build age filtering affects only recognized hash-named Go build objects in saved snapshots, for both archive and chunked storage. It never deletes local files. It is disabled by default because Go cache mtime behavior and suitable grace periods require workload measurements.

Cache errors warn by default. Unavailable runtime credentials, denied writes, and transport failures therefore allow the job to continue. Replay-safe requests (toolchain downloads/metadata, cache lookup/download and deterministic blob uploads) use up to three attempts for selected transient network/HTTP failures, with cancellable jittered backoff and `Retry-After` capped at five seconds. Cache reservation/finalization, partially consumed responses, invalid data and permission failures are not retried. See the [retry boundaries](../internal/cachebackend/PROTOCOL.md) for exact coverage. Use `cache-strict: true` for explicit correctness checks. Tiny jobs can leave caching off or choose restore-only to control total transfer and CPU cost.

## Outputs and environment

With `GITHUB_STEP_SUMMARY`, setup appends toolchain/restore results and post appends a separate save summary. Set `summary: 'false'` to disable both. Summary file failures are nonfatal. Summaries escape text, redact credentials/URLs, and omit full keys and paths.

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

Normal logs show `HIT`, `MISS`, `SKIP`, `SAVED`, or `ERROR`, transferred payload, and elapsed time. Each snapshot shows its full lookup key and any matched key during restore, or its save key during post. The lookup key is the fresh invocation key; restore can match an older key through the dependency or compatible prefix. A matched key alone does not establish successful validation/restoration, and a save key on a skipped or failed operation does not establish publication; check the result label. Partial restores and dependency-compatible fallback are identified. `cache-hit` is true only when every requested snapshot restores. Ordinary misses/skips are informational; operational failures annotate warnings, or errors in strict mode. Diagnostics escape untrusted controls and redact known tokens and HTTP(S) URLs.

Status colors are enabled in GitHub Actions. Set a nonempty `NO_COLOR` to disable them. Standalone diagnostics are plain. Long operations report actual byte/file progress at most once every two seconds as work advances; unknown totals have no percentage and stalled calls create no artificial activity.

Enable [GitHub step debug logging](https://docs.github.com/en/actions/how-tos/monitor-workflows/enable-debug-logging) with `ACTIONS_STEP_DEBUG: true` to receive native diagnostics through `RUNNER_DEBUG=1`. Debug includes full restore/save JSON and snapshot keys. `ACTIONS_RUNNER_DEBUG` controls a separate runner archive. Standalone use can request `--json`.

`cache-report` retains preparation, lookup, transfer, archive times, payload/file counts, keys, matches, skips, and errors. Chunk reports include reuse counts/bytes. `match_type` distinguishes exact/compatible dependency matches; only `hit` establishes successful restoration. `restore_reason` explains misses/errors. JSON reports contain full paths/keys; review them before sharing, as described in the [security model](SECURITY_MODEL.md).

Post saves occur after regular steps. An earlier artifact-upload step cannot collect `<cache-report>.post.json`; use completed debug logs or a later collector. See [measurement boundaries](https://github.com/nicoheidtke/ready-setup-go/blob/fe5d75d54cb3e62a1e95656e3f511cc30fbbfe89/docs/BENCHMARKS.md#metric-boundaries) before interpreting phase timings or bytes.

## Action acquisition

Choose a published distribution commit, not a source commit. Default packages bundle all six native executables and need no action-binary download. Maintainers can also prepare a lightweight distribution with a pinned native-download manifest; the same root action and `experimental/node` entrypoints then download only the runner platform's executable. This is a distribution choice, not a `with:` input: changing an input cannot reduce the repository archive GitHub already acquired.

Lightweight mode verifies exact byte size and SHA-256 from the pinned distribution before executing, and rechecks its temporary copy for post-job reuse. It requires unauthenticated access to the manifest's public GitHub release assets. No `GITHUB_TOKEN`, PAT, or cache credential is forwarded. Use bundled mode for private release assets, offline runners, or environments that cannot reach GitHub asset CDNs. Missing/corrupt downloads fail setup; there is no unverified or alternate-version fallback. The installer-only Bash composite requires a bundled distribution and reports that requirement explicitly for lightweight packages. The examples above assume bundled packaging.

## Installation and platforms

Completed toolchains in the runner's tool cache and the action's installation directory are inspected first. Missing versions are resolved through official Go metadata, downloaded with SHA-256 and size verification, extracted in staging, then published with a directory rename. Interrupted downloads are never marked installed; existing installations are not overwritten.

| Operating system | Architectures |
| --- | --- |
| Linux | amd64, arm64 |
| macOS | amd64, arm64 |
| Windows | amd64, arm64 |

## Standalone executable

```text
ready-setup-go setup --go-version 1.26.8
ready-setup-go setup --go-version 1.26.x --check-latest
ready-setup-go setup --go-version-file go.mod
ready-setup-go setup --go-version 1.26.8 --json result.json
```

Use `--json -` for a report on standard output; diagnostics go to standard error. Installer reports include the selected toolchain, installation source, phase timings, transferred bytes and peak temporary logical file payload.

Outside GitHub Actions, installation uses the operating system's temporary directory when `RUNNER_TEMP` is unset. The executable reports the path and does not modify its parent shell's environment. Automatic cache restore/save requires the GitHub action runtime.
