# Local installer and cache implementation plan

The initial scoped installer/cache implementation and local correctness gate are complete; the 2026-11-08 self-contained performance tranche adds overlapping prefetch and opt-in chunked snapshots; the subsequent user follow-up adds bounded parallel chunk processing. This is a source/local-correctness milestone, not a stable release or performance result. Hosted cache integration and comparative measurements still require user authorization.

## Constraints

- All workflows are temporarily manual-only (`workflow_dispatch`) to avoid GitHub Actions costs during the PoC. Source commit/push was authorized on 2026-11-08. Do not dispatch/rerun workflows, restore automatic triggers, publish a new preview, or modify upstream Exasol without separate authorization.
- Keep installer/cache policy, archives and networking in Go with standard-library dependencies. Minimal Node code bridges runner credentials, main/post lifecycle and process exit.
- Keep performance choices configurable and report measured limits honestly. Cache-off is the default.
- The user authorized installation/cache prefetch overlap and content-defined chunked snapshots on the existing cache service on 2026-11-08. Bounded parallel chunk processing was authorized by the next user follow-up. External storage, GOCACHEPROG, adaptive concurrency and other compatibility additions remain outside this tranche. The user added go.mod version-file support to the pre-push scope on 2026-11-08.

## Deliverables

| Work | Source status | Correctness/measurement gate |
| --- | --- | --- |
| Existing installer and six-platform packaging | Existing, with workflow/runtime modernization local | Historical installer validation recorded separately; pinned current full local checks and six-platform reproducibility passed |
| Explicit version or go.mod/go.work input | Implemented locally 2026-11-08 | Targeted directive/path/precedence tests passed; final full-source checks and hosted file-input execution tracked in development status |
| Overlapping toolchain setup/cache prefetch and opt-in chunked snapshots | Implemented locally 2026-11-08 | Exact-version notification, rootless download, cancellation cleanup, insertion reuse, gzip/plain chunks, corruption/eviction/bounds/conflicts covered locally; no hosted/performance claim |
| Root dependency-free Node lifecycle and Go CLI integration | Local implementation | Windows packaged main/post/alias/Bash fixture and final Node suites passed |
| Scoped fresh immutable cache keys and manifests | Local implementation | Unit coverage plus hosted branch/matrix/fallback checks after authorization |
| GitHub cache-v2 backend and bounded signed-blob transfer | Local implementation | Local protocol fixtures; live service compatibility pending |
| Deterministic tar/gzip, staging, additive merge, permissions and portable paths | Local implementation | Windows unit tests/vet passed; native Unix checks pending |
| Split/combined, gzip/none, transfer tuning, limits, modes and failure policies | Local implementation | Input tests and correctness fixtures; performance choice pending measurements |
| Opt-in archive-only build trimming | Local implementation, default disabled | Canonical build-object filter tests; validate Go mtime behavior before selecting a grace period |
| Cache correctness and paired benchmark tools/workflows | Implemented locally | Manual hosted execution blocked by user instruction |
| Product docs, roadmap, development status and handoff | Updated locally | Updated for the final local implementation and actual validation results |

Archive tests cover corrupt/truncated input before destination writes, unsafe paths and links, duplicates and Unicode case collisions, local-file priority, destination symlinks/conflicts, module read-only modes, fractional mtimes, cancellation, bounds and byte-identical plain/gzip roundtrips.

## Local completion gate

1. Complete: source and meaningful local tests cover unavailable credentials, failure/cancellation policy, immutable writer conflicts, dependency changes and post state.
2. Complete: final frozen-source Go 1.27.1 tests/vet and formatting passed; Node 26.3.0 bridge suites passed six tests; actionlint 1.7.12 passed all workflows without rule exclusions (optional shellcheck/pyflakes disabled). Node 24 runtime validation remains pending.
3. Complete: the checksum-verified pinned Go 1.27.1 compiler built all six binaries twice with matching native manifests and matching bundled/launcher ZIP hashes. All launcher metadata/docs are included. This is cross-compilation, not native validation of five other targets.
4. Complete: rebuilt Windows root/alias main/post caching passed a loopback cache-v2 fixture with a real Go 1.26.4 smoke workload, fresh restore, state/report checks and Bash cache-off offline reuse.
5. Complete: this plan, roadmap, handoff, consumer docs and development/measurement records reflect actual results and remaining limits. Generated binaries remain ignored and untracked.

## After authorization

Run native hosted correctness on all six targets, actual cache-v2/post-hook checks and branch/access/failure scenarios. Then gather at least 20 paired observations per representative workload/scenario, including Exasol's parallel build/test/lint split. Publish a new bundled preview only when authorized and preserve source revision/checksums/executable bits. A stable release still requires the roadmap's correctness, reproducibility and measurement gates.

## Parallel chunk processing

The subsequent self-contained tranche adds an independent chunk worker bound (default 4, range 1-16), concurrent compression/lookup/transfer/decompression, invocation-local duplicate suppression, deterministic manifest ordering and private-archive writes at validated offsets. Queues and buffers are bounded, nested blob-block parallelism is disabled for chunk operations, and all workers join before cleanup or manifest publication. Complete save packing and its raw temporary archive remain, preserving unchanged-module zero-request skipping. No external infrastructure or extra consumer actions are introduced.
