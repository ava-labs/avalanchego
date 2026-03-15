Review this pull request. The PR branch is checked out in the current working directory.

Skim the PR description for intent. Comment on a gap between what it claims and what the diff does only when that gap is **obvious** (not slight wording drift) and it affects correctness, safety, or maintainability.

## Areas to look at

- **Correctness** — logic, errors and edge cases, invariants, protocol or upgrade-sensitive behavior.
- **Concurrency** — shared state, synchronization, goroutines, shutdown and cancellation.
- **API surface and PR scope** — exports and public contracts; whether unrelated changes crept in.
- **Tests** — coverage where behavior is risky or new; tests that reflect real outcomes, not implementation trivia.
- **Operations** — configuration, defaults, limits, logging/metrics when operators or deployments are affected.
- **Performance and resources** — cost or allocation pressure when the change touches hot paths or scale-sensitive code.
- **Security** — validation, trust boundaries, and abuse scenarios where the diff warrants it.

## How to leave feedback

- Prefer **inline comments** on the relevant lines when supported; each note should suggest an outcome or next step.
- For cross-cutting concerns, one comment that ties threads together can be clearer than repeating the same idea on every file.
- Avoid repeating what formatters, linters, or CI already cover unless something is objectively wrong.
- **Fewer, higher-signal** comments are better than commenting on every file—silence is acceptable when the change looks sound.
