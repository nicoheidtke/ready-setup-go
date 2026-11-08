# Development and release preparation

The consumer README describes the action interface. Development status and future ideas belong in [ROADMAP.md](../ROADMAP.md), not the README.

## Current local implementation and validation

The current authorization is to finish the complete planned installer/cache implementation locally before further GitHub testing. On 2026-11-08 the user authorized committing and pushing the current source after switching every workflow to manual-only to avoid GitHub Actions costs during the PoC. Do not dispatch/rerun workflows, restore automatic triggers, or publish a replacement preview without separate authorization. The user authorized the first self-contained performance tranche on 2026-11-08: installation/cache-prefetch overlap and optional chunked snapshots using the existing cache service. Other milestone 3 experiments and compatibility additions remain outside this tranche. See the [implementation plan](IMPLEMENTATION_PLAN.md).

The local working tree adds the root Node lifecycle, Go restore/save policy, cache-v2 backend, deterministic tar/gzip archives, configurable transfer/layout policies, archive-only opt-in build trimming, correctness fixtures and manual measurement definitions. Source completion is separate from hosted validation; the initial Linux/amd64 split/gzip archive cold-save and warm-restore/post-save path has now passed against GitHub; the broader platform/policy matrix remains unverified. The original preview `900c8d6495677c90d124b7a4f0ce0627595a0efb` is historical installation-only code. Current preview `13f611572c059e38af11315888f7add1566bbb2e` includes the cache/version-file/performance implementation and the hosted cache-version fix. README examples therefore require a new bundled distribution SHA rather than using that preview.

Local checks on Windows/amd64 with the current implementation:

- Full `go test ./...` and `go vet ./...` passed with the supported Go 1.26.4 toolchain and the pinned Go 1.27.1 packaging compiler. The earlier integrated checks also passed with the initially installed Go 1.27.0.
- Go 1.27.1 was downloaded locally through the existing checksum-verifying installer; the install report records `source: download`, and its executable reports `go1.27.1 windows/amd64`.
- Two local Go 1.27.1 package builds produced identical SHA-256 manifests for all six native executables and identical bundled/launcher ZIP archives. This verifies reproducibility for that local source/compiler snapshot, not native execution of the other five targets.
- Archive tests cover deterministic gzip/plain roundtrips, fractional file mtimes and permissions, existing-file priority, read-only modules, corrupt/truncated downloads, size bounds, malicious paths/links/Unicode case collisions, destination symlinks, trimming scope and cancellation.
- Dependency-free launcher tests passed locally with Node 26.3.0. The action runtime is Node 24; local Node 26 results do not verify that runtime.
- The opt-in packaged Windows lifecycle integration passed against a local cache-v2 fixture using the Go 1.27.1-built distribution and the existing Go 1.26.4 toolchain. It ran root Node setup, a real `testdata/smoke` workload and post upload; a fresh `experimental/node` invocation restored the combined archive with `cache-hit=true`, reused the Go test cache, and saved a fresh key. State cleanup, reports, no bearer token on blob requests and Bash cache-off offline reuse passed. This verifies local Windows/loopback behavior, not the live GitHub service or comparative performance.
- After final source review, cancelled/expired operation handling honors the warning/strict policy and optional report-write errors follow the same policy. The frozen-source Go 1.27.1 `go test ./... -count=1` and `go vet ./...` passed all packages, including the cache workload helper and workflow reporter. Formatting checks passed.
- Both dependency-free Node bridge suites passed all six tests with Node 26.3.0. Actionlint 1.7.12 passed every workflow with no rule exclusions; optional shellcheck/pyflakes integrations were disabled.
- The final production-fix build was packaged twice with Go 1.27.1: all six binary manifests and both archive hashes matched. The rebuilt distribution passed the packaged lifecycle/alias/Bash smoke again. Final documentation was refreshed in both distributions.
- The planned local implementation/correctness gate is complete. The later 2026-11-09 smoke record below verifies the initial Linux/amd64 archive main/post path on the action runtime. Other native cache platforms, hosted chunked/policy cases and comparative measurements remain pending authorization.

## Historical installer verification

The user authorized validation on 2026-09-22, superseding the earlier implementation-only restriction.

Verified locally on Windows/amd64 with Go 1.26.4:

- Go sources formatted; `go test ./...` and `go vet ./...` passed.
- Windows installer packaged and used to download, verify, and install Go 1.26.4 into a path containing spaces.
- Minor selector reused that installation with HTTP(S) proxies pointing to a closed local port.
- The installed toolchain passed the pinned `google/uuid` and `golang.org/x/sync` smoke test in `testdata/packages`.
- Node and Git Bash launchers passed offline exact-version reuse. Git Bash required execution outside the local sandbox to create its signal pipe.
- All six native executables cross-compiled. Two builds produced identical SHA-256 manifests for the six executables and both ZIP archives. This is packaging verification, not native validation of the other five platforms.
- All workflow files passed actionlint v1.7.12 with optional shellcheck/pyflakes integrations disabled. An initial v1.7.7 run was too old to recognize Node 24 and newer hosted runner labels.
- Exasol Personal generation and Windows/amd64 application compilation passed with the installed Go 1.26.4 toolchain. Its full Windows Go unit suite failed; see [validation record](validation-2026-09-22.md).

The initial source commit `c94080c` is published on `main` at `https://github.com/nicoheidtke/setup-go-better`. [Hosted installer validation](https://github.com/nicoheidtke/setup-go-better/actions/runs/35788775487) passed all nine jobs: unit tests/vet on three operating systems and native cold-install/offline-reuse integration on all six target platforms. Comparative performance measurements remain pending. Local downloads and module verification used workspace caches; initial attempts to write the user's module checksum cache were denied and rerun with a workspace GOPATH. Go also emitted telemetry upload-token permission warnings without failing the completed checks.

The manual Exasol Personal workflow pins commit `54b7e1a7454f49d821102bcab5bfce2c7747f8c6` and runs separate Linux build/test/lint jobs plus a Windows build. The initial hosted run used Go 1.26.4; the locally modernized workflow selects Go 1.26.8, above its declared 1.26.3 minimum. Generation uses upstream's `SKIP_EMBED=true` mode and `containers_image_openpgp` build tag. These are Go workload checks, not release builds or full Python integration/deployment tests. See [workload methodology](BENCHMARKS.md#primary-real-world-workload).

### Local modernization, pending hosted validation

The user requested no further GitHub workflow runs while implementation is completed. The later 2026-11-08 instruction permits source commits/pushes with all workflows manual-only; dispatch/rerun still requires authorization. The published preview remains the previously validated Go 1.26.4 build; the modernized source workflows have not run on GitHub.

- Official action dependencies are pinned to verified releases: checkout v7.0.1, setup-go v7.0.0, upload-artifact v7.0.1. All three declare Node 24. The old checkout/upload-artifact v4 references caused Node 20 deprecation warnings.
- Bootstrap/packaging compiler: Go 1.27.1. Unit CI covers both Go 1.26.8 and 1.27.1. The cold installer integration deliberately retains Go 1.26.0 to exercise the lowest supported release.
- Exasol, consumer examples, and default runtime benchmarks use the current Go 1.26 patch, 1.26.8. This keeps the acceptance workload within Exasol's declared minor version.
- Weekly Dependabot updates cover official action pins and smoke-test module dependencies. The action's own consumer SHA is excluded because it must identify a bundled distribution.
- CI includes formatting and actionlint checks, bounded job timeouts, and cancellation of superseded validation runs. Manually dispatched benchmark jobs are not automatically cancelled.

Official versions were checked against GitHub release metadata and Go's download metadata on 2026-09-22. Changing the packaging compiler requires fresh reproducibility and native validation checks before replacing the preview distribution.

## Source layout

- `cmd/setup-go-better`: native setup and post-job entrypoints.
- `internal/installer`: version selection, installed-toolchain discovery, release metadata, verified extraction, publication.
- `internal/actionenv`: GitHub environment, output and post-state files.
- `internal/cache`: cache inputs, namespace/dependency identity, restore/save lifecycle, private post state and reports.
- `internal/cachearchive`: bounded deterministic tar/gzip snapshots, private staging and safe additive merge.
- `internal/cachebackend`: GitHub cache-v2 control protocol and signed blob transfer; see [protocol notes](../internal/cachebackend/PROTOCOL.md).
- `cmd/package`: reproducible native executables and action archives.
- `cmd/benchmark`, `cmd/acquisition`, `cmd/cache-benchmark`, `cmd/workflow-report`: measurement tools.
- Root `launch.cjs`, `post.cjs`, `launcher.cjs`: dependency-free Node 24 runner bridge.
- `experimental/node`: root lifecycle alias; `experimental/composite`: installer-only Bash launcher experiment.
- `.github/workflows`: validation, packaging, and manually dispatched benchmarks.

No Go modules beyond the standard library are required. No npm toolchain is required.

## Development and consumer testing

Keep source development on `main` and feature branches. All eight workflow definitions currently have only `workflow_dispatch`; pushes and pull requests do not start these workflows. This temporary PoC policy avoids hosted-run costs. Do not add automatic triggers or dispatch runs without authorization. Integration builds a temporary distribution and invokes it with `uses: ./dist`, checking cold installation, dependency compilation, action outputs, and offline reuse. This exercises the actual GitHub environment export and launcher behavior that local unit tests cannot cover.

After explicit authorization resumes hosted testing/publication, publish bundled preview commits on `codex/action-preview` for consumer testing. A preview contains all six native executables, checksums, and `SOURCE_REVISION` identifying the source commit. Preserve Unix binary executable bits in Git's index when preparing a preview on Windows. Pin consumer workflows to the full distribution commit SHA, not a source commit or a moving branch. Previews carry no stable-release or performance guarantee.

The consumer smoke workflow is manual and requires the full bundled distribution commit SHA as `distribution-sha`. It checks out that immutable preview beside the workload, verifies `SOURCE_REVISION` and binary checksums, then invokes it without compiling the installer. It tests `go-version-file` with the pinned package module and strict restore/save caching. Repeat with `expect-hit` enabled to verify the preceding post-save. It does not default to the historical installation-only preview. The Exasol workflow provides the larger Go build/test/lint workload. A full Exasol integration or deployment run is a separate acceptance stage.

The initial bundled preview is `900c8d6495677c90d124b7a4f0ce0627595a0efb`. The repository is currently private. Direct use from the public `exasol/exasol-personal` repository requires a public distribution repository; GitHub's private-action sharing settings only cover private repositories under the same owner. An explicitly authenticated checkout followed by a local action invocation is another option for controlled private testing. No repository visibility or sharing settings were changed.

After correctness and performance gates pass, publish a versioned distribution and update consumers to its SHA. GitHub Marketplace listing is optional for using a repository action. This separation of source commits from bundled distribution commits follows [GitHub's action release guidance](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/release-and-maintain-actions).

### Preparing the next preview

Keep publication separate from local preparation. Before pushing source, rerun tests/vet, the launcher suites, formatting and actionlint after the final changes. Build twice with the pinned compiler and compare all six binary checksums and both ZIP archives. Run the packaged Windows main/post fixture against the rebuilt action. These local checks do not replace native hosted or live cache-service validation.

Source commit/push is authorized with manual-only workflows. When a new bundled preview is separately authorized, build its distribution from the exact committed source revision. Place the full source commit SHA in the preview's `SOURCE_REVISION`, preserve executable bits for the four Unix binaries in the preview's Git index, and verify `SHA256SUMS`. Uncommitted changes cannot truthfully be identified by the existing HEAD SHA. Source pushes and pull requests do not trigger workflows under the current manual-only policy. Preview publication and hosted execution remain separate steps.

Use the newly published bundled commit SHA in the manual consumer workflow and in any separate Go repository. First pass the small pinned-package checks and two-dispatch cache save/restore gate, then the recorded Exasol workload. Cross-repository access must be configured before a private action can be consumed. Branch/default-branch, failed/cancelled jobs and concurrent cache writers remain additional hosted correctness gates. Keep performance workflows manual until correctness passes; a preview does not establish a speedup or a stable release.

## Local validation

Use Go 1.27.1 to match the packaging workflow. Both that compiler and Go 1.26.4 have completed the recorded local checks; rerun the checks after changing source. Local Node 26 is supported for development tests. CI explicitly runs the launcher suites with the runner's Node 24 action executable, rather than whichever `node` happens to be on PATH.

```text
gofmt -w cmd internal testdata/smoke
go test ./...
go vet ./...
go run ./cmd/package --platform windows/amd64
```

Substitute `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, or `windows/arm64` as appropriate. Unit tests serve fixture metadata and archives locally; they do not require public Go downloads. Integration jobs deliberately download official toolchains and execute a real module in a subsequent workflow step.

`dist` is generated and ignored. Local workflow tests use `uses: ./dist`; the source-tree action requires the generated `bin` directory and must not be advertised as a runnable distribution by itself.

## Reproducible distribution

```text
go run ./cmd/package --out dist --archives artifacts
```

The packager uses `CGO_ENABLED=0`, fixed architecture baselines, `-trimpath`, no VCS stamping, no build ID, and stripped symbols. ZIP entry times and modes are deterministic. Reproducibility requires identical sources, compiler, and build settings; the manual packaging workflow compares two builds.

Outputs:

- `dist`: action metadata, launchers, documentation, six executables, and executable checksums.
- `artifacts/bundled.zip`: the complete action distribution.
- `artifacts/launcher.zip`: the same distribution without native executables, for acquisition experiments only.
- `artifacts/setup-go-better-<os>-<arch>[.exe]`: individual native executables.
- `artifacts/SHA256SUMS`: checksums of experiment/release assets.

The launcher-only ZIP is not a runnable action by itself. The acquisition experiment measures fetching it followed by the matching binary; it does not add a runtime-downloading production launcher.

The packaging workflow prepares downloadable artifacts and does **not** create tags, releases, commits, or push to a repository. To publish, place the complete distribution on a release commit, preserve executable modes for Unix binaries, and point the chosen action tag at that commit. A future release process can automate that once repository ownership, versioning, and publication policy are decided.

## Implementation boundaries

Completion is based on a marker, a matching `VERSION` file, and a regular Go executable. This trusts the runner's local filesystem; it is not a revalidation of every installed compiler file. Failed fresh installs are cleaned up. An incomplete pre-existing destination produces an actionable error instead of being overwritten.

Archive links and duplicate file entries are rejected. Extraction writes only into a newly created staging directory and validates archive paths before writing. The action does not promise robust shared writable state across untrusted processes on a self-hosted runner.

Networking uses a bounded client and operation context, honors the standard Go proxy environment, and surfaces errors rather than installing an unverified fallback. There is no automatic retry/backoff policy yet; evaluate availability and latency together before adding one.

The benchmark tools and packaging commands execute subprocesses only when explicitly run. Merely writing these files has not executed them.


## Cache lifecycle and reports

The root Node action receives `ACTIONS_RESULTS_URL` and `ACTIONS_RUNTIME_TOKEN` through the runner and invokes Go for setup/restore and post/save. Composite steps cannot provide the same automatic credential/post lifecycle. No npm dependencies are used. The launcher inherits environment credentials; persisted private state contains cache paths, keys, configuration and module digest, never runtime tokens or signed blob URLs. The post process receives fresh credentials.

The main step exports a uniquely named `SETUP_GO_BETTER_SAVE_ON_FAILURE_<github.action>` environment flag. The action's post expression reads that flag because action inputs are not available when GitHub evaluates post conditions. Successful jobs save by default in restore-save mode; failed jobs require `cache-save-on-failure`; cancelled jobs skip save. Post-state errors remain visible rather than silently accepting untrusted or incompatible state.

Restore merges into the selected Go executable's existing module/build paths. Existing local regular files take priority. Full download, decompression and archive validation precede any destination creation; merge failures can leave new files without replacing local entries. Read-only module directory modes are temporarily relaxed when needed and restored afterward. This does not promise safe shared writable state across hostile processes on self-hosted runners.

Cache failures warn by default; strict mode makes operational errors fail the action. Missing runtime credentials skip caching. Cache-off avoids cache preparation and state. Networking has cancellation/time bounds and no retry policy. Backend conflicts/permissions are distinguished where service responses identify them; immutable keys include attempt identity and a random invocation suffix. GitHub, rather than client-side branch enumeration, enforces branch/default-branch access.

`cache-report` writes the restore report, and the post hook writes the same path plus `.post.json`. Regular upload-artifact steps precede all action post hooks; they cannot collect the later save report. Post logs contain report JSON. Collect final job timings/logs from a subsequent job or external report command, or use the explicit-lifecycle benchmark harness for report artifacts containing both phases. A regular-step artifact containing only restore metrics must not be described as full lifecycle measurement.

The action's 10 GiB maximum applies independently to expanded tar (headers included) and transferred archive bytes. Cache reports count regular-file payload and transfer bytes separately. Full temporary disk usage, filesystem metadata, HTTP/TLS overhead, and cache-service storage cost are not measured by these reports. Restore holds both the downloaded archive and staged extracted payload before merge; save holds a packed archive. Do not reuse the installer's peak-temporary metric as a cache peak metric.

### Repeating the local packaged lifecycle check

After packaging a Windows distribution and making a supported local Go installation available, set `SETUP_GO_BETTER_TEST_DIST` to the absolute distribution directory and run:

```text
go test ./internal/cache -run TestPackagedLifecycle -count=1 -v
```

The test uses a loopback protocol fixture, real launcher processes and the small source workload. `SETUP_GO_BETTER_TEST_BASH` can select Git Bash for the installer-only comparison. Without the opt-in distribution variable, ordinary unit runs skip this packaged integration.

### Version-file input (2026-11-08)

The user requested `go-version-file` before consumer testing. The root action, Node alias, Bash experiment and standalone CLI accept go.mod/go.work paths. Parsing stays in standard-library Go without invoking an existing Go compiler. Explicit selectors bypass file reads; concrete preferred toolchains must meet the language minimum; `default` uses `go`; patch directives retain exact-version behavior. An older language minimum is valid when a supported preferred compiler is supplied. Relative action paths use `GITHUB_WORKSPACE`; standalone paths use the current directory. Checkout must precede file-based setup.

Targeted local version-file and CLI tests passed on Windows with Go 1.27.0, and launcher tests passed on Node 26. The sandbox initially denied loopback fixture access and working-directory restoration; targeted tests passed outside it. Final source including the feature passed `go test ./... -count=1` and `go vet ./...` on both Go 1.26.4 and the pinned Go 1.27.1 compiler. Formatting and `git diff --check` passed. All eight launcher/benchmark-bridge/runtime-locator Node tests passed on Node 26.3.0. Node 24 runner behavior and hosted file-input integration remain unverified until GitHub execution is authorized.

The pre-push review also verified all three official action SHA pins against their upstream release tags. Actionlint 1.7.12 passed the refreshed workflows with no rule exclusions (optional shellcheck/pyflakes disabled). All six binaries and bundled/launcher ZIPs built twice with Go 1.27.1 with matching hashes. The rebuilt Windows distribution passed the combined-cache main/post/Node-alias lifecycle fixture and Git Bash offline reuse with the real Go 1.26.4 smoke workload. The fixture uses an explicit version; file-input parsing and launcher forwarding have separate local tests and prepared hosted integration steps. Generated candidates are in `dist` and `.cache/prepush-artifacts`, with the reproducibility copy in `.cache/prepush-dist-repro` and `.cache/prepush-artifacts-repro`. They contain uncommitted source and are not an immutable published preview; `SOURCE_REVISION` must identify the eventual committed source before publication.

## Self-contained performance tranche (2026-11-08)

`cache-overlap` defaults to true when caching is enabled. The installer announces the exact selected version before cold installation; a joined/cancellable worker fingerprints dependencies and prefetches cache data without guessing Go cache paths. The selected installed executable supplies paths, and complete archive validation precedes merge. Disabled overlap retains sequential single-archive staging. Cache-off creates no prefetch worker.

`cache-storage: chunked` uses content-defined 1-8 MiB raw-tar chunks, independently gzip-compressed or plain. SHA-256 identifies stored chunk bytes within the same job/scope/platform/Go/layout/compression namespace. Exact finalized chunk lookups permit reuse; reservations alone do not. A fresh manifest is uploaded only after all chunks exist. Manifest count/size, chunk sizes, total encoded bytes and raw tar bytes are bounded. Missing referenced entries are misses; corrupt transfers never merge the affected archive. Tokens and signed URLs are not persisted. This uses the existing GitHub cache backend and no extra consumer infrastructure.

The format still repacks the complete cache into a private uncompressed temporary archive and uses bounded chunk buffers. It does not implement incremental filesystem scanning, cross-job or cross-platform reuse, remote per-object access, or chunk garbage collection; GitHub handles entry eviction. Independently evicted chunks can invalidate a manifest. Previously generated prepush candidates predate this tranche. Refreshed candidates are `dist` and `.cache/performance-artifacts`, with reproducibility copies in `.cache/performance-dist-repro` and `.cache/performance-artifacts-repro`; they remain uncommitted candidates, not published immutable previews.

Local correctness checks for this tranche passed with pinned Go 1.27.1: full tests/vet, formatting, eight Node 26.3.0 tests, and actionlint 1.7.12 (optional shellcheck/pyflakes disabled). New tests cover version notification before installation, rootless prefetch, cancellation/join/cleanup, cache-off/opt-out, gzip/plain reuse after an insertion, corrupt/evicted chunks, manifest bounds, and committed/uncommitted upload races. The rebuilt Windows packaged lifecycle passed for both archive and chunked storage against the loopback fixture, including alias restore/post, real Go 1.26.4 smoke workload and Bash offline cache-off reuse. The chunked second save reused all forty fixture chunks and uploaded the fresh manifest; this is a correctness observation, not a performance comparison. Final all-six-platform packages were built twice with matching binary and archive hashes. No hosted runs, commits, pushes, services or third-party consumer action dependencies were added.

## Bounded parallel chunks (2026-11-08)

The next user follow-up authorized parallel chunk processing without extra infrastructure. `cache-chunk-concurrency` defaults to 4 and accepts 1-16; it is exposed through root/alias metadata, CLI, the existing helper/bridge and manual cache workflows. Save uses an unbuffered producer and bounded workers for compression/hash/lookup/upload. Restore validates all references first, groups identical chunks, and writes decoded bytes at predetermined offsets in one private archive; full existing archive validation still precedes the affected-cache merge. Duplicate hashes must have identical declared sizes. Metrics are reduced after join, manifests require all committed chunks, and failures/cancellation cancel and join the workers. Chunk clients preserve supplied HTTP/security settings but use one blob block worker to avoid nested concurrency multiplication.

Complete save packing and the raw temporary archive remain intentionally: unchanged restored modules can still skip all chunk lookups. Payload buffers are bounded by worker count plus the producer's rolling/pending buffers; allow roughly 32 MiB per worker plus 16 MiB producer buffers, excluding gzip/runtime overhead. There is no unbounded restore reorder buffer. Correctness coverage includes synchronized 1-versus-3 worker bounds, out-of-order gzip reconstruction, parent cancellation/failure draining, duplicate transfer suppression, inconsistent duplicate metadata, invalid worker inputs, unchanged-module zero-request behavior and the existing corrupt/evicted chunk no-write cases. Final pinned-Go 1.27.1 full tests/vet, formatting/whitespace, all eight Node 26.3.0 tests and actionlint passed. Both regenerated Windows packaged archive and chunked main/post/alias/Bash lifecycles passed against the existing local fixture with the real Go 1.26.4 smoke workload. Final all-six builds were repeated with matching binary manifests and bundled/launcher ZIP hashes. Latest candidates are `dist` and `.cache/parallel-artifacts`; comparison copies are `.cache/parallel-dist-repro` and `.cache/parallel-artifacts-repro`. Earlier performance/prepush artifact directories describe earlier source. The race detector was not run because no C compiler was available on PATH; deterministic synchronized concurrency/cancellation tests passed. These are local correctness observations, not hosted behavior or performance results. Source remains uncommitted and unpublished, and eventual preview provenance must identify the committed source.

## Preview and first consumer smoke (2026-11-09)

The user authorized a fresh bundled preview and one Linux consumer smoke dispatch. The manual consumer workflow now selects one runner (ubuntu-24.04 by default) instead of starting the six-platform matrix, limiting PoC runner usage. Publish the six-binary distribution with the exact source SHA in SOURCE_REVISION and executable Unix modes, then dispatch consumer.yml once with that distribution SHA and expect-hit=false. No warm-repeat, broader platform, benchmark, Exasol, visibility or automatic-trigger changes are authorized by this step. Record the completed run below after observing its result.

The user subsequently authorized the warm-cache repeat (step 3). Initial Linux run [42533862372](https://github.com/nicoheidtke/setup-go-better/actions/runs/42533862372) verified binary checksums and installed Go 1.26.0, then failed strict cache lookup with HTTP 400 before the package workload or post-save. Review found service-version hashes were truncated to 24 hex characters; the toolkit emits full 64-character SHA-256 versions. The local fix preserves full service versions while keeping short key namespaces, adds a protocol-shape fixture check, and reports the operation/allowlisted invalid_argument code without exposing remote messages. The corrected cold and warm hosted runs below confirmed the fix.

### Completed Linux consumer smoke

Preview `13f611572c059e38af11315888f7add1566bbb2e` on `codex/action-preview` records source `d1187a834d030b7e53cb8e8b270831da4aaa1a9c` in `SOURCE_REVISION`. All six binary checksums and Unix executable modes were checked before publication; only Linux/amd64 was executed in these hosted checks. The source fix passed full pinned-Go 1.27.1 tests/vet and both local packaged Windows storage-mode fixtures before publication.

| Run | Result | Evidence |
| --- | --- | --- |
| [42533862372](https://github.com/nicoheidtke/setup-go-better/actions/runs/42533862372) | Initial preview failed | Installer succeeded; strict cache lookup HTTP 400 exposed the truncated version digest |
| [42534290287](https://github.com/nicoheidtke/setup-go-better/actions/runs/42534290287) | Corrected cold run passed | go.mod selected Go 1.26.0; pinned package tests passed; automatic post saved module and build archives |
| [42534400908](https://github.com/nicoheidtke/setup-go-better/actions/runs/42534400908) | Warm repeat passed | Same preview/runner/scope, `expect-hit=true`; both archives restored; package tests and automatic post passed |

Configuration: one ubuntu-24.04 job per dispatch, root Node 24 action, strict restore-save, split layout, gzip, whole-archive storage, overlap enabled. First save uploaded 95,820 module bytes and 17,263,893 build bytes. The repeat restored those two archives, skipped unchanged module upload and saved a fresh 17,339,078-byte build archive. These are cache payload/correctness observations, not performance comparisons or billing estimates. No other platforms, benchmark workflows, Exasol runs or visibility settings were exercised. All eight workflows remain manual-only, and the authorized cold/warm cycle is complete.
