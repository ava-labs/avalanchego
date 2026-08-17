// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::api::{self, Db as _, Proposal as _};
use firewood::db::{BatchOp, Db, DbConfig};
use humantime::{format_duration, parse_duration};
use std::fs::File;
use std::path::PathBuf;
use std::time::{Duration, Instant};

use crate::DatabasePath;

struct ImportState {
    batch_ops: Vec<BatchOp<Vec<u8>, Vec<u8>>>,
    total_imported: usize,
    total_skipped: usize,
    last_status_time: Instant,
}

#[derive(Debug, clap::ValueEnum, Clone, PartialEq)]
pub enum InputFormat {
    Csv,
}

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    /// The input format of database dump.
    /// Defaults to "csv"
    #[arg(
        short = 'i',
        long,
        required = false,
        value_name = "INPUT_FORMAT",
        value_enum,
        default_value_t = InputFormat::Csv,
        help = "Input format of database dump, default to csv."
    )]
    pub input_format: InputFormat,

    /// The input file name of database dump.
    #[arg(
        short = 'f',
        long,
        required = false,
        value_name = "INPUT_FILE_NAME",
        help = "Input file name of database dump. Reads from stdin if not provided."
    )]
    pub input_file_name: Option<PathBuf>,

    /// Parse the keys and values as hex format.
    #[arg(short = 'x', long, help = "Parse the keys and values as hex format.")]
    pub hex: bool,

    /// Number of records to batch per database commit.
    #[arg(
        short = 'b',
        long,
        default_value_t = 10000,
        help = "Number of records to batch per database commit."
    )]
    pub batch_size: usize,

    /// Time interval between status updates.
    #[arg(
        long,
        default_value = "1m",
        value_parser = parse_duration,
        help = "Time interval between status updates (e.g., 1m, 30s)."
    )]
    pub status_interval: Duration,
}

pub(super) fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("import database {opts:?}");

    let cfg = DbConfig::builder()
        .node_hash_algorithm(opts.database.node_hash_algorithm.into())
        .create_if_missing(true)
        .truncate(false);

    let db = Db::new(opts.database.dbpath.clone(), cfg.build())?;

    let reader: Box<dyn std::io::Read> = if let Some(path) = &opts.input_file_name {
        Box::new(File::open(path)?)
    } else {
        Box::new(std::io::stdin())
    };

    let start_time = Instant::now();
    let mut state = ImportState {
        batch_ops: Vec::with_capacity(opts.batch_size),
        total_imported: 0,
        total_skipped: 0,
        last_status_time: start_time,
    };

    match opts.input_format {
        InputFormat::Csv => {
            process_csv(opts, reader, &db, &mut state, start_time)?;
        }
    }

    // Insert remaining ops
    if !state.batch_ops.is_empty() {
        commit_batch(&mut state.batch_ops, &db, &mut state.total_imported)?;
    }

    let total_duration = start_time.elapsed();
    let final_rate = keys_per_second(state.total_imported, total_duration);
    let formatted_total_duration = format_duration(Duration::from_secs(total_duration.as_secs()));

    let final_msg = format!(
        "Successfully imported {} keys (skipped {} malformed rows) in {formatted_total_duration} ({final_rate} keys/s)",
        state.total_imported, state.total_skipped
    );
    println!("{final_msg}");

    db.close()
}

fn parse_string(s: &str, hex: bool) -> Result<Vec<u8>, api::Error> {
    if hex {
        hex::decode(s).map_err(|e| {
            api::Error::InternalError(Box::new(std::io::Error::new(
                std::io::ErrorKind::InvalidInput,
                e,
            )))
        })
    } else {
        Ok(s.as_bytes().to_vec())
    }
}

fn process_csv(
    opts: &Options,
    reader: Box<dyn std::io::Read>,
    db: &Db,
    state: &mut ImportState,
    start_time: Instant,
) -> Result<(), api::Error> {
    const MAX_CONSECUTIVE_ERRORS: usize = 100;

    let mut rdr = csv::ReaderBuilder::new()
        .has_headers(false)
        .from_reader(reader);

    let mut consecutive_errors = 0;

    let handle_skip_error =
        |consecutive_errors: &mut usize, total_skipped: &mut usize| -> Result<(), api::Error> {
            *consecutive_errors = consecutive_errors.saturating_add(1);
            *total_skipped = total_skipped.saturating_add(1);
            if *consecutive_errors > MAX_CONSECUTIVE_ERRORS {
                return Err(api::Error::InternalError(Box::new(std::io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    "Too many consecutive malformed rows. Aborting import.",
                ))));
            }
            Ok(())
        };

    for (row_idx, result) in rdr.deserialize().enumerate() {
        let display_row_idx = row_idx.saturating_add(1);
        let (key_str, value_str): (String, String) = match result {
            Ok(r) => r,
            Err(e) => {
                handle_skip_error(&mut consecutive_errors, &mut state.total_skipped)?;
                log::warn!("Skipping malformed CSV row {display_row_idx}: {e}");
                continue;
            }
        };

        let key = match parse_string(&key_str, opts.hex) {
            Ok(k) => k,
            Err(e) => {
                handle_skip_error(&mut consecutive_errors, &mut state.total_skipped)?;
                log::warn!("Skipping row {display_row_idx}: failed to parse key: {e}");
                continue;
            }
        };
        let value = match parse_string(&value_str, opts.hex) {
            Ok(v) => v,
            Err(e) => {
                handle_skip_error(&mut consecutive_errors, &mut state.total_skipped)?;
                log::warn!("Skipping row {display_row_idx}: failed to parse value: {e}");
                continue;
            }
        };

        consecutive_errors = 0;
        state.batch_ops.push(BatchOp::Put { key, value });

        if state.batch_ops.len() >= opts.batch_size {
            commit_batch(&mut state.batch_ops, db, &mut state.total_imported)?;
            maybe_log_status(
                state.total_imported,
                start_time,
                &mut state.last_status_time,
                opts.status_interval,
            );
        }
    }

    Ok(())
}

#[allow(clippy::cast_precision_loss, clippy::cast_sign_loss)]
fn maybe_log_status(
    total_imported: usize,
    start_time: Instant,
    last_status_time: &mut Instant,
    status_interval: Duration,
) {
    let now = Instant::now();
    if now.duration_since(*last_status_time) >= status_interval {
        let duration = now.duration_since(start_time);
        let duration_secs = duration.as_secs_f64();
        let rate = if duration_secs > 0.0 {
            (total_imported as f64 / duration_secs) as usize
        } else {
            0
        };
        let formatted_duration = format_duration(Duration::from_secs(duration.as_secs()));
        log::info!("{total_imported} keys in {formatted_duration} ({rate} keys/s)");
        *last_status_time = now;
    }
}

fn commit_batch(
    batch_ops: &mut Vec<BatchOp<Vec<u8>, Vec<u8>>>,
    db: &Db,
    total_imported: &mut usize,
) -> Result<(), api::Error> {
    let remaining = batch_ops.len();
    let proposal = db.propose(batch_ops.drain(..))?;
    proposal.commit()?;
    // Use wrapping_add because overflow is practically unreachable here
    *total_imported = total_imported.wrapping_add(remaining);
    Ok(())
}

/// Computes keys-per-second throughput.
///
/// `total_imported` realistically never exceeds a few billion keys in a single
/// import run, well within `f64`'s 52-bit mantissa precision, and is always
/// non-negative — so the precision/sign-loss lints are safe to suppress here.
#[allow(clippy::cast_precision_loss, clippy::cast_sign_loss)]
fn keys_per_second(total_imported: usize, duration: Duration) -> usize {
    let secs = duration.as_secs_f64();
    if secs > 0.0 {
        (total_imported as f64 / secs) as usize
    } else {
        0
    }
}
