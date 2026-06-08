// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Firewood layer metric definitions.

firewood_metrics::define_metrics! {
    counters: {
        /// Number of proposals created by base type
        PROPOSALS_CREATED      = "firewood_proposals_total",
        /// Number of proposals dropped without commit
        PROPOSALS_DISCARDED    = "firewood_proposals_discarded_total",
        /// Number of insert operations
        INSERT                 = "firewood_node_inserts_total",
        /// Number of remove operations
        REMOVE                 = "firewood_node_removes_total",
        /// Number of next calls to calculate a change proof
        CHANGE_PROOF_NEXT      = "firewood_change_proof_iterations_total",
        /// Number of times commit was blocked waiting for a persist permit
        COMMIT_BLOCKED         = "firewood_commits_blocked_total",
        /// Total commits durably written to disk
        COMMITS_TOTAL          = "firewood_commits_total",
        /// Number of proposal commit operations
        PROPOSAL_COMMITS       = "firewood_proposal_commits_total",
        /// Number of proposal commits absorbed by the trivial fast path
        /// (proposal root hash equals current revision's; no new revision
        /// pushed, but proposal cleanup and reparenting still run)
        PROPOSAL_COMMITS_TRIVIAL = "firewood_proposal_commits_trivial_total",
        /// Number of root store persist operations
        PERSIST_ROOT_STORE     = "firewood_persist_root_store_total",
    },
    gauges: {
        /// Current number of uncommitted proposals
        PROPOSALS_UNCOMMITTED  = "firewood_proposals_uncommitted",
        /// Current number of active revisions
        ACTIVE_REVISIONS       = "firewood_revisions_active",
        /// Maximum number of revisions configured
        MAX_REVISIONS          = "firewood_revisions_limit",
        /// Length of deleted list for committed revisions
        DELETED_LIST_LEN       = "firewood_nodes_pending_deletion",
        /// Number of persist permits currently available
        PERMITS_AVAILABLE      = "firewood_persist_permits_available",
        /// Maximum number of persist permits
        MAX_PERMITS            = "firewood_persist_permits_limit",
    },
    histograms: {
        /// End-to-end proposal creation duration including batch apply and hashing
        PROPOSE_DURATION_SECONDS  = "firewood_propose_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0]),
        /// Duration of each background persist cycle
        PERSIST_CYCLE_DURATION_SECONDS = "firewood_persist_cycle_duration_seconds" buckets([0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]),
        /// Duration of proposal commit operations
        PROPOSAL_COMMITS_DURATION_SECONDS = "firewood_proposal_commit_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]),
        /// Duration of root store persist operations
        PERSIST_ROOT_STORE_DURATION_SECONDS = "firewood_persist_root_store_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]),
        /// Wait time to acquire the in_memory_revisions write lock in commit()
        COMMIT_LOCK_WAIT_SECONDS = "firewood_commit_lock_wait_seconds" native(2.0, 160, 1e-9),
        /// Wait time to acquire the in_memory_revisions read lock in current_revision()
        CURRENT_REVISION_LOCK_WAIT_SECONDS = "firewood_current_revision_lock_wait_seconds" native(2.0, 160, 1e-9),
        /// Wait time to acquire the by_hash read lock in revision()
        BY_HASH_LOCK_WAIT_SECONDS = "firewood_by_hash_lock_wait_seconds" native(2.0, 160, 1e-9),
        /// Duration of submitting a revision to the persist worker channel
        PERSIST_SUBMIT_DURATION_SECONDS = "firewood_persist_submit_duration_seconds" native(2.0, 160, 1e-9),
        /// Number of key/value pairs contained in a generated proof, by proof kind
        PROOF_KEYS = "firewood_proof_keys" buckets([0.0, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0, 64.0, 128.0, 256.0, 512.0, 1024.0, 2048.0, 4096.0, 8192.0, 16_384.0, 32_768.0, 65_536.0, 131_072.0]),
    },
}
