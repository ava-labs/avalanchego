// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Persist Module
//!
//! This module handles all persistence operations for the nodestore, including writing
//! headers, nodes, and metadata to storage with support for different I/O backends.
//!
//! ## I/O Backend Support
//!
//! This module supports multiple I/O backends through conditional compilation:
//!
//! - **Standard I/O** - `#[cfg(not(feature = "io-uring"))]` - Uses standard file operations
//! - **io-uring** - `#[cfg(feature = "io-uring")]` - Uses Linux io-uring for async I/O
//!
//! This feature flag is automatically enabled when running on Linux, and disabled for all other platforms.
//!
//! The io-uring implementation provides:
//! - Asynchronous batch operations
//! - Reduced system call overhead
//! - Better performance for high-throughput workloads
//!
//! ## Performance Considerations
//!
//! - Nodes are written in batches to minimize I/O overhead
//! - Metrics are collected for flush operation timing
//! - Memory-efficient serialization with pre-allocated buffers
//! - Ring buffer management for io-uring operations

use crate::linear::FileIoError;
use coarsetime::Instant;
use metrics::counter;
use std::sync::Arc;

#[cfg(feature = "io-uring")]
use crate::logger::trace;

use crate::{FileBacked, WritableStorage};

#[cfg(feature = "io-uring")]
use crate::ReadableStorage;

use super::header::NodeStoreHeader;
use super::{ImmutableProposal, NodeStore};

impl<T, S: WritableStorage> NodeStore<T, S> {
    /// Persist the header from this proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the header cannot be written.
    pub fn flush_header(&self) -> Result<(), FileIoError> {
        let header_bytes = bytemuck::bytes_of(&self.header);
        self.storage.write(0, header_bytes)?;
        Ok(())
    }

    /// Persist the header, including all the padding
    /// This is only done the first time we write the header
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the header cannot be written.
    pub fn flush_header_with_padding(&self) -> Result<(), FileIoError> {
        let header_bytes = bytemuck::bytes_of(&self.header)
            .iter()
            .copied()
            .chain(std::iter::repeat_n(0u8, NodeStoreHeader::EXTRA_BYTES))
            .collect::<Box<[u8]>>();
        debug_assert_eq!(header_bytes.len(), NodeStoreHeader::SIZE as usize);

        self.storage.write(0, &header_bytes)?;
        Ok(())
    }
}

impl NodeStore<Arc<ImmutableProposal>, FileBacked> {
    /// Persist the freelist from this proposal to storage.
    #[fastrace::trace(short_name = true)]
    pub fn flush_freelist(&self) -> Result<(), FileIoError> {
        // Write the free lists to storage
        let free_list_bytes = bytemuck::bytes_of(self.header.free_lists());
        let free_list_offset = NodeStoreHeader::free_lists_offset();
        self.storage.write(free_list_offset, free_list_bytes)?;
        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    #[fastrace::trace(short_name = true)]
    #[cfg(not(feature = "io-uring"))]
    pub fn flush_nodes(&self) -> Result<(), FileIoError> {
        let flush_start = Instant::now();

        for (addr, (area_size_index, node)) in &self.kind.new {
            let mut stored_area_bytes = Vec::new();
            node.as_bytes(*area_size_index, &mut stored_area_bytes);
            self.storage
                .write(addr.get(), stored_area_bytes.as_slice())?;
        }

        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;

        let flush_time = flush_start.elapsed().as_millis();
        counter!("firewood.flush_nodes").increment(flush_time);

        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    #[fastrace::trace(short_name = true)]
    #[cfg(feature = "io-uring")]
    pub fn flush_nodes(&self) -> Result<(), FileIoError> {
        use std::pin::Pin;

        #[derive(Clone, Debug)]
        struct PinnedBufferEntry {
            pinned_buffer: Pin<Box<[u8]>>,
            offset: Option<u64>,
        }

        /// Helper function to handle completion queue entries and check for errors
        fn handle_completion_queue(
            storage: &FileBacked,
            completion_queue: io_uring::cqueue::CompletionQueue<'_>,
            saved_pinned_buffers: &mut [PinnedBufferEntry],
        ) -> Result<(), FileIoError> {
            for entry in completion_queue {
                let item = entry.user_data() as usize;
                let pbe = saved_pinned_buffers
                    .get_mut(item)
                    .expect("should be an index into the array");

                if entry.result()
                    != pbe
                        .pinned_buffer
                        .len()
                        .try_into()
                        .expect("buffer should be small enough")
                {
                    let error = if entry.result() >= 0 {
                        std::io::Error::other("Partial write")
                    } else {
                        std::io::Error::from_raw_os_error(0 - entry.result())
                    };
                    return Err(storage.file_io_error(
                        error,
                        pbe.offset.expect("offset should be Some"),
                        Some("write failure".to_string()),
                    ));
                }
                pbe.offset = None;
            }
            Ok(())
        }

        const RINGSIZE: usize = FileBacked::RINGSIZE as usize;

        let flush_start = Instant::now();

        let mut ring = self.storage.ring.lock().expect("poisoned lock");
        let mut saved_pinned_buffers = vec![
            PinnedBufferEntry {
                pinned_buffer: Pin::new(Box::new([0; 0])),
                offset: None,
            };
            RINGSIZE
        ];
        for (&addr, &(area_size_index, ref node)) in &self.kind.new {
            let mut serialized = Vec::with_capacity(100); // TODO: better size? we can guess branches are larger
            node.as_bytes(area_size_index, &mut serialized);
            let mut serialized = serialized.into_boxed_slice();
            loop {
                // Find the first available write buffer, enumerate to get the position for marking it completed
                if let Some((pos, pbe)) = saved_pinned_buffers
                    .iter_mut()
                    .enumerate()
                    .find(|(_, pbe)| pbe.offset.is_none())
                {
                    pbe.pinned_buffer = std::pin::Pin::new(std::mem::take(&mut serialized));
                    pbe.offset = Some(addr.get());

                    let submission_queue_entry = self
                        .storage
                        .make_op(&pbe.pinned_buffer)
                        .offset(addr.get())
                        .build()
                        .user_data(pos as u64);

                    // SAFETY: the submission_queue_entry's found buffer must not move or go out of scope
                    // until the operation has been completed. This is ensured by having a Some(offset)
                    // and not marking it None until the kernel has said it's done below.
                    #[expect(unsafe_code)]
                    while unsafe { ring.submission().push(&submission_queue_entry) }.is_err() {
                        ring.submitter().squeue_wait().map_err(|e| {
                            self.storage.file_io_error(
                                e,
                                addr.get(),
                                Some("io-uring squeue_wait".to_string()),
                            )
                        })?;
                        trace!("submission queue is full");
                        counter!("ring.full").increment(1);
                    }
                    break;
                }
                // if we get here, that means we couldn't find a place to queue the request, so wait for at least one operation
                // to complete, then handle the completion queue
                counter!("ring.full").increment(1);
                ring.submit_and_wait(1).map_err(|e| {
                    self.storage
                        .file_io_error(e, 0, Some("io-uring submit_and_wait".to_string()))
                })?;
                let completion_queue = ring.completion();
                trace!("competion queue length: {}", completion_queue.len());
                handle_completion_queue(
                    &self.storage,
                    completion_queue,
                    &mut saved_pinned_buffers,
                )?;
            }
        }
        let pending = saved_pinned_buffers
            .iter()
            .filter(|pbe| pbe.offset.is_some())
            .count();
        ring.submit_and_wait(pending).map_err(|e| {
            self.storage
                .file_io_error(e, 0, Some("io-uring final submit_and_wait".to_string()))
        })?;

        handle_completion_queue(&self.storage, ring.completion(), &mut saved_pinned_buffers)?;

        debug_assert!(
            !saved_pinned_buffers.iter().any(|pbe| pbe.offset.is_some()),
            "Found entry with offset still set: {:?}",
            saved_pinned_buffers.iter().find(|pbe| pbe.offset.is_some())
        );

        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;
        debug_assert!(ring.completion().is_empty());

        let flush_time = flush_start.elapsed().as_millis();
        counter!("firewood.flush_nodes").increment(flush_time);

        Ok(())
    }
}
