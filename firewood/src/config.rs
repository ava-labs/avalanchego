// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

pub use crate::storage::{buffer::DiskBufferConfig, WalConfig};
use typed_builder::TypedBuilder;

/// Database configuration.
#[derive(Clone, TypedBuilder, Debug)]
pub struct DbConfig {
    /// Maximum cached pages for the free list of the item stash.
    #[builder(default = 16384)] // 64M total size by default
    pub meta_ncached_pages: usize,
    /// Maximum cached file descriptors for the free list of the item stash.
    #[builder(default = 1024)] // 1K fds by default
    pub meta_ncached_files: usize,
    /// Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    /// the power of 2 for the file size.
    #[builder(default = 22)] // 4MB file by default
    pub meta_file_nbit: u64,
    /// Maximum cached pages for the item stash. This is the low-level cache used by the linear
    /// store that holds Trie nodes and account objects.
    #[builder(default = 262144)] // 1G total size by default
    pub payload_ncached_pages: usize,
    /// Maximum cached file descriptors for the item stash.
    #[builder(default = 1024)] // 1K fds by default
    pub payload_ncached_files: usize,
    /// Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    /// the power of 2 for the file size.
    #[builder(default = 22)] // 4MB file by default
    pub payload_file_nbit: u64,
    /// Maximum steps of walk to recycle a freed item.
    #[builder(default = 10)]
    pub payload_max_walk: u64,
    /// Region size in bits (should be not greater than `payload_file_nbit`). One file is
    /// partitioned into multiple regions. Just use the default value.
    #[builder(default = 22)]
    pub payload_regn_nbit: u64,
    /// Maximum cached pages for the free list of the item stash.
    #[builder(default = 16384)] // 64M total size by default
    pub root_hash_ncached_pages: usize,
    /// Maximum cached file descriptors for the free list of the item stash.
    #[builder(default = 1024)] // 1K fds by default
    pub root_hash_ncached_files: usize,
    /// Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    /// the power of 2 for the file size.
    #[builder(default = 22)] // 4MB file by default
    pub root_hash_file_nbit: u64,
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    /// Config for accessing a version of the DB.
    #[builder(default = DbRevConfig::builder().build())]
    pub rev: DbRevConfig,
    /// Config for the disk buffer.
    #[builder(default = DiskBufferConfig::builder().build())]
    pub buffer: DiskBufferConfig,
    /// Config for Wal.
    #[builder(default = WalConfig::builder().build())]
    pub wal: WalConfig,
}

/// Config for accessing a version of the DB.
#[derive(TypedBuilder, Clone, Debug)]
pub struct DbRevConfig {
    /// Maximum cached Trie objects.
    #[builder(default = 1 << 20)]
    pub merkle_ncached_objs: usize,
}
