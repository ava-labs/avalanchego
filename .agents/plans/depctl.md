# depctl - tool to manage golang dependencies

## Overview

This repo - avalanchego - is the primary repo of a set of repos that really should be a monorepo but for licensing reasons that's not yet possible. So, we have scripting in each repo that simplifies reading/maintaining the version of avalanchego and cloning the repo at a given revision and go-mod-replacing avalanchego's dependency on these repos. I'd like to switch to a new model - add a golang tool (e.g. tools/depctl/main.go) that implements the functionality, and then have these other repos run this tool with configuration specific to their needs.

The tool will look like the following:

- Aliases
  - the repo target will be one of the following:
    - avalanchego
    - firewood
    - coreth
  - if no repo target is supplied, avalanchego will be used
  - each of the targets will expanded to the module path i.e. `github.com/ava-labs/[repo target]`
  - In the case of a clone operation, the repo path will be `https://[module path]`

- depctl get-version [--dep=[repo target]]
  - retrieves the version of the given dependency as per the example of scripts/versions.sh
- depctl update-version [version] [--dep=[repo target]]
  - updates the version of the given dependency as per the example of scripts/update_avalanchego_version.sh
    - the custom action paths should only be updated if the repo target is avalanchego
    - the workflow path should be all files in .github/workflows
    - the path to search for should be `ava-labs/avalanchego/.github/actions` to allow for use of any custom actions
- depctl clone [--path=[optional clone path, defaulting to the repo name (e.g. avalanchego, firewood, coreth)]] [--dep=[repo target]] [--version]
  - if no version provided, gets the version of the repo target from go.mod with the internal equivalent of `depctl get-version`
  - performs the equivalent of scripts/clone_avalanchego.sh but with a variable repo target
    - if repo doesn't exist, clones it. if exists, tries to update it to the specified version
  - if the repo target is avalanchego, and the go.mod module name is `github.com/ava-labs/coreth`
    - update go.mod to point to the local coreth source path as per the example of the last steps in scripts/clone_avalanchego.sh (with go mod replace && go mod tidy)
  - if the repo target is avalanchego, and the go.mod module name is `github.com/ava-labs/firewood/ffi`
    - update the avalanchego clone's go.mod to point to the firewood clone
      - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethhash=.
  - if the repo target is firewood or coreth and the go.mod module name is `github.com/ava-labs/avalanchego`
    - Perform a go.mod replace to to point the avalanchego chrome to the clone at ./firewood or ./coreth

## Use cases

### Testing firewood with avalanchego

 - from the root of a clone of the firewood repo
 - go tool -modfile=tools/go.mod depctl clone
   - needs to be in run in the ./ffi path of the firewood repo since that's where the go.mod is
   - clones or updates avalanchego at the version set in the tool modfile to ./ffi/avalanchego
   - updates the avalanchego clone's go.mod entry for github.com/ava-labs/firewood-go-ethhash/ffi to point to .
 - cd avalanchego
 - ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

### Testing avalanchego with firewood

 - from the root of a clone of the avalanchego repo
 - go tool depctl clone firewood --version=mytag
   - clones or updates firewood at version mytag ./firewood
   - updates the avalanchego clone's go.mod entry for firewood to point to ./firewood/ffi
     - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethash=./firewood/ffi
 - cd ./firewood/ffi && nix develop && cd ..
   - the nix shell will ensure the golang ffi libary is built and configured for use in the shell
 - ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

### Testing coreth with avalanchego and firewood

 - from the root of a clone of the coreth repo
 - go tool depctl clone
   - clones or updates avalanchego at the version set in go.mod to ./avalanchego
   - updates the avalanchego clone's go.mod entry for coreth to point to .
 - go tool depctl clone --dep=firewood --version=mytag
   - clones or updates firewood at version `mytag` to ./firewood
   - updates coreth's go.mod entry for firewood to point to ./firewood/ffi
     - e.g. go mod edit -replace github.com/ava-labs/firewood-go-ethash=./firewood/ffi
 - cd ./firewood/ffi && nix develop && cd ..
   - the nix shell will ensure the golang ffi libary is built and configured for use in the shell
 - cd avalanchego && ./scripts/run_task.sh [task]
   - e.g. ./scripts/run_task.sh test-unit

## Testing

 - While the primary consumer is intended to be other repos, automated
   validation of all behavior should be performed as unit tests. This
   suggests both unit tests of the internal behavior
   (i.e. reading/updating go.mod) and integration tests involving git
   cloning of repos. It probably makes sense to use shallow-clone to
   minimize the cost of such testing.
 - The `test-unit` task is probably a good task to target where task
   execution is required
 - Since changes to the filesystem should be avoided, the use of
   something like os.MkdirTemp is suggested, with cleanup optionally
   disabled to aid in troubleshooting
