# Monorepo Grafts

This directory is intended to be the initial point of migration for
other git repos being added to this repo. Such 'grafts' are unlikely
to initially adhere to repo standards but will be expected to be
refactored where necessary to meet such standards as part of a
PR-based process of migrating to more permanent locations.

## Suggested Procedure

 - [ ] Add task for subtree merge and import rewrite to the taskfile
 - [ ] Execute the subtree merge task
 - [ ] Remove files made redundant by the migration
   - Prioritizing file removal before modification minimizes churn
 -


### Rebasing Inflight PRs

Provided a subtree merge was used to perform the graft, a common merge
base will exist with which to rebase PRs targeted at the original git
repo. This allows for repo migration and ongoing development to
proceed in parallel. Once a graft has been finalized, outstanding PR
branches from the original repo can be migrated as follows:

```bash
# Fetch the PR branch from the standalone repo's remote
git fetch REMOTE PR_BRANCH_NAME

# Create a local branch from the PR branch.
# Don't track since the new branch is intended to target the new repo.
git checkout -b PR_BRANCH_NAME REMOTE/PR_BRANCH_NAME --no-track

# Rebase onto the target branch in this repo, treating graft/REPO as the subtree root.
# Conflict resolution may be required.
git rebase -Xsubtree=graft/REPO --onto origin/master REMOTE/master

# Push the new PR branch
git push -u origin PR_BRANCH_NAME
```
