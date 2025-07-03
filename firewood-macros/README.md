# Firewood Macros

A Rust procedural macro crate providing zero-allocation metrics instrumentation for the Firewood database.

## Overview

This crate provides the `#[metrics]` attribute macro that automatically instruments functions with performance metrics collection. The macro is designed for high-performance applications where allocation overhead during metrics collection is unacceptable.

## Features

- **Zero Runtime Allocations**: Uses compile-time string concatenation and static label arrays
- **Automatic Timing**: Measures function execution time with microsecond precision
- **Success/Failure Tracking**: Automatically labels metrics based on `Result` return values
- **Metric Descriptions**: Optional human-readable descriptions for better observability
- **Compile-time Validation**: Ensures functions return `Result<T, E>` types

## Usage

Add the dependency to your `Cargo.toml`:

```toml
[dependencies]
firewood-macros.workspace = true
metrics = "0.24"
coarsetime = "0.1"
```

### Basic Usage

```rust
use firewood_macros::metrics;

#[metrics("firewood.example")]
fn example() -> Result<Vec<Data>, DatabaseError> {
    // Your function implementation
    Ok(vec![])
}
```

### With Description

```rust
#[metrics("firewood.example", "example operation")]
fn example(user: User) -> Result<(), DatabaseError> {
    // Your function implementation
    Ok(())
}
```

## Generated Metrics

For each instrumented function, the macro generates two metrics:

1. **Count Metric** (base name): Tracks the number of function calls
2. **Timing Metric** (base name + "_ms"): Tracks execution time in milliseconds

Both metrics include a `success` label:

- `success="true"` for `Ok(_)` results
- `success="false"` for `Err(_)` results

### Example Output

For `#[metrics("firewood.query", "data retrieval")]`:

- `firewood.example{success="true"}` - Count of successful queries
- `firewood.example{success="false"}` - Count of failed queries
- `firewood.example_ms{success="true"}` - Timing of successful queries
- `firewood.example_ms{success="false"}` - Timing of failed queries

## Requirements

- Functions must return a `Result<T, E>` type
- The `metrics` and `coarsetime` crates must be available in scope
- Rust 1.70+ (for `is_some_and` method)

## Performance Characteristics

### Zero Allocations

The macro generates code that avoids all runtime allocations:

```rust
// Static label arrays (no allocation)
static __METRICS_LABELS_SUCCESS: &[(&str, &str)] = &[("success", "true")];
static __METRICS_LABELS_ERROR: &[(&str, &str)] = &[("success", "false")];

// Compile-time string concatenation (no allocation)
metrics::counter!(concat!("my.metric", "_ms"), labels)
```

### Minimal Overhead

- Single timestamp capture at function start, using the coarsetime crate, which is known to be extremely fast
- Branch-free label selection based on `Result::is_err()`
- Direct counter increments without intermediate allocations

## Implementation Details

### Code Generation

The macro transforms this:

```rust
#[metrics("my.operation")]
fn my_function() -> Result<String, Error> {
    Ok("result".to_string())
}
```

Into approximately this:

```rust
fn my_function() -> Result<String, Error> {
    // Register metrics (once per process)
    static __METRICS_REGISTERED: std::sync::Once = std::sync::Once::new();
    __METRICS_REGISTERED.call_once(|| {
        metrics::describe_counter!("my.operation", "Operation counter");
        metrics::describe_counter!(concat!("my.operation", "_ms"), "Operation timing");
    });

    // Start timing
    let __metrics_start = coarsetime::Instant::now();

    // Execute original function
    let __metrics_result = (|| {
        Ok("result".to_string())
    })();

    // Record metrics
    static __METRICS_LABELS_SUCCESS: &[(&str, &str)] = &[("success", "true")];
    static __METRICS_LABELS_ERROR: &[(&str, &str)] = &[("success", "false")];
    let __metrics_labels = if __metrics_result.is_err() {
        __METRICS_LABELS_ERROR
    } else {
        __METRICS_LABELS_SUCCESS
    };

    metrics::counter!("my.operation", __metrics_labels).increment(1);
    metrics::counter!(concat!("my.operation", "_ms"), __metrics_labels)
        .increment(__metrics_start.elapsed().as_millis());

    __metrics_result
}
```

## Testing

The crate includes comprehensive tests:

```bash
cargo test -p firewood-macros
```

## License

This crate is part of the Firewood project and follows the same licensing terms.
See LICENSE.md at the top level for details.
