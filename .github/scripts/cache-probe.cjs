'use strict';
// Assertion-only helper for the manual validation workflow; never calls GitHub.
const fs = require('node:fs');

function observation(env) {
  return {
    repository: env.GITHUB_REPOSITORY, revision: env.GITHUB_SHA,
    workflow_ref: env.GITHUB_WORKFLOW_REF, job: env.GITHUB_JOB,
    ref: env.GITHUB_REF, run_id: env.GITHUB_RUN_ID, attempt: env.GITHUB_RUN_ATTEMPT,
    mode: env.PROBE_MODE, storage: env.PROBE_STORAGE, scope: env.PROBE_SCOPE,
    requested_outcome: env.PROBE_OUTCOME, save_on_failure: env.PROBE_SAVE_FAILURE,
    hit: env.CACHE_HIT, restored_bytes: env.CACHE_BYTES,
    fresh_keys: env.CACHE_KEY, matched_keys: env.MATCHED_KEY,
  };
}

function probe(env) {
  const result = observation(env);
  if (env.EXPECT_HIT === 'true' && env.EXPECT_MISS === 'true') throw new Error('Hit and miss assertions are mutually exclusive.');
  if (env.EXPECT_HIT === 'true' && (result.hit !== 'true' || !(Number(result.restored_bytes) > 0))) throw new Error('Expected every snapshot to restore with a positive payload.');
  if (env.EXPECT_MISS === 'true' && (result.hit !== 'false' || result.matched_keys !== '' || Number(result.restored_bytes) !== 0)) throw new Error('Expected a clean miss with no matched keys or transferred payload.');
  if (env.EXPECTED_MATCH && (result.hit !== 'true' || result.matched_keys !== env.EXPECTED_MATCH)) throw new Error('Restored keys do not equal the reviewed seed keys.');
  return result;
}

if (require.main === module) {
  try {
    const result = observation(process.env);
    fs.writeFileSync(process.argv[2], `${JSON.stringify(result, null, 2)}\n`);
    probe(process.env);
  } catch (error) {
    // Only fixed assertion messages are produced above, never raw input data.
    console.error(error.message);
    process.exitCode = 1;
  }
}
module.exports = { probe };
