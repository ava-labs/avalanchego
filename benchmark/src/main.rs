// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.
//

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 2 occurrences after enabling the lint."
)]
#![expect(
    clippy::match_same_arms,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![doc = include_str!("../README.md")]

use clap::{Parser, Subcommand, ValueEnum};
use fastrace_opentelemetry::OpenTelemetryReporter;
use firewood::logger::trace;
use log::LevelFilter;
use metrics_exporter_prometheus::PrometheusBuilder;
use metrics_util::MetricKindMask;
use sha2::{Digest, Sha256};
use std::borrow::Cow;
use std::error::Error;
use std::fmt::Display;
use std::net::{Ipv6Addr, SocketAddr};
use std::num::NonZeroUsize;
use std::path::PathBuf;
use std::time::Duration;

use firewood::db::{BatchOp, Db, DbConfig};
use firewood::manager::{CacheReadStrategy, RevisionManagerConfig};

use fastrace::collector::Config;

use opentelemetry::InstrumentationScope;
use opentelemetry_otlp::{SpanExporter, WithExportConfig};
use opentelemetry_sdk::Resource;

#[derive(Parser, Debug)]
struct Args {
    #[clap(flatten)]
    global_opts: GlobalOpts,

    #[clap(subcommand)]
    test_name: TestName,
}

#[derive(clap::Args, Debug)]
struct GlobalOpts {
    #[arg(
        short = 'e',
        long,
        default_value_t = false,
        help = "Enable telemetry server reporting"
    )]
    telemetry_server: bool,
    #[arg(short, long, default_value_t = 10000)]
    batch_size: u64,
    #[arg(short, long, default_value_t = 1000)]
    number_of_batches: u64,
    #[arg(short, long, default_value_t = NonZeroUsize::new(1500000).expect("is non-zero"))]
    cache_size: NonZeroUsize,
    #[arg(short, long, default_value_t = 128)]
    revisions: usize,
    #[arg(
        short = 'p',
        long,
        default_value_t = 3000,
        help = "Port to listen for prometheus"
    )]
    prometheus_port: u16,
    #[arg(
        short = 's',
        long,
        default_value_t = false,
        help = "Dump prometheus stats on exit"
    )]
    stats_dump: bool,

    #[arg(
        long,
        short = 'l',
        required = false,
        help = "Log level. Respects RUST_LOG.",
        value_name = "LOG_LEVEL",
        num_args = 1,
        value_parser = ["trace", "debug", "info", "warn", "none"],
        default_value_t = String::from("info"),
    )]
    log_level: String,
    #[arg(
        long,
        short = 'd',
        required = false,
        help = "Use this database name instead of the default",
        default_value = PathBuf::from("benchmark_db").into_os_string(),
    )]
    dbname: PathBuf,
    #[arg(
        long,
        short = 't',
        required = false,
        help = "Terminate the test after this many minutes",
        default_value_t = 65
    )]
    duration_minutes: u64,
    #[arg(
        long,
        short = 'C',
        required = false,
        help = "Read cache strategy",
        default_value_t = ArgCacheReadStrategy::WritesOnly
    )]
    cache_read_strategy: ArgCacheReadStrategy,
}
#[derive(Debug, PartialEq, ValueEnum, Clone)]
pub enum ArgCacheReadStrategy {
    WritesOnly,
    BranchReads,
    All,
}
impl Display for ArgCacheReadStrategy {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ArgCacheReadStrategy::WritesOnly => write!(f, "writes-only"),
            ArgCacheReadStrategy::BranchReads => write!(f, "branch-reads"),
            ArgCacheReadStrategy::All => write!(f, "all"),
        }
    }
}
impl From<ArgCacheReadStrategy> for CacheReadStrategy {
    fn from(arg: ArgCacheReadStrategy) -> Self {
        match arg {
            ArgCacheReadStrategy::WritesOnly => CacheReadStrategy::WritesOnly,
            ArgCacheReadStrategy::BranchReads => CacheReadStrategy::BranchReads,
            ArgCacheReadStrategy::All => CacheReadStrategy::All,
        }
    }
}

mod create;
mod single;
mod tenkrandom;
mod zipf;

#[derive(Debug, Subcommand, PartialEq)]
enum TestName {
    /// Create a database
    Create,

    /// Insert batches of random keys
    TenKRandom,

    /// Insert batches of keys following a Zipf distribution
    Zipf(zipf::Args),

    /// Repeatedly update a single row
    Single,
}

trait TestRunner {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>>;

    fn generate_inserts(
        start: u64,
        count: u64,
    ) -> impl Iterator<Item = BatchOp<Box<[u8]>, Box<[u8]>>> {
        (start..start + count)
            .map(|inner_key| {
                let digest: Box<[u8]> = Sha256::digest(inner_key.to_ne_bytes())[..].into();
                trace!(
                    "inserting {:?} with digest {}",
                    inner_key,
                    hex::encode(&digest),
                );
                (digest.clone(), digest)
            })
            .map(|(key, value)| BatchOp::Put { key, value })
            .collect::<Vec<_>>()
            .into_iter()
    }
}

#[global_allocator]
#[cfg(unix)]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

#[tokio::main(flavor = "multi_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let args = Args::parse();

    if args.global_opts.telemetry_server {
        let reporter = OpenTelemetryReporter::new(
            SpanExporter::builder()
                .with_tonic()
                .with_endpoint("http://127.0.0.1:4317".to_string())
                .with_protocol(opentelemetry_otlp::Protocol::Grpc)
                .with_timeout(opentelemetry_otlp::OTEL_EXPORTER_OTLP_TIMEOUT_DEFAULT)
                .build()
                .expect("initialize oltp exporter"),
            Cow::Owned(
                Resource::builder()
                    .with_service_name("avalabs.firewood.benchmark")
                    .build(),
            ),
            InstrumentationScope::builder("firewood")
                .with_version(env!("CARGO_PKG_VERSION"))
                .build(),
        );
        fastrace::set_reporter(reporter, Config::default());
    }

    assert!(
        !(args.test_name == TestName::Single && args.global_opts.batch_size > 1000),
        "Single test is not designed to handle batch sizes > 1000"
    );

    env_logger::Builder::new()
        .filter_level(match args.global_opts.log_level.as_str() {
            "debug" => LevelFilter::Debug,
            "info" => LevelFilter::Info,
            "trace" => LevelFilter::Trace,
            "none" => LevelFilter::Off,
            _ => LevelFilter::Info,
        })
        .init();

    // Manually set up prometheus
    let builder = PrometheusBuilder::new();
    let (prometheus_recorder, listener_future) = builder
        .with_http_listener(SocketAddr::new(
            Ipv6Addr::UNSPECIFIED.into(),
            args.global_opts.prometheus_port,
        ))
        .idle_timeout(
            MetricKindMask::COUNTER | MetricKindMask::HISTOGRAM,
            Some(Duration::from_secs(10)),
        )
        .build()
        .expect("unable in run prometheusbuilder");

    // Clone the handle so we can dump the stats at the end
    let prometheus_handle = prometheus_recorder.handle();
    metrics::set_global_recorder(prometheus_recorder)?;
    tokio::spawn(listener_future);

    let mgrcfg = RevisionManagerConfig::builder()
        .node_cache_size(args.global_opts.cache_size)
        .free_list_cache_size(
            NonZeroUsize::new(4 * args.global_opts.batch_size as usize).expect("batch size > 0"),
        )
        .cache_read_strategy(args.global_opts.cache_read_strategy.clone().into())
        .max_revisions(args.global_opts.revisions)
        .build();
    let cfg = DbConfig::builder()
        .truncate(matches!(args.test_name, TestName::Create))
        .manager(mgrcfg)
        .build();

    let db = Db::new(args.global_opts.dbname.clone(), cfg).expect("db initiation should succeed");

    match args.test_name {
        TestName::Create => {
            let runner = create::Create;
            runner.run(&db, &args).await?;
        }
        TestName::TenKRandom => {
            let runner = tenkrandom::TenKRandom;
            runner.run(&db, &args).await?;
        }
        TestName::Zipf(_) => {
            let runner = zipf::Zipf;
            runner.run(&db, &args).await?;
        }
        TestName::Single => {
            let runner = single::Single;
            runner.run(&db, &args).await?;
        }
    }

    if args.global_opts.stats_dump {
        println!("{}", prometheus_handle.render());
    }

    fastrace::flush();

    Ok(())
}
