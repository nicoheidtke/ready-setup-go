# Roadmap

Priorities are complete workflow cost, resource efficiency, and maintainability, in that order. Local microbenchmarks alone do not establish a workflow speedup.

## Before a stable release

- Complete native cache lifecycle checks on Linux, macOS, and Windows, on amd64 and arm64, using the action's Node 24 runtime.
- Validate hosted chunked storage, branch/default-branch access, restricted tokens, failed/cancelled jobs, concurrent writers, and presentation. See [validation](docs/DEVELOPMENT.md#validation).
- Collect controlled cold/warm and change-scenario comparisons for small modules, parallel build/test/lint jobs, and monorepos. Include acquisition and post saves; follow the [measurement methodology](docs/BENCHMARKS.md).
- Retain clean-source reproducibility, distribution integrity, and executable-mode verification as [release gates](docs/RELEASES.md).

## Candidates to evaluate

| Candidate | Decision evidence |
| --- | --- |
| Matching-platform downloads or bundles | Acquisition latency, bytes, integrity, and release complexity versus the six-platform bundle |
| Transfer/merge pipelining and extraction changes | Complete restore time, disk contention, memory, cancellation, and platform consistency |
| Chunked storage and worker defaults | Complete workload time, requests, encoded bytes, temporary disk, and eviction behavior |
| External storage or `GOCACHEPROG` | Authentication, per-object latency, operational cost, and end-to-end results |
| Metadata/toolchain archive caching | Freshness contract and benefit over installed discovery and official downloads |

These are investigation candidates, not promised features. Existing storage, compression, overlap, and concurrency choices remain configurable.

## Compatibility

Future work may define stronger self-hosted persistence/concurrency guarantees, custom mirrors with a clear integrity model, and optional Go diagnostic problem matchers. Go versions before 1.26, consumer-side source builds, and arbitrary version-range syntax are outside the intended scope.

Current support and validation limits are in the [development guide](docs/DEVELOPMENT.md#support-and-validation-status); the public interface is in [usage](docs/USAGE.md).
