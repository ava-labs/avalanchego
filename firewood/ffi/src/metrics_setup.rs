// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use parking_lot::Mutex;
use std::borrow::Cow;
use std::collections::{HashMap, HashSet};
use std::error::Error;
use std::fmt::Write;
use std::net::Ipv6Addr;
use std::ops::Deref;
use std::sync::Arc;
use std::sync::OnceLock;
use std::sync::atomic::Ordering;
use std::time::SystemTime;

use oxhttp::Server;
use oxhttp::model::{Body, Response, StatusCode};
use std::net::Ipv4Addr;
use std::time::Duration;

use chrono::{DateTime, Utc};

use metrics::Key;
use metrics_util::registry::{AtomicStorage, Registry};

static RECORDER: OnceLock<TextRecorder> = OnceLock::new();

/// Starts metrics recorder.
/// This happens on a per-process basis, meaning that the metrics system cannot
/// be initialized if it has already been set up in the same process.
pub fn setup_metrics() -> Result<(), Box<dyn Error>> {
    let inner = TextRecorderInner {
        registry: Registry::atomic(),
        help: Mutex::new(HashMap::new()),
    };
    let recorder = TextRecorder {
        inner: Arc::new(inner),
    };

    metrics::set_global_recorder(recorder.clone())?;
    RECORDER
        .set(recorder)
        .map_err(|_| "recorder already initialized")?;

    Ok(())
}

/// Starts metrics recorder along with an exporter over a specified port.
/// This happens on a per-process basis, meaning that the metrics system
/// cannot be initialized if it has already been set up in the same process.
pub fn setup_metrics_with_exporter(metrics_port: u16) -> Result<(), Box<dyn Error>> {
    setup_metrics()?;

    let recorder = RECORDER.get().ok_or("recorder not initialized")?;
    Server::new(move |request| {
        if request.method() == "GET" {
            Response::builder()
                .status(StatusCode::OK)
                .header("Content-Type", "text/plain")
                .body(Body::from(recorder.stats()))
                .expect("failed to build response")
        } else {
            Response::builder()
                .status(StatusCode::METHOD_NOT_ALLOWED)
                .body(Body::from("Method not allowed"))
                .expect("failed to build response")
        }
    })
    .bind((Ipv4Addr::LOCALHOST, metrics_port))
    .bind((Ipv6Addr::LOCALHOST, metrics_port))
    .with_global_timeout(Duration::from_secs(60 * 60))
    .with_max_concurrent_connections(2)
    .spawn()?;
    Ok(())
}

/// Returns the latest metrics for this process.
pub fn gather_metrics() -> Result<String, String> {
    let Some(recorder) = RECORDER.get() else {
        return Err(String::from("recorder not initialized"));
    };
    Ok(recorder.stats())
}

/// Internal data structure for the [`TextRecorder`] containing the metrics registry and help text.
///
/// This structure holds:
/// - A metrics registry that stores all counter and gauge values with atomic storage
/// - A mutex-protected map of help text for each metric name
///
/// The atomic storage ensures thread-safe updates to metric values, while the mutex
/// protects the help text map during concurrent access.
#[derive(Debug)]
struct TextRecorderInner {
    registry: Registry<Key, AtomicStorage>,
    help: Mutex<HashMap<String, String>>,
}

/// A metrics recorder that outputs metrics in Prometheus text exposition format.
///
/// This recorder implements the [`metrics::Recorder`] trait and formats all
/// collected metrics as text in the format expected by Prometheus. It supports
/// counters and gauges, but not histograms.
///
/// The recorder is thread-safe and can be shared across multiple threads.
/// All metrics are stored in memory and can be retrieved via [`Self::stats`].
#[derive(Debug, Clone)]
struct TextRecorder {
    inner: Arc<TextRecorderInner>,
}

impl TextRecorder {
    fn stats(&self) -> String {
        let mut output = String::new();
        let systemtime_now = SystemTime::now();
        let utc_now: DateTime<Utc> = systemtime_now.into();
        let epoch_duration = systemtime_now
            .duration_since(SystemTime::UNIX_EPOCH)
            .expect("system time is before Unix epoch");
        let epoch_ms = epoch_duration
            .as_secs()
            .saturating_mul(1000)
            .saturating_add(u64::from(epoch_duration.subsec_millis()));
        writeln!(output, "# {utc_now}").expect("write to string cannot fail");

        let help_guard = self.inner.help.lock();

        let counters = self.registry.get_counter_handles();
        let mut seen_counters = HashSet::new();
        for (key, counter) in counters {
            let sanitized_key_name = self.sanitize_key_name(key.name());
            if !seen_counters.contains(sanitized_key_name.as_ref()) {
                if let Some(help) = help_guard.get(sanitized_key_name.as_ref()) {
                    writeln!(output, "# HELP {sanitized_key_name} {help}").expect("write error");
                }
                writeln!(output, "# TYPE {} counter", &sanitized_key_name).expect("write error");
                seen_counters.insert(sanitized_key_name.to_string());
            }
            write!(output, "{sanitized_key_name}").expect("write error");
            self.write_labels(&mut output, key.labels());

            writeln!(output, " {} {}", counter.load(Ordering::Relaxed), epoch_ms)
                .expect("write error");
        }

        // Get gauge handles: Uses self.registry.get_gauge_handles() to retrieve all registered gauges
        let gauges = self.registry.get_gauge_handles();
        // Track seen gauges. We don't reuse the hashset above to allow us to check for duplicates.
        let mut seen_gauges = HashSet::new();
        for (key, gauge) in gauges {
            let sanitized_key_name = self.sanitize_key_name(key.name());
            if !seen_gauges.contains(sanitized_key_name.as_ref()) {
                // Output type declaration: Writes # TYPE {name} gauge for each unique gauge name
                if let Some(help) = help_guard.get(sanitized_key_name.as_ref()) {
                    writeln!(output, "# HELP {sanitized_key_name} {help}").expect("write error");
                }
                writeln!(output, "# TYPE {sanitized_key_name} gauge").expect("write error");
                seen_gauges.insert(sanitized_key_name.to_string());
            }
            // Format metric line: Outputs the gauge name, labels (if any), current value, and timestamp
            write!(output, "{sanitized_key_name}").expect("write error");
            self.write_labels(&mut output, key.labels());
            // Load gauge value: Uses gauge.load(Ordering::Relaxed) to get the current gauge value
            let value = gauge.load(Ordering::Relaxed);
            writeln!(output, " {} {}", f64::from_bits(value), epoch_ms).expect("write error");
        }

        // Prometheus does not support multiple TYPE declarations for the same metric,
        // but we will emit them anyway, and panic if we're in debug mode.
        debug_assert_eq!(
            seen_gauges.intersection(&seen_counters).count(),
            0,
            "duplicate name(s) for gauge and counter: {:?}",
            seen_gauges.intersection(&seen_counters).collect::<Vec<_>>()
        );
        drop(help_guard);
        writeln!(output).expect("write error");

        output
    }

    // helper to write labels to the output, minimizing allocations
    fn write_labels<'a, I: Iterator<Item = &'a metrics::Label>>(
        &self,
        output: &mut impl Write,
        labels: I,
    ) {
        let mut label_iter = labels.into_iter();
        if let Some(first_label) = label_iter.next() {
            write!(
                output,
                "{{{}=\"{}\"",
                first_label.key(),
                first_label.value()
            )
            .expect("write error");
            label_iter.for_each(|label| {
                write!(output, ",{}=\"{}\"", label.key(), label.value()).expect("write error");
            });
            write!(output, "}}").expect("write error");
        }
    }

    // remove dots from key names, if they are present
    fn sanitize_key_name<'a>(&self, key_name: &'a str) -> Cow<'a, str> {
        if key_name.contains('.') {
            Cow::Owned(key_name.replace('.', "_"))
        } else {
            Cow::Borrowed(key_name)
        }
    }
}

impl Deref for TextRecorder {
    type Target = Arc<TextRecorderInner>;

    fn deref(&self) -> &Self::Target {
        &self.inner
    }
}

impl metrics::Recorder for TextRecorder {
    fn describe_counter(
        &self,
        key: metrics::KeyName,
        _unit: Option<metrics::Unit>,
        description: metrics::SharedString,
    ) {
        self.inner
            .help
            .lock()
            .insert(key.as_str().to_string(), description.to_string());
    }

    fn describe_gauge(
        &self,
        key: metrics::KeyName,
        _unit: Option<metrics::Unit>,
        description: metrics::SharedString,
    ) {
        self.inner
            .help
            .lock()
            .insert(key.as_str().to_string(), description.to_string());
    }

    fn describe_histogram(
        &self,
        _key: metrics::KeyName,
        _unit: Option<metrics::Unit>,
        _description: metrics::SharedString,
    ) {
    }

    fn register_counter(
        &self,
        key: &metrics::Key,
        _metadata: &metrics::Metadata<'_>,
    ) -> metrics::Counter {
        self.inner
            .registry
            .get_or_create_counter(key, |c| c.clone().into())
    }

    fn register_gauge(
        &self,
        key: &metrics::Key,
        _metadata: &metrics::Metadata<'_>,
    ) -> metrics::Gauge {
        self.inner
            .registry
            .get_or_create_gauge(key, |c| c.clone().into())
    }

    fn register_histogram(
        &self,
        key: &metrics::Key,
        _metadata: &metrics::Metadata<'_>,
    ) -> metrics::Histogram {
        self.inner
            .registry
            .get_or_create_histogram(key, |c| c.clone().into())
    }
}
