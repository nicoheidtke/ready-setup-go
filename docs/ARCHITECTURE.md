# Architecture and resource behavior

The root action uses GitHub's Node 24 runtime for credentials and automatic post-job hooks. Minimal, dependency-free launcher glue invokes a precompiled Go executable; installation, cache policy, archive handling and networking stay in Go. Consumers do not install npm packages or build the action. A bundled distribution contains six executables, so action acquisition transfers binaries for platforms the current job does not use.

The `experimental/node` path aliases the root lifecycle. The Bash composite at `experimental/composite` installs Go only and exists for launcher comparisons; it does not provide automatic caching. See [usage](USAGE.md) for the public interface, [development](DEVELOPMENT.md) for packaging and verification, and the [cache protocol](../internal/cachebackend/PROTOCOL.md) for service details.

## Installation

Completed matching toolchains are discovered in the runner tool cache and the action's installation directory. Missing versions are resolved through official Go metadata, downloaded with SHA-256 and size verification, extracted privately and published with a directory rename. An interrupted install is never marked complete; existing installations are not overwritten.

New toolchains live under `RUNNER_TEMP/setup-go-better/toolchains`. Linux/macOS tar archives stream through extraction. Windows ZIP archives require temporary storage for random access. Toolchain persistence depends on the runner; a warm dependency cache does not imply an installed compiler on a fresh runner.

## Cache lifecycle and transfer

Cache lookup and download can begin after exact version selection, overlapping toolchain installation. Destination paths still come from the selected Go executable. `cache-overlap: false` retains sequential operation. Overlap may retain both split downloads temporarily; sequential restore retains one archive at a time. Split downloads and merges themselves remain ordered rather than forming a transfer/merge pipeline.

Whole-archive downloads use bounded parallel HTTP ranges when the service supports them. A full response to the initial range request is consumed directly as a single-download fallback. Download workers and range sizes are independent of upload controls; concurrency 1 uses a single GET. Workers stream bounded buffers rather than retaining full ranges in memory, and cancellation joins workers before cleanup.

Restore validates the complete tar/compression stream in private staging before changing destination cache entries. Staging uses the destination filesystem where possible. Validated files are published with non-overwriting hard links; unsupported filesystems retain an exclusive-create copy fallback. Existing regular files win. Private staging and temporarily relaxed read-only directory modes are cleaned up/restored. These guarantees target trusted runner filesystems, not hostile concurrent writers.

Whole-archive saves walk and pack the cache into a temporary file. In split mode, a restored module tree is first fingerprinted by paths, permissions and full file contents, ignoring timestamps. Equal content skips packing and upload; changed modules pay the check plus packing. Build snapshots remain fresh. This trades repeated upload avoidance against local reads, CPU, temporary disk and cache entry growth.

## Optional chunked storage

`cache-storage: chunked` splits the uncompressed deterministic tar into content-defined chunks of 1–8 MiB, approximately 4 MiB target. Each chunk is compressed independently when gzip is selected. Missing immutable chunks are uploaded before publishing a fresh manifest. The same GitHub cache service and credentials are used; no additional consumer action or infrastructure is required.

Chunk workers perform save hash/compression/lookup/upload and restore lookup/download/decompression. Duplicate chunks in one snapshot transfer once. Every referenced chunk is checksum-verified. Restore writes validated offsets in a private archive, then validates the complete archive before merge. Missing/evicted referenced chunks produce a cache miss; corruption follows the configured error policy. Chunked and whole-archive namespaces are separate, and reuse does not cross jobs or architectures.

`cache-chunk-concurrency` independently bounds chunk workers. Chunk blob operations use one upload/download worker each, avoiding nested request multiplication. Allow approximately 32 MiB of chunk buffers per active worker plus 16 MiB for the save producer: about 144 MiB at the default four workers, excluding gzip/runtime overhead. This is a planning allowance, not a total memory measurement. There is no unbounded payload queue or restore reorder buffer.

Chunking still scans/packs the complete tree and retains a full temporary uncompressed tar on save/restore. It can avoid repeated encoded transfers, while adding manifests, lookups, requests, buffers and independently evictable entries. Neither chunking, parallelism nor compression is universally preferable. [Benchmark methodology](BENCHMARKS.md) defines the measurements; [the roadmap](../ROADMAP.md) records deferred alternatives.
