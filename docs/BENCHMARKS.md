# Performance measurement

Measure complete workflow cost: acquisition, setup, restore, workload, and post saves. Separate cold/warm cases. Cross-compilation establishes neither native behavior nor performance. [Correctness validation](DEVELOPMENT.md#validation) precedes measurement.

## Comparison rules

- Fix action/compiler/workload/dependency revisions, runner image, OS/architecture, and cache settings. Record baseline revisions and their own configuration.
- Collect at least 20 successful paired samples per comparison, alternating order. Retain failures and raw observations; label smaller groups incomplete. Report paired differences, median, and p95.
- Cover small modules, parallel build/test/lint jobs, and monorepos; include cold, unchanged, source-change, dependency-change, and Go-upgrade scenarios.
- Isolate sample namespaces and reset local caches. Seed outside measurement. Go-upgrade seeds must use a different exact compiler; dependency mutations must change the fingerprint.
- Record requests, bytes, CPU, and temporary disk where measured. More workers, fewer uploads, or smaller archives alone establish no speedup.

Page caches, module proxies, shared-runner load, and CDN state remain uncontrolled. Repeat close/suspicious outcomes and disclose changing latest-patch selections. Preserve baseline warmup and tool-cache costs. Never average unrelated scenarios into one speedup.

## Tools

These commands remain in use by the manual workflows below. They are development tools, not additional consumer setup steps. Workflows consume runner time/cache storage; command help provides full options. Store raw observations in ignored directories such as `.cache/` or `benchmark-results/`.

| Tool / workflow | Purpose and boundary |
| --- | --- |
| `cmd/benchmark` / `benchmark.yml` | Installed exact/minor, latest minor, and cold installer scenarios; launcher timing or upstream-action comparisons |
| `cmd/acquisition` / `acquisition.yml` | Verify/unpack complete bundle versus launcher ZIP plus native binary; not an exact GitHub repository-CDN model |
| `cmd/cache-benchmark` / `cache-benchmark.yml` | Explicit restore/workload/save pairs for small, monorepo, and supported Exasol jobs |
| `cmd/workflow-report` | Coarse completed job/step times; cannot reconstruct native phase reports |

```text
go run ./cmd/benchmark --dist dist --version 1.26.0 --scenario installed-exact --samples 20 --out .cache/installed-exact.json
```

Select Node 24 with `--node` and Bash with `--shell`. Subprocess timing includes startup/export, excludes runner dispatch. Installed-version scenarios block HTTP through a closed proxy; unit transports count unexpected requests. A proxy is not an OS sandbox.

Cache benchmarks need injected runtime credentials. `--validation` permits fewer correctness observations without a complete performance summary. Synchronous restore/save captures both reports before artifact upload but excludes runner post dispatch and installation/prefetch overlap. The helper creates remote entries and does not delete them.

Exasol is one acceptance workload. Preserve its build/test/lint split, generation, and warmup. The harness excludes separate tool-cache lifecycle, Python/integration/deployment work, and runner post dispatch; optional tool-initialization outcomes remain in raw records. It is not a complete upstream-pipeline comparison. `workload.yml` checks installer correctness, not speed.

## Metric boundaries

| Metric | Meaning / limitation |
| --- | --- |
| Installer phase times | Native setup; body-read time belongs to download even when tar extraction overlaps |
| `launcher_and_export_ms` | Subprocess minus native setup; includes CLI, serialization, and environment export |
| Installer `peak_temporary_bytes` | Logical staging/marker/ZIP payload; excludes existing toolchains, executable, metadata, and block rounding |
| Cache `preparation_ms` | Path discovery and dependency fingerprinting; excluded from restore `total_ms` |
| `lookup_ms`, `transfer_ms`, `archive_ms` | Wall time; transfer may include hashing/compression/control operations |
| Stage/preflight/merge/cleanup times | Restore diagnostics within archive work; not independent CPU measurements |
| `module_check_ms`, `save_reason` | Full-content check and save/skip reason; changed modules pay checking plus packing |
| `transferred_bytes`, `archive_payload_bytes` | HTTP payload and regular-file content; exclude HTTP/TLS overhead and remote-storage/peak-disk costs |
| Chunk reuse counts/bytes | Existing encoded chunks avoided on save; not total service-storage savings |

Installer/cache work can overlap. `prefetch_wait_ms` records the later join wait; summing native phase totals double-counts overlap. Use complete observed setup/workload elapsed time. Retries count toward elapsed phases; successful-payload/progress counters exclude failed attempts and error bodies. Publication counts newly published files. Progress is operational feedback, not a benchmark.

Save retains a packed archive; restore retains input and staged payload. Chunking retains an uncompressed archive too. Size limits independently bound transferred and expanded tar bytes, including headers, not peak disk/RSS. [Architecture](ARCHITECTURE.md) describes worker buffer allowances.

## Implementation comparisons

Alternate sequential/parallel settings on identical corpora. Measure complete restore and full packing alongside isolated merge/compression. Fixed 4 MiB gzip members produce identical bytes across worker counts, but differ from a single-member baseline; record encoded size. Uncompressed buffering preserves bytes; fewer writes alone prove no elapsed improvement.

Compare archive/chunked and split/combined with explicit compression, concurrency, block sizes, and filtering. Include gzip, zstd, and none on identical corpora; record the pinned codec version, executable/acquisition size, and peak memory alongside encoded size and complete workflow time. Chunking still packs the tree and adds requests, manifests, buffers, and eviction risks. Include these costs and separate cold seed from warm repeat.

Ordinary artifacts precede post hooks and cannot collect later save JSON; use debug logs or completed-log collection. Restore-only artifacts are not full lifecycle evidence. GitHub's **Set up job** combines acquisition with other work and cannot be attributed to one action.

When comparing lightweight distributions, include the bootstrap download and main/post rehash costs in complete workflow time. The acquisition helper's launcher-ZIP/native-asset transfer experiment does not execute the bootstrap or establish hosted acquisition behavior. Compare it with a bundled distribution built from the same source/compiler, and record public-release/CDN availability separately from performance.

The acquisition helper stops on the first failed observation, writes earlier observations and the failed sample's partial counters, and exits unsuccessfully. Its report marks whether collection completed and labels fewer than 20 successful pairs incomplete. Paired deltas are matching-platform acquisition minus bundled acquisition and include only pairs where both variants succeeded. Per-variant summaries exclude failed samples. Failure fields use fixed categories rather than raw errors; report URLs must be public HTTPS locators without credentials, queries or fragments. Reports still contain public asset paths and runner/revision metadata; review them before sharing. Collection does not guarantee a report after abrupt process termination or a report-write failure.

### Module save policy experiment

`TestMeasureModuleSavePolicies` compares the current check-first module-save policy with experimental packing that computes the same content identity during file reads. `Pack` can compute this identity through its internal `ContentIdentity` option; normal saves keep using the existing check-first policy. Hashing preserves archive bytes, cache identities and decoder compatibility. Concurrent file changes detected during opt-in hashing fail the operation.

Set `SETUP_GO_FASTER_MODULE_POLICY_MEASURE=1`, then run:

```text
go test ./internal/cachearchive -run '^TestMeasureModuleSavePolicies$' -count=1 -v -timeout=5m
```

Retain the complete output in an ignored directory. Each comparison has 20 alternating pairs and prints raw observations plus median/p95 paired differences (pack-and-hash minus check-first). The bounded synthetic corpora contain 256 small files totaling 1 MiB or 16 larger files totaling 4 MiB, alternating repetitive source-like bytes and fixed-seed pseudo-random payloads. The changed case adds a small file after baseline fingerprinting. Each corpus/state is tested with none, gzip and zstd using default compression settings and concurrency. A failed operation fails the test and leaves prior observations in its output; do not treat that comparison as complete.

For a current tree with `P` regular-file bytes and an encoded archive of `A` bytes, the policies have these logical costs:

| State | Check-first source reads / temporary bytes | Pack-and-hash source reads / temporary bytes |
| --- | --- | --- |
| Unchanged | `P` / `0`; no compression | `P` / `A`; compresses an archive that is discarded |
| Changed | `2P` / `A`; fingerprints then packs | `P` / `A`; hashes while packing |

The diagnostic includes temporary-file creation, packing/fingerprinting, close and removal. Its counters describe logical payload reads, peak logical temporary archive bytes, and candidate archive bytes for upload; they exclude physical I/O, filesystem allocation/metadata, network transfer, installation, restore and the workload. Page caches remain warm and uncontrolled. These are local policy comparisons, not complete workflow measurements or evidence to change the default. Retain check-first until representative workload change frequency and full lifecycle results justify the extra unchanged-save compression and disk cost.

`BenchmarkModuleSavePolicy` separately reports gzip elapsed time, allocations, logical reads, and temporary/candidate archive bytes for a 4 MiB corpus of 64 files. To investigate CPU work, profile each policy/state separately, for example:

```text
go test ./internal/cachearchive -run '^$' -bench '^BenchmarkModuleSavePolicy/changed=false/check-first$' -benchtime=3s -benchmem -cpuprofile .cache/module-check.cpu -o .cache/module-policy.test
go tool pprof -top .cache/module-check.cpu
```

Repeat with `pack-and-hash` and `changed=true`. CPU profiles contain fixture setup and test overhead as well as timed operations; compare sampled call costs rather than profile totals from different iteration counts. Allocation volume is not peak RSS. Diagnostic logs use synthetic names and counters, but CPU profiles may contain local source/build paths; review profiles before sharing.
