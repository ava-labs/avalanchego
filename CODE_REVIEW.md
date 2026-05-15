# Code Review Guidelines

Apply these firewood-specific checks whenever reviewing code, suggesting changes, or
providing inline feedback — in addition to general best practices.

## Rust

- **Overflow arithmetic**: Reject `saturating_*` or `checked_*` for operations that are
  provably impossible to overflow — they add runtime cost for no benefit. Scrutinize
  `wrapping_*` for operations where wrapping would silently produce incorrect results.
- **`unwrap()`**: Hard reject outside `#[cfg(test)]` modules. Also reject
  `#[allow(clippy::unwrap_used)]` and `#[expect(clippy::unwrap_used)]` outside test
  modules.
- **Lint suppression**: Prefer `#[expect(...)]` over `#[allow(...)]`. For lints that
  only apply under specific build features, use
  `#[cfg_attr(feature = "...", expect(...))]`. Any surviving `#[allow]` must include an
  inline comment explaining why `#[expect]` is insufficient.
- **Error propagation (`?`)**: Ensure propagated errors carry enough context to diagnose
  the failure at the call site. Reject silently discarded errors and uses where the error
  should be handled locally rather than propagated upward.

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
