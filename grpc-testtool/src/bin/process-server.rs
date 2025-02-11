// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use chrono::Local;
use clap::Parser;
use env_logger::Builder;
use log::{info, LevelFilter};
use rpc::process_server::process_server_service_server::ProcessServerServiceServer;
use rpc::rpcdb::database_server::DatabaseServer as RpcServer;
use rpc::sync::db_server::DbServer as SyncServer;
use rpc::DatabaseService;
use serde::Deserialize;
use std::error::Error;
use std::io::Write;
use std::net::IpAddr::V4;
use std::net::Ipv4Addr;
use std::path::PathBuf;
use std::str::FromStr;
use std::sync::Arc;
use tonic::transport::Server;

#[derive(Clone, Debug, Deserialize)]
struct Options {
    #[serde(default = "Options::history_length_default")]
    history_length: u32,
}

impl Options {
    // used in two cases:
    //  serde deserializes Options and there was no history_length
    // OR
    //  Options was not present
    const fn history_length_default() -> u32 {
        100
    }
}

impl FromStr for Options {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        serde_json::from_str(s).map_err(|e| format!("error parsing options: {}", e))
    }
}

/// A GRPC server that can be plugged into the generic testing framework for merkledb

#[derive(Debug, Parser)]
#[command(author, version, about, long_about = None)]
struct Opts {
    #[arg(short, long)]
    //// Port gRPC server listens on
    grpc_port: u16,

    #[arg(short, long)]
    db_dir: PathBuf,

    #[arg(short, long, default_value_t = LevelFilter::Info)]
    log_level: LevelFilter,

    #[arg(short, long)]
    config: Option<Options>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    // parse command line options
    let args = Opts::parse();

    // configure the logger
    Builder::new()
        .format(|buf, record| {
            writeln!(
                buf,
                "{} [{}] - {}",
                Local::now().format("%Y-%m-%dT%H:%M:%S"),
                record.level(),
                record.args()
            )
        })
        .format_target(true)
        .filter(None, args.log_level)
        .init();

    // tracing_subscriber::fmt::init();

    // log to the file and to stderr
    info!("Starting up: Listening on {}", args.grpc_port);

    let svc = Arc::new(
        DatabaseService::new(
            args.db_dir,
            args.config
                .map(|o| o.history_length)
                .unwrap_or_else(Options::history_length_default),
        )
        .await?,
    );

    // TODO: graceful shutdown
    Ok(Server::builder()
        .trace_fn(|_m| tracing::debug_span!("process-server"))
        .add_service(RpcServer::from_arc(svc.clone()))
        .add_service(SyncServer::from_arc(svc.clone()))
        .add_service(ProcessServerServiceServer::from_arc(svc.clone()))
        .serve(std::net::SocketAddr::new(
            V4(Ipv4Addr::LOCALHOST),
            args.grpc_port,
        ))
        .await?)
}
