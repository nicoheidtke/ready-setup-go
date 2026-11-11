# Cache lifecycle validation

These are correctness procedures for a future explicitly authorized hosted run. No workflow execution is implied by preparing them. All repository workflows remain manual-only. Local fixtures cannot establish GitHub cache access boundaries or the runner's evaluation of post conditions.

## Local coverage and native execution

`go test ./internal/cache` covers cancellation/worker joining, corrupt or missing post state, missing credentials, all runner access modes, read-only policy narrowing between main and post, and coordinated immutable-writer races. The archive race permits one winner without replacement. The chunk race requires one committed shared chunk plus two independently restorable manifests; an uncommitted reservation remains an error.

`TestPackagedLifecycle` consumes a prebuilt action and an already installed Go toolchain. It checks the actual root main/post, Node alias, Bash reuse, report/state boundaries, and distinct main/post summaries against a local cache-v2 fixture. Its environment inputs are documented in the test. The existing manual `ci.yml` native integration jobs now run it for archive and chunked storage on Linux/macOS/Windows, amd64/arm64, using the action's Node runtime. Cross-building is not native execution; these prepared jobs still need authorized runs.

## Controlled hosted probes

Use **Manual cache lifecycle validation** (`cache-validation.yml`) with a new, recorded `scope`. Keep the source revision, runner, exact Go version, dependency files, job identity, storage, layout, compression and concurrency settings fixed within each probe. The workflow builds that checked-out source locally; it does not prove that a separately published distribution contains it.

`probe.json` records the source revision, workflow/ref, job, run/attempt, settings and fresh/matched keys before the workload. `fresh_keys` names the entries the post step intends to save, not proof they were saved. Review the completed post logs and conclusion before calling them seed keys. Ordinary artifact steps precede post and cannot capture later save reports. Enable step debug before a probe if full post JSON is needed.

The `expect-hit` assertion requires every snapshot to restore with positive payload. `expect-miss` requires no matched keys and zero payload, so a partial or discarded restore cannot pass. `expected-match` optionally requires the exact comma-separated seed keys and a complete hit. Assertion failures preserve the observation artifact for investigation. Expected misses need a fresh scope; unrelated compatible snapshots otherwise confound a negative result.

| Probe | Seed / trigger | Subsequent assertion and evidence |
| --- | --- | --- |
| Cold and warm | Default `mode: restore-save`, `outcome: success`; inspect actual successful post-save | Same settings/scope, `mode: restore`, `expect-hit: true`, and `expected-match` equal to reviewed seed `fresh_keys` |
| Restore-only | Fresh scope, `mode: restore`, `outcome: success` | No save work or snapshot; the runner may invoke a no-op post launcher without saved state. Repeat `mode: restore`, `expect-miss: true` |
| Failed job, default policy | Fresh scope, `outcome: failure`, `save-on-failure: false`; expected job conclusion is failure | No save post should execute; repeat `mode: restore`, `expect-miss: true` |
| Failed job, opted-in save | Fresh scope, `outcome: failure`, `save-on-failure: true`; expected job conclusion is failure | Inspect successful post-save, then repeat restore with `expect-hit: true` and exact reviewed seed keys |
| Cancelled job | Fresh scope, `outcome: cancel-wait`; manually cancel only after the workload reaches its wait step; repeat for both save-on-failure values | Require actual cancelled conclusion and absence of save-post execution; subsequent restore-only must be a clean miss. The wait expires after ten minutes with an explicit inconclusive failure; expiry is not a cancellation pass |
| Concurrent snapshots | Manually start two otherwise identical success probes sharing a new scope, preferably chunked | Both invocations must have distinct fresh keys. Review both post results; a conflict alone is not a committed chunk. A later restore must match one reviewed completed seed and validate its workload. Contention is timing-dependent; local coordinated fixtures prove the forced race separately |

Do not label an intentionally failed policy probe as a passing workflow. Record its expected failure and independently inspect post behavior. A later collector job reports coarse completed-step times; it does not turn missing post metrics into a successful save.

## Branch and cache access boundaries

GitHub documents current/default-branch lookup and isolation from sibling/child branches in its [dependency caching reference](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching). This action deliberately leaves branch suffixes out of its compatibility namespace and sends lookup prefixes to the cache service; the service and its scoped token enforce visibility. Do not emulate that service boundary in a unit test and report it as hosted verification.

For a default-branch fallback probe, first seed a unique scope on the repository's default branch. On a pre-existing test branch carrying the same implementation, dispatch the same workflow/job with identical settings and dependency fingerprints, use restore-only, and require the exact reviewed default-branch seed keys. Confirm that no current-branch seed exists in that scope. Record the source revisions of both runs and keep changes unrelated to the cache fingerprint out of the comparison.

For negative isolation, seed a separate unique scope only on branch A, with no default-branch entries. Restore-only from sibling branch B and from the default branch must each be clean misses. A tag-scope case needs its own explicitly authorized tag and seed. These procedures require separately authorized branch/tag setup when those refs do not already exist; this implementation did not create them.

The runner exposes effective cache access through `ACTIONS_CACHE_MODE`; local tests verify `none`, `read`, `write-only`, and normal writable handling, including a narrowed post policy. Actual token enforcement requires future jobs with the corresponding workflow/job `cache-mode` settings. Do not substitute an environment variable override and call it a token-permission test. No extra GitHub token permission or policy bypass is added here.

Fork and pull-request merge-ref behavior remains a separate gate. A manual dispatch is not equivalent to an untrusted pull-request token or merge ref. Testing that case requires an explicitly authorized workflow/event arrangement; automatic PR triggers remain disabled. Never introduce `pull_request_target` or grant broader cache access merely to make a probe succeed.

## Record actual results

Retain immutable source/distribution revisions, runner image and architecture, Go version, every input, source/event/ref context, seed and matched keys, job/post conclusions, warnings, report artifacts and raw logs. List unavailable runners and incomplete cases. Hosted presentation (groups/colors/progress/debug/summaries), service compatibility, native platform behavior and performance are distinct claims; a correctness pass establishes no speedup.
