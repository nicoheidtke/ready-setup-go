# Contributing

Use [Issues](https://github.com/nicoheidtke/setup-go-faster/issues) for bugs and proposals. For a bug, include a minimal workflow, bundled action SHA, runner/Go versions, and expected versus actual behavior. Redact credentials, signed URLs, private module names, and source from logs or reports. Report vulnerabilities privately through [SECURITY.md](SECURITY.md).

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

For concurrency changes, add `--race` to run uncached, ten-minute-bounded race tests for the cache, archive, backend, logging, and retry packages. This requires a supported Go race-detector platform, cgo enabled, and a compatible C compiler. Missing prerequisites fail visibly; ordinary checks do not imply race coverage. Parser fuzz seeds run with ordinary Go tests; see [bounded fuzzing](docs/DEVELOPMENT.md#bounded-fuzzing) for optional mutation runs.

Keep source UTF-8 with LF line endings, following `.editorconfig` and `.gitattributes`. Fix reported Go formatting with `gofmt`. Generated distribution bytes must remain unchanged after packaging.

## OpenSpec workflow

Use OpenSpec for new features, compatibility changes, and substantial refactors; small fixes and routine documentation edits can follow the ordinary workflow. A deliberate baseline of existing public behavior is maintained in `openspec/specs/`. Read the relevant capability specs and active changes before planning; maintain the baseline incrementally as behavior changes, using observable requirements and scenarios rather than transcribing the existing documentation.

Install the contributor-only OpenSpec CLI version **2.0.0** on Windows, macOS, or Linux. With the Node prerequisite above, either command installs the pinned version:

```text
npm install --global @fission-ai/openspec@2.0.0
bun add --global @fission-ai/openspec@2.0.0
```

Choose the package manager that manages your existing install, then check `openspec --version`. OpenSpec is not an action runtime or packaging dependency.

The committed `.agents/skills/openspec-*` files are the shared workflow instructions. Codex loads them directly; Claude Code loads the small `.claude/skills/openspec/SKILL.md` adapter, which reads the requested shared workflow. All files are ordinary files: no symlinks, junctions, platform-specific bootstrap, or duplicated workflow bodies are required. Restart your agent session after pulling new skills.

| Operation | Codex skill | Claude Code invocation |
| --- | --- | --- |
| Explore an idea | `$openspec-explore` | `/openspec explore` |
| Propose a change | `$openspec-propose` | `/openspec propose` |
| Implement tasks | `$openspec-apply-change` | `/openspec apply` |
| Revise a plan | `$openspec-update-change` | `/openspec update-change` |
| Sync requirements | `$openspec-sync-specs` | `/openspec sync` |
| Archive a change | `$openspec-archive-change` | `/openspec archive` |

Append the idea or change name to the selected invocation. In Codex desktop, select the corresponding skill from the available skills. Proposing produces planning artifacts; request apply when ready to implement. Before archiving, run the applicable contribution checks and validate the change:

```text
openspec validate <change-name> --strict --no-interactive
```

`openspec/config.yaml` contains only shared context pointers and artifact-specific rules. `openspec/specs/` owns behavioral requirements and scenarios; `openspec/changes/` owns proposals, designs, tasks, and their archive. Keep overview, usage, architecture, development, release, and roadmap facts in their existing owning documents as described in [AGENTS.md](AGENTS.md#change-and-validation-workflow). Reference those documents rather than duplicating them in specs or config. The populated behavioral baseline is the starting point for future capability changes; update the relevant specs while preserving these document ownership boundaries.

Refresh generated workflows with the pinned CLI and the explicit repository profile, then review the diff:

```text
openspec init --tools codex --profile core
```

The generator target in `.agents/skills/.openspec-target` is deliberately `codex`, which generates the shared files without Claude-specific copies or links. The Claude adapter is maintained separately and survives regeneration. Use the explicit refresh command above because `openspec update` follows the machine-wide profile, which may remove workflows used by the adapter. If using update, first inspect `openspec update --dry-run` and ensure the selected profile includes all six workflows listed above. Avoid adding the `claude` generator target: it installs per-workflow links or copies instead of using this repository's adapter. Tools that read `.agents/skills/` can reuse the shared files; add a thin adapter only for tools that need another discovery location. Review CLI upgrades separately and keep the adapter's operation mapping aligned with the installed workflows.

## Validate the change

Use focused tests while developing, then run the canonical checks before review. Add regression coverage for changed behavior, including relevant error and cancellation paths. Input or cache changes should cover validation, defaults, main/post compatibility, existing-file preservation, and format/key compatibility. Follow the [security model](docs/SECURITY_MODEL.md) when handling credentials, restored content, or diagnostics.

Packaging and launcher/lifecycle changes also need the relevant [native integration checks](docs/DEVELOPMENT.md#validation). Release candidates require clean-source reproducibility and [distribution verification](docs/RELEASES.md). These checks are separate from ordinary unit tests; skipped opt-in fixtures are not passing integration results. Check behavior on the Go 1.26 line and the release compiler where affected. See [support and validation limits](docs/DEVELOPMENT.md#support-and-validation-status).

Performance claims need reproducible evidence following the [measurement methodology](docs/BENCHMARKS.md), including complete workflow cost and resource tradeoffs. State the workload, revisions, configuration, and limitations.

## Submit a pull request

Keep the change focused and update the document that owns the affected behavior. Explain the problem and resulting behavior, compatibility implications, checks run, and anything skipped or unverified. Include a reproduction for a bug and representative evidence for a performance claim. Review considers correctness, public compatibility, resource bounds, maintainability, and adequate validation.

Repository workflows are manual. Maintainers control hosted execution and publication; the boundaries are in [AGENTS.md](AGENTS.md#maintainer-operations). Source-only revisions are not runnable action distributions.

Contributions are made under the project's [MIT license](LICENSE).
