# Contributing

Use the bug and feature forms in [Issues](https://github.com/nicoheidtke/setup-go-better/issues). Include a minimal workflow, the bundled action SHA and relevant runner/Go versions. Redact credentials, signed URLs, private module names and source before attaching logs or JSON reports. Report vulnerabilities through the [security policy](SECURITY.md).

## Scope and support

The action selects Go 1.26+ and targets GitHub-hosted Linux, macOS and Windows on amd64 and arm64. Prereleases receive best-effort support without a response-time or backport commitment. Use [development status](docs/DEVELOPMENT.md) to distinguish implemented behavior, local fixtures and hosted validation; self-hosted guarantees and cache service cases remain subject to those limits.

Discuss changes to inputs, cache compatibility or defaults in an issue before a substantial implementation. Keep runtime work in dependency-free Go and launcher glue small. The priorities are complete workflow cost, resource efficiency and maintainability, in that order. Performance changes need representative measurements rather than assumed speedups. Read the [roadmap](ROADMAP.md), [development guide](docs/DEVELOPMENT.md) and [benchmark methodology](docs/BENCHMARKS.md).

## Local checks

Source files use LF line endings through `.gitattributes` and `.editorconfig`. If a Windows installation inherits automatic CRLF conversion, align this checkout with `git config --local core.autocrlf false` and `git config --local core.eol lf`; global Git settings need not change. Release packages have their own `.gitattributes` to preserve exact checksummed bytes, so do not normalize packaged files after building them.

Use the supported compiler versions recorded in the development guide. After changing Go or launcher behavior, run the relevant checks:

```text
gofmt -w cmd internal testdata/smoke
go test ./...
go vet ./...
node --test launcher.test.cjs .github/actions/cache-benchmark/run.test.cjs .github/actions/node-runtime/locate.test.cjs .github/scripts/cache-probe.test.cjs
```

Lint workflow edits with the pinned actionlint version in [the development guide](docs/DEVELOPMENT.md). Packaging, reproducibility and distribution verification commands are in [release procedures](docs/RELEASES.md); consumers require bundled executables, not a source-only commit. Local packaged fixtures are opt-in and use an existing toolchain. Add meaningful regression coverage for behavior changes and retain error/cancellation cleanup.

All repository workflows are manual-only during the proof of concept. Do not dispatch or rerun hosted workflows, enable automatic triggers, change visibility/settings, publish a release or modify the upstream Exasol repository as part of a contribution without explicit maintainer authorization. Preparing workflow definitions is separate from executing them.

Keep the README focused on consumers. Put build/validation records in the development guide, decisions in the roadmap and measurement definitions in the benchmark guide. A pull request should explain the resulting behavior, relevant checks and any unverified limits.

Contributions are made under the project's [MIT license](LICENSE).
