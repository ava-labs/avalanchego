// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! FFI layer metric definitions.

use firewood_metrics::{HistogramBucketConfig, register_histogram_with_buckets};
use metrics::describe_counter;

pub const COMMIT_MS: &str = "ffi.commit_ms";
pub const COMMIT_COUNT: &str = "ffi.commit";
pub const COMMIT_MS_BUCKET: &str = "ffi.commit_ms_bucket";

pub const PROPOSE_MS: &str = "ffi.propose_ms";
pub const PROPOSE_COUNT: &str = "ffi.propose";
pub const PROPOSE_MS_BUCKET: &str = "ffi.propose_ms_bucket";

pub const BATCH_MS: &str = "ffi.batch_ms";
pub const BATCH_COUNT: &str = "ffi.batch";
pub const BATCH_MS_BUCKET: &str = "ffi.batch_ms_bucket";

pub const CACHED_VIEW_MISS: &str = "ffi.cached_view.miss";
pub const CACHED_VIEW_HIT: &str = "ffi.cached_view.hit";

pub const MERGE_COUNT: &str = "firewood.ffi.merge";

const FFI_TIMING_BUCKETS: &[f64] = &[
    0.1, 0.25, 0.5, 0.75, 1.0, 1.5, 2.0, 2.5, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 10.0, 12.0, 15.0, 20.0,
    30.0, 50.0,
];

/// Registers all FFI metric descriptions.
///
/// Histogram bucket configurations are collected into the provided vector.
pub fn register(histogram_configs: &mut Vec<HistogramBucketConfig>) {
    describe_counter!(COMMIT_MS, "Time spent committing via FFI (ms)");
    describe_counter!(COMMIT_COUNT, "Count of commit operations via FFI");
    register_histogram_with_buckets(
        histogram_configs,
        COMMIT_MS_BUCKET,
        "Commit duration via FFI in milliseconds",
        FFI_TIMING_BUCKETS,
    );

    describe_counter!(PROPOSE_MS, "Time spent proposing via FFI (ms)");
    describe_counter!(PROPOSE_COUNT, "Count of proposal operations via FFI");
    register_histogram_with_buckets(
        histogram_configs,
        PROPOSE_MS_BUCKET,
        "Propose duration via FFI in milliseconds",
        FFI_TIMING_BUCKETS,
    );

    describe_counter!(BATCH_MS, "Time spent processing batches (ms)");
    describe_counter!(BATCH_COUNT, "Count of batch operations completed");
    register_histogram_with_buckets(
        histogram_configs,
        BATCH_MS_BUCKET,
        "Batch processing duration in milliseconds",
        FFI_TIMING_BUCKETS,
    );

    describe_counter!(CACHED_VIEW_MISS, "Count of cached view misses");
    describe_counter!(CACHED_VIEW_HIT, "Count of cached view hits");
    describe_counter!(MERGE_COUNT, "Count of range proof merges via FFI");
}
