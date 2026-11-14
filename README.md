# setup-go-faster

Set up Go in GitHub Actions with optional, fresh module and build caches for each job.

- Reuse matching installed Go versions or download verified official releases.
- Keep recent build/test/lint snapshots without waiting for dependencies to change.
- Choose no caching, restore-only, or automatic post-job saves, with configurable storage and detailed cache reports.
- Follow setup and caching through grouped logs, progress updates and job summaries.

Supports **Go 1.26+** on Linux, macOS and Windows, each on amd64 and arm64. Caching is **off by default**.

## Quick start

No distribution is published yet. After publication, replace `DISTRIBUTION_COMMIT_SHA` with a verified bundled distribution commit:

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

You can also set `go-version: '1.26.x'` or an exact patch. Minor selectors prefer an installed matching patch; `check-latest: true` requests the latest available patch. Version files support `go.mod` and `go.work`.

For matrices, include the differing dimensions in `cache-scope`. Use `cache: restore` to avoid post-job uploads. See the [usage guide and full input reference](docs/USAGE.md) for examples and standalone use. Pin a published distribution; source-only commits cannot run as an action. The default bundle is self-contained; maintainers can also publish lightweight distributions that download only the matching platform executable. See [action acquisition](docs/USAGE.md#action-acquisition) for access requirements.

With `cache: restore-save`, build caches are saved even after a restore hit; unchanged module contents skip upload in the default split layout. To refresh builds selectively while still saving changed modules, use `cache-build-save` with a workflow expression. See [choosing a save policy](docs/USAGE.md#choosing-a-save-policy) for branch-based and less frequent saves, including cold-cache behavior.

## Compared with actions/setup-go

GitHub's official action offers broader version syntax and familiar defaults. This action focuses on fresh, isolated snapshots and explicit cache policy. Those choices trade cache freshness and control against additional uploads, storage and local work. Read the [concise feature and behavior comparison](docs/COMPARISON.md), and compare complete workflow cost before choosing on performance grounds.

## Documentation

- [Usage and input reference](docs/USAGE.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Comparison with setup-go](docs/COMPARISON.md)
- [Architecture](docs/ARCHITECTURE.md) and [security model](docs/SECURITY_MODEL.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md), and [releases](docs/RELEASES.md)
- [Security policy](SECURITY.md), [roadmap](ROADMAP.md), and [MIT license](LICENSE)
