# Code Review Guidelines

Apply these firewood-specific checks whenever reviewing code, suggesting changes, or
providing inline feedback — in addition to general best practices.

## Rust

- **Overflow arithmetic**: Do not leave bare arithmetic under
  `#[expect(clippy::arithmetic_side_effects)]`. Use the contextually correct explicit
  method: `checked_*` when overflow is possible and must be handled, `saturating_*` when
  clamping at the bound is the correct behavior, and — for provably-impossible overflow in
  **production** code — `debug_assert!(<invariant>)` paired with `wrapping_*` (zero release
  cost, and the assertion restores the dev/test overflow canary). Do not use
  `checked_*`/`saturating_*` for provably-safe operations; they add runtime cost for no
  benefit. Reserve a reasoned `#[expect(clippy::arithmetic_side_effects, reason = "...")]`
  for the rare expression where every rewrite hurts clarity. Test-only arithmetic already
  runs under the dev-profile overflow canary, so a reasoned `#[expect]` there is
  acceptable. See **Lint Suppression Policy** below.
- **`unwrap()`**: Hard reject outside `#[cfg(test)]` modules. Also reject
  `#[allow(clippy::unwrap_used)]` and `#[expect(clippy::unwrap_used)]` outside test
  modules.
- **Lint suppression**: No local `#[allow(...)]` / `#![allow(...)]`. Use a reasoned
  `#[expect(...)]` (or `#[cfg_attr(<cfg>, expect(...))]` for feature-gated lints),
  scoped as tightly as possible. See **Lint Suppression Policy** below for the full rules
  and per-lint guidance.
- **Error propagation (`?`)**: Ensure propagated errors carry enough context to diagnose
  the failure at the call site. Reject silently discarded errors and uses where the error
  should be handled locally rather than propagated upward.
- **Nibble range bounds**: A nibble-path lower bound uses the empty slice for
  "unbounded" — the empty slice already sorts as the minimum key. An upper bound must be
  `Option<&[u8]>` with `None` for unbounded (+∞); reusing the empty slice there judges
  every key out of range. When either bound is threaded through new code, check every
  comparison site. See `merkle::collapse::CollapseRange`.

## Lint Suppression Policy

Every suppression must be deliberate, minimally scoped, and self-documenting.

### Mechanism

- Prefer a **fix** that avoids the lint when it is local and low-risk (e.g. `.get()`
  instead of indexing; write the `# Errors`/`# Panics` doc; make a fn `const`).
- Otherwise use `#[expect(lint, reason = "why the code is correct / why the lint can't be
  satisfied")]`. The `reason` is the `reason = "..."` attribute field, not a `//` comment.
  A reason that merely restates the lint or counts occurrences is not a reason.
- Feature-conditional lints (fire only under some feature combinations) use
  `#[cfg_attr(<cfg where the lint fires>, expect(lint, reason = "..."))]`.
- **No local `#[allow]` / `#![allow]`.** Use a reasoned `#[expect]` instead. A
  workspace-level `allow` in `Cargo.toml` `[workspace.lints]` (a deliberate, documented,
  project-wide policy such as `cast_possible_truncation`) is a different thing and is
  permitted.

### Scope

- Start at the tightest scope (expression / statement / item).
- Elevate exactly **one** level when the *same lint* appears **3+** times in the wider
  scope: 3+ suppressible statements in a block → the block; 3+ in a function → the
  function/item.
- This ladder **stops before module level.** A module-level suppression (`#![expect(...)]`
  inside a `mod`, or `#[expect]` on a `mod`) carries a higher bar and **must not** sit on a
  module that has non-test submodules — push it down. Exceptions: a module whose only
  submodule is a `#[cfg(test)] mod tests` (or an all-test subtree) may keep a module-level
  suppression; and `ffi/src/lib.rs` keeps a single crate-root
  `#![expect(unsafe_code, ...)]` as a documented FFI-boundary carve-out.

### `unsafe_code` exemption

`#[expect(unsafe_code)]` needs **no** `reason`. For a per-block `unsafe { ... }` the
`// SAFETY:` comment (required by `undocumented_unsafe_blocks`) is the justification; for
`#[unsafe(no_mangle)]`/`extern "C"` functions and `unsafe impl` bridges the item's own
doc-comment is. An existing reason is fine but not required.

### Per-lint guidance

For lints we leave suppressible:

| Lint | Fix first | Suppress when | Reason states |
| --- | --- | --- | --- |
| `arithmetic_side_effects` | `checked_`/`saturating_` per context; provably-safe → `debug_assert!` + `wrapping_` | rewrite hurts clarity | why overflow can't occur / is handled |
| `indexing_slicing` | `.get()` / iterators (allowed in tests) | non-test, index provably in bounds, `.get()` adds noise | what bounds the index |
| `unwrap_used` | non-test → return `Result` (allowed in tests) | never outside tests | n/a in tests |
| `missing_errors_doc` / `missing_panics_doc` | write the `# Errors` / `# Panics` section | only the uniform `Error`, nothing method-specific | where conditions are documented |
| `cast_precision_loss` / `cast_sign_loss` | avoid the cast if feasible | intrinsic to f64 metric/progress math | why precision/sign loss is acceptable |
| `cast_possible_truncation` | — | — | handled globally (documented workspace `allow`) |
| `disallowed_methods` / `disallowed_types` (`split_at`, …) | use the recommended replacement | the panicking precondition is provably met | the in-bounds/length argument |
| `unsafe_code` | — | when unsafe is required | no reason (see exemption above) |
| `unused_variables` (feature-gated) | use the binding, or `_`-prefix | used only under some features | `cfg_attr(<cfg where unused>, expect(...))` |
| `too_many_lines` / `too_many_arguments` / `large_types_passed_by_value` / `type_complexity` / `dead_code` / … | split / borrow / delete where reasonable | the shape is intentional | why the shape is intended |

## General

- **Comments and messages**: Every comment, log message, and error string must be
  accurate, useful, and free of tautologies. Log and error messages must be unique enough
  to locate their source quickly.
- **Atomic operations**: Verify correct memory ordering (`Ordering`) for every load,
  store, and read-modify-write operation.
- **Locks**: Check for potential deadlocks, excessively long critical sections, and
  inappropriate `RwLock` vs `Mutex` choices given the actual access patterns.
- **Indexing and panic potential**: Prefer iterator methods over direct indexing —
  treat index access as a code smell worth scrutiny. Flag unchecked arithmetic and
  `unreachable!()` in paths that could actually be reached.
- **Anti-patterns**: Recommend modern idioms. Avoid `ToString::to_string` — prefer
  `to_owned()` for `&str → String` conversions.
- **Visibility**: Reject unnecessary `pub` exports. Prefer the tightest visibility
  (`pub(crate)`, `pub(super)`) that satisfies the interface requirements.
- **Tests**: New non-trivial code requires tests. Tests must be targeted (not covering
  too broad a scope), DRY, and cover edge cases. Scrutinize test names for clarity.
  Group similar tests together.

## FFI and Unsafe Code

When any unsafe Rust or Go code is present, or when a change crosses the FFI boundary:

- Analyze memory safety and soundness end-to-end across the boundary.
- Verify lifetime correctness for all values crossing the FFI boundary.
- Check correct use of `ManuallyDrop`, `Box::from_raw`, and pointer validity invariants.
- Assume all platforms are 64-bit. Defensive 32-bit guards are acceptable; proactive
  32-bit support changes are not needed.
- Auto-generated C-style comments in `ffi/firewood.h` may retain Rust doc style — do
  not flag these.
- Go handle types that hold a borrow on the `Database` must register a lease in the
  `keepAliveRegistry` via `lease.attach`. The registered drop function must be a method
  on the inner handle (or any type that does not back-reference the outer wrapper) so
  that `runtime.AddCleanup` can reclaim the wrapper when the user drops their last
  reference. See `ffi/keepalive.go` for the lock-ordering invariants and the
  closed-registry contract.

## Labeling

- `area/*` is auto-applied from changed paths and `kind/*` from the PR title.
- **`area/c-chain` is not auto-applied** — add it manually when the change
  touches C-Chain reexecution or libevm integration.
- Add `breaking-change` when any PR-template breaking-change box is checked.
