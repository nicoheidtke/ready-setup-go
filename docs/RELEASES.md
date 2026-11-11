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

## Hosted preparation and attestations

The existing **Prepare action distribution** workflow remains `workflow_dispatch` only. It requires a clean checkout, runs tests/vet, builds twice, compares both trees and verifies all assets. Its default only uploads preparation artifacts. The optional `attest` input adds a separate job with `id-token: write` and `attestations: write`; ordinary preparation retains `contents: read`. The signing job downloads only the artifact produced by the successful package job in the same run, rechecks the checksums, and uses pinned [`actions/attest`](https://github.com/actions/attest/tree/508db95dd578ae2727ebd6217d5ba78e4fbda05d) to create build provenance for all nine files, including the checksum manifest. It uploads the returned signature bundle separately. It does not create a release, tag, distribution commit or stable alias.

This is prepared workflow code, not evidence that an attestation has been issued. A future run needs explicit authorization. Current GitHub plans support attestations for public repositories; private/internal repositories require Enterprise Cloud support. Do not change repository visibility or silently omit failed signing to work around plan limits. See [GitHub's attestation prerequisites and verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

After a supported authorized run, consumers can verify a downloaded artifact against this repository:

```text
gh attestation verify bundled.zip -R nicoheidtke/setup-go-better
```

Review the verified source revision, signer workflow and invocation against the release's recorded provenance, rather than accepting any attestation from the repository. Attestations bind bytes to a workflow identity; they do not establish correctness, performance or a safe workload. Keep the bundle with the release evidence; local preparation alone cannot mint GitHub-hosted provenance.

## Publish versioned releases

Publication and repository-setting changes are separate authorized operations. Before using immutable releases, a maintainer must explicitly enable repository release immutability and verify the resulting release is marked immutable. A fixed Git SHA alone does not lock a GitHub release's tag/assets. GitHub recommends preparing a draft, attaching every asset, verifying sizes/digests and provenance, then publishing it. Immutability locks the release tag and assets and generates a release attestation; this differs from the build provenance attestation above. See [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases) and [enabling immutability](https://docs.github.com/en/code-security/how-tos/secure-your-supply-chain/establish-provenance-and-integrity/prevent-release-changes).

Use dated prerelease tags while acceptance and measurement gates remain incomplete. Never relabel an old local build as hosted-attested, or claim an existing release is immutable without checking it. Record native execution, service compatibility and performance separately from cross-builds and checksums.

A stable release must first satisfy the [roadmap gates](../ROADMAP.md). Then use a full semantic version such as `v1.0.0` for an immutable release-specific tag. Optional convenience aliases such as `v1` may move only to compatible, validated distribution commits and must not be tags attached to immutable releases. Document changes to defaults or output semantics and use a new major version for breaking changes. SHA-pinned consumers keep explicit update control. This work creates no stable tag or alias; see [GitHub's action release guidance](https://docs.github.com/en/actions/how-tos/create-and-publish-actions/using-immutable-releases-and-tags-to-manage-your-actions-releases).
