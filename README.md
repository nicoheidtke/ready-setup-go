# setup-go-better

Set up Go in GitHub Actions with optional, fresh module and build caches for each job.

- Reuse matching installed Go versions or download verified official releases.
- Keep recent build/test/lint snapshots without waiting for dependencies to change.
- Choose no caching, restore-only, or automatic post-job saves, with configurable storage and detailed cache reports.
- Follow setup and caching through grouped logs, progress updates and job summaries.

Supports **Go 1.26+** on Linux, macOS and Windows, each on amd64 and arm64. Caching is **off by default**.

## Quick start

Check out your project, then pin a bundled preview:

```yaml
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: nicoheidtke/setup-go-better@952566112acde233b6b762a78ebbceb743605f9a
        with:
          go-version-file: go.mod
          cache: restore-save
      - run: go test ./...
```

You can also set `go-version: '1.26.x'` or an exact patch. Minor selectors prefer an installed matching patch; `check-latest: true` requests the latest available patch. Version files support `go.mod` and `go.work`.

For matrices, include the differing dimensions in `cache-scope`. Use `cache: restore` to avoid post-job uploads. See the [usage guide and full input reference](docs/USAGE.md) for examples and standalone use. Pin a bundled distribution; source-only commits cannot run as an action.

## Compared with actions/setup-go

GitHub's official action offers broader version syntax and familiar defaults. This action focuses on fresh, isolated snapshots and explicit cache policy. Those choices trade cache freshness and control against additional uploads, storage and local work. Read the [concise feature and behavior comparison](docs/COMPARISON.md), and compare complete workflow cost before choosing on performance grounds.

## Documentation

- [Usage and input reference](docs/USAGE.md)
- [Comparison with setup-go](docs/COMPARISON.md)
- [Architecture](docs/ARCHITECTURE.md) and [security model](docs/SECURITY_MODEL.md)
- [Contributing](CONTRIBUTING.md), [development](docs/DEVELOPMENT.md), and [releases](docs/RELEASES.md)
- [Security policy](SECURITY.md), [roadmap](ROADMAP.md), and [MIT license](LICENSE)
