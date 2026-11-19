# ready-setup-go

Set up Go in GitHub Actions with reusable dependency baselines and configurable cache refreshes.

Your code changes more often than your dependencies. With caching enabled, the default policy saves a baseline on the default branch and skips uploads after matching dependency restores. Other branches reuse that baseline. You can enable branch-local saves and more frequent refreshes when retaining recent compiled results is worth the upload cost. Different jobs keep separate build caches.

This is still a personal experiment, targeting **Go 1.26+** on Linux, macOS and Windows, amd64 and arm64. I haven't tested every scenario yet; the [validation status](docs/DEVELOPMENT.md#support-and-validation-status) explains what's covered. If you try it, I'd love to hear how it goes in [issues](https://github.com/nicoheidtke/ready-setup-go/issues).

## Quick start

Caching is **off by default**. Here's how to turn it on:

Version 2 changes the enabled-cache save defaults. Existing v1 pins keep their previous behavior; see the [short migration guide](docs/RELEASES.md#baseline-cache-policy-migration).

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
          go-version-file: go.mod
          cache: restore-save
      - run: go test ./...
```

Use `go.mod`, `go.work`, or `go-version: '1.26.x'`. For a fixed revision, pin the distribution SHA from the [release notes](https://github.com/nicoheidtke/ready-setup-go/releases).

## How is it different from actions/setup-go?

Both actions install Go and can save dependencies and compiled results for later runs. Where this one takes a different approach is how that saved work gets refreshed and shared.

| What matters | `actions/setup-go` built-in caching | `ready-setup-go` |
| --- | --- | --- |
| Keeping recent build results | Skips saving after restoring an exact cache match. | Skips matching dependency baselines by default; opt into fresh build snapshots with `cache-save-policy: always`. |
| Saving dependencies | Downloaded dependencies and compiled results are saved together. | Saves them separately by default, with independent baseline decisions. |
| Different jobs | Matching Go, runner and dependency settings share a cache. | Build and test jobs retain their own useful work in separate caches. |
| Choosing when to upload | Caching is on by default, with automatic saves on cache misses. | Off by default; enabled caching seeds misses on the default branch. Branch writers and refresh frequency are configurable. |
| Multiple Go modules | Configure dependency paths for modules outside the repository root. | Finds module and workspace dependency files throughout the repository by default. |

If you use a matrix, set `cache-scope` to keep variants such as race and ordinary tests separate. If you only want to restore caches without uploading anything, use `cache: restore`. The [usage guide](docs/USAGE.md) has more examples.

There's a cost to this: saving fresh results takes upload time, and keeping jobs separate can use more storage. The official action also supports older Go versions, more version formats and download mirrors. You'll start with cold caches when switching, too. The [full comparison](docs/COMPARISON.md) covers the details.

## Why implement it differently?

Most of the action is written in Go. Partly because I just don't like the JavaScript ecosystem. :-) But there's a practical side, too: Go lets us spread compression across CPU cores and coordinate downloads, uploads and file operations in one compiled program. A small Node launcher connects it to GitHub Actions and runs the save step when your job finishes.

Here's where we're trying to do less waiting and less repeated work:

- **Do more at once.** Workers download and upload different parts of a large cache at the same time, while gzip and zstd compression can use multiple CPU cores. Cache downloads can also start while Go installs.
- **Touch the filesystem less.** Where possible, restored files are linked into place rather than copied. On Windows, once a restored directory has been checked, we can move it into place in one go if the destination doesn't exist. Files that still need individual handling can be put in place concurrently.
- **Reuse memory.** Transfers stream through limited buffers, and compression workers reuse their memory. Small archive writes are grouped into larger ones. That keeps queued data and memory allocations down, though archives still need temporary disk space.
- **Skip work we've already done.** Unchanged dependencies don't need packing or uploading again. Optional chunked storage lets us reuse unchanged pieces of a cache, sometimes skipping their compression too.

Compression support comes bundled, so it doesn't depend on which archive tools your runner has installed. It all uses GitHub's existing cache service; there's no extra service or credentials to set up.

If the Go version you need is already installed, we reuse it. Otherwise, we download it from Go and verify it before installing. With either action, you don't need to compile the installer or run npm in your workflow.

Go makes these choices practical, but using Go doesn't automatically make an action faster. What matters is how long your **whole job** takes, including cache uploads. If you're curious, the [architecture](docs/ARCHITECTURE.md) explains the internals and the [measurement guide](docs/BENCHMARKS.md) explains how to compare them.

## Benchmark

I compared four Go setup configurations in my [Exasol Personal fork](https://github.com/nicoheidtke/exasol-personal), using Go 1.27.4 on GitHub-hosted `ubuntu-24.04` and `windows-2025` runners. The table records complete pipeline elapsed times; each observation links to its workflow run.

| Configuration | Cold | Warm 1 | Warm 2 | Warm 3 | Warm median |
| --- | ---: | ---: | ---: | ---: | ---: |
| `actions/setup-go` v7.1.0 | [11m 35s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44138265814) | [3m 40s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44146187305) | [3m 59s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44147217831) | [3m 16s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44148365168) | 3m 40s |
| `ready-setup-go` v2.1.0 | [16m 12s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44388907787) | [2m 50s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44391866535) | [2m 40s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44392882367) | [3m 35s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44406745294) | 2m 50s |
| `willabides/setup-go-faster` v1.19.0 + `actions/cache` v6.1.0 | [11m 07s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44199244057) | [3m 41s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44201678826) | [2m 58s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44202588895) | [3m 01s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44203558214) | 3m 01s |
| `cloudx-io/setup-go` v1.0.0 | [14m 05s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44205098656) | [4m 41s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44207413204) | [3m 26s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208173698) | [4m 12s](https://github.com/nicoheidtke/exasol-personal/actions/runs/44208979971) | 4m 12s |

Each configuration had one cold run after clearing repository caches, followed by three sequential warm runs with unchanged source and inputs. Application source, workload commands and job dependencies stayed the same across candidates; workflow revisions and cache configurations differed. Restores and saves were checked in the logs.

The workflows used compile mode (`full=false`): Go and Terraform linting, coverage-instrumented launcher builds on Linux and Windows, and Linux test compilation with race detection and coverage. Tests were compiled without execution; Python checks were replaced with a placeholder. Elapsed time runs from the first job starting to the last job completing, including setup, cache saves and scheduling between dependent jobs. It excludes the initial queue and pauses between runs.

These are small sequential batches with varying runner load and image versions. The [detailed results](docs/EXASOL_BENCHMARK.md) include setup/post timings, cache entry counts and sizes, and the available download/restore breakdown. See the [benchmark protocol and candidate settings](https://github.com/nicoheidtke/exasol-personal/blob/a03fdf27b115f6fb480958651f70eefe82e78aec/doc/ci-benchmark.md) for configuration details.

## More details

- [Usage and input reference](docs/USAGE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Architecture](docs/ARCHITECTURE.md) and [security model](docs/SECURITY_MODEL.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md), and [releases](docs/RELEASES.md)
- [Security policy](SECURITY.md), [roadmap](ROADMAP.md), and [MIT license](LICENSE)
