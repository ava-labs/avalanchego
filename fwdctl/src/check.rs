// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::path::PathBuf;
use std::sync::Arc;

use clap::Args;
use firewood::v2::api;
use firewood_storage::{CacheReadStrategy, CheckOpt, DBStats, FileBacked, NodeStore};
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

    /// Whether to fix observed inconsistencies
    #[arg(
        long,
        required = false,
        default_value_t = false,
        help = "Should fix observed inconsistencies"
    )]
    pub fix: bool,
}

pub(super) fn run(opts: &Options) -> Result<(), api::Error> {
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

    let check_ops = CheckOpt {
        hash_check: opts.hash_check,
        progress_bar: Some(progress_bar),
    };

    let nodestore = NodeStore::open(storage)?;
    let db_stats = if opts.fix {
        let report = nodestore.check_and_fix(check_ops);
        println!("Fixed Errors ({}):", report.fixed.len());
        for error in report.fixed {
            println!("\t{error}");
        }
        println!();
        println!("Unfixable Errors ({}):", report.unfixable.len(),);
        for (error, io_error) in report.unfixable {
            println!("\t{error}");
            if let Some(io_error) = io_error {
                println!("\t\tError encountered while fixing: {io_error}");
            }
        }
        report.db_stats
    } else {
        let report = nodestore.check(check_ops);
        println!("Errors ({}):", report.errors.len());
        for error in report.errors {
            println!("\t{error}");
        }
        report.db_stats
    };
    println!();

    print_stats_report(db_stats);

    Ok(())
}

#[expect(clippy::cast_precision_loss)]
#[expect(clippy::too_many_lines)]
fn print_stats_report(db_stats: DBStats) {
    let total_branch_area_count = db_stats.trie_stats.branch_area_counts.values().sum::<u64>();
    let total_leaf_area_count = db_stats.trie_stats.leaf_area_counts.values().sum::<u64>();
    let total_trie_area_count = total_branch_area_count.saturating_add(total_leaf_area_count);
    let total_branch_area_bytes = db_stats
        .trie_stats
        .branch_area_counts
        .iter()
        .map(|(area_size, count)| area_size.saturating_mul(*count))
        .sum::<u64>();
    let total_leaf_area_bytes = db_stats
        .trie_stats
        .leaf_area_counts
        .iter()
        .map(|(area_size, count)| area_size.saturating_mul(*count))
        .sum::<u64>();
    let total_trie_area_bytes = total_branch_area_bytes.saturating_add(total_leaf_area_bytes);
    let total_free_list_area_count = db_stats.free_list_stats.area_counts.values().sum::<u64>();
    let total_free_list_area_bytes = db_stats
        .free_list_stats
        .area_counts
        .iter()
        .map(|(area_size, count)| area_size.saturating_mul(*count))
        .sum::<u64>();

    // Basic stats
    println!("\nBasic Stats: ");
    println!(
        "\tFirewood Image Size / High Watermark (high_watermark): {}",
        db_stats.high_watermark
    );
    println!(
        "\tTotal Key-Value Count (kv_count): {}",
        db_stats.trie_stats.kv_count
    );
    println!(
        "\tTotal Key-Value Bytes (kv_bytes): {}",
        db_stats.trie_stats.kv_bytes
    );

    // Trie statistics
    println!("\nTrie Stats: ");
    println!(
        "\tBranching Factor Distribution: {:?}",
        db_stats.trie_stats.branching_factors
    );
    println!("\tDepth Distribution: {:?}", db_stats.trie_stats.depths);

    // Branch area distribution
    println!("\nBranch Area Stats: ");
    println!(
        "\tTotal Branch Data Bytes (branch_bytes): {}",
        db_stats.trie_stats.branch_bytes
    );
    println!("\tTotal Branch Area Count (branch_area_count): {total_branch_area_count}");
    println!("\tTotal Branch Area Bytes (branch_area_bytes): {total_branch_area_bytes}");
    println!(
        "\tBranch Area Distribution: {:?}",
        db_stats.trie_stats.branch_area_counts
    );
    println!(
        "\tBranches that Can Fit Into Smaller Area (low_occupancy_branch_area): {} ({:.2}%)",
        db_stats.trie_stats.low_occupancy_branch_area_count,
        (db_stats.trie_stats.low_occupancy_branch_area_count as f64
            / total_branch_area_count as f64)
            * 100.0
    );

    // Leaf area distribution
    println!("\nLeaf Area Stats: ");
    println!(
        "\tTotal Leaf Data Bytes (leaf_bytes): {}",
        db_stats.trie_stats.leaf_bytes,
    );
    println!("\tTotal Leaf Area Count (leaf_area_count): {total_leaf_area_count}");
    println!("\tTotal Leaf Area Bytes (leaf_area_bytes): {total_leaf_area_bytes}");
    println!(
        "\tLeaf Area Distribution: {:?}",
        db_stats.trie_stats.leaf_area_counts
    );
    println!(
        "\tLeaves that Can Fit Into Smaller Area (low_occupancy_leaf_area): {} ({:.2}%)",
        db_stats.trie_stats.low_occupancy_leaf_area_count,
        (db_stats.trie_stats.low_occupancy_leaf_area_count as f64 / total_leaf_area_count as f64)
            * 100.0
    );

    // Free list area distribution
    println!("\nFree List Area Stats: ");
    println!("\tFree List Area Counts (free_list_area_counts): {total_free_list_area_count}");
    println!("\tTotal Free List Area Bytes (free_list_area_bytes): {total_free_list_area_bytes}");
    println!(
        "\tFree List Area Distribution: {:?}",
        db_stats.free_list_stats.area_counts
    );

    // alignment stats
    println!("\nAlignment Stats: ");
    println!(
        "\tTrie Areas Spanning Extra Page Due to Unalignment: {} ({:.2}%)",
        db_stats.trie_stats.area_extra_unaligned_page,
        (db_stats.trie_stats.area_extra_unaligned_page as f64 / total_trie_area_count as f64)
            * 100.0
    );
    println!(
        "\tFree List Areas Spanning Extra Page Due to Unalignment: {} ({:.2}%)",
        db_stats.free_list_stats.area_extra_unaligned_page,
        (db_stats.free_list_stats.area_extra_unaligned_page as f64
            / total_free_list_area_count as f64)
            * 100.0
    );
    println!(
        "\tTrie Nodes Spanning Extra Page Due to Unalignment: {} ({:.2}%)",
        db_stats.trie_stats.node_extra_unaligned_page,
        (db_stats.trie_stats.node_extra_unaligned_page as f64 / total_trie_area_count as f64)
            * 100.0
    );

    println!("\nAdvanced Stats: ");
    println!(
        "\tStorage Overhead: high_watermark / kv_bytes = {:.2}x",
        (db_stats.high_watermark as f64 / db_stats.trie_stats.kv_bytes as f64)
    );
    let total_trie_bytes = db_stats
        .trie_stats
        .branch_bytes
        .saturating_add(db_stats.trie_stats.leaf_bytes);
    println!(
        "\tInternal Fragmentation: 1 - (branch_bytes + leaf_bytes) / (branch_area_bytes + leaf_area_bytes) = {:.2}%",
        (1f64 - (total_trie_bytes as f64 / total_trie_area_bytes as f64)) * 100.0
    );
    let low_occupancy_area_count = db_stats
        .trie_stats
        .low_occupancy_branch_area_count
        .saturating_add(db_stats.trie_stats.low_occupancy_leaf_area_count);
    println!(
        "\tAreas that Can Fit Into Smaller Area: low_occupancy_branch_area + low_occupancy_leaf_area = {} ({:.2}%)",
        low_occupancy_area_count,
        (low_occupancy_area_count as f64 / total_trie_area_count as f64) * 100.0
    );
}
