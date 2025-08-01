// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[macro_export]
/// Macro to register and use a counter metric with description and labels.
/// This macro is a wrapper around the `metrics` crate's `counter!` and `describe_counter!`
/// macros. It ensures that the description is registered just once.
///
/// Usage:
///   `firewood_counter!("metric_name", "description")`
///   `firewood_counter!("metric_name", "description", "label" => "value")`
///
/// Call `.increment(val)` or `.absolute(val)` on the result as appropriate.
macro_rules! firewood_counter {
    // With labels
    ($name:expr, $desc:expr, $($labels:tt)+) => {
        {
            static ONCE: std::sync::Once = std::sync::Once::new();
            ONCE.call_once(|| {
                metrics::describe_counter!($name, $desc);
            });
            metrics::counter!($name, $($labels)+)
        }
    };
    // No labels
    ($name:expr, $desc:expr) => {
        {
            static ONCE: std::sync::Once = std::sync::Once::new();
            ONCE.call_once(|| {
                metrics::describe_counter!($name, $desc);
            });
            metrics::counter!($name)
        }
    };
}

#[macro_export]
/// Macro to register and use a gauge metric with description and labels.
/// This macro is a wrapper around the `metrics` crate's `gauge!` and `describe_gauge!`
/// macros. It ensures that the description is registered just once.
///
/// Usage:
///   `firewood_gauge!("metric_name", "description")`
///   `firewood_gauge!("metric_name", "description", "label" => "value")`
///
/// Call `.increment(val)` or `.decrement(val)` on the result as appropriate.
macro_rules! firewood_gauge {
    // With labels
    ($name:expr, $desc:expr, $($labels:tt)+) => {
        {
            static ONCE: std::sync::Once = std::sync::Once::new();
            ONCE.call_once(|| {
                metrics::describe_counter!($name, $desc);
            });
            metrics::gauge!($name, $($labels)+)
        }
    };
    // No labels
    ($name:expr, $desc:expr) => {
        {
            static ONCE: std::sync::Once = std::sync::Once::new();
            ONCE.call_once(|| {
                metrics::describe_counter!($name, $desc);
            });
            metrics::gauge!($name)
        }
    };
}
