ultracode

# Mission: revive and land the SAE Subnet-EVM (L1) VM

We are resurrecting PR #5387 ("SAE Subnet-EVM spike", branch `ceyonur/subnetevm-sae-spike-cleanup`) — a months-old spike that ported subnet-evm onto the SAE architecture (`vms/saevm`). The spike is badly stale: it diverged from master at `35f36b0e5c` (2026-05-12) and master has ~194 commits since, ~4,700 lines of churn in the shared SAE core alone. A test merge of `origin/master` into the branch conflicts in 61 files.

There are three deliverables, all mandatory:

1. **A working SAE Subnet-EVM L1 VM** (`vms/saevm/subnetevm`) built against *current* master's SAE core — unit tests pass and the warp e2e suite (`task test-e2e-warp-sae`) passes.
2. **The existing C-Chain SAE VM (`vms/saevm/cchain`) stays fully functional** — its behavior on master is the reference; all its existing tests must still pass. Changing shared code (hook signatures etc.) to accommodate subnetevm is expected and allowed, but cchain must be adapted in the same change, never left broken.
3. **Deduplicated shared code.** The spike was built by copying the cchain harness wholesale and editing, so duplication is pervasive, not incidental. Once the port stabilizes, do a **systematic duplication audit** across `vms/saevm/cchain` and `vms/saevm/subnetevm` (and their support packages), and hoist chain-agnostic code into shared packages under `vms/saevm/`. Warp is one *confirmed* instance — on the spike branch, `vms/saevm/cchain/warp/{storage,predicates,precompile_accept}.go` and the subnetevm copies differ by <10 lines each; there must end up being exactly ONE shared SAE warp implementation (suggested home: `vms/saevm/warp`), with chain-specific behavior (e.g. subnet-evm's validator-uptime verification in `subnetevm/warp/verifier.go` and `warp/messages`) as a small extension point. But warp is an example, not the list: audit the state packages, config plumbing, block-builder scaffolding in the `hook/` subpackages, plugin/VM wiring, and API glue for the same copy-and-tweak pattern, and record the audit findings (hoisted, kept-separate, and why) in PORT_STATUS.md. The bar for hoisting: literal/near-literal copies, or code whose differences reduce cleanly to an extension point. Do NOT invent speculative abstractions for code that merely looks similar — genuinely chain-specific logic stays in the chain packages.

## Setup

- `git fetch origin master ceyonur/subnetevm-sae-spike-cleanup`
- Create branch `JonathanOppenheimer/subnetevm-sae-port` off `origin/ceyonur/subnetevm-sae-spike-cleanup`.
- This repo is a Go workspace (`go.work`) with four modules: root, `graft/coreth`, `graft/evm`, `graft/subnet-evm`. "It builds" means `go build ./...` in all four.

## Git rules (absolute)

- **All work happens on `JonathanOppenheimer/subnetevm-sae-port` and nowhere else.** Never commit to, reset, rebase, or otherwise modify any other branch — in particular `ceyonur/subnetevm-sae-spike-cleanup` and `master`. Verify you are on the right branch before every commit.
- **Commit regularly** — at every milestone and at any coherent intermediate checkpoint. Frequent small commits beat rare large ones; they are the resume points for future sessions.
- **Bypass commit signing**: Jonathan will not be attending his signing key, so sign-prompts would hang the session. Commit with `git commit --no-gpg-sign ...` (or set `git config --local commit.gpgsign false` once at the start).
- Do not push or open a PR; this stays local until Jonathan reviews.

## Read these BEFORE touching anything

- `vms/saevm/subnetevm/README.md` and `vms/saevm/subnetevm/PORT_CONTEXT.md` (on the spike branch) — the original author's design handover: porting model, feature map, differences vs `cchain` and legacy `graft/subnet-evm`, and known TODOs. These are your spec for what the L1 VM must do.
- `vms/saevm/README.md` and `vms/saevm/hook/hook.go` **as they exist on `origin/master`** — this is the current SAE core you must target.
- `graft/README.md` — rules for the grafted (vendored-with-history) coreth/subnet-evm/evm trees. Keep deltas to grafted code minimal and deliberate.

## Strategy: one merge, with an explicit resolution policy

Do a single `git merge origin/master` (do NOT attempt to rebase the branch's 553 commits). Resolve with this policy:

- **Master wins wholesale** for the shared SAE core and everything that exists on master: `vms/saevm/{sae,hook,blocks,saexec,txgossip,gasprice,gastime,worstcase,params,types,saetest,saedb,adaptor,cmputils}`, `vms/saevm/cchain/**` (including `cchain/warp`, which master restructured after the spike copied its old shape), `vms/transitionvm/**`, `graft/coreth/**`, `node/node.go`, `.github/workflows/ci.yml`, `tests/e2e/c/**`, go.mod/go.sum/MODULE.bazel/lock files (regenerate rather than hand-merge). The spike's edits to these files existed only to serve subnetevm and were written against a 3-month-old core — treat them as *requirements to re-express*, not diffs to preserve. After the merge commit, mine the spike's shared-core deltas (`git diff 35f36b0e5c..origin/ceyonur/subnetevm-sae-spike-cleanup -- vms/saevm/hook vms/saevm/sae vms/saevm/blocks vms/saevm/saexec vms/saevm/worstcase`) and deliberately re-apply what subnetevm still needs against master's shapes.
- **Branch wins** for everything master doesn't have or didn't change: `vms/saevm/subnetevm/**`, `vms/saevm/intmath`, the spike's `graft/subnet-evm` feature work (feemanager retirement, customtypes SAE header extras, params/extras changes), `scripts/build_subnet_evm_sae.sh`, the new Taskfile targets (`build-subnet-evm-sae`, `test-e2e-warp-sae`, `test-e2e-warp-sae-ci`), and the spike's `tests/e2e` and `tests/fixture` additions — then fix all of it to compile and behave against master's core.

### The hard part: hook reconciliation

The `vms/saevm/hook.Points` interface diverged on BOTH sides. Master now has `SettledBy(*types.Header) Settled`, `VerifyBlockSyntax`, a `StartExecutingBlock`/`FinishExecutingBlock`/`AfterExecutingBlock` split, and an error-less `GasConfigAfter`. The spike instead added `ExecutionArtifact`, `GasConfigAt(header, state) (…, err)`, `RequiresTransactionAdmissionCheck`, `SettledHeight`, `BeforeExecutingBlock`, and `BlockBuilder.FinalizeHeader`. **Master's interface is the base.** Re-express the spike's needs (gaspricemanager runtime artifact, allowlist admission checks, reward-manager fee routing, settled-state reads) as minimal extensions to master's shape, and update `cchain`, `hookstest`, and `subnetevm` together. Study how master's cchain implements the current hooks first; follow those idioms.

## Milestones (commit after each; keep the build green from milestone 2 onward)

1. **Merge committed** — conflicts resolved per the policy above (`go build` may still fail).
2. **Workspace compiles** — `go build ./...` green in all four modules; `task lint` passes.
3. **cchain green** — all `vms/saevm/...` (excluding subnetevm) and `vms/transitionvm` unit tests pass, proving the shared core + cchain match master's behavior plus any hook extensions.
4. **subnetevm green** — all `vms/saevm/subnetevm/...` unit tests pass (the `vm_*_test.go` files are the feature spec: warp, validators, allowlists, nativeminter, rewardmanager, gaspricemanager, feemanager retirement, eth extras).
5. **Shared code deduplicated** — duplication audit completed and documented in PORT_STATUS.md; chain-agnostic copies hoisted into shared `vms/saevm/` packages (warp among them: one shared implementation used by both VMs); all tests still green.
6. **Full validation** — `task test-unit` passes repo-wide; `task test-e2e-warp-sae` passes; the pre-existing C-Chain e2e suite still passes.

## Working practices

- Maintain `vms/saevm/subnetevm/PORT_STATUS.md` (committed): milestone checklist with current state, what was decided and why (especially hook-reconciliation decisions), current blockers, and the exact next action. Update it every time you commit. This file is how future sessions resume — write it for a reader with zero context from this session.
- Never leave the tree in a state where `git status` + PORT_STATUS.md can't tell a fresh session exactly where things stand.
- Follow repo conventions in CLAUDE.local.md (error wrapping, require-with-message, table-driven tests, etc.) for all new/rewritten code.
- Validation-first: before rewiring each subnetevm feature, identify the existing test that pins it and make that test the target.
- Use workflows/subagents for fan-out (per-package conflict resolution, test triage, the duplication audit, adversarial review of the dedup work), but keep the hook-interface reconciliation itself in one context — it's the load-bearing design decision and can't be sharded.

Start by reading the four documents listed above, then write your reconciliation plan for the hook interface into PORT_STATUS.md before resolving any conflicts.
