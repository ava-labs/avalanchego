Continue the SAE Subnet-EVM port on branch `JonathanOppenheimer/subnetevm-sae-port` (reviving PR #5387: a stale spike of the subnet-evm L1 VM at `vms/saevm/subnetevm`, being brought up to date with master's SAE core). The full brief is in `SAE_PORT_INITIAL_PROMPT.md` at the repo root — consult it if PORT_STATUS.md leaves anything ambiguous.

Each iteration:
1. Confirm you are on `JonathanOppenheimer/subnetevm-sae-port`, then read `vms/saevm/subnetevm/PORT_STATUS.md` and `git log --oneline -15` to re-establish state. That file is the source of truth for milestone progress and the next action.
2. Verify the current milestone's gate honestly (build → lint → cchain tests → subnetevm tests → duplication audit + dedup → `task test-unit` + `task test-e2e-warp-sae`). Run the cheapest gate that could fail first. The dedup milestone means a systematic audit of cchain-vs-subnetevm duplication with chain-agnostic copies hoisted into shared `vms/saevm/` packages (warp is one confirmed instance, not the whole list) — the audit findings belong in PORT_STATUS.md.
3. Before implementing whatever you picked, gate it: "This sounds like a great idea — am I confident I can implement it with no unnecessary complexity, and is it highly likely to work?" If the honest answer to either is no, do not start implementing — spend this iteration researching instead (read the relevant master code, the spike's version, the pinning tests, prior art in cchain), write what you learned and the now-derisked plan into PORT_STATUS.md, and implement it next iteration. An iteration that produces only research and a sharper plan is a good iteration; a half-implemented guess is not.
4. Do the unit of work. Prefer finishing the in-progress milestone over starting the next. Re-express spike requirements against master's shapes rather than reverting master's code; keep cchain and subnetevm adapted together whenever a shared hook changes.
5. Commit, and update PORT_STATUS.md (state, decisions, blockers, exact next action) — assume the next iteration has no memory of this one.

Git rules (absolute):
- Commit regularly, and ONLY to `JonathanOppenheimer/subnetevm-sae-port`. Never commit to, reset, rebase, or otherwise modify any other branch — in particular `ceyonur/subnetevm-sae-spike-cleanup` and `master`.
- Bypass commit signing — Jonathan is not attending his signing key, so a sign-prompt would hang the session. Use `git commit --no-gpg-sign ...` (or ensure `git config --local commit.gpgsign false` is set).
- Do not push or open a PR.

Rules: keep the build green after every commit from milestone 2 onward; never weaken or delete a failing test to pass a gate — fix the code, or record in PORT_STATUS.md why the test's expectation is wrong.

Stop the loop when: all six milestones in PORT_STATUS.md are checked and the full gates (repo-wide `task test-unit` AND `task test-e2e-warp-sae` AND the pre-existing C-Chain e2e) have passed in a single final run — then write a PR-ready summary section into PORT_STATUS.md and stop. Also stop if genuinely blocked on a decision only Jonathan can make: record it under a "BLOCKED" heading in PORT_STATUS.md with the options and your recommendation, then stop.
