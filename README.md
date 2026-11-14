# setup-go-faster

Set up Go in GitHub Actions, with more control over how your module and build caches get reused and refreshed.

It reuses a matching Go installation if one is already there. Otherwise, it downloads an official release and verifies it before installing it. The action itself runs prebuilt Go executables, so there's nothing to compile and no npm packages to install in your workflow.

The cache behavior is the main difference. With `cache: restore-save`, a job can restore a build cache and still save a fresh snapshot when it's done. You don't have to wait for a dependency change to refresh it. Module caches are handled separately by default, and unchanged module contents aren't uploaded again.

It supports **Go 1.26+** on Linux, macOS and Windows, on both amd64 and arm64. **Caching is off by default.** Turn it on if you want it.

## Quick start

There's no published distribution yet. Once one is available, replace `DISTRIBUTION_COMMIT_SHA` below with its verified bundled distribution commit. A source commit won't work as an action; it needs the packaged executables.

```yaml
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: nicoheidtke/setup-go-faster@DISTRIBUTION_COMMIT_SHA
        with:
          go-version-file: go.mod
          cache: restore-save
      - run: go test ./...
```

You can read the version from `go.mod` or `go.work`, or set it directly with something like `go-version: '1.26.x'` or an exact patch. A minor selector reuses an installed matching patch if it can. Set `check-latest: true` if you want the latest available patch instead.

Use `cache: restore` if you only want to read caches without uploading anything at the end. For matrix jobs, put the dimensions that differ between jobs in `cache-scope` so their caches stay separate. If you want to save build caches less often while still saving changed modules, there's [`cache-build-save`](docs/USAGE.md#choosing-a-save-policy) for that.

The default distribution bundles everything the action needs. There's also a lightweight option that downloads just the executable for the runner's platform and verifies it against the pinned metadata. The [usage guide](docs/USAGE.md) covers the inputs and examples, including [how the action is distributed](docs/USAGE.md#action-acquisition).

## Compared with actions/setup-go

GitHub's official action supports more Go versions and version formats. This one focuses on fresh caches, keeping job caches separate, and letting you decide what gets restored and saved.

That comes with tradeoffs. Fresh snapshots mean more uploads, and separate job caches can use more storage. I haven't established a controlled end-to-end speedup yet, so whether this saves time in your workflow still needs measuring. Include setup, cache restore, the actual work, and the final cache upload when you compare them.

The [comparison](docs/COMPARISON.md) goes through the differences. If you want to measure it yourself, the [benchmark guide](docs/BENCHMARKS.md) explains how.

## More details

- [Usage and input reference](docs/USAGE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Architecture](docs/ARCHITECTURE.md) and [security model](docs/SECURITY_MODEL.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md), and [releases](docs/RELEASES.md)
- [Security policy](SECURITY.md), [roadmap](ROADMAP.md), and [MIT license](LICENSE)
