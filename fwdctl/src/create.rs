// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::{value_parser, Args};
use firewood::{
    db::{Db, DbConfig, DbRevConfig, DiskBufferConfig, WalConfig},
    v2::api,
};

#[derive(Args)]
pub struct Options {
    /// DB Options
    #[arg(
        required = true,
        value_name = "NAME",
        help = "A name for the database. A good default name is firewood."
    )]
    pub name: String,

    #[arg(
        long,
        required = false,
        default_value_t = 16384,
        value_name = "META_NCACHED_PAGES",
        help = "Maximum cached pages for the free list of the item stash."
    )]
    pub meta_ncached_pages: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 1024,
        value_name = "META_NCACHED_FILES",
        help = "Maximum cached file descriptors for the free list of the item stash."
    )]
    pub meta_ncached_files: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "META_FILE_NBIT",
        help = "Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    the power of 2 for the file size."
    )]
    pub meta_file_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 262144,
        value_name = "PAYLOAD_NCACHED_PAGES",
        help = "Maximum cached pages for the item stash. This is the low-level cache used by the linear
        store that holds trie nodes and account objects."
    )]
    pub payload_ncached_pages: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 1024,
        value_name = "PAYLOAD_NCACHED_FILES",
        help = "Maximum cached file descriptors for the item stash."
    )]
    pub payload_ncached_files: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "PAYLOAD_FILE_NBIT",
        help = "Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    the power of 2 for the file size."
    )]
    pub payload_file_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 10,
        value_name = "PAYLOAD_MAX_WALK",
        help = "Maximum steps of walk to recycle a freed item."
    )]
    pub payload_max_walk: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "PAYLOAD_REGN_NBIT",
        help = "Region size in bits (should be not greater than PAYLOAD_FILE_NBIT). One file is
    partitioned into multiple regions. Suggested to use the default value."
    )]
    pub payload_regn_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 16384,
        value_name = "ROOT_HASH_NCACHED_PAGES",
        help = "Maximum cached pages for the free list of the item stash."
    )]
    pub root_hash_ncached_pages: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 1024,
        value_name = "ROOT_HASH_NCACHED_FILES",
        help = "Maximum cached file descriptors for the free list of the item stash."
    )]
    pub root_hash_ncached_files: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "ROOT_HASH_FILE_NBIT",
        help = "Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    the power of 2 for the file size."
    )]
    pub root_hash_file_nbit: u64,

    #[arg(
        long,
        required = false,
        value_parser = value_parser!(bool),
        default_missing_value = "false",
        default_value_t = false,
        value_name = "TRUNCATE",
        help = "Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    existing contents will be lost. [default: false]"
    )]
    pub truncate: bool,

    /// Revision options
    #[arg(
        long,
        required = false,
        default_value_t = 1 << 20,
        value_name = "REV_MERKLE_NCACHED",
        help = "Config for accessing a version of the DB. Maximum cached trie objects.")]
    merkle_ncached_objs: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 4096,
        value_name = "REV_BLOB_NCACHED",
        help = "Maximum cached Blob objects."
    )]
    blob_ncached_objs: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 65536,
        value_name = "DISK_BUFFER_MAX_PENDING",
        help = "Maximum number of pending pages."
    )]
    max_pending: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 1024,
        value_name = "DISK_BUFFER_MAX_AIO_REQUESTS",
        help = "Maximum number of concurrent async I/O requests."
    )]
    max_aio_requests: u32,

    #[arg(
        long,
        required = false,
        default_value_t = 128,
        value_name = "DISK_BUFFER_MAX_AIO_RESPONSE",
        help = "Maximum number of async I/O responses that firewood polls for at a time."
    )]
    max_aio_response: u16,

    #[arg(
        long,
        required = false,
        default_value_t = 128,
        value_name = "DISK_BUFFER_MAX_AIO_SUBMIT",
        help = "Maximum number of async I/O requests per submission."
    )]
    max_aio_submit: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 256,
        value_name = "DISK_BUFFER_WAL_MAX_AIO_REQUESTS",
        help = "Maximum number of concurrent async I/O requests in WAL."
    )]
    wal_max_aio_requests: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 1024,
        value_name = "DISK_BUFFER_WAL_MAX_BUFFERED",
        help = "Maximum buffered WAL records."
    )]
    wal_max_buffered: usize,

    #[arg(
        long,
        required = false,
        default_value_t = 4096,
        value_name = "DISK_BUFFER_WAL_MAX_BATCH",
        help = "Maximum batched WAL records per write."
    )]
    wal_max_batch: usize,

    /// WAL Config
    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "WAL_FILE_NBIT",
        help = "Size of WAL file."
    )]
    file_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 15,
        value_name = "WAL_BLOCK_NBIT",
        help = "Size of WAL blocks."
    )]
    block_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 100,
        value_name = "Wal_MAX_REVISIONS",
        help = "Number of revisions to keep from the past. This preserves a rolling window
    of the past N commits to the database."
    )]
    max_revisions: u32,
}

pub(super) const fn initialize_db_config(opts: &Options) -> DbConfig {
    DbConfig {
        meta_ncached_pages: opts.meta_ncached_pages,
        meta_ncached_files: opts.meta_ncached_files,
        meta_file_nbit: opts.meta_file_nbit,
        payload_ncached_pages: opts.payload_ncached_pages,
        payload_ncached_files: opts.payload_ncached_files,
        payload_file_nbit: opts.payload_file_nbit,
        payload_max_walk: opts.payload_max_walk,
        payload_regn_nbit: opts.payload_regn_nbit,
        root_hash_ncached_pages: opts.payload_ncached_pages,
        root_hash_ncached_files: opts.root_hash_ncached_files,
        root_hash_file_nbit: opts.root_hash_file_nbit,
        truncate: opts.truncate,
        rev: DbRevConfig {
            merkle_ncached_objs: opts.merkle_ncached_objs,
        },
        buffer: DiskBufferConfig {
            max_pending: opts.max_pending,
            max_aio_requests: opts.max_aio_requests,
            max_aio_response: opts.max_aio_response,
            max_aio_submit: opts.max_aio_submit,
            wal_max_aio_requests: opts.wal_max_aio_requests,
            wal_max_buffered: opts.wal_max_buffered,
            wal_max_batch: opts.wal_max_batch,
        },
        wal: WalConfig {
            file_nbit: opts.file_nbit,
            block_nbit: opts.block_nbit,
            max_revisions: opts.max_revisions,
        },
    }
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    let db_config = initialize_db_config(opts);
    log::debug!("database configuration parameters: \n{:?}\n", db_config);

    Db::new(opts.name.clone(), &db_config).await?;
    println!("created firewood database in {:?}", opts.name);
    Ok(())
}
