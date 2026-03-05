// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Shared metric helpers for Firewood.
//!
//! This crate provides:
//! - Recording macros that are simple to use at callsites
//! - Thread-local context for gating expensive metrics
//! - Histogram bucket registry for bucket configuration
//!
//! Each crate defines its own metric registry (e.g., `ffi::registry`, `storage::registry`).
//!
//! # Usage
//!
//! ```ignore
//! use firewood_metrics::firewood_increment;
//! use crate::registry;
//!
//! // At startup - register metrics and their bucket configurations
//! registry::register();
//!
//! // Always increment
//! firewood_increment!(registry::COMMIT_COUNT, 1);
//!
//! // Expensive histogram (only records if expensive metrics enabled)
//! firewood_record!(registry::COMMIT_MS_BUCKET, elapsed_ms, expensive);
//! ```
//!
//! # Histogram Bucket Registration
//!
//! Use [`register_histogram_with_buckets`] to specify custom buckets for histogram metrics.
//! This should be called in your crate's `register()` function, collecting configs into
//! a vector:
//!
//! ```ignore
//! use firewood_metrics::{HistogramBucketConfig, register_histogram_with_buckets};
//!
//! pub fn register(histogram_configs: &mut Vec<HistogramBucketConfig>) {
//!     // Register histogram with custom buckets
//!     register_histogram_with_buckets(
//!         histogram_configs,
//!         "my.latency_ms",
//!         "Latency in milliseconds",
//!         &[1.0, 5.0, 10.0, 50.0, 100.0],
//!     );
//! }
//! ```
//!
//! # Macro Overview
//!
//! All recording macros accept an optional trailing `expensive` flag:
//!
//! | Macro | Description |
//! |-------|-------------|
//! | `firewood_increment!(name, value)` | Always increment a counter |
//! | `firewood_increment!(name, value, expensive)` | Increment only if expensive metrics enabled |
//! | `firewood_set!(name, value)` | Always set a gauge value |
//! | `firewood_set!(name, value, expensive)` | Set only if expensive metrics enabled |
//! | `firewood_record!(name, value)` | Always record to histogram |
//! | `firewood_record!(name, value, expensive)` | Record only if expensive metrics enabled |
//!
//! For registration, use `metrics::describe_*` macros or [`register_histogram_with_buckets`].

use std::cell::Cell;

/// Metric configuration context for the current thread.
///
/// This is set at API boundaries (e.g., FFI entrypoints) and read when deciding
/// whether to record expensive metrics.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct MetricsContext {
    expensive_metrics_enabled: bool,
}

impl MetricsContext {
    /// Create a new [`MetricsContext`].
    #[must_use]
    pub const fn new(expensive_metrics_enabled: bool) -> Self {
        Self {
            expensive_metrics_enabled,
        }
    }

    /// Whether expensive metrics are enabled.
    #[must_use]
    pub const fn expensive_metrics_enabled(self) -> bool {
        self.expensive_metrics_enabled
    }
}

thread_local! {
    static METRICS_CONTEXT: Cell<Option<MetricsContext>> = const { Cell::new(None) };
}

/// A guard that restores the previous thread-local [`MetricsContext`].
#[derive(Debug)]
pub struct MetricsContextGuard {
    previous: Option<MetricsContext>,
}

impl Drop for MetricsContextGuard {
    fn drop(&mut self) {
        METRICS_CONTEXT.set(self.previous);
    }
}

/// Sets the thread-local metrics context for the duration of the returned guard.
#[must_use]
pub fn set_metrics_context(context: Option<MetricsContext>) -> MetricsContextGuard {
    let previous = METRICS_CONTEXT.replace(context);
    MetricsContextGuard { previous }
}

/// Returns the current thread-local metrics context, if one is set.
#[must_use]
pub fn current_metrics_context() -> Option<MetricsContext> {
    METRICS_CONTEXT.get()
}

/// Returns whether expensive metrics are enabled for the current thread.
///
/// If no context is set, this returns `false`.
#[must_use]
pub fn expensive_metrics_enabled() -> bool {
    current_metrics_context().is_some_and(MetricsContext::expensive_metrics_enabled)
}

/// Entry for a registered histogram with custom buckets.
#[derive(Debug, Clone)]
pub struct HistogramBucketConfig {
    /// The metric name (e.g., `ffi.commit_ms_bucket`).
    pub name: &'static str,
    /// The bucket boundaries.
    pub buckets: &'static [f64],
}

/// Registers a histogram metric with custom bucket boundaries.
///
/// This function:
/// 1. Calls `metrics::describe_histogram!` to register the metric description
/// 2. Appends the bucket configuration to the provided vector
///
/// # Arguments
///
/// * `configs` - Mutable vector to collect bucket configurations
/// * `name` - The metric name (must be a `&'static str`)
/// * `description` - Human-readable description of the metric
/// * `buckets` - Bucket boundaries for the histogram (must be `&'static [f64]`)
pub fn register_histogram_with_buckets(
    configs: &mut Vec<HistogramBucketConfig>,
    name: &'static str,
    description: &'static str,
    buckets: &'static [f64],
) {
    // Register the metric description
    metrics::describe_histogram!(name, description);

    // Collect bucket configuration
    configs.push(HistogramBucketConfig { name, buckets });
}

/// Increments a counter metric.
///
/// # Usage
/// ```ignore
/// firewood_increment!(registry::COMMIT_COUNT, 1);
/// firewood_increment!(registry::COMMIT_COUNT, 1, "label" => "value");
/// firewood_increment!(registry::SLOW_OP_COUNT, 1, expensive);
/// ```
#[macro_export]
macro_rules! firewood_increment {
    ($name:expr, $value:expr, expensive) => {
        if $crate::expensive_metrics_enabled() {
            ::metrics::counter!($name).increment($value);
        }
    };
    ($name:expr, $value:expr) => {
        ::metrics::counter!($name).increment($value)
    };
    ($name:expr, $value:expr, $($labels:tt)+) => {
        ::metrics::counter!($name, $($labels)+).increment($value)
    };
}

/// Returns a counter handle for advanced operations.
///
/// # Usage
/// ```ignore
/// let counter = firewood_counter!(registry::COMMIT_COUNT);
/// counter.increment(1);
/// counter.absolute(100);
/// ```
#[macro_export]
macro_rules! firewood_counter {
    ($name:expr) => {
        ::metrics::counter!($name)
    };
    ($name:expr, $($labels:tt)+) => {
        ::metrics::counter!($name, $($labels)+)
    };
}

/// Sets a gauge metric value.
///
/// # Usage
/// ```ignore
/// firewood_set!(registry::ACTIVE_REVISIONS, count);
/// firewood_set!(registry::QUEUE_SIZE, size, "queue" => "main");
/// firewood_set!(registry::DETAILED_STAT, value, expensive);
/// ```
#[macro_export]
macro_rules! firewood_set {
    ($name:expr, $value:expr, expensive) => {
        if $crate::expensive_metrics_enabled() {
            ::metrics::gauge!($name).set($value as f64);
        }
    };
    ($name:expr, $value:expr) => {
        ::metrics::gauge!($name).set($value as f64)
    };
    ($name:expr, $value:expr, $($labels:tt)+) => {
        ::metrics::gauge!($name, $($labels)+).set($value as f64)
    };
}

/// Returns a gauge handle for advanced operations.
///
/// # Usage
/// ```ignore
/// let gauge = firewood_gauge!(registry::ACTIVE_REVISIONS);
/// gauge.set(10.0);
/// gauge.increment(1.0);
/// gauge.decrement(1.0);
/// ```
#[macro_export]
macro_rules! firewood_gauge {
    ($name:expr) => {
        ::metrics::gauge!($name)
    };
    ($name:expr, $($labels:tt)+) => {
        ::metrics::gauge!($name, $($labels)+)
    };
}

/// Records a value to a histogram metric.
///
/// # Usage
/// ```ignore
/// firewood_record!(registry::LATENCY_MS, elapsed_ms);
/// firewood_record!(registry::LATENCY_MS, elapsed_ms, "op" => "read");
/// firewood_record!(registry::COMMIT_MS_BUCKET, elapsed_ms, expensive);
/// ```
#[macro_export]
macro_rules! firewood_record {
    ($name:expr, $value:expr, expensive) => {
        if $crate::expensive_metrics_enabled() {
            ::metrics::histogram!($name).record($value);
        }
    };
    ($name:expr, $value:expr) => {
        ::metrics::histogram!($name).record($value)
    };
    ($name:expr, $value:expr, $($labels:tt)+) => {
        ::metrics::histogram!($name, $($labels)+).record($value)
    };
}

/// Returns a histogram handle for advanced operations.
///
/// # Usage
/// ```ignore
/// let histogram = firewood_histogram!(registry::LATENCY_MS);
/// histogram.record(elapsed_ms);
/// ```
#[macro_export]
macro_rules! firewood_histogram {
    ($name:expr) => {
        ::metrics::histogram!($name)
    };
    ($name:expr, $($labels:tt)+) => {
        ::metrics::histogram!($name, $($labels)+)
    };
}

#[cfg(test)]
mod tests {
    use super::*;

    fn isolated<F, R>(f: F) -> R
    where
        F: FnOnce() -> R,
    {
        let _guard = set_metrics_context(None);
        f()
    }

    #[test]
    fn context_defaults_to_none() {
        isolated(|| {
            assert_eq!(current_metrics_context(), None);
            assert!(!expensive_metrics_enabled());
        });
    }

    #[test]
    fn nested_guards_restore_in_correct_order() {
        isolated(|| {
            let outer = MetricsContext::new(false);
            let inner = MetricsContext::new(true);

            let guard1 = set_metrics_context(Some(outer));
            {
                let _guard2 = set_metrics_context(Some(inner));
                assert_eq!(current_metrics_context(), Some(inner));
            }
            assert_eq!(current_metrics_context(), Some(outer));

            drop(guard1);
            assert_eq!(current_metrics_context(), None);
        });
    }
}
