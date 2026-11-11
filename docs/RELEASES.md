# Release integrity and maintenance

Distribution commits contain generated executables; source commits are not directly runnable actions. Keep the immutable source commit, distribution commit, release tag, compiler, artifact checksums and validation results together in [development status](DEVELOPMENT.md). A reproducible local build verifies byte consistency, not the identity or security of its publisher.

## Prepare and verify locally

Use the pinned compiler recorded in development status and an output path ignored by Git. The packager records `SOURCE_REVISION` and `SOURCE_PROVENANCE.json`, then checks that source did not change while packaging:

```text
go run ./cmd/package --require-clean --out .cache/release/dist --archives .cache/release/artifacts
go run ./cmd/package --require-clean --out .cache/release/dist-repro --archives .cache/release/artifacts-repro
go run ./cmd/verify-distribution --dist .cache/release/dist --archives .cache/release/artifacts
```

Compare both output trees and asset directories byte for byte. `SHA256SUMS` inside the distribution covers every packaged regular file except itself; the asset manifest covers both ZIPs and all six standalone executables. The verifier requires the complete platform set by default, compares standalone binaries with their bundled counterparts, validates provenance/marker consistency, rejects unlisted or missing files, and checks every ZIP entry's bytes and portable executable mode. Directory executable modes are also checked on Unix; Windows directory metadata cannot establish Unix Git modes. Before publication, independently check the distribution commit's four Unix binary entries are Git mode `100755`. ZIP Unix binaries must be `0755`, other ZIP entries `0644`.

For native-only diagnostics, pass the same `--platform windows/amd64` (or another supported target) to packaging and verification. Dirty builds require omission of `--require-clean` and explicit `--allow-dirty` during verification. They record `<HEAD>-dirty` in `SOURCE_REVISION`; never describe that marker as an exact source commit. The source digest is a SHA-256 over sorted nonignored working-tree file paths and contents (including untracked files and deletion markers), not a Git tree ID or signature. Ignored files and filesystem permissions are outside this content digest. The compiler record identifies the Go executable used from `PATH`; use the pinned toolchain, not an incidental system compiler. Keep outputs ignored or outside the source tree so packaging does not create new source inputs.

Checksum verification detects corruption and packaging mismatches relative to the supplied manifest. An attacker who can replace both files and their manifest can pass it; authenticate the manifest/release separately. The verifier deliberately makes no claim that an executable was produced from its stated source. Rebuilding from the reviewed source and comparing bytes supplies that evidence.

The packaged `.gitattributes` disables text conversion so Git checkout preserves every checksummed file, including when a consumer enables `core.autocrlf`. Preserve that file in the distribution commit. Verify the committed blobs against the packaged bytes before pushing; publication must not normalize line endings after checksums and ZIPs are produced.

## Hosted preparation and attestations

The existing **Prepare action distribution** workflow remains `workflow_dispatch` only. It requires a clean checkout, runs tests/vet, builds twice, compares both trees and verifies all assets. Its default only uploads preparation artifacts. The optional `attest` input adds a separate job with `id-token: write` and `attestations: write`; ordinary preparation retains `contents: read`. The signing job downloads only the artifact produced by the successful package job in the same run, rechecks the checksums, and uses pinned [`actions/attest`](https://github.com/actions/attest/tree/508db95dd578ae2727ebd6217d5ba78e4fbda05d) to create build provenance for all nine files, including the checksum manifest. It uploads the returned signature bundle separately. It does not create a release, tag, distribution commit or stable alias.

This is prepared workflow code, not evidence that an attestation has been issued. A future run needs explicit authorization. Current GitHub plans support attestations for public repositories; private/internal repositories require Enterprise Cloud support. Do not change repository visibility or silently omit failed signing to work around plan limits. See [GitHub's attestation prerequisites and verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

After a supported authorized run, consumers can verify a downloaded artifact against this repository:

```text
gh attestation verify bundled.zip -R nicoheidtke/setup-go-better
```

Review the verified source revision, signer workflow and invocation against the release's recorded provenance, rather than accepting any attestation from the repository. Attestations bind bytes to a workflow identity; they do not establish correctness, performance or a safe workload. Keep the bundle with the release evidence; local preparation alone cannot mint GitHub-hosted provenance.

## Publish versioned releases

Use the tracked maintainer command instead of editing scripts in `.cache`. It is a prerelease publisher for this repository, with three explicit phases. First commit the intended source and use the pinned Go compiler to build the two copies above. Keep the checkout clean and do not edit source/docs between building and completing publication. If the preview branch is not available locally, run `git fetch origin codex/action-preview` before planning. Put reviewed release notes in an ignored file such as `.cache/release/notes.md`.

```text
go run ./cmd/release plan --tag v0.0.0-preview.YYYYMMDD --notes .cache/release/notes.md
go run ./cmd/release prepare
go run ./cmd/release publish
go run ./cmd/release publish --execute
```

Replace `YYYYMMDD` with the intended release date; an unused `.N` suffix permits another preview on that date. `plan` verifies both complete build copies, source/compiler provenance, checksums, ZIP modes, local branch identity and remote destinations. It writes `.cache/release/plan.json` containing the source, expected remote refs, file/asset digests and notes, without changing Git refs or GitHub data. Review this file. Its paths are absolute and belong to this checkout; keep it private if release notes contain unpublished details. Alternate `--root`, `--dist`, `--assets`, `--repro-dist`, `--repro-assets`, `--plan`, `--repository`, `--remote`, `--source-ref` and `--preview-ref` values are available. The supported remote URLs are plain HTTPS or `git@github.com:owner/name.git`; embedded credentials and multiple/different push destinations are refused.

`prepare` adds exact raw blobs through a private temporary index, preserves all four Unix executable modes, verifies the committed files, and creates an annotated **local** tag. It leaves the source checkout/index untouched. The distribution commit and tag object are saved in the plan; rerunning preparation reuses them. It does not push. `publish` without `--execute` performs a dry-run preflight and never retrieves credentials or calls GitHub write APIs. Only `publish --execute` performs external writes. Its GitHub API requests use `GITHUB_TOKEN`, `GH_TOKEN`, or the configured Git credential helper. Git push separately uses the named remote's configured Git/SSH authentication; an environment API token alone does not configure Git transport, and no token is placed in Git command arguments. The source/preview/tag push is atomic and uses exact ref leases. Source updates must be fast-forwards; preview commits must have the planned previous preview as their parent. It creates no workflow runs and changes no settings or stable aliases.

After a successful push, the command discovers existing drafts through GitHub's authenticated, bounded release list. It creates a draft only when none matches the unused tag, resumes matching uploaded assets, verifies all nine server SHA-256 digests/sizes, then publishes as a prerelease without marking it latest. A final published-release fetch checks every asset again. No ambiguous create/upload/publish request is automatically retried. If interrupted or a response is lost, rerun **the same** `publish --execute` command with the saved plan: exact already-pushed refs and matching draft/published data are accepted without duplicate writes. Changed branch refs, different notes/assets/tags, incomplete/mismatched remote assets, duplicate releases and published-release modifications are refused for manual inspection. Do not delete/re-plan over an existing tag to bypass these checks. An error after publishing can mean publication succeeded but final verification or saving the report did not; inspect/resume rather than creating another release.

The command verifies packaging and the two-build comparison; it does not run the test/lint/native lifecycle suite itself or replace the roadmap's release gates. Run and record those before planning. GitHub API behavior is exercised through local mock-server tests; the command does not generate a hosted attestation or enable release immutability. Publication notes and the saved plan are release evidence, not proof that hosted presentation or platform/policy gates passed.

Publication and repository-setting changes are separate authorized operations. Before using immutable releases, a maintainer must explicitly enable repository release immutability and verify the resulting release is marked immutable. A fixed Git SHA alone does not lock a GitHub release's tag/assets. GitHub recommends preparing a draft, attaching every asset, verifying sizes/digests and provenance, then publishing it. Immutability locks the release tag and assets and generates a release attestation; this differs from the build provenance attestation above. See [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases) and [enabling immutability](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes).

Use dated prerelease tags while acceptance and measurement gates remain incomplete. Never relabel an old local build as hosted-attested, or claim an existing release is immutable without checking it. Record native execution, service compatibility and performance separately from cross-builds and checksums.

A stable release must first satisfy the [roadmap gates](../ROADMAP.md). Then use a full semantic version such as `v1.0.0` for an immutable release-specific tag. Optional convenience aliases such as `v1` may move only to compatible, validated distribution commits and must not be tags attached to immutable releases. Document changes to defaults or output semantics and use a new major version for breaking changes. SHA-pinned consumers keep explicit update control. This work creates no stable tag or alias; see [GitHub's action release guidance](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases).
