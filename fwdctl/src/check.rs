// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::path::PathBuf;
use std::sync::Arc;

use clap::Args;
use firewood::v2::api;
use firewood_storage::{CacheReadStrategy, CheckOpt, CheckerReport, FileBacked, NodeStore};
use indicatif::{ProgressBar, ProgressFinish, ProgressStyle};
use nonzero_ext::nonzero;

use crate::DatabasePath;

// TODO: (optionally) add a fix option
#[derive(Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    /// Whether to perform hash check
    #[arg(
        long,
        required = false,
        default_value_t = false,
        help = "Should perform hash check"
    )]
    pub hash_check: bool,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    let db_path = PathBuf::from(&opts.database.dbpath);
    let node_cache_size = nonzero!(1usize);
    let free_list_cache_size = nonzero!(1usize);

    let fb = FileBacked::new(
        db_path,
        node_cache_size,
        free_list_cache_size,
        false,
        false,                         // don't create if missing
        CacheReadStrategy::WritesOnly, // we scan the database once - no need to cache anything
    )?;
    let storage = Arc::new(fb);

    let progress_bar = ProgressBar::no_length()
        .with_style(
            ProgressStyle::with_template("{wide_bar} {bytes}/{total_bytes} [{msg}]")
                .expect("valid template")
                .progress_chars("#>-"),
        )
        .with_finish(ProgressFinish::WithMessage("Check Completed!".into()));

    let report = NodeStore::open(storage)?.check(CheckOpt {
        hash_check: opts.hash_check,
        progress_bar: Some(progress_bar),
    });

    print_checker_report(report);

    Ok(())
}

#[expect(clippy::cast_precision_loss)]
fn print_checker_report(report: CheckerReport) {
    println!("Raw Report: {report:?}\n");

    println!("Errors ({}): ", report.errors.len());
    for error in report.errors {
        println!("\t{error}");
    }
    println!();

    println!("Advanced Data: ");
    let total_trie_area_bytes = report
        .trie_stats
        .area_counts
        .iter()
        .map(|(area_size, count)| area_size.saturating_mul(*count))
        .sum::<u64>();
    println!(
        "\tStorage Overhead: {} / {} = {:.2}x",
        report.high_watermark,
        report.trie_stats.kv_bytes,
        (report.high_watermark as f64 / report.trie_stats.kv_bytes as f64)
    );
    println!(
        "\tInternal Fragmentation: 1 - ({} / {total_trie_area_bytes}) = {:.2}%",
        report.trie_stats.trie_bytes,
        (1f64 - (report.trie_stats.trie_bytes as f64 / total_trie_area_bytes as f64)) * 100.0
    );
    println!(
        "\tTrie Area Size Distribution: {:?}",
        report.trie_stats.area_counts
    );
    println!(
        "\tFree List Area Size Distribution: {:?}",
        report.free_list_stats.area_counts
    );
    println!(
        "\tBranching Factor Distribution: {:?}",
        report.trie_stats.branching_factors
    );
    println!("\tDepth Distribution: {:?}", report.trie_stats.depths);
    let total_trie_area_count = report.trie_stats.area_counts.values().sum::<u64>();
    println!(
        "\tLow Occupancy Rate: {} / {total_trie_area_count} = {:.2}%",
        report.trie_stats.low_occupancy_area_count,
        (report.trie_stats.low_occupancy_area_count as f64 / total_trie_area_count as f64) * 100.0
    );
    println!(
        "\tTrie Extra Unaligned Page Read Rate: {} / {total_trie_area_count} = {:.2}%",
        report.trie_stats.extra_unaligned_page_read,
        (report.trie_stats.extra_unaligned_page_read as f64 / total_trie_area_count as f64) * 100.0
    );
    let total_free_list_area_count = report.free_list_stats.area_counts.values().sum::<u64>();
    println!(
        "\tFree List Extra Unaligned Page Read Rate: {} / {total_free_list_area_count} = {:.2}%",
        report.free_list_stats.extra_unaligned_page_read,
        (report.free_list_stats.extra_unaligned_page_read as f64
            / total_free_list_area_count as f64)
            * 100.0
    );
}
