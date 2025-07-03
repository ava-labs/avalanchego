use std::collections::HashSet;
use std::error::Error;
use std::io::Write;
use std::net::Ipv6Addr;
use std::ops::Deref;
use std::sync::Arc;
use std::sync::atomic::Ordering;
use std::time::SystemTime;

use oxhttp::Server;
use oxhttp::model::{Body, Response, StatusCode};
use std::net::Ipv4Addr;
use std::time::Duration;

use chrono::{DateTime, Utc};

use metrics::Key;
use metrics_util::registry::{AtomicStorage, Registry};

/// Sets up a metrics server over a specified port.
/// This happens on a per-process basis, meaning that the metrics system
/// cannot be initialized if it has already been set up in the same process.
pub(crate) fn setup_metrics(metrics_port: u16) -> Result<(), Box<dyn Error>> {
    let inner: TextRecorderInner = TextRecorderInner {
        registry: Registry::atomic(),
    };
    let recorder = TextRecorder {
        inner: Arc::new(inner),
    };
    metrics::set_global_recorder(recorder.clone())?;

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

#[derive(Debug)]
struct TextRecorderInner {
    registry: Registry<Key, AtomicStorage>,
}

#[derive(Debug, Clone)]
struct TextRecorder {
    inner: Arc<TextRecorderInner>,
}

impl TextRecorder {
    fn stats(&self) -> String {
        let mut output = Vec::new();
        let systemtime_now = SystemTime::now();
        let utc_now: DateTime<Utc> = systemtime_now.into();
        let epoch_duration = systemtime_now
            .duration_since(SystemTime::UNIX_EPOCH)
            .expect("system time is before Unix epoch");
        let epoch_ms = epoch_duration
            .as_secs()
            .saturating_mul(1000)
            .saturating_add(u64::from(epoch_duration.subsec_millis()));
        writeln!(output, "# {utc_now}").unwrap();

        let counters = self.registry.get_counter_handles();
        let mut seen = HashSet::new();
        for (key, counter) in counters {
            let sanitized_key_name = key.name().to_string().replace('.', "_");
            if !seen.contains(&sanitized_key_name) {
                writeln!(
                    output,
                    "# TYPE {} counter",
                    key.name().to_string().replace('.', "_")
                )
                .expect("write error");
                seen.insert(sanitized_key_name.clone());
            }
            write!(output, "{sanitized_key_name}").expect("write error");
            if key.labels().len() > 0 {
                write!(
                    output,
                    "{{{}}}",
                    key.labels()
                        .map(|label| format!("{}=\"{}\"", label.key(), label.value()))
                        .collect::<Vec<_>>()
                        .join(",")
                )
                .expect("write error");
            }
            writeln!(output, " {} {}", counter.load(Ordering::Relaxed), epoch_ms)
                .expect("write error");
        }
        writeln!(output).expect("write error");
        output.flush().expect("flush error");

        std::str::from_utf8(output.as_slice())
            .expect("failed to convert to string")
            .into()
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
        _key: metrics::KeyName,
        _unit: Option<metrics::Unit>,
        _description: metrics::SharedString,
    ) {
    }

    fn describe_gauge(
        &self,
        _key: metrics::KeyName,
        _unit: Option<metrics::Unit>,
        _description: metrics::SharedString,
    ) {
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
