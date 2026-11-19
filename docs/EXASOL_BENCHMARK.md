# Exasol Personal benchmark details

These observations accompany the [README benchmark](../README.md#benchmark). Each configuration uses Go 1.27.4 and has one cold and three sequential warm compile-mode runs.

## Setup and post stages

Times come from saved GitHub job/step timestamps, rounded to whole seconds. Setup covers the local wrapper, including Task setup, Go installation, cache restore and environment recording. Post covers its Go/cache post hooks. Separate linter-cache steps, checkout, artifacts and compilation are outside these columns; complete job/pipeline times include them. Warm columns are independent medians of three observations. A displayed 0s can include subsecond work.

| Configuration | Job | Cold setup | Cold post | Warm setup median | Warm post median |
| --- | --- | ---: | ---: | ---: | ---: |
| actions/setup-go v7.1.0 | lint-go | 8s | 12s | 46s | 0s |
| actions/setup-go v7.1.0 | build (ubuntu-24.04) | 8s | 5s | 12s | 0s |
| actions/setup-go v7.1.0 | build (windows-2025) | 23s | 12s | 74s | 0s |
| actions/setup-go v7.1.0 | tests-unit | 6s | 7s | 16s | 0s |
| ready-setup-go v2.1.0 | lint-go | 9s | 30s | 34s | 0s |
| ready-setup-go v2.1.0 | build (ubuntu-24.04) | 7s | 7s | 20s | 0s |
| ready-setup-go v2.1.0 | build (windows-2025) | 11s | 11s | 55s | 0s |
| ready-setup-go v2.1.0 | tests-unit | 18s | 14s | 13s | 0s |
| WillAbides v1.19.0 + actions/cache v6.1.0 | lint-go | 6s | 16s | 32s | 0s |
| WillAbides v1.19.0 + actions/cache v6.1.0 | build (ubuntu-24.04) | 4s | 6s | 14s | 0s |
| WillAbides v1.19.0 + actions/cache v6.1.0 | build (windows-2025) | 13s | 13s | 59s | 1s |
| WillAbides v1.19.0 + actions/cache v6.1.0 | tests-unit | 12s | 11s | 18s | 0s |
| CloudX v1.0.0 | lint-go | 14s | 14s | 42s | 13s |
| CloudX v1.0.0 | build (ubuntu-24.04) | 9s | 6s | 16s | 7s |
| CloudX v1.0.0 | build (windows-2025) | 26s | 19s | 73s | 17s |
| CloudX v1.0.0 | tests-unit | 11s | 12s | 17s | 7s |

## Stored cache entries and sizes

Each cell is the number of entries and their summed stored bytes after that run, including the auxiliary linter cache and older snapshots still listed. GB means 1,000,000,000 bytes. These repository inventory snapshots are not the bytes downloaded by that run or the expanded size on disk.

| Configuration | After cold | After warm 1 | After warm 2 | After warm 3 |
| --- | ---: | ---: | ---: | ---: |
| actions/setup-go v7.1.0 | 5 / 3.497 GB | 5 / 3.497 GB | 5 / 3.497 GB | 5 / 3.497 GB |
| ready-setup-go v2.1.0 | 9 / 3.539 GB | 9 / 3.539 GB | 9 / 3.539 GB | 9 / 3.539 GB |
| WillAbides v1.19.0 + actions/cache v6.1.0 | 9 / 3.498 GB | 9 / 3.498 GB | 9 / 3.498 GB | 9 / 3.498 GB |
| CloudX v1.0.0 | 5 / 3.497 GB | 9 / 6.966 GB | 13 / 10.433 GB | 15 / 11.782 GB |

## Download and extraction boundaries

The following tables use **warm run 3 only**, linked for each configuration. Archive sizes are compressed bytes in MiB (1,048,576 bytes). Counts refer to cache archives, not files inside them. Separate linter-cache operations are excluded here.

For configurations using actions/cache, fetch is estimated from the cache-hit log line to the final 100% download line. This includes intervening overhead and is not an instrumented network-only duration. Extraction runs from the tar extraction command to the successful-restore log line, including decompression and filesystem work. Lookup before the hit, compiler installation and post saves are outside these intervals. Values are rounded to 0.1s.

| Configuration / run | Job | Archive | Size (MiB) | Estimated fetch | Logged extraction |
| --- | --- | --- | ---: | ---: | ---: |
| [actions/setup-go v7.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44148365168) | lint-go | combined modules/build | 1394.4 | 16.8s | 16.3s |
| [actions/setup-go v7.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44148365168) | build (ubuntu-24.04) | combined modules/build | 626.4 | 2.5s | 1.4s |
| [actions/setup-go v7.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44148365168) | build (windows-2025) | combined modules/build | 627.7 | 5.3s | 31.7s |
| [actions/setup-go v7.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44148365168) | tests-unit | combined modules/build | 660.6 | 3.1s | 4.5s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | lint-go | modules | 976.2 | 6.8s | 9.2s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | lint-go | build | 418.6 | 3.9s | 6.2s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | build (ubuntu-24.04) | modules | 336.4 | 1.8s | 0.5s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | build (ubuntu-24.04) | build | 290.6 | 1.0s | 0.8s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | build (windows-2025) | modules | 336.5 | 2.3s | 23.4s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | build (windows-2025) | build | 291.3 | 2.2s | 16.7s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | tests-unit | modules | 385.9 | 1.9s | 0.8s |
| [WillAbides v1.19.0 + actions/cache v6.1.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | tests-unit | build | 274.6 | 1.0s | 1.3s |
| [CloudX v1.0.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208979971) | lint-go | combined modules/build | 1391.9 | 8.8s | 21.3s |
| [CloudX v1.0.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208979971) | build (ubuntu-24.04) | combined modules/build | 626.3 | 8.8s | 1.7s |
| [CloudX v1.0.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208979971) | build (windows-2025) | combined modules/build | 627.7 | 5.5s | 41.1s |
| [CloudX v1.0.0](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208979971) | tests-unit | combined modules/build | 660.1 | 3.5s | 4.0s |

Ready logs report each snapshot's lookup/transfer/archive time and the combined cache-work elapsed time. Prefetch and restore work overlap, including overlap with compiler preparation; snapshot times cannot be added to infer setup time. A complete separate download/decode/validation/publication/cleanup breakdown is absent from these logs. These values retain Ready's own timing definitions.

| Ready warm 3 job | Modules (MiB) | Reported modules time | Build (MiB) | Reported build time | Combined cache work |
| --- | ---: | ---: | ---: | ---: | ---: |
| lint-go | 983.9 | 32.03s | 425.1 | 17.52s | 39.33s |
| build (ubuntu-24.04) | 340.8 | 10.72s | 294.7 | 7.45s | 14.64s |
| build (windows-2025) | 340.8 | 35.23s | 295.5 | 18.48s | 49.77s |
| tests-unit | 386.8 | 10.05s | 281.7 | 7.32s | 13.21s |

Ready source: [warm run 3](https://github.com/nicoheidtke/exasol-personal/actions/runs/44406745294).

## Evidence and limits

The local evidence is in `exasol-personal/bin/benchmarks/`: each candidate directory contains per-run summaries, job/step API records, cache inventories and timestamped logs. The README links all sixteen accepted runs. The [benchmark protocol](https://github.com/nicoheidtke/exasol-personal/blob/a03fdf27b115f6fb480958651f70eefe82e78aec/doc/ci-benchmark.md) records candidate settings and workload.

These small sequential batches used varying hosted-runner load and image versions. Application source, workload commands and job dependencies stayed unchanged, while workflow revisions and cache configuration differed. Tests were compiled without execution. Archive file counts and expanded byte totals were not consistently captured across candidates; stored archive count and compressed size are available. Ready's optional detailed cache reports can record additional phases and file counts, but those reports are absent from this saved batch.
