// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Replay command for replaying recorded database operations.

use std::path::PathBuf;
use std::time::Instant;

use clap::Args;
use firewood::api::{self, Db as DbApi};
use firewood::db::{Db, DbConfig};
use firewood_replay::replay_from_file;

use crate::DatabasePath;

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    /// Path to the replay log file.
    #[arg(
        short = 'r',
        long,
        required = true,
        value_name = "REPLAY_LOG",
        help = "Path to the replay log file containing recorded operations"
    )]
    pub replay_log: PathBuf,

    /// Maximum number of commits to replay (default: all).
    #[arg(
        short = 'm',
        long,
        required = false,
        value_name = "MAX_COMMITS",
        help = "Maximum number of commits to replay"
    )]
    pub max_commits: Option<u64>,

    /// Truncate the database before replaying (default: true for new databases).
    #[arg(
        long,
        required = false,
        default_value_t = true,
        value_name = "TRUNCATE",
        help = "Truncate the database before replaying"
    )]
    pub truncate: bool,
}

pub(super) fn run(opts: &Options) -> Result<(), api::Error> {
    log::info!(
        "Replaying {} to {}",
        opts.replay_log.display(),
        opts.database.dbpath.display()
    );

    let cfg = DbConfig::builder()
        .node_hash_algorithm(opts.database.node_hash_algorithm.into())
        .truncate(opts.truncate)
        .build();
    let db = Db::new(opts.database.dbpath.clone(), cfg)?;

    let start = Instant::now();

    let result = replay_from_file(&opts.replay_log, &db, opts.max_commits).map_err(|e| {
        api::Error::InternalError(Box::new(std::io::Error::other(format!(
            "replay failed: {e}"
        ))))
    })?;

    let elapsed = start.elapsed();

    if let Some(hash) = result {
        println!("Replay completed in {elapsed:.2?}");
        println!("Final root hash: {}", hex::encode(&hash));
    } else {
        println!("Replay completed in {elapsed:.2?} (no commits)");
    }

    // Print the root hash from the database for verification
    if let Some(root) = DbApi::root_hash(&db) {
        println!("Database root: {}", hex::encode(root));
    }

    db.close()
}
