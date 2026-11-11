# Contributing

Use [Issues](https://github.com/nicoheidtke/setup-go-better/issues) for bugs and proposals. For a bug, include a minimal workflow, bundled action SHA, runner/Go versions, and expected versus actual behavior. Redact credentials, signed URLs, private module names, and source from logs or reports. Report vulnerabilities privately through [SECURITY.md](SECURITY.md).

Discuss substantial changes to public inputs, defaults, or cache compatibility before implementation. Small fixes and documentation improvements can go straight to a pull request. Human and automated contributions follow the same requirements; [AGENTS.md](AGENTS.md) describes engineering constraints.

## Get started

Install Git, Go 1.26 or newer, and Node 24, then clone the repository and enter its root directory. Release builds use Go 1.27.1. Runtime code uses the Go standard library plus the pinned `github.com/klauspost/compress/zstd` codec, with dependency-free launcher files; no npm installation is needed. Dependency upgrades must preserve decoder resource limits and review chunk identity changes; refresh `THIRD_PARTY_NOTICES.txt` when upstream license notices change.

Run the canonical local checks:

```text
go run ./cmd/check
```

This validates Go formatting without rewriting files, runs Go tests and vet, runs all four Node test suites, and checks documentation links and the input reference. To select a Node 24 executable outside `PATH`, use `--node PATH`. Use `--help` for options. Tests use temporary files and local protocol fixtures; this command does not run hosted workflows or publish anything.

For workflow changes, also run:

```text
go run ./cmd/check --workflows
```

This adds actionlint 1.7.12 through `go run`, which may download the pinned tool and its dependencies. Optional shellcheck and pyflakes integrations are disabled. Go can also download a compiler selected by its toolchain settings; use `GOTOOLCHAIN=local` when checks must use only an already installed compiler.

Keep source UTF-8 with LF line endings, following `.editorconfig` and `.gitattributes`. Fix reported Go formatting with `gofmt`. Generated distribution bytes must remain unchanged after packaging.

## Validate the change

Use focused tests while developing, then run the canonical checks before review. Add regression coverage for changed behavior, including relevant error and cancellation paths. Input or cache changes should cover validation, defaults, main/post compatibility, existing-file preservation, and format/key compatibility. Follow the [security model](docs/SECURITY_MODEL.md) when handling credentials, restored content, or diagnostics.

Packaging and launcher/lifecycle changes also need the relevant [native integration checks](docs/DEVELOPMENT.md#validation). Release candidates require clean-source reproducibility and [distribution verification](docs/RELEASES.md). These checks are separate from ordinary unit tests; skipped opt-in fixtures are not passing integration results. Check behavior on the Go 1.26 line and the release compiler where affected. See [support and validation limits](docs/DEVELOPMENT.md#support-and-validation-status).

Performance claims need reproducible evidence following the [measurement methodology](docs/BENCHMARKS.md), including complete workflow cost and resource tradeoffs. State the workload, revisions, configuration, and limitations.

## Submit a pull request

Keep the change focused and update the document that owns the affected behavior. Explain the problem and resulting behavior, compatibility implications, checks run, and anything skipped or unverified. Include a reproduction for a bug and representative evidence for a performance claim. Review considers correctness, public compatibility, resource bounds, maintainability, and adequate validation.

Repository workflows are manual. Maintainers control hosted execution and publication; the boundaries are in [AGENTS.md](AGENTS.md#maintainer-operations). Source-only revisions are not runnable action distributions.

Contributions are made under the project's [MIT license](LICENSE).
