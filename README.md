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

## An anecdote from my day job

I work on [exasol/exasol-personal](https://github.com/exasol/exasol-personal) in my day job. To try this out, I [forked it](https://github.com/nicoheidtke/exasol-personal), switched actions and adjusted the cache and warmup steps. With warm caches, several jobs finished noticeably sooner. These times include setup and post steps:

| Workflow / job | [Upstream run 43348902097](https://github.com/exasol/exasol-personal/actions/runs/43348902097) | [Upstream run 43442456182](https://github.com/exasol/exasol-personal/actions/runs/43442456182) | [Fork run 43666086120](https://github.com/nicoheidtke/exasol-personal/actions/runs/43666086120) |
| --- | --- | --- | --- |
| **Workflow elapsed** | **10m 18s** | **11m 00s** | **5m 51s** |
| Build (Ubuntu) | 1m 44s | 1m 40s | 39s |
| Build (Windows) | 2m 35s | 3m 01s | 1m 39s |
| Unit tests | 2m 41s | 2m 35s | 1m 03s |
| Launcher tests (Ubuntu) | 2m 09s | 2m 01s | 1m 44s |
| Launcher tests (Windows) | 5m 08s | 4m 59s | 2m 22s |
| Go lint | 52s | 1m 24s | 1m 39s |
| Python lint | 29s | 27s | 28s |
| SonarQube | 1m 29s | 1m 22s | Skipped |

The upstream used `actions/setup-go` v6.5.0; the fork used `ready-setup-go` v1.0.0 with separate dependency/build caches, zstd compression and matrix scopes. Build and unit-test logs confirmed cache restores; launcher tests had Go caching disabled in the fork.

That's encouraging, but it's **not a controlled comparison**. The runs used different revisions and triggers, I changed surrounding steps, and cache histories and runner load weren't controlled. SonarQube was skipped in the fork, and Go lint was slower, too. So I can't say how much of the improvement came from the action itself, but it's why I'm pursuing this.

## More details

- [Usage and input reference](docs/USAGE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Architecture](docs/ARCHITECTURE.md) and [security model](docs/SECURITY_MODEL.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md), and [releases](docs/RELEASES.md)
- [Security policy](SECURITY.md), [roadmap](ROADMAP.md), and [MIT license](LICENSE)
