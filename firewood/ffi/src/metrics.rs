// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::error::Error;
use std::net::Ipv6Addr;
use std::sync::OnceLock;

use firewood_metrics::{HistogramBucketConfig, MetricsContext};
use metrics_exporter_prometheus::{Matcher, PrometheusBuilder, PrometheusHandle};
use oxhttp::Server;
use oxhttp::model::{Body, Response, StatusCode};
use std::net::Ipv4Addr;
use std::time::Duration;

static RECORDER: OnceLock<PrometheusHandle> = OnceLock::new();

/// Trait for types that carry a [`MetricsContext`].
///
/// Implemented for FFI handle types.
/// Concrete impls live in their respective modules (handle, revision, proposal, iterator).
pub(crate) trait MetricsContextExt {
    fn metrics_context(&self) -> Option<MetricsContext>;
}

// some blanket implementations. can't go with Deref approach because of
// tuple handle in range proofs.
impl<T: MetricsContextExt + ?Sized> MetricsContextExt for Box<T> {
    fn metrics_context(&self) -> Option<MetricsContext> {
        (**self).metrics_context()
    }
}

impl<T: MetricsContextExt + ?Sized> MetricsContextExt for &T {
    fn metrics_context(&self) -> Option<MetricsContext> {
        (**self).metrics_context()
    }
}

impl<T: MetricsContextExt + ?Sized> MetricsContextExt for &mut T {
    fn metrics_context(&self) -> Option<MetricsContext> {
        (**self).metrics_context()
    }
}

/// Starts metrics recorder.
/// This happens on a per-process basis, meaning that the metrics system cannot
/// be initialized if it has already been set up in the same process.
pub fn setup_metrics() -> Result<(), Box<dyn Error>> {
    // Collect histogram bucket configurations from all crates
    let mut histogram_configs: Vec<HistogramBucketConfig> = Vec::new();
    crate::registry::register(&mut histogram_configs);
    firewood::registry::register();
    firewood_storage::registry::register();
    #[cfg(feature = "block-replay")]
    firewood_replay::registry::register();

    // Build the Prometheus exporter with bucket configurations from the registry
    // TODO: Switch to Prometheus's native histograms
    // they are cheaper, more efficient, and easier to configure (no predefined buckets)
    // proper default support will start in prometheus v3.9 and v4.0; once our infra switches,
    // we should switch too.
    let mut builder = PrometheusBuilder::new();

    // Apply bucket configurations from the global registry
    for config in histogram_configs {
        builder = builder
            .set_buckets_for_metric(Matcher::Full(config.name.to_string()), config.buckets)?;
    }

    let handle = builder.install_recorder()?;

    RECORDER
        .set(handle)
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
                .body(Body::from(recorder.render()))
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
    Ok(recorder.render())
}
