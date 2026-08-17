// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use metrics_exporter_prometheus::render;

use super::Maybe;
use super::owned::{OwnedBytes, OwnedSlice};

pub(crate) trait MapIntoCollection<T>: IntoIterator {
    fn map_into<U, I>(self) -> I
    where
        Self: Sized,
        Self::Item: Into<U>,
        I: FromIterator<U>,
    {
        self.into_iter().map(Into::into).collect()
    }
}

impl<C> MapIntoCollection<C::Item> for C where C: IntoIterator {}

/// A C-compatible representation of a label pair.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedLabelPair {
    pub label: OwnedBytes,
    pub value: OwnedBytes,
}

/// A C-compatible representation of a quantile.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedQuantile {
    pub quantile: f64,
    pub value: f64,
}

/// A C-compatible representation of a summary metric.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedSummary {
    pub sample_count: u64,
    pub sample_sum: f64,
    pub quantiles: OwnedSlice<OwnedQuantile>,
}

/// A C-compatible representation of a histogram bucket.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedBucket {
    pub cumulative_count: u64,
    pub upper_bound: f64,
}

/// A C-compatible representation of a classic histogram metric.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedClassicHistogram {
    pub sample_count: u64,
    pub sample_sum: f64,
    pub buckets: OwnedSlice<OwnedBucket>,
}

/// A C-compatible representation of a native histogram bucket span.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedBucketSpan {
    pub offset: i32,
    pub length: u32,
}

/// A C-compatible representation of a native histogram metric.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedNativeHistogram {
    pub sample_count: u64,
    pub sample_sum: f64,
    pub zero_threshold: f64,
    pub schema: i32,
    pub zero_count: u64,
    pub positive_spans: OwnedSlice<OwnedBucketSpan>,
    pub positive_deltas: OwnedSlice<i64>,
    pub negative_spans: OwnedSlice<OwnedBucketSpan>,
    pub negative_deltas: OwnedSlice<i64>,
}

/// A C-compatible tagged union representing a metric value.
#[derive(Debug)]
#[repr(C, usize)]
pub enum OwnedMetricValue {
    Counter(u64),
    Gauge(f64),
    Summary(OwnedSummary),
    ClassicHistogram(OwnedClassicHistogram),
    NativeHistogram(OwnedNativeHistogram),
    Unknown, // For forward compatibility if new metric types are added
}

/// A C-compatible representation of a single metric with its labels and value.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedMetric {
    pub labels: OwnedSlice<OwnedLabelPair>,
    pub value: OwnedMetricValue,
}

/// A C-compatible representation of a metric family.
#[derive(Debug)]
#[repr(C)]
pub struct OwnedMetricFamily {
    pub name: OwnedBytes,
    pub help: Maybe<OwnedBytes>,
    pub metrics: OwnedSlice<OwnedMetric>,
}

/// A C-compatible representation of rendered metrics.
pub type OwnedRenderedMetrics = OwnedSlice<OwnedMetricFamily>;

// --- From conversions ---

impl From<render::LabelPair> for OwnedLabelPair {
    fn from(lp: render::LabelPair) -> Self {
        Self {
            label: lp.label.into(),
            value: lp.value.into(),
        }
    }
}

impl From<render::Quantile> for OwnedQuantile {
    fn from(q: render::Quantile) -> Self {
        Self {
            quantile: q.quantile,
            value: q.value,
        }
    }
}

impl From<render::Summary> for OwnedSummary {
    fn from(s: render::Summary) -> Self {
        Self {
            sample_count: s.sample_count,
            sample_sum: s.sample_sum,
            quantiles: s.quantiles.map_into(),
        }
    }
}

impl From<render::Bucket> for OwnedBucket {
    fn from(b: render::Bucket) -> Self {
        Self {
            cumulative_count: b.cumulative_count,
            upper_bound: b.upper_bound,
        }
    }
}

impl From<render::ClassicHistogram> for OwnedClassicHistogram {
    fn from(h: render::ClassicHistogram) -> Self {
        Self {
            sample_count: h.sample_count,
            sample_sum: h.sample_sum,
            buckets: h.buckets.map_into(),
        }
    }
}

impl From<render::BucketSpan> for OwnedBucketSpan {
    fn from(bs: render::BucketSpan) -> Self {
        Self {
            offset: bs.offset,
            length: bs.length,
        }
    }
}

impl From<render::NativeHistogram> for OwnedNativeHistogram {
    fn from(h: render::NativeHistogram) -> Self {
        Self {
            sample_count: h.sample_count,
            sample_sum: h.sample_sum,
            zero_threshold: h.zero_threshold,
            schema: h.schema,
            zero_count: h.zero_count,
            positive_spans: h.positive_spans.map_into(),
            positive_deltas: h.positive_deltas.into(),
            negative_spans: h.negative_spans.map_into(),
            negative_deltas: h.negative_deltas.into(),
        }
    }
}

impl From<render::MetricValue> for OwnedMetricValue {
    fn from(mv: render::MetricValue) -> Self {
        match mv {
            render::MetricValue::Counter(v) => Self::Counter(v),
            render::MetricValue::Gauge(v) => Self::Gauge(v),
            render::MetricValue::Summary(v) => Self::Summary(v.into()),
            render::MetricValue::ClassicHistogram(v) => Self::ClassicHistogram(v.into()),
            render::MetricValue::NativeHistogram(v) => Self::NativeHistogram(v.into()),
            _ => Self::Unknown, // For forward compatibility if new metric types are added
        }
    }
}

impl From<render::Metric> for OwnedMetric {
    fn from(m: render::Metric) -> Self {
        Self {
            labels: m.labels.map_into(),
            value: m.value.into(),
        }
    }
}

impl From<render::MetricFamily> for OwnedMetricFamily {
    fn from(mf: render::MetricFamily) -> Self {
        Self {
            name: mf.name.into(),
            help: mf.help.map(Into::into).into(),
            metrics: mf.metrics.map_into(),
        }
    }
}
