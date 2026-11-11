# Release procedures

Consumers need distribution commits containing generated executables; source commits are not runnable actions. Keep source/distribution revisions, compiler, checksums, and validation results with each release. [Development](DEVELOPMENT.md) records current support limits and published provenance.

## Build and verify

Run the [contribution checks](../CONTRIBUTING.md) and relevant [native lifecycle checks](DEVELOPMENT.md#validation). Commit the intended source, use the release compiler recorded in development, and keep the checkout unchanged through publication. Build twice into new or empty ignored directories. Packaging refuses nonempty output and asset directories, preventing stale files from entering a new distribution:

```text
go run ./cmd/package --require-clean --out .cache/release/dist --archives .cache/release/artifacts
go run ./cmd/package --require-clean --out .cache/release/dist-repro --archives .cache/release/artifacts-repro
go run ./cmd/verify-distribution --dist .cache/release/dist --archives .cache/release/artifacts
```

Compare both distribution trees and asset directories byte for byte. The packager records `SOURCE_REVISION` and `SOURCE_PROVENANCE.json` and rejects source changes during packaging. The compiler record identifies the Go executable on `PATH`.

`SHA256SUMS` covers every packaged regular file except itself. The asset manifest covers both ZIPs and six standalone executables. The verifier checks the complete platform set, provenance consistency, file coverage, standalone/bundled equality, and ZIP bytes/modes. Before pushing, verify committed blobs match packaged bytes and all four Unix binaries have Git mode `100755`. ZIP Unix binaries use `0755`; other files use `0644`. Windows filesystem modes cannot establish Unix Git modes.

Preserve the packaged `.gitattributes`: it prevents checkout text conversion from changing checksummed bytes. Do not normalize files after packaging.

For native diagnostics, supply the same `--platform windows/amd64` (or other target) to packaging and verification. Dirty builds omit `--require-clean`, require verification's `--allow-dirty`, and record `<HEAD>-dirty`; they are not release candidates. The source digest covers sorted nonignored working-tree paths/content, including untracked files and deletion markers; it excludes ignored files and filesystem modes. Keep outputs ignored or outside the source tree. Bundles include runtime files and consumer documentation; links to development-only guides point to the source commit. In dirty diagnostic builds those links refer to the base commit, not uncommitted edits.

Checksums detect corruption relative to a manifest, not publisher identity. An attacker replacing both can pass verification. Authenticate the release separately; rebuild reviewed source and compare bytes to establish reproducibility.

## Plan, prepare, and publish

The maintainer command publishes dated prereleases. If needed, fetch `codex/action-preview` before planning. Write reviewed notes to an ignored file, then run:

```text
go run ./cmd/release plan --tag v0.0.0-preview.YYYYMMDD --notes .cache/release/notes.md
go run ./cmd/release prepare
go run ./cmd/release publish
go run ./cmd/release publish --execute
```

Replace `YYYYMMDD` with the release date; an unused `.N` suffix allows another preview that day. Use command help for alternate paths, repository, remote, and refs.

| Phase | Effect |
| --- | --- |
| `plan` | Verifies both builds, provenance, checksums, modes, branch identity, and destinations; writes `.cache/release/plan.json` without changing refs or GitHub data |
| `prepare` | Uses a temporary index to create exact-byte distribution objects and an annotated local tag, preserves Unix modes, verifies committed files, and saves object IDs in the plan; does not push or alter the source index |
| `publish` | Dry-run preflight; retrieves no credentials and calls no GitHub write APIs |
| `publish --execute` | Atomically pushes source/preview/tag with exact leases, uploads and verifies assets, and publishes the prerelease |

Review the plan before publication. Its absolute paths belong to this checkout; protect unpublished notes. Preparation is repeatable with the same plan. Source updates must be fast-forwards; distribution commits retain the planned previous preview as parent. Remote URLs must be plain HTTPS or `git@github.com:owner/name.git`; embedded credentials and ambiguous push destinations are refused.

GitHub API authentication uses `GITHUB_TOKEN`, `GH_TOKEN`, or the configured Git credential helper. Git push separately uses the remote's configured Git/SSH authentication; an API environment token does not configure transport. Tokens are not placed in Git command arguments. Publication creates no workflow runs and changes no settings or stable aliases.

The publisher finds or creates a matching draft, resumes matching uploads, verifies all nine server SHA-256 digests/sizes, publishes without marking latest, and fetches the release to verify assets again. Ambiguous create/upload/publish requests are not automatically retried.

If interrupted, rerun the **same** `publish --execute` command with the saved plan. Exact already-pushed refs and matching draft/published data are accepted. Changed refs, notes, assets, tags, duplicate releases, or modifications to published releases require inspection. Do not delete the plan or re-plan over an existing tag. A final-verification error can occur after successful publication; inspect/resume before attempting a new release.

The publisher checks packaging and reproducibility, not the test/lint/native suite. Complete those gates before planning. API recovery is covered by mock-server tests; that does not establish hosted service results or issue an attestation.

## Hosted provenance and release policy

The manual **Prepare action distribution** workflow runs checks, builds twice, compares outputs, and verifies assets. By default it uploads preparation artifacts only. Its optional `attest` job rechecks the package job's artifacts and uses pinned `actions/attest` to generate provenance for all nine assets. Only that job requests `id-token: write` and `attestations: write`; ordinary preparation uses `contents: read`. The workflow creates no release, tag, or distribution commit.

After an attested build, consumers can verify an artifact:

```text
gh attestation verify bundled.zip -R nicoheidtke/setup-go-better
```

Check source revision, signer workflow, and invocation against the release record. Attestations bind bytes to a workflow identity; they do not establish correctness or performance. Local builds are not GitHub-hosted attestations. See [artifact attestation verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

A commit SHA identifies fixed content; it does not lock release tags/assets. Verify a release's immutable status before claiming it. For immutable releases, prepare a draft, attach and verify all assets, then publish. Release immutability and its release attestation differ from build provenance. See [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).

Use dated prereleases until the [stable-release gates](../ROADMAP.md#before-a-stable-release) are met. Stable releases use full semantic versions such as `v1.0.0`; breaking inputs, defaults, or output semantics require a new major version. Optional `v1` aliases may move to compatible validated distribution commits and must remain separate from immutable release tags. SHA-pinned consumers retain explicit update control.
