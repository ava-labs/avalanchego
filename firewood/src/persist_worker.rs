// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Deferred persistence for committed revisions.
//!
//! This module decouples commit operations from disk I/O by offloading persistence
//! to a background thread. Commits return immediately after updating in-memory state,
//! while disk writes happen asynchronously.
//!
//! [`PersistWorker`] is the main entry point. It spawns a background thread and provides
//! methods to send revisions for persistence, with built-in backpressure to limit
//! the number of unpersisted commits.
//!
//! The diagram below shows how commits are handled under deferred persistence.
//! The main thread updates shared state and signals the background thread via
//! condition variables. Backpressure is enforced by waiting when all permits
//! are exhausted.
//!
//! Below is an example when `commit_count` is set to 10:
//!
//! ```mermaid
//! sequenceDiagram
//!     participant Caller
//!     participant Main as Main Thread
//!     participant BG as Background Thread
//!     participant Disk
//!
//!     loop Commits 1-4
//!         Caller->>Main: commit()
//!         Main->>Main: store latest revision, count -= 1
//!         Note right of BG: Waiting (count > threshold)
//!     end
//!
//!     Caller->>Main: commit() (5th)
//!     Main->>Main: store latest revision, count -= 1
//!     Main->>BG: notify persist_ready
//!     BG->>Disk: persist latest revision
//!     Note right of Disk: Sub-interval (10/2) reached
//!
//!     loop Commits 6-8
//!         Caller->>Main: commit()
//!         Main->>Main: store latest revision, count -= 1
//!         Note right of BG: Waiting (count > threshold)
//!     end
//!
//!     Caller->>Main: close()
//!     Main->>BG: shutdown signal
//!     BG->>Disk: persist last committed revision
//!     Note right of Disk: Latest committed revision is persisted
//! ```

use std::{
    num::NonZeroU64,
    panic::resume_unwind,
    sync::{Arc, OnceLock},
    thread::{self, JoinHandle},
};

use firewood_storage::{
    Committed, FileBacked, FileIoError, HashedNodeReader, LinearAddress, NodeStore,
    NodeStoreHeader, TrieHash,
};
use parking_lot::{Condvar, Mutex, MutexGuard};

use crate::{manager::CommittedRevision, root_store::RootStore};

use firewood_storage::logger::error;

/// Error type for persistence operations.
#[derive(Clone, Debug, thiserror::Error)]
pub enum PersistError {
    #[error("IO error during persistence: {0}")]
    FileIo(#[from] Arc<FileIoError>),
    #[error("RootStore error during persistence: {0}")]
    RootStore(#[source] Arc<dyn std::error::Error + Send + Sync>),
    #[error("Persist worker has shut down")]
    Shutdown,
}

/// Handle for managing the background persistence thread.
#[derive(Debug)]
pub(crate) struct PersistWorker {
    /// The background thread responsible for persisting commits async.
    handle: Mutex<Option<JoinHandle<Result<(), PersistError>>>>,

    /// Shared state with background thread.
    shared: Arc<SharedState>,
}

impl PersistWorker {
    /// Creates a new `PersistWorker` and starts the background thread.
    ///
    /// Returns the worker for sending messages to the background thread.
    #[allow(clippy::large_types_passed_by_value)]
    pub(crate) fn new(
        commit_count: NonZeroU64,
        header: NodeStoreHeader,
        root_store: Option<Arc<RootStore>>,
    ) -> Self {
        let persist_interval = NonZeroU64::new(commit_count.get().div_ceil(2))
            .expect("a nonzero div_ceil(2) is always positive");
        let persist_threshold = commit_count.get().wrapping_sub(persist_interval.get());

        let shared = Arc::new(SharedState {
            error: OnceLock::new(),
            root_store,
            header: Mutex::new(header),
            persist_on_shutdown: OnceLock::new(),
            channel: PersistChannel::new(commit_count, persist_threshold),
        });

        let bg_shared = shared.clone();
        let handle = thread::spawn(move || PersistLoop { shared: bg_shared }.run());

        Self {
            handle: Mutex::new(Some(handle)),
            shared,
        }
    }

    /// Sends `committed` to the background thread for persistence. This call
    /// blocks if the limit of unpersisted commits has been reached.
    ///
    /// ## Errors
    ///
    /// Returns an error if the background thread has shut down or if it
    /// exited due to an earlier failure.
    ///
    /// ## Panics
    ///
    /// Propagates any panic from the background thread.
    pub(crate) fn persist(&self, committed: CommittedRevision) -> Result<(), PersistError> {
        if self.shared.channel.push(committed).is_err() {
            self.join_handle();
            self.check_error()?;
        }

        Ok(())
    }

    /// Sends `nodestore` to the background thread for reaping if archival mode
    /// is disabled. Otherwise, the `nodestore` is dropped.
    ///
    /// ## Errors
    ///
    /// Returns an error if the background thread has shut down or if it
    /// exited due to an earlier failure.
    ///
    /// ## Panics
    ///
    /// Propagates any panic from the background thread.
    pub(crate) fn reap(
        &self,
        nodestore: NodeStore<Committed, FileBacked>,
    ) -> Result<(), PersistError> {
        if self.shared.root_store.is_none() && self.shared.channel.reap(nodestore).is_err() {
            self.join_handle();
            self.check_error()?;
        }

        Ok(())
    }

    /// Gets a lock to the header of the database.
    pub(crate) fn locked_header(&self) -> MutexGuard<'_, NodeStoreHeader> {
        self.shared.header.lock()
    }

    /// Checks if the persist worker has errored.
    pub(crate) fn check_error(&self) -> Result<(), PersistError> {
        match self.shared.error.get() {
            Some(err) => Err(err.clone()),
            None => Ok(()),
        }
    }

    /// Closes the persist worker and persists `latest_committed_revision`.
    ///
    /// ## Errors
    ///
    /// Returns an error if the background thread exited due to an earlier
    /// failure.
    ///
    /// ## Panics
    ///
    /// Propagates any panic from the background thread.
    pub(crate) fn close(
        self,
        latest_committed_revision: CommittedRevision,
    ) -> Result<(), PersistError> {
        self.shared
            .persist_on_shutdown
            .set(latest_committed_revision)
            .expect("should be empty");

        self.shared.channel.close();
        self.join_handle();
        self.check_error()
    }

    /// Joins the background thread if the handle is still available.
    ///
    /// This is a no-op if the handle was already taken (e.g., by a prior call
    /// to `close()`), which guarantees idempotency.
    ///
    /// ## Panics
    ///
    /// Propagates the panic if the background thread panicked.
    fn join_handle(&self) {
        if let Some(handle) = self.handle.lock().take()
            && let Err(payload) = handle.join()
        {
            resume_unwind(payload);
        }
    }

    /// Waits until all pending commits have been persisted.
    #[cfg(test)]
    pub(crate) fn wait_persisted(&self) {
        self.shared.wait_all_released();
    }
}

/// Abstraction for coordinating between the main thread and the background
/// persistence thread.
#[derive(Debug)]
struct PersistChannel {
    state: Mutex<PersistChannelState>,
    /// Condition variable to wake blocked committers when space is available.
    commit_not_full: Condvar,
    /// Condition variable to wake the persister when persistence is needed.
    persist_ready: Condvar,
}

impl PersistChannel {
    const fn new(max_permits: NonZeroU64, persist_threshold: u64) -> Self {
        Self {
            state: Mutex::new(PersistChannelState {
                permits_available: max_permits.get(),
                max_permits,
                persist_threshold,
                shutdown: false,
                pending_reaps: Vec::new(),
                latest_committed: None,
            }),
            commit_not_full: Condvar::new(),
            persist_ready: Condvar::new(),
        }
    }

    /// Returns `true` if no permits have been consumed (i.e., no unpersisted
    /// commits are outstanding).
    fn empty(&self) -> bool {
        let state = self.state.lock();
        state.permits_available == state.max_permits.get()
    }

    /// Enqueues `nodestore` for reaping.
    ///
    /// ## Errors
    ///
    /// Returns an error if the channel has been shut down.
    fn reap(&self, nodestore: NodeStore<Committed, FileBacked>) -> Result<(), PersistError> {
        let mut state = self.state.lock();
        if state.shutdown {
            return Err(PersistError::Shutdown);
        }
        state.pending_reaps.push(nodestore);
        self.persist_ready.notify_one();

        Ok(())
    }

    /// Stores a committed revision and consumes one permit. Blocks if all
    /// permits have been consumed until the background thread releases them.
    ///
    /// ## Errors
    ///
    /// Returns an error if the channel has been shut down.
    fn push(&self, revision: CommittedRevision) -> Result<(), PersistError> {
        let mut state = self.state.lock();
        while state.permits_available == 0 && !state.shutdown {
            self.commit_not_full.wait(&mut state);
        }

        if state.shutdown {
            return Err(PersistError::Shutdown);
        }

        state.latest_committed = Some(revision);
        state.permits_available = state.permits_available.wrapping_sub(1);

        // Wake the persister once we reach the threshold.
        if state.permits_available <= state.persist_threshold {
            self.persist_ready.notify_one();
        }
        Ok(())
    }

    /// Blocks until there is work to do, then returns a [`PersistDataGuard`]
    /// containing the pending reaps and/or the latest committed revision.
    ///
    /// ## Errors
    ///
    /// Returns an error if the channel has been shut down.
    fn pop(&self) -> Result<PersistDataGuard<'_>, PersistError> {
        let (permits_to_release, pending_reaps, latest_committed) = {
            let mut state = self.state.lock();
            loop {
                // Shutdown requested. Return error.
                if state.shutdown {
                    return Err(PersistError::Shutdown);
                }
                // Unblock to persist when permits available <= threshold
                if state.permits_available <= state.persist_threshold
                    && state.latest_committed.is_some()
                {
                    break (
                        state
                            .max_permits
                            .get()
                            .wrapping_sub(state.permits_available),
                        std::mem::take(&mut state.pending_reaps),
                        state.latest_committed.take(),
                    );
                }
                // Unblock even if we haven't met the threshold if there are pending reaps.
                // Permits to release is set to 0, and committed revision is not taken.
                if !state.pending_reaps.is_empty() {
                    break (0, std::mem::take(&mut state.pending_reaps), None);
                }
                // Block until it is woken up by the committer thread.
                self.persist_ready.wait(&mut state);
            }
        };

        Ok(PersistDataGuard {
            channel: self,
            permits_to_release,
            pending_reaps,
            latest_committed,
        })
    }

    /// Signals shutdown and wakes all waiting threads (both committers and
    /// the background persister).
    fn close(&self) {
        let mut state = self.state.lock();
        state.shutdown = true;
        self.persist_ready.notify_one();
        self.commit_not_full.notify_one();
    }

    #[cfg(test)]
    #[allow(clippy::arithmetic_side_effects)]
    fn wait_all_released(&self) {
        use firewood_storage::logger::warn;
        use std::time::Duration;

        const WARN_INTERVAL: Duration = Duration::from_secs(60);

        let mut state = self.state.lock();
        let mut elapsed_secs = 0;
        while state.permits_available < state.max_permits.get() {
            let result = self.commit_not_full.wait_for(&mut state, WARN_INTERVAL);
            if result.timed_out() && state.permits_available < state.max_permits.get() {
                elapsed_secs += WARN_INTERVAL.as_secs();
                warn!("all permits have not been released back after {elapsed_secs}s");
            }
        }
    }
}

#[derive(Debug)]
struct PersistChannelState {
    /// Number of permits currently available for new commits.
    permits_available: u64,
    /// Maximum number of unpersisted commits allowed.
    max_permits: NonZeroU64,
    /// Persist when remaining permits are at or below this threshold.
    persist_threshold: u64,
    /// Set to `true` when the channel has been closed.
    shutdown: bool,
    /// Nodestores awaiting reaping by the background thread.
    pending_reaps: Vec<NodeStore<Committed, FileBacked>>,
    /// The most recent committed revision, replaced on each push.
    latest_committed: Option<CommittedRevision>,
}

/// RAII guard returned by [`PersistChannel::pop`] that carries the data for
/// one persistence cycle (a committed revision and/or pending reaps).
///
/// On drop, it returns consumed permits back to the channel and notifies
/// blocked committers via [`commit_not_full`](PersistChannel::commit_not_full),
/// ensuring backpressure is released even if the persist loop exits early due
/// to an error.
struct PersistDataGuard<'a> {
    channel: &'a PersistChannel,
    /// Number of permits consumed by commits since the last persist cycle.
    /// Released back to the channel on drop.
    permits_to_release: u64,
    /// Expired node stores whose deleted nodes should be returned to free lists.
    pending_reaps: Vec<NodeStore<Committed, FileBacked>>,
    /// The latest committed revision to persist, if the threshold was reached.
    /// `None` when this cycle was triggered solely by pending reaps.
    latest_committed: Option<CommittedRevision>,
}

impl Drop for PersistDataGuard<'_> {
    /// Releases permits back to the channel on drop of the guard.
    fn drop(&mut self) {
        if self.permits_to_release == 0 {
            return;
        }

        let mut state = self.channel.state.lock();
        state.permits_available = state
            .permits_available
            .saturating_add(self.permits_to_release)
            .min(state.max_permits.get());

        self.channel.commit_not_full.notify_all();
    }
}

/// Shared state between `PersistWorker` and `PersistLoop`.
#[derive(Debug)]
struct SharedState {
    /// Shared error state that can be checked without joining the thread.
    error: OnceLock<PersistError>,
    /// Persisted metadata for the database.
    /// Updated on persists or when revisions are reaped.
    header: Mutex<NodeStoreHeader>,
    /// Optional persistent store for historical root addresses.
    root_store: Option<Arc<RootStore>>,
    /// Revision to persist on shutdown if there are outstanding unpersisted
    /// commits.
    persist_on_shutdown: OnceLock<CommittedRevision>,
    /// Channel for coordinating persist and reap operations.
    channel: PersistChannel,
}

/// The background persistence loop that runs in a separate thread.
struct PersistLoop {
    /// Shared state for coordination with `PersistWorker`.
    shared: Arc<SharedState>,
}

impl Drop for PersistLoop {
    /// Closes the persist channel so blocked committers are woken up and see the
    /// shutdown state rather than blocking indefinitely.
    fn drop(&mut self) {
        self.shared.channel.close();
    }
}

impl PersistLoop {
    /// Runs the persistence loop until shutdown or error.
    ///
    /// If the event loop exits with an error, it is stored in shared state
    /// so the main thread can observe it without joining.
    fn run(self) -> Result<(), PersistError> {
        let result = self.event_loop();
        if let Err(ref err) = result {
            self.shared.error.set(err.clone()).expect("should be empty");
        }
        result
    }

    /// Processes pending work until shutdown or an error occurs.
    fn event_loop(&self) -> Result<(), PersistError> {
        while let Ok(mut persist_data) = self.shared.channel.pop() {
            for nodestore in std::mem::take(&mut persist_data.pending_reaps) {
                self.reap(nodestore)?;
            }

            if let Some(revision) = persist_data.latest_committed.take() {
                self.persist_to_disk(&revision)
                    .and_then(|()| self.maybe_save_to_root_store(&revision))?;
            }
        }

        // Persist the last unpersisted revision on shutdown
        if let Some(revision) = self.shared.persist_on_shutdown.get().cloned()
            && !self.shared.channel.empty()
        {
            self.persist_to_disk(&revision)
                .and_then(|()| self.maybe_save_to_root_store(&revision))?;
        }

        Ok(())
    }

    /// Persists the revision to disk.
    fn persist_to_disk(&self, revision: &CommittedRevision) -> Result<(), PersistError> {
        let mut header = self.shared.header.lock();
        revision.persist(&mut header).map_err(|e| {
            error!("Failed to persist revision: {e}");
            PersistError::FileIo(Arc::new(e))
        })
    }

    /// Adds the nodes of this revision to the free lists.
    fn reap(&self, nodestore: NodeStore<Committed, FileBacked>) -> Result<(), PersistError> {
        nodestore
            .reap_deleted(&mut self.shared.header.lock())
            .map_err(|e| {
                error!("Failed to reap deleted nodes: {e}");
                PersistError::FileIo(Arc::new(e))
            })
    }

    /// Saves the revision's root address to `RootStore` if configured.
    fn maybe_save_to_root_store(&self, revision: &CommittedRevision) -> Result<(), PersistError> {
        if let Some(ref store) = self.shared.root_store
            && let (Some(hash), Some(addr)) = (revision.root_hash(), revision.root_address())
        {
            save_to_root_store(store, &hash, &addr)?;
        }

        Ok(())
    }
}

#[crate::metrics("persist.root_store", "persist revision address to root store")]
fn save_to_root_store(
    store: &RootStore,
    hash: &TrieHash,
    addr: &LinearAddress,
) -> Result<(), PersistError> {
    store.add_root(hash, addr).map_err(|e| {
        error!("Failed to persist revision address to RootStore: {e}");
        PersistError::RootStore(e.into())
    })
}

#[cfg(test)]
impl SharedState {
    fn wait_all_released(&self) {
        self.channel.wait_all_released();
    }
}
