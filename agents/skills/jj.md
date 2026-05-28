---
name: jj
description: Use Jujutsu (jj) for version control on repos where the contributor works with jj as the front-end over git. Triggers on commit, branch, status, history, rebase, or PR-conflict-resolution requests in such repos. Covers the jj-vs-git command mapping and the preferred merge-commit flow for resolving conflicts on PR branches.
---

# jj (Jujutsu)

When the user works in a jj-managed repo, drive all VCS operations through `jj`, not raw `git`. Using `git` directly desyncs jj's view of the working copy and creates confusing parallel state.

Throughout this document, `<default-branch>` is a placeholder for the repository's default branch — `main`, `master`, `develop`, or whatever the repo uses. Substitute the actual name when running the commands.

## Detecting a jj repo

Run `jj status` (or check for a `.jj/` directory at the repo root). If it succeeds, the repo is jj-managed and this skill applies. If it errors with "no jj repo", fall back to git.

## Core command mapping

| Task | jj | Notes |
| --- | --- | --- |
| Show working copy state | `jj status` | Not `git status`. |
| Set/edit current commit message | `jj describe -m "..."` | The working copy **is** a commit; there is no separate "commit" step. |
| Finalize working copy and start a new one | `jj commit -m "..."` | Shorthand for `jj describe -m "..."` followed by `jj new`. Prefer this when you're done with the current change and want a fresh empty working copy on top — one command, one permission prompt. |
| Create a new empty commit on top | `jj new -m "..."` | Use this when starting fresh work without describing `@` first. |
| Create a branch | `jj bookmark create <name>` | jj calls branches "bookmarks". When the user says "branch," they mean bookmark. |
| Move a bookmark to current commit | `jj bookmark move <name>` | Defaults to `--to @` (the working copy). |
| List history | `jj log` | Default view is concise; use `-r ::` for full graph. |
| Show commits on a branch ahead of the default branch | `jj log -r '<default-branch>..<branch>'` | Quote the revset — `..` confuses some shells. |
| Show a commit's contents | `jj show --git <rev>` | Pass `--git` for git-style diffs (familiar format). |
| Diff working copy or revisions | `jj diff --git` | Always pass `--git` — produces standard git-style diffs instead of jj's native format. |
| Diff a whole branch vs. the default branch | `jj diff --git --from 'fork_point(<default-branch> \| <branch>)' --to <branch>` | See note below — `-r '<default-branch>..<branch>'` does **not** work, and `--from <default-branch>` is usually wrong. |
| Read a file at a revision | `jj file show -r <rev> <path>` | **Not** `git show <rev>:<path>`. |
| Fold working-copy edits into a parent commit | `jj squash --into <change-id-or-@->` | Useful when iterating on a not-yet-pushed change. `@-` = parent of working copy. |
| Target a specific commit for describe/squash | `<change-id>` or positional ref like `@-` | Prefer `@-` over the change-id when targeting "the commit you just squashed into" — it survives rewrites and stays unambiguous. |
| Push a bookmark | `jj git push --bookmark <name>` | If GPG signing times out, just retry the same command — the retry surfaces the unlock dialog. |
| Fetch | `jj git fetch` | Prefer this over raw `git fetch`. |
| Create a PR | `gh pr create --head <bookmark> --base <base>` | jj leaves git's `HEAD` detached, so `gh pr create` can't infer the branch — always pass `--head` and `--base` explicitly. |

Only fall back to raw `git` when jj genuinely lacks the feature — this is rare.

### Diffing a branch against the default branch

Three traps to avoid:

- **`jj diff -r '<default-branch>..<branch>'` errors out** with `Cannot diff revsets with gaps in`. `jj diff -r` resolves to a *single* revision, not a range. (`jj log -r '<default-branch>..<branch>'` works because `jj log` accepts revsets; `jj diff` does not.)
- **`jj diff --from <default-branch> --to <branch>`** includes every commit that has landed on the default branch since the branch forked — so the diff shows your branch's changes **inverted** alongside the unrelated new default-branch work. Almost never what you want.
- **The right invocation** anchors `--from` at the actual fork point:

  ```bash
  jj diff --git --from 'fork_point(<default-branch> | <branch>)' --to <branch>
  jj diff --git --from 'fork_point(<default-branch> | <branch>)' --to <branch> --stat   # file-level summary
  ```

  `fork_point()` takes a **single revset** (a set of commits, expressed with `|`) and returns their common ancestor — equivalent to `git merge-base <default-branch> <branch>`. Note the `|` (set union), not a comma: `fork_point(<default-branch>, <branch>)` errors with "Expected 1 arguments". This gives you exactly the changes the branch introduces, regardless of how far the default branch has moved.

When the branch is the working copy, `<branch>` can be `@`.

## Commit message format

- **First line: Conventional Commits, where the repo enforces them.** Use `<type>(<scope>): <subject>` (e.g., `fix(storage): correct off-by-one in free list`). Allowed types are typically `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `style`, `test`. Some projects enforce this on PR titles via CI, and the PR title is derived from the first commit message — so get it right at `jj describe` time. Repos that do not enforce Conventional Commits may use a freer style; check recent `jj log` history before picking a format.
- **Body: follow the project's PR template if one exists.** Check `.github/pull_request_template.md` (and `.github/PULL_REQUEST_TEMPLATE/` for multi-template repos). If present, structure the commit body to match its sections (commonly: *Why this should be merged*, *How this works*, *How this was tested*, *Breaking Changes*). Not every commit needs every section — match the scope of the change.
- If the repo has a `CONTRIBUTING.md` or `CLAUDE.md` with stricter rules, those win over the defaults above.

## Stacking changes: new commit vs. amend the working copy

In jj, the working copy is itself a commit, so edits flow into `@` by default — effectively amending. Decide deliberately whether to stay on `@` or start a new commit on top with `jj new -m "..."`.

**Start a new commit (`jj new`) when:**

- Splitting work into separate reviewable items (one logical change per commit/PR).
- Addressing reviewer comments on an open PR — keep the response visible as its own commit so reviewers can see exactly what changed since their last pass.
- The PR is already in review and you want a follow-on commit for incremental review, rather than rewriting history reviewers have already seen.

**Amend the working copy (stay on `@`, just edit and `jj describe`) when:**

- Iterating on a not-yet-pushed change.
- Fixing up your own work before any reviewer has looked at it.
- The change is a trivial continuation of the current commit's intent.

**If it's not obvious which applies, ask the user.** Don't silently fold reviewer-comment fixes into an existing commit, and don't fragment a single coherent change into multiple commits without checking.

## Conflict markers

jj uses a richer conflict-marker format than git. In a conflicted file you may see:

```text
<<<<<<< Conflict 1 of 1
%%%%%%% Changes from base to side #1
-old line
+side-1 line
+++++++ Contents of side #2
side-2 line
>>>>>>> end
```

- `%%%%%%%` block: a *diff* from the merge base to one side.
- `+++++++` block: the *full content* of the other side.
- Resolve by editing the file to the desired final state and removing all markers.

List unresolved conflicts:

```bash
jj resolve --list
```

Take one side wholesale (faster than hand-editing markers):

```bash
jj resolve --tool :ours   <path> [<path>...]   # keep our side (the PR branch in a merge)
jj resolve --tool :theirs <path> [<path>...]   # keep their side (e.g., the default branch)
```

For a manual edit, just open the file, fix the markers to the desired final state, and save. After resolution, no `git add` is needed — jj sees it automatically.

## Resolving conflicts on a PR branch: rebase vs. merge commit

The choice mirrors the stacking-changes rule: it depends on whether reviewers have already seen the branch.

**Pre-review (no reviewer has looked yet):** rebase onto the default branch and force-push. History is still yours to rewrite.

```bash
jj git fetch
jj rebase -d <default-branch>
# resolve any conflicts (see Conflict markers section)
jj git push --bookmark <branch> --allow-new   # force-push as needed
```

**Post-review (the PR is in review or has comments):** use a **merge commit via jj** — not a rebase, not a squash. An explicit merge preserves the history reviewers have already seen and records that the default branch was integrated, with a description documenting the resolution.

If it's not clear which phase the PR is in, ask.

Merge-commit steps:

1. **Fetch from the remote:**

   ```bash
   jj git fetch
   ```

2. **Create the merge commit** with two parents (the PR branch and the default branch):

   ```bash
   jj new <branch> <default-branch> -m "merge(<scope>): merge <default-branch> into <branch>"
   ```

   ⚠️ **Parent order matters.** It must be `jj new <branch> <default-branch>`, *not* the other way around. The first parent becomes the "main line" of the merge; if the default branch is listed first, GitHub renders the PR as if the branch were merging *into* itself from the default branch and the diff/PR view gets very confused. Always put the PR branch first, the default branch second.

3. **Resolve conflicts** by editing the marked files. See the conflict-marker section above.

4. **Describe the merge commit** with a message that explains *what had to change during the merge* — which conflict, which side won, and why. The user specifically asks for this, so don't skip it:

   ```bash
   jj describe -m "merge(<scope>): merge <default-branch> into <branch>

   <explanation of conflict and resolution>"
   ```

5. **Keep the merge commit narrow.** Only two kinds of content belong in it:
   - Conflict resolution.
   - Small review-cleanup edits in direct response to comments on this PR — mention them in the description.

   **Do not** bundle unrelated fixes into the merge commit, even when the merge from the default branch is what made them visible. Two common traps:
   - CI was failing on the PR before the merge; merging in the default branch (e.g. a workflow update) lets the actual test run and surface the failure. The fix is *for a pre-existing PR bug*, not for the merge.
   - A lint/doc warning was already on the PR but masked by other red CI; the merge unblocks CI and the warning becomes visible.

   In both cases the merge is the *messenger*. Putting the fix in the merge commit makes a reviewer read it as "the merge introduced this bug" — exactly the opposite of what happened. Land those fixes as their own commits on top of the merge so blame/bisect points at the real cause.

   If you've already made the mistake (working copy was positioned at the merge commit when you started editing), un-bundle with `jj squash --from <merge> --into <new-commit> <paths>` (see *Splitting a commit non-interactively* below).

6. **Move the bookmark, don't squash:**

   ```bash
   jj bookmark move <branch>
   ```

   Preserve the merge commit as a real merge — squashing or rebasing erases the "default branch was integrated here" signal.

7. **Push:**

   ```bash
   jj git push --bookmark <branch>
   ```

8. **Do not** mark the PR ready-for-review automatically. The user reviews manually first.

9. **Create a new revision.** Run `jj new` after pushing to prevent additional changes on already-pushed GitHub revisions. All pushes should be considered immutable.

10. **Addressing comments.** Offer to respond to each comment. Start the response with `Fixed in <sha>.` if applicable. If it isn't obvious from the change, explain how it was fixed, keeping the response terse. The reviewer can always read the commit comment for more details. An example of an obvious change is if the reviewer says "remove this pub(crate)" and that's what we did — no need to add more details.

### Turning an existing commit into a merge commit

If `@` already has the changes you want and you just need to add a second parent (e.g., bringing in `<default-branch>` after you've already started editing), don't create a new merge commit on top — rebase `@` to gain the extra parent:

```bash
jj rebase -r @ -d @- -d <default-branch>
```

**Use repeated `-d` flags**, one per parent. `-d 'all:(p1 | p2)'` is **not** valid (`all:` is a revset prefix, not a `-d` modifier) and `-d 'p1|p2'` resolves to a single ambiguous revision.

After the rebase, describe the commit, move the bookmark, and push as in the merge-commit steps above.

## Rebasing a branch with stale `merge: merge <default-branch>` commits

A long-lived branch often carries one or more old `merge: merge <default-branch> into <branch>` commits accumulated from prior catch-ups. When you `jj rebase -b <branch> -d <default-branch>`, jj rebases each of those merge commits forward — but they're now **empty** (the default branch has moved past whatever they previously merged), and they clutter `jj log` for no benefit.

The cleanup pattern, after rebase:

```bash
jj rebase -b <branch> -d <default-branch>
jj log -r '<default-branch>..<branch>'                # spot the (empty) merges
jj abandon <empty-merge-1> <empty-merge-2> ...        # change-IDs, space-separated
jj bookmark set <branch> --to <real-tip-change-id>    # re-anchor; the abandon may have deleted the bookmark
jj log -r '<default-branch>..<branch>'                # confirm clean linear history
```

`jj abandon` will delete the bookmark if it pointed at an abandoned commit; `jj bookmark set --to <change>` puts it back on the real tip. The hint after abandon (`Deleted bookmarks can be pushed by name…`) is a reminder that the bookmark needs re-anchoring before pushing.

## Splitting a commit non-interactively

`jj split` opens a diff editor and needs a TTY — it will fail with `Device not configured` in a non-interactive shell. To reshape commits non-interactively, use `jj squash` to move changes between commits instead:

```bash
# Create the target commits first (empty, on top of the source)
jj new <src>     -m "fix(a): ..."   # creates child commit A on top of <src>
jj new           -m "fix(b): ..."   # creates child commit B on top of A

# Move specific paths from the source into each target.
# --keep-emptied preserves the source even if it becomes empty (important
# for merge commits — you want the merge itself to stay).
jj squash --keep-emptied --from <src> --into <commit-a> path/to/file_a.rs
jj squash --keep-emptied --from <src> --into <commit-b> path/to/file_b.rs other/file.rs
```

After moving the changes out, the original commit retains only the structural meaning (a clean merge, an empty placeholder, etc.) while the logical changes live in their own reviewable commits.

If `jj describe`-ing the source still references work that has moved out, update its message too — `jj describe <src> -m "..."` rewrites it and rebases descendants automatically.

## What not to do

- Don't run `git commit`, `git checkout -b`, `git branch`, `git rebase`, or `git merge` in a jj repo — use the jj equivalents.
- Don't squash or rebase a PR branch that is already in review; use the merge-commit flow. (Pre-review, rebase is fine.)
- Don't use `git show <rev>:<path>` to read a file at a revision; use `jj file show -r <rev> <path>`.
- Don't auto-mark PRs ready-for-review after pushing a merge resolution.
- Don't leave empty `merge: merge <default-branch> into <branch>` commits sitting on a rebased branch — abandon them and re-anchor the bookmark. They confuse `jj log` and serve no purpose post-rebase.
