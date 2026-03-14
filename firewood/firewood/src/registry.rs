// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Firewood layer metric definitions.

use metrics::{describe_counter, describe_gauge};

/// Number of proposals created.
pub const PROPOSALS: &str = "proposals";

/// Number of proposals created by base type.
pub const PROPOSALS_CREATED: &str = "proposals.created";

/// Number of proposals discarded (dropped without commit).
pub const PROPOSALS_DISCARDED: &str = "proposals.discarded";

/// Current number of uncommitted proposals.
pub const PROPOSALS_UNCOMMITTED: &str = "proposals.uncommitted";

/// Number of insert operations.
pub const INSERT: &str = "insert";

/// Number of remove operations.
pub const REMOVE: &str = "remove";

/// Number of next calls to calculate a change proof.
pub const CHANGE_PROOF_NEXT: &str = "change_proof.next";

/// Commit latency in milliseconds.
pub const COMMIT_LATENCY_MS: &str = "commit_latency_ms";

/// Current number of active revisions.
pub const ACTIVE_REVISIONS: &str = "active_revisions";

/// Maximum number of revisions configured.
pub const MAX_REVISIONS: &str = "max_revisions";

/// Registers all firewood metric descriptions.
pub fn register() {
    describe_counter!(PROPOSALS, "Number of proposals created");
    describe_counter!(
        PROPOSALS_CREATED,
        "Number of proposals created by base type"
    );
    describe_counter!(
        PROPOSALS_DISCARDED,
        "Number of proposals dropped without commit"
    );
    describe_gauge!(
        PROPOSALS_UNCOMMITTED,
        "Current number of uncommitted proposals"
    );
    describe_counter!(INSERT, "Number of insert operations");
    describe_counter!(REMOVE, "Number of remove operations");
    describe_counter!(
        CHANGE_PROOF_NEXT,
        "Number of next calls to calculate a change proof"
    );
    describe_counter!(COMMIT_LATENCY_MS, "Commit latency (ms)");
    describe_gauge!(ACTIVE_REVISIONS, "Current number of active revisions");
    describe_gauge!(MAX_REVISIONS, "Maximum number of revisions configured");
}
