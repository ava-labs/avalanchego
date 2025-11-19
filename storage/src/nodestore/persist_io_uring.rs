// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # io-uring Persistence Module
//!
//! This module contains io-uring-specific implementations for batch node persistence.
//! It is only compiled when the `io-uring` feature is enabled.

use super::alloc::NodeAllocator;
use super::header::NodeStoreHeader;
use super::persist::process_unpersisted_nodes;
use super::{Committed, NodeStore};
use crate::LinearAddress;
use crate::linear::{FileIoError, ReadableStorage, WritableStorage};
use crate::logger::trace;
use crate::{FileBacked, MaybePersistedNode, firewood_counter};

/// Entry in the pinned buffer array tracking in-flight io-uring operations
#[derive(Clone, Debug)]
struct BufferEntry<'a> {
    buffer: &'a [u8],
    address: LinearAddress,
    node: MaybePersistedNode,
}

/// Helper function to retry `submit_and_wait` on EINTR
fn submit_and_wait_with_retry(
    ring: &mut io_uring::IoUring,
    wait_nr: u32,
    storage: &FileBacked,
    operation_name: &str,
) -> Result<(), FileIoError> {
    use std::io::ErrorKind::Interrupted;

    loop {
        match ring.submit_and_wait(wait_nr as usize) {
            Ok(_) => return Ok(()),
            Err(e) => {
                // Retry if the error is an interrupted system call
                if e.kind() == Interrupted {
                    continue;
                }
                // For other errors, return the error
                return Err(storage.file_io_error(
                    e,
                    0,
                    Some(format!("io-uring {operation_name}")),
                ));
            }
        }
    }
}

/// Helper function to handle completion queue entries and check for errors
fn handle_completion_queue(
    storage: &FileBacked,
    completion_queue: io_uring::cqueue::CompletionQueue<'_>,
    saved_pinned_buffers: &mut [Option<BufferEntry<'_>>],
    cached_nodes: &mut Vec<MaybePersistedNode>,
) -> Result<(), FileIoError> {
    for entry in completion_queue {
        // user data contains the index of the entry in the saved_pinned_buffers array
        let item = entry.user_data() as usize;
        let pbe_entry = saved_pinned_buffers
            .get_mut(item)
            .expect("completed item user_data should point to an entry")
            .take()
            .expect("completed items are always in use");

        let expected_len: i32 = pbe_entry
            .buffer
            .len()
            .try_into()
            .expect("buffer length will fit into an i32");
        if entry.result() != expected_len {
            let error = if entry.result() >= 0 {
                std::io::Error::other("Partial write")
            } else {
                std::io::Error::from_raw_os_error(0 - entry.result())
            };
            return Err(storage.file_io_error(
                error,
                pbe_entry.address.get(),
                Some("write failure".to_string()),
            ));
        }
        // I/O completed successfully - mark node as persisted and cache it
        pbe_entry.node.allocate_at(pbe_entry.address);
        cached_nodes.push(pbe_entry.node);
    }
    Ok(())
}

impl NodeStore<Committed, FileBacked> {
    /// Persist all the nodes of a proposal to storage using io-uring.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    #[fastrace::trace(short_name = true)]
    pub(super) fn flush_nodes_io_uring(&mut self) -> Result<NodeStoreHeader, FileIoError> {
        use bumpalo::Bump;

        let mut header = self.header;
        let mut node_allocator = NodeAllocator::new(self.storage.as_ref(), &mut header);
        let mut bump = Bump::with_capacity(super::INITIAL_BUMP_SIZE);

        process_unpersisted_nodes(
            &mut bump,
            &mut node_allocator,
            self,
            super::INITIAL_BUMP_SIZE,
            |allocated_objects| self.ring_writes(allocated_objects),
        )?;

        Ok(header)
    }

    fn ring_writes(
        &self,
        allocated_objects: Vec<(&[u8], LinearAddress, MaybePersistedNode)>,
    ) -> Result<(), FileIoError> {
        let mut ring = self.storage.ring.lock();

        let mut saved_pinned_buffers =
            vec![Option::<BufferEntry<'_>>::None; FileBacked::RINGSIZE as usize];

        // Collect addresses and nodes for caching
        let mut cached_nodes = Vec::new();

        for (serialized, persisted_address, node) in allocated_objects {
            loop {
                // Find the first available write buffer, enumerate to get the position for marking it completed
                if let Some((pos, pbe)) = saved_pinned_buffers
                    .iter_mut()
                    .enumerate()
                    .find(|(_, pbe)| pbe.is_none())
                {
                    *pbe = Some(BufferEntry {
                        buffer: serialized,
                        address: persisted_address,
                        node: node.clone(),
                    });

                    let submission_queue_entry = self
                        .storage
                        .make_op(serialized)
                        .offset(persisted_address.get())
                        .build()
                        .user_data(pos as u64);

                    #[expect(unsafe_code)]
                    // SAFETY: the submission_queue_entry's found buffer must not move or go out of scope
                    // until the operation has been completed. This is ensured by having a Some(offset)
                    // and not marking it None until the kernel has said it's done below.
                    while unsafe { ring.submission().push(&submission_queue_entry) }.is_err() {
                        ring.submitter().squeue_wait().map_err(|e| {
                            self.storage.file_io_error(
                                e,
                                persisted_address.get(),
                                Some("io-uring squeue_wait".to_string()),
                            )
                        })?;
                        trace!("submission queue is full");
                        firewood_counter!("ring.full", "amount of full ring").increment(1);
                    }
                    break;
                }
                // if we get here, that means we couldn't find a place to queue the request, so wait for at least one operation
                // to complete, then handle the completion queue
                firewood_counter!("ring.full", "amount of full ring").increment(1);
                submit_and_wait_with_retry(&mut ring, 1, &self.storage, "submit_and_wait")?;
                let completion_queue = ring.completion();
                trace!("competion queue length: {}", completion_queue.len());
                handle_completion_queue(
                    &self.storage,
                    completion_queue,
                    &mut saved_pinned_buffers,
                    &mut cached_nodes,
                )?;
            }
        }
        let pending = saved_pinned_buffers
            .iter()
            .filter(|pbe| pbe.is_some())
            .count();
        submit_and_wait_with_retry(
            &mut ring,
            pending as u32,
            &self.storage,
            "final submit_and_wait",
        )?;

        handle_completion_queue(
            &self.storage,
            ring.completion(),
            &mut saved_pinned_buffers,
            &mut cached_nodes,
        )?;

        debug_assert!(
            !saved_pinned_buffers
                .iter()
                .any(std::option::Option::is_some),
            "Found entry with node still set: {:?}",
            saved_pinned_buffers.iter().find(|pbe| pbe.is_some())
        );

        self.storage.write_cached_nodes(cached_nodes)?;
        debug_assert!(ring.completion().is_empty());

        // All references to batch.bump are now dropped, caller can reset it
        Ok(())
    }
}
