## Goal

Migrate the [firewood repo](https://github.com/ava-labs/firewood/) to the [avalanchego repo](https://github.com/ava-labs/avalanchego/) and have its CI take no longer than CI currently takes in the firewood repo.

The goal is ensuring CI runtime for a firewood PR stays roughly the same in avalanchego as in the firewood repo.

## Tasks

* Migrate firewood’s https://github.com/ava-labs/firewood/tree/main/.github path into
 https://github.com/ava-labs/avalanchego/tree/master/.github
  * Reproduce the existing CI jobs
  * For each file (maybe even at the job granularity)
    * Detail / document what was done (migrated/deleted/etc) and why
    * Simplify the job of the firewood team in reviewing the migration
  * Where possible, try to ensure that jobs only run when the /firewood path is modified
* Minimize execution of avalanchego CI jobs when only firewood/ path is modified
