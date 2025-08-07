// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![doc = include_str!("../README.md")]

use std::path::PathBuf;

use clap::{Parser, Subcommand};
use firewood::v2::api;

pub mod check;
pub mod create;
pub mod delete;
pub mod dump;
pub mod get;
pub mod graph;
pub mod insert;
pub mod root;

#[derive(Clone, Debug, Parser)]
pub struct DatabasePath {
    /// The database path. Defaults to firewood.db
    #[arg(
        long = "db",
        short = 'd',
        required = false,
        value_name = "DB_NAME",
        default_value_os_t = default_db_path(),
        help = "Name of the database"
    )]
    pub dbpath: PathBuf,
}

#[derive(Parser)]
#[command(author, version, about, long_about = None)]
#[command(propagate_version = true)]
#[command(version = concat!(env!("CARGO_PKG_VERSION"), " (", env!("GIT_COMMIT_SHA"), ", ", env!("ETHHASH_FEATURE"), ")"))]
struct Cli {
    #[command(subcommand)]
    command: Commands,
    #[arg(
        long,
        short = 'l',
        required = false,
        help = "Log level. Respects RUST_LOG.",
        value_name = "LOG_LEVEL",
        num_args = 1,
        value_parser = ["debug", "info"],
        default_value_t = String::from("info"),
    )]
    log_level: String,
}

#[derive(Subcommand)]
enum Commands {
    /// Create a new firewood database
    Create(create::Options),
    /// Insert a key/value pair into the database
    Insert(insert::Options),
    /// Get values associated with a key
    Get(get::Options),
    /// Delete values associated with a key
    Delete(delete::Options),
    /// Display key/value trie root hash
    Root(root::Options),
    /// Dump contents of key/value store
    Dump(dump::Options),
    /// Produce a dot file of the database
    Graph(graph::Options),
    /// Runs the checker on the database
    Check(check::Options),
}

#[tokio::main]
async fn main() -> Result<(), api::Error> {
    let cli = Cli::parse();

    env_logger::init_from_env(
        env_logger::Env::default()
            .filter_or(env_logger::DEFAULT_FILTER_ENV, cli.log_level.to_string()),
    );

    match &cli.command {
        Commands::Create(opts) => create::run(opts).await,
        Commands::Insert(opts) => insert::run(opts).await,
        Commands::Get(opts) => get::run(opts).await,
        Commands::Delete(opts) => delete::run(opts).await,
        Commands::Root(opts) => root::run(opts).await,
        Commands::Dump(opts) => dump::run(opts).await,
        Commands::Graph(opts) => graph::run(opts).await,
        Commands::Check(opts) => check::run(opts).await,
    }
}

fn default_db_path() -> PathBuf {
    PathBuf::from("firewood.db")
}
