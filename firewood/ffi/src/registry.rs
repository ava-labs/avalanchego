// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! FFI layer metric definitions.

firewood_metrics::define_metrics! {
    counters: {
        /// Count of range proof merges via FFI
        MERGE_COUNT = "firewood_proof_merges_total",
    },
    histograms: {
        /// Wall-clock duration of gather_rendered_metrics calls
        GATHER_DURATION_SECONDS = "firewood_gather_duration_seconds" native(2.0, 160, 1e-9),
    },
}
