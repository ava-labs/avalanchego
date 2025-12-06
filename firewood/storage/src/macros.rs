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

#[macro_export]
#[cfg(test)]
/// Macro to create an `AreaIndex` from a literal value at compile time.
/// This macro performs bounds checking at compile time and panics if the value is out of bounds.
///
/// Usage:
///   `area_index!(0)` - creates an `AreaIndex` with value 0
///   `area_index!(23)` - creates an `AreaIndex` with value 23
///
/// The macro will panic at compile time if the value is negative or >= `NUM_AREA_SIZES`.
macro_rules! area_index {
    ($v:expr) => {
        const {
            match $crate::nodestore::primitives::AreaIndex::new($v as u8) {
                Some(v) => v,
                None => panic!("Constant area index out of bounds"),
            }
        }
    };
}
