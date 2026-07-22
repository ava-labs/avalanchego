// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Storage layer metric definitions.

firewood_metrics::define_metrics! {
    counters: {
        /// Amount of space reused from free lists (bytes)
        SPACE_REUSED                   = "firewood_storage_bytes_reused_total",
        /// Amount of space allocated from end of file (bytes)
        SPACE_FROM_END                 = "firewood_storage_bytes_appended_total",
        /// Amount of space freed (bytes)
        SPACE_FREED                    = "firewood_storage_bytes_freed_total",
        /// Count of deleted nodes
        DELETE_NODE                    = "firewood_nodes_deleted_total",
        /// Count of nodes allocated per free-list size class
        NODES_ALLOCATED                = "firewood_nodes_allocated_total",
        /// Bytes of internal fragmentation per free-list size class (allocated - needed)
        BYTES_WASTED                   = "firewood_storage_bytes_wasted_total",
        /// Number of node reads
        READ_NODE                      = "firewood_node_reads_total",
        /// Number of node cache accesses
        CACHE_NODE                     = "firewood_node_cache_accesses_total",
        /// Number of freelist cache accesses
        CACHE_FREELIST                 = "firewood_freelist_cache_accesses_total",
        /// Number of IO read operations
        IO_READ_COUNT                  = "firewood_io_reads_total",
        /// Number of IO write operations
        IO_WRITE_COUNT                 = "firewood_io_writes_total",
        /// Total bytes read from disk
        IO_BYTES_READ                  = "firewood_io_bytes_read_total",
        /// Total bytes written to disk
        IO_BYTES_WRITTEN               = "firewood_io_bytes_written_total",
        /// Number of proposals reparented to committed parent
        REPARENTED_PROPOSAL_COUNT      = "firewood_proposals_reparented_total",
        /// Fetch attempts from the rootstore
        ROOTSTORE_GET                  = "firewood_rootstore_lookups_total",
        /// Count of EAGAIN write retries
        RING_EAGAIN_WRITE_RETRY        = "firewood_io_uring_eagain_retries_total",
        /// Count of ring buffer full events
        RING_FULL                      = "firewood_io_uring_ring_full_total",
        /// Count of submission queue waits
        RING_SQ_WAIT                   = "firewood_io_uring_sq_waits_total",
        /// Count of partial write retries
        RING_PARTIAL_WRITE_RETRY       = "firewood_io_uring_partial_write_retries_total",
    },
    gauges: {
        /// Current number of entries in freelist cache
        FREELIST_CACHE_SIZE            = "firewood_freelist_cache_entries",
        /// Current memory used by node cache (bytes)
        CACHE_MEMORY_USED              = "firewood_node_cache_bytes",
        /// Maximum memory capacity of node cache (bytes)
        CACHE_MEMORY_LIMIT             = "firewood_node_cache_limit_bytes",
        /// Current database file size (bytes)
        DATABASE_SIZE_BYTES            = "firewood_database_size_bytes",
        /// Number of entries per free-list size class
        FREE_LIST_ENTRIES              = "firewood_free_list_entries",
    },
    histograms: {
        /// Duration of node flush operations
        FLUSH_DURATION_SECONDS         = "firewood_flush_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]),
        /// Duration of node reap operations
        REAP_DURATION_SECONDS          = "firewood_reap_duration_seconds" buckets([0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]),
        /// Duration of individual IO read operations
        IO_READ_DURATION_SECONDS       = "firewood_io_read_duration_seconds" native(2.0, 160, 1e-9),
    },
}
