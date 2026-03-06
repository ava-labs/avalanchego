// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Replay layer metric definitions.

/// Time spent in propose (ns).
pub const PROPOSE_NS: &str = "replay.propose_ns";

/// Number of propose calls.
pub const PROPOSE_COUNT: &str = "replay.propose";

/// Time spent in commit (ns).
pub const COMMIT_NS: &str = "replay.commit_ns";

/// Number of commit calls.
pub const COMMIT_COUNT: &str = "replay.commit";

/// Registers all replay metric descriptions.
pub fn register() {
    use metrics::describe_counter;

    describe_counter!(PROPOSE_NS, "Time spent in propose (ns)");
    describe_counter!(PROPOSE_COUNT, "Number of propose calls");
    describe_counter!(COMMIT_NS, "Time spent in commit (ns)");
    describe_counter!(COMMIT_COUNT, "Number of commit calls");
}
