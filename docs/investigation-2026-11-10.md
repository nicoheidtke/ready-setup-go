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

## Follow-up: improved preview consumer run

The user supplied completed run [42862214728](https://github.com/nicoheidtke/exasol-personal/actions/runs/42862214728). All jobs succeeded (SonarQube skipped). It checked out consumer `76322ee01b0481d7bffd98d45abb89d2674f2a76` and improved preview `e6e119a8b0b524a6ac1606d660c48603c11753df`. The only consumer change from the preceding warm run was the action SHA in `.github/actions/setup-go-better/action.yml`; no application/dependency files changed. Go remained 1.27.0, split/gzip archive restore-save, overlap enabled, four download/upload workers with 32 MiB blocks, no trimming. Strict errors remained off, but no cache warnings/errors appeared. No workflows were dispatched during this investigation.

| Job | Previous warm elapsed | New elapsed | Previous restore / save | New restore / save |
| --- | ---: | ---: | ---: | ---: |
| lint-go | 199s | 135s | 109.6s / 40.9s | 51.4s / 27.5s |
| build Linux | 88s | 83s | 36.9s / 20.5s | 23.5s / 20.5s |
| build Windows | 185s | 185s | 115.4s / 26.2s | 97.9s / 30.0s |
| tests-unit | 102s | 64s | 55.4s / 21.3s | 17.3s / 20.8s |
| tests-launcher Linux | 88s | 101s | 1.2s / 1.2s | 2.0s / 2.0s |
| tests-launcher Windows | 200s | 134s | 4.7s / 2.7s | 3.2s / 1.9s |

Definitions remain timestamped job-log elapsed intervals including post hooks, not billed/queued time. Across six Go jobs the aggregate changed from 861.5s to 701.7s; their span changed from 597.3s to 469.3s (about 9m57s to 7m49s). These are single-run observations with runner and cache-content variation, not a measured general speedup. The Windows build's initial checkout took about 21s versus about 7s previously, masking part of its cache restore reduction. Windows launcher pytest itself changed from 128.41s to 89.13s, so its whole-job gain must not be credited to the action.

### What the new metrics establish

- All four warm lint/build/unit jobs restored module and build archives. Launcher build archives restored; empty module caches again produced expected misses. Go jobs logged no module downloads and unit tests still reused cached package results.
- Every published restored file used hardlinks, with no copy fallback reported, on both Linux and Windows. This verifies the intended no-second-payload-copy path in these hosted jobs, not every filesystem/platform/policy case.
- Lint module upload skipped with `unchanged-module-content`; checking the 2.11GB / 80,189-file module tree took 3.64s. Previously its skip required 13.08s of packing. The latest lint build archive still took 20.77s to pack and 3.00s to upload about 507MB.
- Each of Linux build, Windows build and unit tests gained exactly one regular module-cache file and 151 payload bytes. Counts changed 17,992→17,993, 18,066→18,067 and 30,101→30,102 respectively. Those jobs re-uploaded about 375MB, 376MB and 437MB of modules. This is a real inventory change, unlike the earlier identical-count metadata-churn symptom; logs do not identify the file or establish that no other files changed. The cause must not be guessed or filtered away without inspection.
- Windows build restore still spent 88.78s in archive work, versus 8.92s transfer. Modules used 21.16s staging and 36.84s merge; build objects used 13.67s staging and 16.99s merge. Cached preflight took only 0.11s in total. Since every file linked successfully, the next Windows target is per-file staging/publication/metadata work, not an unobserved copy fallback or more download workers. Antivirus/volume effects are hypotheses until separately measured.
- Go cache transfers totalled 3,836,918,808 bytes restored and 2,704,294,728 bytes saved; saved modules account for 1,187,781,554 bytes and build archives 1,516,513,174. Module skip improvements have not yet reduced total uploads materially because those three trees acquired an additional file.

### Next experiment order

1. Repeat the same consumer SHA, preview SHA and settings unchanged once. This tests whether the extra 151-byte file is one-time cache convergence and whether the three module uploads then skip. It also gives another observation of noisy Windows setup/test durations. If inventory changes persist, capture relative path, size and hash differences before/after the workload; current aggregate reports cannot name the file.
2. Test `cache-storage: chunked` at the same workload revision with default chunk concurrency 4 and `cache-strict: true`. Its namespace is separate, so the first run seeds chunks and a second unchanged run tests warm reuse. Compare the second run against warm archive storage, counting complete restore/workload/post time, transferred/reused bytes, chunk requests and failures. Prioritize lint and Windows build if the consumer workflow can select jobs. Chunking targets repeated uploads, but still packs/stages the full tree and adds control requests; it is not a proven fix for Windows publication time.
3. A compression experiment (`cache-compression: none`) is a separate seed/warm pair, not a simultaneous change to chunk storage. It can reduce compression CPU while expanding transferred bytes and temporary disk. Lint packing is a concrete target, but Windows file-count overhead may dominate regardless.

Next local implementation candidates are finer bounded module change diagnostics, measured reduction/concurrency of Windows file publication and packing/compression work. Retain full validation, no-overwrite publication, preserved modes/mtimes and worker cancellation/join. Do not blindly exclude the unidentified module file, assume higher concurrency is faster, or change several experimental dimensions together. Further dispatches remain for the user to authorize or execute.

## Unchanged repeat: module caches converged

User-supplied run [42867214965](https://github.com/nicoheidtke/exasol-personal/actions/runs/42867214965) repeated consumer `76322ee01b0481d7bffd98d45abb89d2674f2a76`, preview `e6e119a8b0b524a6ac1606d660c48603c11753df` and the same default split/gzip archive settings. All jobs succeeded (SonarQube skipped); no cache warnings/errors or Go module downloads appeared. All four populated module snapshots reported `unchanged-module-content` and zero uploaded bytes; launcher module trees remained empty and skipped. The extra 151-byte inventory addition did not recur in this repeat. Its filename/cause remains unidentified, but it is no longer evidence of ongoing module re-upload churn at this unchanged revision.

| Job | Previous post-save | Repeat post-save | Module content check |
| --- | ---: | ---: | ---: |
| lint-go | 27.5s | 22.4s | 3.85s |
| build Linux | 20.5s | 13.9s | 1.10s |
| build Windows | 30.0s | 17.9s | 4.31s |
| tests-unit | 20.8s | 18.8s | 1.75s |

Go cache uploads totalled 1,517,307,593 bytes, all build snapshots, versus 2,704,294,728 bytes previously. Avoided module uploads account for 1,187,781,554 bytes; small build-snapshot differences account for the remaining net delta. Restored bytes were 3,838,727,124. Unit tests again had 28 cached package-result lines. All restored published files used hardlinks.

Timing remains noisy: six-Go-job span was 515.4s (8m35s), versus 469.3s (7m49s) previously; aggregate job elapsed was 731.6s versus 701.7s. Windows launcher pytest increased from 89.13s to 140.22s. Lint restore increased 51.4→68.0s, with module transfer increasing 10.7→27.1s; Windows build restore increased 97.9→109.3s, predominantly archive processing. Reduced upload bytes therefore must not be advertised as a whole-pipeline speedup.

The unchanged-repeat diagnostic is complete. The next storage experiment is the separate chunked seed/warm pair described above, preserving consumer revision, preview SHA, gzip and worker defaults, with strict errors for correctness. Its first run is cold because storage namespaces differ; compare the second against this steady archive baseline. The main remaining upload target is 1.52GB of fresh complete build archives, while Windows per-file restore work remains a separate implementation target. No workflow was dispatched during inspection, and no code/publication changes were made.
