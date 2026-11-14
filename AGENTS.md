# Engineering guidance

This file applies to the whole repository and to human and automated contributors. Start with [CONTRIBUTING.md](CONTRIBUTING.md) for prerequisites, canonical checks, and pull request expectations.

## Project map

- `cmd/setup-go-faster`: native setup and post-job entrypoints.
- `internal/installer`: Go version selection, discovery, verified installation.
- `internal/cache`, `internal/cachearchive`, `internal/cachebackend`: cache policy, archive safety/publication, and service transfers.
- `internal/actionenv`, `internal/actionlog`, `internal/httpretry`: runner protocols, presentation, and bounded retries.
- Root `.cjs` files: dependency-free Node launcher and post-job bridge.
- `internal/distribution`, `internal/release`, and their `cmd/` entrypoints: packaging, integrity, and publication.
- `testdata/`, `.github/`, and other `cmd/` tools: fixtures, workflows, and measurements.

See [architecture](docs/ARCHITECTURE.md) for design, [usage](docs/USAGE.md) for the public interface, and [development](docs/DEVELOPMENT.md) for integration checks and support boundaries.

## Design constraints

- Optimize complete workflow cost first, resource efficiency second, maintainability third. Use the [measurement methodology](docs/BENCHMARKS.md); local timings or reduced bytes alone do not establish a workflow speedup.
- Keep runtime logic in Go, preferring standard-library dependencies. The pinned `github.com/klauspost/compress/zstd` implementation is the approved exception for zstd compression. Keep launcher glue small. Consumers run bundled executables without compiling the action or installing npm packages.
- Preserve public inputs, outputs, defaults, cache identities/formats, and main/post compatibility unless the change deliberately defines a migration or breaking release.
- Verify downloaded toolchains before publication. Validate complete restored archives in private staging, reject unsafe entries, and preserve existing usable local files.
- Bound concurrency and retained buffers. Propagate cancellation, join workers before cleanup, and surface finalization errors before publishing success.
- Treat cache contents as build inputs. Keep credentials out of state/reports and sanitize presentation; machine-readable reports require separate privacy review. See the [security model](docs/SECURITY_MODEL.md).

## Change and validation workflow

- Read the relevant code, tests, and documentation before editing. Keep changes focused and preserve unrelated work.
- Use the checks in [CONTRIBUTING.md](CONTRIBUTING.md), adding meaningful regression coverage for changed behavior. Packaging or lifecycle changes also require the relevant [integration checks](docs/DEVELOPMENT.md#validation) and [distribution verification](docs/RELEASES.md).
- Distinguish local fixtures, cross-compilation, native execution, hosted service behavior, and measured performance. Report skipped checks and unresolved limits accurately.
- Update the document that owns the behavior: README for an overview, usage for inputs/examples, architecture for design, development for procedures/support, releases for publication, and roadmap for future work. Avoid duplicating facts or adding execution diaries.

## OpenSpec

Use OpenSpec for new features, compatibility changes, and substantial refactors. Small fixes and routine documentation edits can use the ordinary contribution workflow unless explicitly requested otherwise. Read existing relevant specs and active changes before starting an OpenSpec change. See [the contributor workflow](CONTRIBUTING.md#openspec-workflow) for setup, invocation, and document ownership.

## Maintainer operations

Local edits and checks follow the requested task scope. Hosted workflow dispatch/rerun, changing workflow triggers or repository settings/visibility, publishing releases, and modifying upstream repositories require an explicit maintainer request. Keep prepared workflow changes separate from executing them.
