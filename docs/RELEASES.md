# Release procedures

## Maintainer checklist

All source development happens through pull requests into `main`. Include README, usage, and other documentation changes in the same PR as the behavior they describe. There are no source release branches to merge back.

1. Merge the reviewed PRs into `main`.
2. Open **Actions → Release action → Run workflow**, select `main`, and enter an unused exact version such as `v1.0.0` or `v1.0.1`.
3. Enable **publish**. Leave **promote** and **attest** enabled for a normal release. Notes are optional; leaving them empty generates notes from merged pull requests.
4. Run the workflow and wait for success. Open the release linked in its summary and check the recorded distribution SHA and assets.

The workflow runs the existing contributor and six-native validation suite against its selected source commit, builds all six executables twice with Go 1.27.1, compares bytes, verifies archives and executable modes, and publishes only after its gates pass. It records the exact source revision and validation run in the release. A newer merge after dispatch belongs to the next release; publication refuses a conflicting source update rather than overwriting it.

The equivalent normal dispatch is:

```text
gh workflow run release.yml --ref main -f tag=v1.0.1 -F publish=true
```

Without **publish**, the workflow prepares artifacts and a release plan for inspection. With an empty tag it only prepares distribution artifacts. Workflows remain manual and maintainer-controlled. Run the [hosted cache-service probes](DEVELOPMENT.md#hosted-lifecycle) when relevant; native fixtures do not establish every service policy or a performance speedup.

## Versions and branches

`main` contains source and documentation. `dist` contains generated distributions and is maintained only by the publisher; never merge it into `main`, edit it manually, or use it as the consumer pin. The first release creates it automatically. Full version tags such as `v1.0.0` point at exact packaged distribution commits, including executables, rather than source commits.

After verifying publication, a normal release updates its major and minor convenience tags (`v1` and `v1.0`) and becomes GitHub's latest release. Documentation examples use `v1`, so patch releases need no follow-up README commit. Users who require fixed bytes can pin the full distribution SHA shown in the release notes. Source provenance, compiler, checksums, and validation evidence live with each release rather than in a changing source-documentation table.

Use patch versions for fixes, minor versions for compatible features, and a new major for breaking inputs, defaults, output semantics, or cache contracts. Update the recommended major in the usage guide and consumer examples as part of a major-version PR. Dated private-era previews are no longer a publication path.

For an older maintenance version, disable **promote** (locally, `release plan --promote=false`). This publishes the exact version without changing latest or aliases. Promotion rejects a version older than an existing version, including an older major; explicitly choose maintenance publication in that case. Major/minor aliases move with exact leases only after release verification; version tags and existing published assets are never overwritten.

## Local publication and recovery

The hosted workflow is the normal path. Local publication uses the same publisher after completing [contributor checks](../CONTRIBUTING.md) and relevant native checks, building twice as below, and fetching `origin` so existing distribution objects are available. Write reviewed notes to `.cache/release/notes.md`, then run:

```text
go run ./cmd/release plan --tag v1.0.0 --notes .cache/release/notes.md
go run ./cmd/release prepare
go run ./cmd/release publish
go run ./cmd/release publish --execute
```

Planning saves a reviewable ignored plan without changing refs or GitHub data. Preparation creates exact-byte Git objects and an annotated version tag without changing the source index. Publish defaults to a credential-free dry run. Only `--execute` pushes the source/distribution/version refs atomically with exact leases, uploads and verifies all nine asset sizes and server SHA-256 digests, publishes the release, fetches it again, and promotes aliases when requested. The candidate must already equal the remote source branch; publication cannot introduce unmerged source work. The source lease guards it against concurrent merges; distribution commits retain the previous distribution parent when present and the exact source as a parent. Source ancestry lets generated notes identify newly merged PRs across releases.

Authentication uses `GITHUB_TOKEN`, `GH_TOKEN`, or Git's credential helper for the API, and the configured Git/SSH transport for pushes. API credentials do not configure Git transport. URLs with embedded credentials and ambiguous push destinations are refused. The release command never dispatches workflows or changes repository settings.

If publication fails or is interrupted, inspect the run and release before dispatching again. A failure can happen after successful publication. Do not delete tags/assets, overwrite a plan, or rebuild the same version to force a retry.

The workflow uploads **release-recovery** before remote writes. It contains the plan, exact distribution/assets, notes, and a Git bundle retaining the source, distribution, and annotated tag objects. Download it within its 14-day retention period. Restore the bundle's refs and files into a clean checkout at the absolute paths recorded in the plan, using its recorded compiler. Reconstruct Git authentication, then resume:

```text
go run ./cmd/release publish --plan .cache/package/plan.json
go run ./cmd/release publish --execute --plan .cache/package/plan.json
```

For a local publication, resume with the original local plan path instead. Matching already-pushed refs, draft uploads, published data, and promoted aliases are reused. Changed refs, notes, assets, classification, or duplicate releases require inspection. Ambiguous API writes are not automatically retried. If only alias promotion failed, the same command verifies the published release before finishing promotion.

The release CLI verifies packaging and reproducibility; it does not execute the test/lint/native suites. The workflow supplies those gates. Mock-server recovery tests establish local behavior, while each hosted run supplies its own execution evidence.

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

For native diagnostics, supply the same `--platform windows/amd64` (or other target) to packaging and verification. Dirty builds omit `--require-clean`, require verification's `--allow-dirty`, and record `<HEAD>-dirty`; they are not release candidates. The source digest covers sorted nonignored working-tree paths/content, including untracked files and deletion markers; it excludes ignored files and filesystem modes. Keep outputs ignored or outside the source tree. Bundles include runtime files, consumer documentation, and `THIRD_PARTY_NOTICES.txt` with bundled dependency licenses; links to development-only guides point to the source commit. In dirty diagnostic builds those links refer to the base commit, not uncommitted edits.

Checksums detect corruption relative to a manifest, not publisher identity. An attacker replacing both can pass verification. Authenticate the release separately; rebuild reviewed source and compare bytes to establish reproducibility.

## Lightweight distributions

Default packaging remains bundled. To prepare a lightweight root action, choose the exact version tag before building and use the same fixed public locator for both reproducibility builds:

```text
go run ./cmd/package --require-clean --out .cache/release/dist --archives .cache/release/artifacts --native-download-base https://github.com/OWNER/REPO/releases/download/v1.0.0
```

Repeat with fresh reproduction directories and identical arguments, then run the ordinary distribution verifier. This mode compiles the requested platform set privately, writes exact sizes and SHA-256 values to `NATIVE_DOWNLOADS.json`, emits a distribution without `bin/`, and retains the same standalone native release assets. Both ZIP names remain part of the existing nine-asset publication contract; without bundled binaries their payloads are identical. Do not reuse an earlier bundled directory: packaging requires empty outputs. `--native-download-base` requires `--archives`; `--platform` still permits local single-target diagnostics.

The normal release plan/prepare/publish commands work for either distribution. Planning and subsequent validation require the manifest locator to name the selected repository and tag; asset verification compares every native size/digest against the pinned manifest. Publish all native assets before consumers use the lightweight distribution. Pin the resulting lightweight distribution commit, not the source commit or a stripped older bundle. Release URLs are mutable locators, not an authenticity guarantee; pinned digests prevent substituted bytes from executing, while asset deletion can still break availability. Private release authentication is deliberately unsupported by the bootstrap, so retain bundled distributions for private/offline use. No live publication or hosted acquisition is implied by local packaging checks.

## Hosted provenance and immutability

The default-enabled optional **attest** job rechecks the preparation artifacts and uses pinned `actions/attest` to generate provenance for all nine assets. Only that job requests `id-token: write` and `attestations: write`. The package job needs `contents: write` for opt-in publication; checkout does not persist credentials, and the API token is supplied only to release preparation/publication steps. The reusable validation jobs use `contents: read`.

After a successful attested run, consumers can verify an asset:

```text
gh attestation verify bundled.zip -R nicoheidtke/setup-go-faster
```

Check source revision, signer workflow, and invocation against the release. Attestations bind bytes to a workflow identity; they do not establish correctness or performance. Local builds do not have GitHub-hosted build attestations. See [artifact attestation verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

A commit SHA identifies fixed content; it does not lock release tags/assets. Check a release's immutable status before claiming it. The publisher prepares a draft, verifies all assets, then publishes, which is compatible with immutable releases. Repository immutability settings remain a separate maintainer decision; mutable major/minor aliases stay separate from full release tags. See [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).

[Development](DEVELOPMENT.md#support-and-validation-status) records support limits; the [roadmap](../ROADMAP.md) records remaining service and performance work. Stable versioning defines compatibility and does not imply every roadmap investigation has been completed.
