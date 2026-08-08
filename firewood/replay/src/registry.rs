// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Replay layer metric definitions.

firewood_metrics::define_metrics! {
    histograms: {
        /// Duration of propose calls during replay
        PROPOSE_DURATION_SECONDS = "firewood_replay_propose_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]),
        /// Duration of commit calls during replay
        COMMIT_DURATION_SECONDS  = "firewood_replay_commit_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0]),
    },
}
