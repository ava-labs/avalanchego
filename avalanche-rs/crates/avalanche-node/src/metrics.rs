//! Prometheus metrics for Avalanche node.
//!
//! Provides comprehensive metrics for:
//! - Consensus performance
//! - Network health
//! - VM execution
//! - Resource usage

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use parking_lot::RwLock;

/// Metric types.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum MetricType {
    Counter,
    Gauge,
    Histogram,
    Summary,
}

/// Metric value.
#[derive(Debug, Clone)]
pub enum MetricValue {
    Counter(u64),
    Gauge(f64),
    Histogram(HistogramValue),
    Summary(SummaryValue),
}

/// Histogram data.
#[derive(Debug, Clone)]
pub struct HistogramValue {
    pub count: u64,
    pub sum: f64,
    pub buckets: Vec<(f64, u64)>, // (upper_bound, count)
}

/// Summary data.
#[derive(Debug, Clone)]
pub struct SummaryValue {
    pub count: u64,
    pub sum: f64,
    pub quantiles: Vec<(f64, f64)>, // (quantile, value)
}

/// Metric descriptor.
#[derive(Debug, Clone)]
pub struct MetricDesc {
    pub name: String,
    pub help: String,
    pub metric_type: MetricType,
    pub labels: Vec<String>,
}

/// Counter metric.
pub struct Counter {
    desc: MetricDesc,
    values: RwLock<HashMap<Vec<String>, AtomicU64>>,
}

impl Counter {
    /// Creates a new counter.
    pub fn new(name: &str, help: &str, labels: Vec<&str>) -> Self {
        Self {
            desc: MetricDesc {
                name: name.to_string(),
                help: help.to_string(),
                metric_type: MetricType::Counter,
                labels: labels.into_iter().map(String::from).collect(),
            },
            values: RwLock::new(HashMap::new()),
        }
    }

    /// Increments the counter.
    pub fn inc(&self) {
        self.inc_by(1);
    }

    /// Increments by a value.
    pub fn inc_by(&self, v: u64) {
        let key = vec![];
        let mut values = self.values.write();
        values
            .entry(key)
            .or_insert_with(|| AtomicU64::new(0))
            .fetch_add(v, Ordering::Relaxed);
    }

    /// Increments with labels.
    pub fn with_labels(&self, labels: Vec<&str>) -> CounterVec {
        CounterVec {
            counter: self,
            labels: labels.into_iter().map(String::from).collect(),
        }
    }

    /// Gets the current value.
    pub fn get(&self) -> u64 {
        self.values
            .read()
            .get(&vec![])
            .map(|v| v.load(Ordering::Relaxed))
            .unwrap_or(0)
    }

    /// Gets the descriptor.
    pub fn desc(&self) -> &MetricDesc {
        &self.desc
    }
}

/// Counter with labels.
pub struct CounterVec<'a> {
    counter: &'a Counter,
    labels: Vec<String>,
}

impl<'a> CounterVec<'a> {
    pub fn inc(&self) {
        self.inc_by(1);
    }

    pub fn inc_by(&self, v: u64) {
        let mut values = self.counter.values.write();
        values
            .entry(self.labels.clone())
            .or_insert_with(|| AtomicU64::new(0))
            .fetch_add(v, Ordering::Relaxed);
    }
}

/// Gauge metric.
pub struct Gauge {
    desc: MetricDesc,
    values: RwLock<HashMap<Vec<String>, f64>>,
}

impl Gauge {
    /// Creates a new gauge.
    pub fn new(name: &str, help: &str, labels: Vec<&str>) -> Self {
        Self {
            desc: MetricDesc {
                name: name.to_string(),
                help: help.to_string(),
                metric_type: MetricType::Gauge,
                labels: labels.into_iter().map(String::from).collect(),
            },
            values: RwLock::new(HashMap::new()),
        }
    }

    /// Sets the gauge value.
    pub fn set(&self, v: f64) {
        self.values.write().insert(vec![], v);
    }

    /// Increments the gauge.
    pub fn inc(&self) {
        self.add(1.0);
    }

    /// Decrements the gauge.
    pub fn dec(&self) {
        self.sub(1.0);
    }

    /// Adds to the gauge.
    pub fn add(&self, v: f64) {
        let mut values = self.values.write();
        let entry = values.entry(vec![]).or_insert(0.0);
        *entry += v;
    }

    /// Subtracts from the gauge.
    pub fn sub(&self, v: f64) {
        self.add(-v);
    }

    /// Gets the current value.
    pub fn get(&self) -> f64 {
        self.values.read().get(&vec![]).copied().unwrap_or(0.0)
    }

    /// Sets with labels.
    pub fn set_with_labels(&self, labels: Vec<&str>, v: f64) {
        let key: Vec<String> = labels.into_iter().map(String::from).collect();
        self.values.write().insert(key, v);
    }

    /// Gets the descriptor.
    pub fn desc(&self) -> &MetricDesc {
        &self.desc
    }
}

/// Histogram metric.
pub struct Histogram {
    desc: MetricDesc,
    buckets: Vec<f64>,
    values: RwLock<HistogramData>,
}

#[derive(Default)]
struct HistogramData {
    count: u64,
    sum: f64,
    bucket_counts: Vec<u64>,
}

impl Histogram {
    /// Creates a new histogram with default buckets.
    pub fn new(name: &str, help: &str) -> Self {
        Self::with_buckets(
            name,
            help,
            vec![0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0],
        )
    }

    /// Creates with custom buckets.
    pub fn with_buckets(name: &str, help: &str, buckets: Vec<f64>) -> Self {
        let bucket_counts = vec![0; buckets.len()];
        Self {
            desc: MetricDesc {
                name: name.to_string(),
                help: help.to_string(),
                metric_type: MetricType::Histogram,
                labels: vec![],
            },
            buckets,
            values: RwLock::new(HistogramData {
                count: 0,
                sum: 0.0,
                bucket_counts,
            }),
        }
    }

    /// Observes a value.
    pub fn observe(&self, v: f64) {
        let mut data = self.values.write();
        data.count += 1;
        data.sum += v;

        for (i, &bucket) in self.buckets.iter().enumerate() {
            if v <= bucket {
                data.bucket_counts[i] += 1;
            }
        }
    }

    /// Times a function and observes the duration.
    pub fn time<F, R>(&self, f: F) -> R
    where
        F: FnOnce() -> R,
    {
        let start = Instant::now();
        let result = f();
        self.observe(start.elapsed().as_secs_f64());
        result
    }

    /// Gets histogram value.
    pub fn value(&self) -> HistogramValue {
        let data = self.values.read();
        HistogramValue {
            count: data.count,
            sum: data.sum,
            buckets: self
                .buckets
                .iter()
                .zip(data.bucket_counts.iter())
                .map(|(&b, &c)| (b, c))
                .collect(),
        }
    }

    /// Gets the descriptor.
    pub fn desc(&self) -> &MetricDesc {
        &self.desc
    }
}

/// Metric registry.
pub struct Registry {
    counters: RwLock<HashMap<String, Arc<Counter>>>,
    gauges: RwLock<HashMap<String, Arc<Gauge>>>,
    histograms: RwLock<HashMap<String, Arc<Histogram>>>,
    prefix: String,
}

impl Registry {
    /// Creates a new registry.
    pub fn new(prefix: &str) -> Self {
        Self {
            counters: RwLock::new(HashMap::new()),
            gauges: RwLock::new(HashMap::new()),
            histograms: RwLock::new(HashMap::new()),
            prefix: prefix.to_string(),
        }
    }

    /// Registers a counter.
    pub fn register_counter(&self, name: &str, help: &str) -> Arc<Counter> {
        let full_name = format!("{}_{}", self.prefix, name);
        let counter = Arc::new(Counter::new(&full_name, help, vec![]));
        self.counters.write().insert(name.to_string(), counter.clone());
        counter
    }

    /// Registers a gauge.
    pub fn register_gauge(&self, name: &str, help: &str) -> Arc<Gauge> {
        let full_name = format!("{}_{}", self.prefix, name);
        let gauge = Arc::new(Gauge::new(&full_name, help, vec![]));
        self.gauges.write().insert(name.to_string(), gauge.clone());
        gauge
    }

    /// Registers a histogram.
    pub fn register_histogram(&self, name: &str, help: &str) -> Arc<Histogram> {
        let full_name = format!("{}_{}", self.prefix, name);
        let histogram = Arc::new(Histogram::new(&full_name, help));
        self.histograms.write().insert(name.to_string(), histogram.clone());
        histogram
    }

    /// Gets a counter by name.
    pub fn counter(&self, name: &str) -> Option<Arc<Counter>> {
        self.counters.read().get(name).cloned()
    }

    /// Gets a gauge by name.
    pub fn gauge(&self, name: &str) -> Option<Arc<Gauge>> {
        self.gauges.read().get(name).cloned()
    }

    /// Gets a histogram by name.
    pub fn histogram(&self, name: &str) -> Option<Arc<Histogram>> {
        self.histograms.read().get(name).cloned()
    }

    /// Exports all metrics in Prometheus format.
    pub fn export(&self) -> String {
        let mut output = String::new();

        // Export counters
        for (_, counter) in self.counters.read().iter() {
            output.push_str(&format!(
                "# HELP {} {}\n",
                counter.desc().name,
                counter.desc().help
            ));
            output.push_str(&format!("# TYPE {} counter\n", counter.desc().name));
            output.push_str(&format!("{} {}\n", counter.desc().name, counter.get()));
        }

        // Export gauges
        for (_, gauge) in self.gauges.read().iter() {
            output.push_str(&format!(
                "# HELP {} {}\n",
                gauge.desc().name,
                gauge.desc().help
            ));
            output.push_str(&format!("# TYPE {} gauge\n", gauge.desc().name));
            output.push_str(&format!("{} {}\n", gauge.desc().name, gauge.get()));
        }

        // Export histograms
        for (_, histogram) in self.histograms.read().iter() {
            let value = histogram.value();
            output.push_str(&format!(
                "# HELP {} {}\n",
                histogram.desc().name,
                histogram.desc().help
            ));
            output.push_str(&format!("# TYPE {} histogram\n", histogram.desc().name));

            let mut cumulative = 0u64;
            for (bucket, count) in &value.buckets {
                cumulative += count;
                output.push_str(&format!(
                    "{}_bucket{{le=\"{}\"}} {}\n",
                    histogram.desc().name,
                    bucket,
                    cumulative
                ));
            }
            output.push_str(&format!(
                "{}_bucket{{le=\"+Inf\"}} {}\n",
                histogram.desc().name,
                value.count
            ));
            output.push_str(&format!("{}_sum {}\n", histogram.desc().name, value.sum));
            output.push_str(&format!("{}_count {}\n", histogram.desc().name, value.count));
        }

        output
    }
}

impl Default for Registry {
    fn default() -> Self {
        Self::new("avalanche")
    }
}

/// Standard Avalanche metrics.
pub struct AvalancheMetrics {
    pub registry: Registry,

    // Consensus metrics
    pub polls_successful: Arc<Counter>,
    pub polls_failed: Arc<Counter>,
    pub blocks_accepted: Arc<Counter>,
    pub blocks_rejected: Arc<Counter>,
    pub consensus_latency: Arc<Histogram>,

    // Network metrics
    pub peers_connected: Arc<Gauge>,
    pub messages_sent: Arc<Counter>,
    pub messages_received: Arc<Counter>,
    pub bytes_sent: Arc<Counter>,
    pub bytes_received: Arc<Counter>,
    pub message_latency: Arc<Histogram>,

    // VM metrics
    pub txs_processed: Arc<Counter>,
    pub txs_rejected: Arc<Counter>,
    pub block_build_time: Arc<Histogram>,
    pub tx_execution_time: Arc<Histogram>,

    // Resource metrics
    pub db_reads: Arc<Counter>,
    pub db_writes: Arc<Counter>,
    pub db_size: Arc<Gauge>,
    pub memory_usage: Arc<Gauge>,
    pub goroutines: Arc<Gauge>,
}

impl AvalancheMetrics {
    /// Creates a new metrics collection.
    pub fn new() -> Self {
        let registry = Registry::new("avalanche");

        Self {
            // Consensus
            polls_successful: registry.register_counter(
                "consensus_polls_successful_total",
                "Total successful consensus polls",
            ),
            polls_failed: registry.register_counter(
                "consensus_polls_failed_total",
                "Total failed consensus polls",
            ),
            blocks_accepted: registry.register_counter(
                "consensus_blocks_accepted_total",
                "Total blocks accepted",
            ),
            blocks_rejected: registry.register_counter(
                "consensus_blocks_rejected_total",
                "Total blocks rejected",
            ),
            consensus_latency: registry.register_histogram(
                "consensus_latency_seconds",
                "Time to reach consensus on a block",
            ),

            // Network
            peers_connected: registry.register_gauge(
                "network_peers_connected",
                "Number of connected peers",
            ),
            messages_sent: registry.register_counter(
                "network_messages_sent_total",
                "Total messages sent",
            ),
            messages_received: registry.register_counter(
                "network_messages_received_total",
                "Total messages received",
            ),
            bytes_sent: registry.register_counter(
                "network_bytes_sent_total",
                "Total bytes sent",
            ),
            bytes_received: registry.register_counter(
                "network_bytes_received_total",
                "Total bytes received",
            ),
            message_latency: registry.register_histogram(
                "network_message_latency_seconds",
                "Message round-trip latency",
            ),

            // VM
            txs_processed: registry.register_counter(
                "vm_txs_processed_total",
                "Total transactions processed",
            ),
            txs_rejected: registry.register_counter(
                "vm_txs_rejected_total",
                "Total transactions rejected",
            ),
            block_build_time: registry.register_histogram(
                "vm_block_build_seconds",
                "Time to build a block",
            ),
            tx_execution_time: registry.register_histogram(
                "vm_tx_execution_seconds",
                "Transaction execution time",
            ),

            // Resources
            db_reads: registry.register_counter("db_reads_total", "Total database reads"),
            db_writes: registry.register_counter("db_writes_total", "Total database writes"),
            db_size: registry.register_gauge("db_size_bytes", "Database size in bytes"),
            memory_usage: registry.register_gauge("memory_usage_bytes", "Memory usage in bytes"),
            goroutines: registry.register_gauge("goroutines", "Number of active goroutines/tasks"),

            registry,
        }
    }

    /// Exports metrics in Prometheus format.
    pub fn export(&self) -> String {
        self.registry.export()
    }
}

impl Default for AvalancheMetrics {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_counter() {
        let counter = Counter::new("test_counter", "A test counter", vec![]);
        assert_eq!(counter.get(), 0);

        counter.inc();
        assert_eq!(counter.get(), 1);

        counter.inc_by(5);
        assert_eq!(counter.get(), 6);
    }

    #[test]
    fn test_gauge() {
        let gauge = Gauge::new("test_gauge", "A test gauge", vec![]);
        assert_eq!(gauge.get(), 0.0);

        gauge.set(10.0);
        assert_eq!(gauge.get(), 10.0);

        gauge.inc();
        assert_eq!(gauge.get(), 11.0);

        gauge.dec();
        assert_eq!(gauge.get(), 10.0);

        gauge.add(5.0);
        assert_eq!(gauge.get(), 15.0);

        gauge.sub(3.0);
        assert_eq!(gauge.get(), 12.0);
    }

    #[test]
    fn test_histogram() {
        let histogram = Histogram::new("test_histogram", "A test histogram");

        histogram.observe(0.001);
        histogram.observe(0.01);
        histogram.observe(0.1);
        histogram.observe(1.0);
        histogram.observe(5.0);

        let value = histogram.value();
        assert_eq!(value.count, 5);
        assert!((value.sum - 6.111).abs() < 0.001);
    }

    #[test]
    fn test_histogram_time() {
        let histogram = Histogram::new("test_timing", "Test timing");

        let result = histogram.time(|| {
            std::thread::sleep(Duration::from_millis(10));
            42
        });

        assert_eq!(result, 42);
        let value = histogram.value();
        assert_eq!(value.count, 1);
        assert!(value.sum >= 0.01);
    }

    #[test]
    fn test_registry() {
        let registry = Registry::new("test");

        let counter = registry.register_counter("requests", "Total requests");
        let gauge = registry.register_gauge("connections", "Active connections");
        let histogram = registry.register_histogram("latency", "Request latency");

        counter.inc();
        gauge.set(5.0);
        histogram.observe(0.1);

        assert_eq!(registry.counter("requests").unwrap().get(), 1);
        assert_eq!(registry.gauge("connections").unwrap().get(), 5.0);
    }

    #[test]
    fn test_prometheus_export() {
        let registry = Registry::new("test");

        let counter = registry.register_counter("requests", "Total requests");
        counter.inc_by(100);

        let gauge = registry.register_gauge("connections", "Active connections");
        gauge.set(42.0);

        let output = registry.export();

        assert!(output.contains("# HELP test_requests Total requests"));
        assert!(output.contains("# TYPE test_requests counter"));
        assert!(output.contains("test_requests 100"));

        assert!(output.contains("# HELP test_connections Active connections"));
        assert!(output.contains("# TYPE test_connections gauge"));
        assert!(output.contains("test_connections 42"));
    }

    #[test]
    fn test_avalanche_metrics() {
        let metrics = AvalancheMetrics::new();

        metrics.blocks_accepted.inc();
        metrics.peers_connected.set(10.0);
        metrics.consensus_latency.observe(0.5);

        assert_eq!(metrics.blocks_accepted.get(), 1);
        assert_eq!(metrics.peers_connected.get(), 10.0);

        let output = metrics.export();
        assert!(output.contains("avalanche_consensus_blocks_accepted_total"));
        assert!(output.contains("avalanche_network_peers_connected"));
    }

    #[test]
    fn test_counter_with_labels() {
        let counter = Counter::new("http_requests", "HTTP requests", vec!["method", "status"]);

        counter.with_labels(vec!["GET", "200"]).inc();
        counter.with_labels(vec!["POST", "201"]).inc();
        counter.with_labels(vec!["GET", "200"]).inc();

        // Note: The base get() returns unlabeled value
        // In a full implementation, we'd have methods to query by labels
    }
}
