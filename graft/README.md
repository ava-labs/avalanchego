# Monorepo Grafting

This directory is intended to be the initial point of migration for
other git repos being added to this repo. Such 'grafts' do not have to
initially adhere to repo standards but will be expected to be
refactored where necessary to meet such standards as part of a
PR-based process of migrating to more permanent locations.

## Migrating a Repo

### Stacked Branches

As with with any changes to a codebase, minimizing review friction is essential. While it would be possible to require review of a huge PR commit-by-commit, any changes to those commits would need to be correlated with their originals and the friction would be
considerable. Instead, a [stacked branch](https://andrewlock.net/working-with-stacked-branches-in-git-part-1/) approach is suggested to enable effective review of a large migration in a piecemeal fashion:

- Create a branch per reviewable task
  - Subtree merge would be one task, import rewrite another, etc
  - The branch for a task subsequent to the initial task would be
     based on the previous task branch
- Create a PR per branch
  - The initial task would use the master branch as its base
  - A subsequent task would use the previous task's branch as its base
  - Mark each PR as draft to avoid premature merge
- Request review in order from the initial PR but do not merge yet
- Once all PRs in the series have been approved
  - Freeze development on the origin repo
  - Merge from the top down into the subtree merge PR
    - Avoids cascading rebases and merge conflicts
- Once only the subtree merge PR is left, manually merge the branch
  - Merging the PR with squash enabled would discard the history that
     we want to retain
- Close the subtree merge PR
- Archive the original repo

Tooling such as
[git-machete](https://github.com/VirtusLab/git-machete) or
[jujutsu](https://github.com/jj-vcs/jj) is suggested to simplify
maintaining the series of stacked branches.

### Suggested Procedure

The following do not represent an exhaustive list of tasks and are used for example purposes only. Regardless of the steps involved, the creation of an initial branch from master is assumed before the first step, and the commit of all changes and creation of a new branch from the current branch before beginning a subsequent step.

- [ ] Add tasks for subtree merge and import rewrite to graft/Taskfile.yml (as per the example of existing tasks)
  - These tasks are intended to simplify the repeated invocation that
     will be required when a repo is being developed in parallel with
     migration.
- [ ] Execute the subtree merge task (it will commit the result automatically)
- [ ] Remove files made redundant by the migration
  - Prioritizing file removal before modification minimizes the changes requiring review
- [ ] Execute the rewrite imports task (it will commit the result automatically)
- [ ] Perform required go module changes
- [ ] Migrate CI jobs (unit test, e2e, linting, etc)
- [ ] Get CI jobs passing

### Refreshing the graft PR

Development on the repository to be grafted may be ongoing while the graft PR (the result of the subtree merge task) is open. To refresh the graft PR, and get new changes from the repository to be grafted, do the following: 

```bash
git fetch origin

# The target branch might be master, or a tooling PR 
git reset --hard origin/[TARGET BRANCH]
git push --force

# Do the graft again
task [REPOSITORY NAME]-subtree-merge
```

### Refreshing the graft PR

Development on the repository to be grafted may be ongoing while the graft PR (the result of the subtree merge task) is open. To refresh the graft PR, and get new changes from the repository to be grafted, do the following:

```bash
git fetch origin

# The target branch might be master, or a tooling PR 
git reset --hard origin/[TARGET BRANCH]

cd graft

# Do the graft again
task [REPOSITORY NAME]-subtree-merge
git push --force
```

### Rebasing Inflight PRs

Provided a subtree merge was used to perform the graft, a common merge base will exist with which to rebase PRs targeted at the original git repo. This allows for repo migration and ongoing development to proceed in parallel. Once a graft has been finalized, outstanding PR branches from the original repo can be migrated as follows:

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
