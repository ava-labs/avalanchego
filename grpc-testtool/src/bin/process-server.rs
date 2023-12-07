// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use chrono::Local;
use clap::Parser;
use env_logger::{Builder, Target};
use log::{info, LevelFilter};
use rpc::{
    process_server::process_server_service_server::ProcessServerServiceServer,
    rpcdb::database_server::DatabaseServer as RpcServer, sync::db_server::DbServer as SyncServer,
    DatabaseService,
};
use std::{
    error::Error,
    io::Write,
    net::{IpAddr::V4, Ipv4Addr},
    path::PathBuf,
    sync::Arc,
};
use tempdir::TempDir;
use tonic::transport::Server;

/// A GRPC server that can be plugged into the generic testing framework for merkledb

#[derive(Debug, Parser)]
#[command(author, version, about, long_about = None)]
struct Opts {
    #[arg(short = 'g', long, default_value_t = 10000)]
    //// Port gRPC server listens on
    grpc_port: u16,

    #[arg(short = 'G', long, default_value_t = 10001)]
    /// Port gRPC gateway server, which HTTP requests can be made to
    _gateway_port: u16,

    #[arg(short = 'l', long, default_value = temp_path())]
    log_dir: PathBuf,

    #[arg(short = 'L', long, default_value_t = LevelFilter::Info)]
    log_level: LevelFilter,

    #[arg(short = 'b', long)]
    _branch_factor: Option<u16>,

    #[arg(short = 'p', long)]
    _process_name: String,

    #[arg(short = 'd')]
    db_dir: PathBuf,

    #[arg(short = 'H')]
    _history_length: Option<u32>,

    #[arg(short = 'N')]
    _node_cache_size: Option<u32>,
}

fn temp_path() -> clap::builder::OsStr {
    let tmpdir = TempDir::new("process-server").expect("unable to create temporary directory");
    // we leak because we want the temporary directory to stick around forever (emulating what happens in golang)
    Box::leak(Box::new(tmpdir.into_path())).as_os_str().into()
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    // parse command line options
    let args = Opts::parse();

    match std::fs::create_dir_all(&args.log_dir) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
        Err(e) => panic!("Unable to create directory\n{}", e),
    }

    // set up the log file
    let logfile = std::fs::OpenOptions::new()
        .create(true)
        .write(true)
        .append(true)
        .open(args.log_dir.join("server.log"))?;

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
        .target(Target::Pipe(Box::new(logfile)))
        .filter(None, args.log_level)
        .init();

    log_panics::Config::new()
        .backtrace_mode(log_panics::BacktraceMode::Unresolved)
        .install_panic_hook();

    // log to the file and to stderr
    eprintln!("Database-Server listening on: {}", args.grpc_port);
    info!("Starting up: Listening on {}", args.grpc_port);

    let svc = Arc::new(DatabaseService::default());

    // TODO: graceful shutdown
    Ok(Server::builder()
        .add_service(RpcServer::from_arc(svc.clone()))
        .add_service(SyncServer::from_arc(svc.clone()))
        .add_service(ProcessServerServiceServer::from_arc(svc.clone()))
        .serve(std::net::SocketAddr::new(
            V4(Ipv4Addr::LOCALHOST),
            args.grpc_port,
        ))
        .await?)
}
