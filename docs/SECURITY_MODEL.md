# Permissions and cache trust

Use this guide when choosing cache policy or running the action on a persistent runner. The [usage reference](USAGE.md) describes inputs; [architecture](ARCHITECTURE.md) describes transfer and filesystem behavior. Pin a bundled distribution commit containing the executable for your platform.

## Permissions and credentials

The installer downloads official Go releases over HTTPS. The root Node action receives GitHub's runtime cache credentials and passes them to Go; it does not use `GITHUB_TOKEN` to install Go or restore/save caches. Normal use needs no PAT, token input, `actions: write`, or `id-token: write` permission.

| Operation | Required access |
| --- | --- |
| Install Go, write action outputs and job summaries | No `GITHUB_TOKEN` scope; runner directories and environment files must be writable |
| Restore/save caches | Runner-issued cache access, further limited by the action's `cache` input |
| Check out the consumer repository | Normally `contents: read` for `actions/checkout` |

For a normal checkout/build job, set `permissions: { contents: read }`. The action itself can run with `permissions: {}` if the job does not need repository API access. Other steps may need their own scopes. See [checkout's permissions](https://github.com/actions/checkout#recommended-permissions) and [GitHub's permission syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#permissions).

## Action policy and runner policy

The `cache` input chooses what this action requests: `off` does no cache work, `restore` never registers a save, and `restore-save` requests both. It cannot grant access denied by GitHub. GitHub's workflow/job `cache-mode` separately controls the scoped cache token; it is not a `with:` input. Its modes are `read`, `write` (read and write), `write-only`, and `none`. See [the workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#cache-mode).

For example, a job that should only read caches can use both controls:

```yaml
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    cache-mode: read
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: nicoheidtke/setup-go-better@952566112acde233b6b762a78ebbceb743605f9a
        with:
          go-version-file: go.mod
          cache: restore
      - run: go test ./...
```

The action honors the runner's `ACTIONS_CACHE_MODE` signal and checks it again in post. A known read-only policy skips saving; write-only skips restoring; `none` skips both. Unset/unknown signals preserve older-runner behavior and let the service enforce access. Operational denials warn by default or fail with `cache-strict: true`; an intentional policy skip is not an operational failure. Do not inject runtime tokens or override the environment signal to bypass policy: the token's effective scope remains authoritative.

## What restoring a cache trusts

Cache directories are shared with other runs allowed by GitHub's access rules. Keep credentials and other secrets out of them; private module source and generated build data may also be sensitive. Fork pull requests can read eligible base-branch caches. Access includes current/default branches and pull-request bases, with restrictions for sibling/child branches. Pull-request-created caches normally belong to that merge ref. See [GitHub's cache access restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#restrictions-for-accessing-a-cache).

Treat a restored build cache as executable build input. `cache: restore` prevents this action uploading data, but still reads artifacts that later build/test commands consume. Archive checksums and content hashes establish transport/content consistency, not who produced an artifact or whether its contents are safe.

Repository/workflow/job/scope, Go version, platform and dependency fingerprints separate compatible snapshots. These names and hashes are not cryptographic authorization or trust isolation. `cache-scope` separates workloads; it does not make an untrusted writer trustworthy. A privileged job should restore only caches whose eligible producers it trusts, or use `cache: off` and clean local cache directories. Disabling remote caching does not erase local cache contents.

Do not check out or execute untrusted code in jobs with secrets, elevated write permissions or a persistent privileged runner. GitHub restricts cache writes for low-trust triggers; retain those restrictions rather than adding a write-capable `cache-mode` to make a restore/save succeed. `pull_request_target` is not a shortcut for testing fork code. See [secure workflow use](https://docs.github.com/en/actions/reference/security/secure-use#mitigating-the-risks-of-untrusted-code-checkout).

## Filesystem and persistent runners

Restores validate the complete archive in private staging before publishing files. They reject links, unsafe paths, special entries and collisions. Existing regular files win; valid files publish without replacement using hardlinks or exclusive-create copies. A corrupt archive does not merge. Publication is not a whole-tree transaction: an I/O failure during merge may leave successfully added files. These checks assume a trusted filesystem and do not defend against a hostile process concurrently changing paths.

Fresh toolchain archives are size/SHA-256 checked against official release metadata before publication. Reused toolchains are checked for completion markers, version metadata and executable presence; they are not rehashed or independently authenticated on each use. A local user who can modify the toolchain or cache directories is inside the trust boundary.

The initial target is GitHub-hosted runners. On self-hosted runners, the operator owns directory permissions, cleanup, disk quotas, process isolation and separation between repositories or trust levels. `RUNNER_TEMP`, `RUNNER_TOOL_CACHE`, `GOCACHE` and `GOMODCACHE` can persist according to runner configuration. New toolchains live under `RUNNER_TEMP/setup-go-better/toolchains`; the action does not manage a shared persistent installation service or repair ownership of existing trees.

Private staging and no-replace publication reduce accidental interference, but there is no cross-job installation lock or validated guarantee for shared/network filesystems and simultaneous owners. A competing completed installation can win; an incomplete existing destination fails instead of being replaced. Isolate jobs' temporary/toolchain/cache paths or serialize shared use. GitHub also documents the broader [persistence risks of self-hosted runners](https://docs.github.com/en/actions/reference/security/secure-use#hardening-for-self-hosted-runners).

## Cancellation and diagnostic data

The automatic post condition excludes cancelled jobs and saves failed jobs only with `cache-save-on-failure: true`. In-process cancellation asks workers to stop and joins them before normal cleanup; a blocking filesystem call may still finish first. Abrupt process termination can leave local staging/state or remote uncommitted blobs; cleanup is best effort and remote expiry belongs to the service. The [validation guide](DEVELOPMENT.md#validation) distinguishes local fixtures from unverified hosted policy cases.

Runtime bearer credentials authenticate cache control requests only; signed URLs authenticate blob transfers without a bearer header. Neither is written into private post state. Post state does contain configuration, cache paths, keys and content identities, and runs under the same runner account as other job processes. It is not a secret store or a boundary against malicious code in that job.

Ordinary logs, annotations, debug text and summaries sanitize controls and redact the current `ACTIONS_RUNTIME_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN` values and HTTP(S) URLs. This is defense in depth, not a guarantee for arbitrary secrets, transformed/encoded values or output from other tools. Debug logging includes full cache keys and selected toolchain paths. Summaries omit full keys/paths, escape text, and can be disabled with `summary: false`.

JSON report files and action outputs are machine data, not redacted presentation. Reports can contain paths, full keys and error text; installer `--json` includes the toolchain directory. Review them before publishing artifacts or issue attachments. Restrict their storage and retention as appropriate for the repository; never put credentials in scopes, paths or other inputs expecting masking to make them safe.
