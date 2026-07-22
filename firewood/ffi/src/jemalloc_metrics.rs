// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Jemalloc memory allocator metrics.
//!
//! Reads jemalloc stats via `tikv-jemalloc-ctl` and publishes them as gauges
//! through the `metrics` crate, mirroring the same stats that `metriki-jemalloc`
//! exposes: active, allocated, metadata, mapped, resident, retained.

use firewood_metrics::GaugeExt;
use firewood_storage::logger;
use metrics::describe_gauge;
use tikv_jemalloc_ctl::{epoch, stats};

/// A jemalloc stat to expose as a gauge metric.
struct JemallocStat {
    name: &'static str,
    description: &'static str,
    read: fn() -> tikv_jemalloc_ctl::Result<usize>,
}

static JEMALLOC_STATS: &[JemallocStat] = &[
    JemallocStat {
        name: "jemalloc_active_bytes",
        description: "Bytes in active pages allocated by jemalloc",
        read: stats::active::read,
    },
    JemallocStat {
        name: "jemalloc_allocated_bytes",
        description: "Total bytes allocated by the application via jemalloc",
        read: stats::allocated::read,
    },
    JemallocStat {
        name: "jemalloc_metadata_bytes",
        description: "Bytes of jemalloc internal metadata overhead",
        read: stats::metadata::read,
    },
    JemallocStat {
        name: "jemalloc_mapped_bytes",
        description: "Bytes in active extents mapped by the allocator",
        read: stats::mapped::read,
    },
    JemallocStat {
        name: "jemalloc_resident_bytes",
        description: "Bytes in physically resident data pages mapped by jemalloc",
        read: stats::resident::read,
    },
    JemallocStat {
        name: "jemalloc_retained_bytes",
        description: "Bytes in virtual memory mappings retained by jemalloc",
        read: stats::retained::read,
    },
];

/// Registers all jemalloc metric descriptions.
pub fn register() {
    for stat in JEMALLOC_STATS {
        describe_gauge!(stat.name, stat.description);
    }
}

/// Advances the jemalloc epoch and updates all gauge metrics with current values.
///
/// Call this before gathering metrics to ensure fresh data. Errors reading
/// individual stats are silently ignored (the gauge simply won't update).
pub fn refresh() {
    // Advance the epoch so jemalloc refreshes its cached stats.
    if let Err(e) = epoch::advance() {
        logger::warn!("jemalloc epoch advance failed: {e}");
        return;
    }

    for stat in JEMALLOC_STATS {
        match (stat.read)() {
            Ok(v) => ::metrics::gauge!(stat.name).set_integer(v),
            Err(e) => {
                logger::warn!("failed to read jemalloc stat {}: {e}", stat.name);
            }
        }
    }
}
