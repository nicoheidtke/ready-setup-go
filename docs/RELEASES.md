# Release procedures

## Maintainer checklist

Merge normal source and documentation PRs into `main`. Include README, usage, and other documentation changes in the PR that changes the behavior. Generated distributions never merge back into source branches.

1. Open **Actions → Release action → Run workflow** and leave **Use workflow from** set to `main`.
2. Enter an unused exact version, such as `v1.0.0`. For an ordinary release, leave **source_branch** as `main` and **source_commit** empty.
3. Enable **publish**. Keep **update_aliases** and **attest** enabled and **make_latest** set to `auto`. Notes are optional; empty notes are generated within the version's minor/major line.
4. Run the workflow and wait for success. Its summary links the release and records the full action distribution SHA and exact action source.

The ordinary CLI dispatch is:

```text
gh workflow run release.yml --ref main -f tag=v1.0.1 -F publish=true
```

The workflow resolves one source SHA before validation. Every unit/native check and package build uses that SHA, even if the branch advances later. It runs six-native validation, builds all six executables twice with Go 1.27.1, compares bytes, and verifies archives and executable modes. Requested attestations complete before publication. Candidate code runs without publication credentials; a separate publication job uses the current trusted publisher and a request binding the selected source, destinations, notes, and policies.

Without **publish**, the workflow prepares artifacts and a plan without writing remote Git refs or release data. An empty tag prepares distribution artifacts only. Hosted cache-service probes and performance measurements remain separate checks; see [hosted lifecycle](DEVELOPMENT.md#hosted-lifecycle).

## Maintenance branches and older commits

Keep **Use workflow from** on `main` in every case. **source_branch** selects action code, not automation. Use a source branch in this repository, such as `release/v1`; fork branches and PR refs are not accepted as source branches. Merge reviewed backports into the maintained branch before releasing. Individual fixes can be backported between source branches; `dist` is never merged into them.

For a v1 maintenance release after v2 exists:

```text
gh workflow run release.yml --ref main -f source_branch=release/v1 -f tag=v1.4.2 -F publish=true
```

For an older snapshot, also enter its full lowercase 40-character **source_commit** SHA. It must be reachable from **source_branch**. Use the source revision recorded in a release, not its packaged distribution SHA or version tag:

```text
gh workflow run release.yml --ref main -f source_branch=release/v1 -f source_commit=FULL_SOURCE_COMMIT_SHA -f tag=v1.4.2 -F publish=true
```

No temporary source branch or branch rewind is needed to select an older commit. That snapshot must still provide compatible packaging/check commands and pass its own tests, documentation checks, and native fixtures. The publisher comes from current main even if the selected snapshot predates it. If an old snapshot fails validation or lacks required packaging files, create a backport branch with the necessary fixes rather than bypassing the gates.

## Versions, aliases, and latest

Full version tags such as `v1.4.2` identify exact packaged distributions. `dist` is generated output history; never edit it manually or use it as a consumer pin. Major/minor tags are convenience aliases. Documentation can keep using `v1` through compatible patch/minor releases; users needing fixed bytes use the full distribution SHA recorded with each release.

**update_aliases** advances each eligible series independently. For example:

| New release | Existing newer versions | Aliases advanced | Latest with `auto` |
| --- | --- | --- | --- |
| `v2.1.0` | `v2.0.0` | `v2`, `v2.1` | `v2.1.0` |
| `v1.4.2` | `v1.4.1`, `v2.0.0` | `v1`, `v1.4` | Keeps `v2.0.0` |
| `v1.4.2` | `v1.5.0` | `v1.4` only | Keeps `v1.5.0` |

An alias never moves backwards to an older version in its series. Disable **update_aliases** to publish only the exact version. **make_latest** independently supports `auto`, `true`, and `false`: auto selects latest only when no higher stable version tag exists, true deliberately marks the release latest, and false leaves GitHub latest unchanged. Planning freezes these choices; changed version policy or alias refs require inspection before publication/resumption.

Use patch versions for fixes, minor versions for compatible features, and a new major for breaking inputs/defaults/output semantics or cache contracts. Update the recommended major and examples in the major-version PR. Dated private-era previews remain unsupported.

## Local publication and recovery

The hosted workflow is the normal path. Local publication uses the same publisher after completing [contributor checks](../CONTRIBUTING.md) and relevant native checks, building twice as below, and fetching `origin` so existing distribution and source-anchor objects are available. To select a historical source locally, build the current release CLI to a file outside the source checkout before checking out the desired source; invoke that trusted executable for all release phases. Write reviewed notes to `.cache/release/notes.md`, then run:

```text
go run ./cmd/release plan --tag v1.0.0 --notes .cache/release/notes.md
go run ./cmd/release prepare
go run ./cmd/release publish
go run ./cmd/release publish --execute
```

For maintenance source, add `--source-ref refs/heads/release/v1` to `plan` and check out the branch tip or the desired ancestor first. Use `--update-aliases=false` for exact-version-only publication and `--make-latest=auto|true|false` independently. `release source --branch release/v1 --commit FULL_SOURCE_COMMIT_SHA` resolves/validates a source without remote writes. Historical checkout selection does not require a local branch to point at HEAD.

The old `--promote` flag and unpublished plan schema are replaced by these independent controls. Recreate only unpublished plans. An already-pushed release must resume with its original matching tool and plan; do not overwrite its tag or rebuild that version.

Planning saves a reviewable ignored plan without changing refs or GitHub data. Preparation creates exact-byte Git objects and an annotated version tag without changing the source index. Publish defaults to a credential-free dry run. Only `--execute` pushes the distribution/version refs atomically with exact leases, uploads and verifies all nine asset sizes and server SHA-256 digests, publishes the release, fetches it again, and advances eligible aliases when requested. The candidate must belong to the recorded remote source branch history. Source branches are never pushed or rewound; distribution commits retain the previous distribution parent when present and the exact source as a parent. Source ancestry lets generated notes identify newly merged PRs across releases.

Authentication uses `GITHUB_TOKEN`, `GH_TOKEN`, or Git's credential helper for the API, and the configured Git/SSH transport for pushes. API credentials do not configure Git transport. URLs with embedded credentials and ambiguous push destinations are refused. The release command never dispatches workflows or changes repository settings.

If publication fails or is interrupted, inspect the run and release before dispatching again. A failure can happen after successful publication. Do not delete tags/assets, overwrite a plan, or rebuild the same version to force a retry.

The workflow uploads **release-controller** and **release-preparation** before remote writes. Together they retain the trusted publisher executable/request, notes, plan, exact distribution/assets, and a Git bundle containing the selected source anchor, distribution, and annotated version tag. Download it within its 14-day retention period. Restore the selected source checkout and preparation files at the absolute paths recorded in the plan, using its recorded compiler. Restore the trusted controller artifact separately, outside candidate files. Artifact transport strips executable bits, so restore the controller and four known Unix distribution binaries to executable mode. Import the prepared Git objects without running preparation again:

```text
git fetch .cache/package/recovery.bundle '+refs/tags/*:refs/tags/*' '+refs/setup-go-faster/source-anchor:refs/setup-go-faster/source-anchor'
```

Reconstruct Git authentication and use the saved controller, not the historical source's release command:

```text
PATH_TO_CONTROLLER/release publish --plan .cache/package/plan.json --request PATH_TO_CONTROLLER/request.json
PATH_TO_CONTROLLER/release publish --execute --plan .cache/package/plan.json --request PATH_TO_CONTROLLER/request.json
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

The default-enabled optional **attest** job rechecks the preparation artifacts and uses pinned `actions/attest` to generate provenance for all nine assets. Only that job requests `id-token: write` and `attestations: write`. The trusted source/notes job needs `contents: write` because [GitHub requires it for generated notes](https://docs.github.com/en/rest/releases/releases#generate-release-notes-content-for-a-release); it never executes candidate code. Candidate validation and packaging use `contents: read`. A separate publication job has `contents: write`, uses an independently downloaded trusted publisher/request, and exposes its token only to publication. Checkout does not persist credentials.

After a successful attested run, consumers can verify an asset:

```text
gh attestation verify bundled.zip -R nicoheidtke/setup-go-faster
```

Check signer workflow and invocation against the release. Hosted provenance identifies the automation revision on main; the selected action source can differ. Its exact revision and source-content digest are recorded in `SOURCE_REVISION` and `SOURCE_PROVENANCE.json` inside the verified distribution, and the release notes distinguish the automation revision from that source. Attestations bind bytes to a workflow identity; they do not establish correctness or performance. Local builds do not have GitHub-hosted build attestations. See [artifact attestation verification](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

A commit SHA identifies fixed content; it does not lock release tags/assets. Check a release's immutable status before claiming it. The publisher prepares a draft, verifies all assets, then publishes, which is compatible with immutable releases. Repository immutability settings remain a separate maintainer decision; mutable major/minor aliases stay separate from full release tags. See [immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).

[Development](DEVELOPMENT.md#support-and-validation-status) records support limits; the [roadmap](../ROADMAP.md) records remaining service and performance work. Stable versioning defines compatibility and does not imply every roadmap investigation has been completed.
