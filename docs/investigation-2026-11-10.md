# Exasol consumer cache investigation, 2026-11-10

Read-only inspection of existing runs and the local consumer branch; no workflows were dispatched, repositories pushed, or implementation changed. The user supplied these runs after executing the consumer workflow independently.

## Evidence and limits

- Candidate cold: [42718543844](https://github.com/nicoheidtke/exasol-personal/actions/runs/42718543844).
- Candidate warm: [42851797566](https://github.com/nicoheidtke/exasol-personal/actions/runs/42851797566).
- Upstream comparison: [42824419472](https://github.com/exasol/exasol-personal/actions/runs/42824419472).

Both candidate runs checked out consumer `33bdb44b3612c8139e9fb150d73b108efa1558ad` and distribution `13f611572c059e38af11315888f7add1566bbb2e` (source `d1187a834d030b7e53cb8e8b270831da4aaa1a9c`). They used Go 1.27.0, split/gzip whole archives, restore-save and default overlap. Runner labels were ubuntu-latest/windows-latest; logs identify Ubuntu 26.04 and Windows 2025, amd64. Chunked storage was not exercised.

Upstream used setup-go v6.5.0 at `924ae3a1cded613372ab5595356fb5720e22ba16`, Go 1.27.0 and workload `b1c1cc52d34c8fd852a416f74efc9c8f16fdb11a`. It is 18 commits beyond the fork's workload base `9b7e95dccab17fdf155b7cbc97ec7755c73eb09e`, including cleanup-module dependencies and application/test changes. Launcher tests passed 264/238 cases on Linux/Windows upstream, versus 245/219 in the candidate. These single observations are not a paired benchmark or a general speedup claim.

## Observed job times

Approximate elapsed seconds between first and last timestamped job-log lines, including post hooks; excludes queue time and preparation before the first line. SonarQube is excluded because it was skipped in the fork.

| Job | Candidate cold | Candidate warm | Upstream warm |
| --- | ---: | ---: | ---: |
| lint-go | 289 | 199 | 68 |
| build Linux | 141 | 88 | 88 |
| build Windows | 244 | 185 | 195 |
| tests-unit | 310 | 102 | 170 |
| tests-launcher Linux | 109 | 88 | 127 |
| tests-launcher Windows | 156 | 200 | 290 |

The six jobs sum to 1,249 / 862 / 939 seconds respectively. The span from earliest start to latest finish among these jobs is 856 / 597 / 568 seconds. Candidate warm therefore used about 77 fewer aggregate job seconds than the upstream observation but finished about 29 seconds later. This is not billed runner time. Lint gates both builds, which gate tests; its regression is on the critical path. The Python lint job finished before Go lint in all three runs.

## Cache correctness and useful reuse

All four candidate warm lint/build/unit jobs restored both archives from the cold run, with no cache errors reported. Launcher jobs restored their small build archives; their module misses are expected because the cold jobs had empty module caches and skipped those saves. All candidate warm jobs logged zero `go: downloading` lines. Most unit-test packages explicitly reported `(cached)`.

The Linux build task fell from roughly 96 seconds cold to 6 seconds warm; Windows build from 182 to 11 seconds; the unit task from 273 to 15 seconds. Fresh per-job snapshots capture build flags, race/coverage objects and test results specific to each workload. Upstream's shared immutable snapshot did not produce cached package test results in this observation; its unit task took about 124 seconds. Different workload revisions prevent attributing the full difference to the action.

The consumer branch also removes unnecessary linter/plugin restores and tool warmup from non-lint jobs, and puts Go setup before Python setup in launcher jobs. Upstream launcher jobs logged an automatic Go 1.27.0 download during `task tests-setup`, before explicit setup-go. Candidate launcher jobs avoid that ordering problem. These workflow improvements must be distinguished from installer/backend improvements.

## Where time and bytes go

Native cache-report seconds, rounded. Restore includes prefetch work that can overlap installation; do not add installer time to restore totals as if sequential.

| Candidate warm job | Restore | Post save | Restore archive processing |
| --- | ---: | ---: | ---: |
| lint-go | 109.6 | 40.9 | 53.0 |
| build Linux | 36.9 | 20.5 | 14.6 |
| build Windows | 115.4 | 26.2 | 103.2 |
| tests-unit | 55.4 | 21.3 | 22.8 |
| tests-launcher Linux | 1.2 | 1.2 | 0.4 |
| tests-launcher Windows | 4.7 | 2.7 | 3.4 |

1. **Lint restore is expensive.** Its module archive alone is 1,134,432,396 transferred bytes / 80,189 files / 2,107,552,092 payload bytes. Module transfer took 50.1 seconds, module archive work 38.5 seconds; build transfer/archive work took 6.3/14.5 seconds. Upstream restored its shared 1,466,237,501-byte zstd archive in about 36 seconds in the lint job. Candidate lint then spends about 34 seconds on lint tasks and 41 seconds on post save. Its module save correctly skips upload but still spends 13.1 seconds repacking; build packing/upload takes 15.2/12.3 seconds.
2. **Windows restore is dominated by filesystem/archive work.** Modules take 70.4 seconds to restore and build objects 32.8 seconds, versus only 11.8 seconds total transfer. `cachearchive.Restore` writes staging, preflights destinations and copies every absent file again during merge, with repeated ancestor checks. These are concrete optimization candidates; the logs do not split decompression, staging, preflight, merge and cleanup, so their individual costs remain unknown.
3. **Unchanged-module skipping is incomplete in practice.** Linux build re-uploads 375,155,650 bytes versus 375,155,642 originally; Windows build 375,522,982 versus 375,523,067; unit tests 436,863,781 versus 436,863,772. Each retains exactly the same file count and payload size, and there were no module downloads. This warrants a per-file content/metadata comparison; it does not prove contents are identical. Current skipping compares complete packed-archive hashes, including modes and file mtimes. Metadata churn or small same-size mutable cache files are hypotheses, not established causes.
4. **Fresh whole-build snapshots re-upload nearly identical data.** Warm Linux build adds 357 files and 62,475 payload bytes but uploads 327,530,209 bytes; Windows adds 357 files and the same 62,475 payload bytes but uploads 328,667,698 bytes. Freshness is intentional; whole-archive transfer is its current cost. Across six jobs warm restore receives 3,836,148,193 bytes and post uploads 2,702,486,412 bytes, excluding toolchain, private checkout, Python, linter/plugin and artifact transfers.
5. **Download/restore concurrency is limited.** Whole-archive backend download is one GET with a serial copy. Prefetch loops through module/build archives sequentially, then merge processes them sequentially. Overlap hides only roughly 3 seconds of Linux or 8-10 seconds of Windows installation here; it does not parallelize archive work. Upload concurrency does not tune downloads. Chunk concurrency applies only to opt-in chunked storage.
6. **Acquisition adds fixed overhead.** Private-action checkout alone takes roughly 4-6 seconds on Linux and 9-11 seconds on Windows in the warm run. Every job downloads Go anew (about 2.7-3.6 seconds Linux and 7.7-10.0 seconds Windows). Dependency warmth does not imply a persistent compiler installation on fresh runners. Both baseline and candidate installed Go in these observations; no isolated installer/acquisition comparison exists.

Windows launcher warm total regressed relative to candidate cold primarily alongside pytest increasing from 99.54 to 128.41 seconds; its cache restore/save cost is small. Do not attribute that test-runtime variability to a cache miss.

The separate golangci-lint analysis cache restored successfully in both candidate runs, but its exact immutable key prevents subsequent saves. This is separate from the Go build/module cache and can preserve old analysis data. The current logs do not establish its contribution to lint duration.

## Proposed next work

Prioritize local correctness-preserving investigation before another hosted run:

1. Reproduce module save churn and add diagnostics identifying content versus metadata changes. Avoid full recompression merely to determine whether modules changed, while retaining correct handling of mutable cache files.
2. Split archive restore instrumentation into validation/decompression, staging, preflight, publication and cleanup. Reduce repeated filesystem traversal and second payload copies, considering same-filesystem staged publication while preserving existing-file priority and corrupt-archive no-write guarantees.
3. Evaluate bounded whole-archive range downloads and pipelining module/build transfer with validation; account for request count, contention, memory and temporary disk. These are proposals, not implemented changes.
4. Compare compression and the existing chunked path after correctness gates. Chunking targets redundant transfers, but retains complete packing/staging and adds requests; it is not a demonstrated fix for Windows restore work. Assess fresh analysis-cache saves separately in the consumer workflow.
5. Eventually compare at the same workload/dependency revision and fixed runner image, separating action implementation from workflow restructuring. Include post hooks and critical-path time alongside aggregate job seconds, bytes and cache growth.

No further dispatch, automatic trigger, publication or upstream change is authorized by this investigation request.
