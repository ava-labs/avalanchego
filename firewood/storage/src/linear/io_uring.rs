// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{
    cmp::Ordering,
    io::{self, ErrorKind},
    os::fd::RawFd,
};

use derive_where::derive_where;
use io_uring::IoUring;
use parking_lot::Mutex;

use crate::logger::{debug, trace};
use firewood_metrics::firewood_increment;

pub use self::errors::{BatchError, BatchErrors};

/// The amount of time (in milliseconds) that the io-uring kernel thread
/// processing the submission queue should spin before idling to sleep.
///
/// A higher value increases CPU utilization when idle but reduces latency from
/// submissions to the queue. A lower value may increase the latency when
/// processing many small batches with gaps of idleness in between, but reduces
/// wasted CPU cycles from the busy wait loop.
const SUBMISSION_QUEUE_IDLE_TIME_MS: u32 = 1000;

/// The amount of entries in the shared submission queue.
///
/// The kernel copies entries from this queue to its own internal queue once it
/// begins processing them freeing us to add more entries while it processes
/// existing ones. This means the submission queue is effectively double this
/// size.
const SUBMISSION_QUEUE_SIZE: u32 = 32;

/// The amount of entries in the shared completion queue.
///
/// The kernel copies entries to this queue as operations complete, failure
/// or success. The kernel requires this queue to be strictly larger than the
/// submission queue and will round up to the next power of two if necessary.
///
/// We must ensure the completion queue is continuously worked to prevent the
/// kernel from stalling when it cannot push completed entries to it.
///
/// If the kernel doesn't support [IORING_FEAT_NODROP], it will drop completion
/// events when the completion queue is full, which leads to us missing events
/// or errors.
///
/// [IORING_FEAT_NODROP]: https://man7.org/linux/man-pages/man2/io_uring_setup.2.html#:~:text=since%20kernel%205.4.-,IORING_FEAT_NODROP,-If%20this%20flag
const COMPLETION_QUEUE_SIZE: u32 = SUBMISSION_QUEUE_SIZE * 2;

/// A thread-safe proxy around an io-uring instance.
#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct IoUringProxy(Mutex<IoUring>);

/// The state of an ongoing io-uring write batch.
struct WriteBatch<'batch, 'ring, I> {
    /// The file descriptor all writes are applied to.
    ///
    /// There is no requirement that all entries write to the same file;
    /// however, this implementation assumes that is the case for simplicity.
    fd: RawFd,

    /// The io-uring submission interface.
    ///
    /// This is a higher-level interface for actually interacting with the
    /// kernel, after we have prepared our submission queue entries and after
    /// the kernel has processed some completion queue entries.
    submitter: io_uring::Submitter<'ring>,

    /// The kernel submission queue.
    ///
    /// The kernel keeps its own internal submission queue and copies entries
    /// from this queue to its own when it begins processing them. This allows
    /// us to continue adding entries to the queue while the kernel is busy.
    ///
    /// We must call `sync` on this queue after adding entries and after calling
    /// `submit` to ensure the kernel sees our changes and we see its changes.
    /// The call can be batched after adding multiple entries for efficiency.
    sq: io_uring::squeue::SubmissionQueue<'ring>,

    /// The kernel completion queue.
    ///
    /// We must continuously work this queue to prevent the kernel from
    /// stalling. We must also call `sync` on this queue after calling `wait`,
    /// which we do via `submit_and_wait`, to ensure we see the kernel's
    /// updates.
    cq: io_uring::cqueue::CompletionQueue<'ring>,

    /// A btree of outstanding entries that have been submitted to the kernel
    /// but have not yet completed.
    ///
    /// The drop guard ensures this table is empty when the batch is complete,
    /// if we somehow return from `write_batch` with outstanding entries, that
    /// will result in undefined behavior because the buffers may be dropped
    /// while the kernel is still using them.
    outstanding: self::drop_guard::DropGuard<Outstanding<'batch>>,

    /// A backlog of entries that were taken from the `entries` iterator but
    /// could not be submitted to the kernel yet.
    ///
    /// This also contains entries for items that were partially written and
    /// need to be re-submitted.
    ///
    /// New entries are added to the back of the queue, while partially written
    /// entries are re-added to the front of the queue for priority processing.
    backlog: std::collections::VecDeque<QueueEntry<'batch>>,

    /// The undiscovered, unsubmitted entries in the batch.
    entries: I,

    /// The total number of bytes successfully written so far.
    ///
    /// An error is returned if the batch causes an overflow; however, the batch
    /// will continue processing all entries in the batch after such an error.
    written: usize,
}

#[derive(Debug)]
struct Outstanding<'batch> {
    btree: Vec<QueueEntry<'batch>>,
}

/// An entry in the io-uring write batch.
#[derive(Debug, PartialEq)]
struct QueueEntry<'batch> {
    /// The original offset of the entry.
    original_offset: u64,

    /// The current offset of the entry.
    ///
    /// If different from `original_offset`, this entry has been partially written.
    offset: u64,

    /// The contents to write to the file descriptor.
    buffer: &'batch [u8],

    /// The number of times this entry has been submitted to the kernel.
    submission_count: u32,
}

enum EnqueueResult {
    Empty,
    Full,
    Enqueued,
}

impl IoUringProxy {
    const fn from_ring(ring: IoUring) -> Self {
        Self(Mutex::new(ring))
    }

    pub fn new() -> io::Result<Self> {
        io_uring::IoUring::builder()
            // don't give the ring to child processes
            .dontfork()
            // Use a kernel thread to poll the submission queue. This avoids
            // context switching on submit; but, does add a significant
            // amount of overhead for small writes and after the thread sleeps.
            .setup_sqpoll(SUBMISSION_QUEUE_IDLE_TIME_MS)
            // do not stop processing the submission queue on error, submit
            // all events and report errors in the completion queue
            .setup_submit_all()
            // NOTE: .setup_defer_taskrun() is not used because it conflicts
            // with the `setup_sqpoll` thread and causes the kernel to return
            // EINVAL when initializing the ring. With `setup_single_issuer` and
            // `setup_sqpoll`, we are allowed to submit from multiple threads
            // and "single issuer" here means "single process" instead of
            // "single thread".
            .setup_single_issuer()
            // completion queue must be greater than the submission queue
            // and is rounded up to the next power of two if necessary
            .setup_cqsize(COMPLETION_QUEUE_SIZE)
            .build(SUBMISSION_QUEUE_SIZE)
            .map(Self::from_ring)
    }

    /// Writes a batch of buffers to the given file descriptor at the specified
    /// offsets.
    ///
    /// We do not guarantee the ordering of writes in the batch; however, we
    /// do guarantee that all writes are attempted even if some writes fail;
    /// except if an incurable error occurs that prevents further communication
    /// with the kernel.
    ///
    /// If all writes succeed, the total number of bytes written is returned. If
    /// any writes fail, a collection of errors is returned. If an incurable
    /// error occurs, it will be indicated in the returned errors.
    pub fn write_batch<'a, I: IntoIterator<Item = (u64, &'a [u8])> + Clone>(
        &self,
        fd: RawFd,
        writes: I,
    ) -> Result<usize, BatchErrors> {
        let mut errors = Vec::<BatchError>::new();
        macro_rules! bail {
            ($reason:expr) => {{
                errors.push(BatchError {
                    batch_offset: 0,
                    error_offset: 0,
                    err: $reason,
                    incurable: true,
                });
                // guaranteed non-empty since we just pushed the incurable error
                return Err(BatchErrors { errors });
            }};
        }

        trace!("starting io-uring write batch");
        let mut ring = self.0.lock();
        let mut batch = WriteBatch::new(&mut ring, fd, writes.into_iter().map(QueueEntry::new));

        // Before entering the main loop, seed the submission queue with as many
        // entries as possible ensuring `is_finished` returns false unless there
        // is nothing to do.
        batch.seed_submission_queue();

        while !batch.is_finished() {
            trace!(
                "io-uring write batch status: outstanding={}, backlog={}, sq_asleep={}, sq_dropped={}, sq_cq_overflow={}, sq_len={}, sq_full={}, cq_overflow={}, cq_len={}, cq_full={}, iter_size_hint={:?}, errors={}",
                batch.outstanding.len(),
                batch.backlog.len(),
                batch.sq.need_wakeup(),
                batch.sq.dropped(),
                batch.sq.cq_overflow(),
                batch.sq.len(),
                batch.sq.is_full(),
                batch.cq.overflow(),
                batch.cq.len(),
                batch.cq.is_full(),
                batch.entries.size_hint(),
                errors.len(),
            );

            // Top of the loop: synchronize the completion queue pointers and
            // maybe block until at least one completion is available.
            if let Err(err) = batch.sync_completion_queue() {
                bail!(err);
            }

            // Drain the completion queue and backlog any entries we need to
            // re-submit before flushing to the submission queue.
            batch.flush_completion_queue(&mut errors);

            // Finally, try to flush more entries to the submission queue
            // prioritizing resubmits before new entries.
            if let Err(err) = batch.flush_submission_queue() {
                bail!(err);
            }
        }

        // batch.is_finished() is true here, so we can forget the drop guard
        let outstanding = batch.outstanding.forget();
        debug_assert!(
            outstanding.is_empty(),
            "io-uring write batch finished with {} outstanding entries",
            outstanding.len()
        );

        if errors.is_empty() {
            Ok(batch.written)
        } else {
            Err(BatchErrors { errors })
        }
    }
}

impl<'a> QueueEntry<'a> {
    const fn new((offset, buffer): (u64, &'a [u8])) -> Self {
        Self {
            original_offset: offset,
            offset,
            buffer,
            submission_count: 0,
        }
    }

    fn build_submission_queue_entry(&self, fd: RawFd) -> io_uring::squeue::Entry {
        io_uring::opcode::Write::new(
            io_uring::types::Fd(fd),
            self.buffer.as_ptr(),
            // writes are limited to i32::MAX bytes. If the length is larger,
            // we will automatically resubmit the remaining data.
            //
            // for larger writes in a single call, we need to switch to using
            // `writev(2)` instead of `write(2)` or automatically split the
            // entries beforehand.
            //
            // this is fine for now since we usually only write 16 MiB chunks
            // at most, but this is here for future-proofing.
            (self.buffer.len() & 0x7FFF_FFFF) as u32,
        )
        .offset(self.offset)
        .build()
        .user_data(self.original_offset)
    }

    /// Handles the result of a completed write operation.
    ///
    /// If the operation was successful, returns the number of bytes written. If
    /// the operation failed, returns a `BatchError` describing the failure.
    ///
    /// If `length` is non-zero after a successful write, the entry must be
    /// re-submitted to write the remaining data.
    fn handle_result(&mut self, res: i32) -> Result<usize, BatchError> {
        match res.cmp(&0) {
            Ordering::Less => {
                #[expect(clippy::arithmetic_side_effects)]
                let err = io::Error::from_raw_os_error(-res);
                // rust stdlib maps EAGAIN to WouldBlock; re-submit in that case
                if matches!(err.kind(), ErrorKind::WouldBlock) {
                    debug!(
                        "io-uring write at offset {} returned EAGAIN, re-submitting",
                        self.original_offset
                    );
                    firewood_increment!(crate::registry::ring::EAGAIN_WRITE_RETRY, 1);
                    return Ok(0);
                }

                Err(BatchError {
                    batch_offset: self.original_offset,
                    error_offset: self.offset,
                    err,
                    incurable: false,
                })
            }
            // This check is to prevent infinite loops. The kernel should never
            // return Ok(0) for a write operation via this API unless the storage
            // driver is broken (or we weren't writing to a file--how did we get here?)
            Ordering::Equal => Err(BatchError {
                batch_offset: self.original_offset,
                error_offset: self.offset,
                err: io::Error::new(ErrorKind::WriteZero, "kernel wrote zero bytes"),
                incurable: false,
            }),
            Ordering::Greater => {
                #[expect(clippy::cast_sign_loss)]
                let written = res as usize;
                let Some(remaining) = self.buffer.get(written..) else {
                    // concious choice: if the kernel writes more than we asked for,
                    // panic instead of returning an error. This indicates a serious
                    // bug in the kernel or storage driver and continuing execution
                    // could lead to data corruption.
                    unreachable!(
                        "kernel wrote more data than requested: wrote {written}, requested {}",
                        self.buffer.len(),
                    )
                };

                // if this is non-empty, we need to re-submit the remaining data
                self.buffer = remaining;
                self.offset = self.offset.wrapping_add(written as u64);

                Ok(written)
            }
        }
    }
}

impl<'batch> Outstanding<'batch> {
    fn with_capacity(capacity: usize) -> Self {
        Self {
            btree: Vec::with_capacity(capacity),
        }
    }

    const fn is_empty(&self) -> bool {
        self.btree.is_empty()
    }

    const fn len(&self) -> usize {
        self.btree.len()
    }

    fn insert(&mut self, entry: QueueEntry<'batch>) {
        let offset = entry.original_offset;
        match self
            .btree
            // O(log n) search for existing entry or insertion point
            .binary_search_by(move |probe| probe.original_offset.cmp(&offset))
        {
            Ok(index) => {
                // binary_search guarantees `index` is valid and the compiler
                // knows this too but clippy does not
                #[expect(clippy::indexing_slicing)]
                let existing = &self.btree[index];
                // This is a critical logic error and should never happen. It
                // indicates that either we allocated a node at the same offset
                // in the same batch twice, which is a violation of the
                // allocator contract; or, we have a bug in our loop where we
                // did not remove an existing entry before re-inserting it.
                //
                // either way, this is unrecoverable and indicates a serious bug
                unreachable!(
                    "attempting to insert duplicate io-uring write entry for offset {offset}: existing={existing:?}, new={entry:?}"
                );
            }
            Err(index) => {
                // O(n) worse case to shift elements
                self.btree.insert(index, entry);
            }
        }
    }

    fn remove(&mut self, offset: u64) -> Option<QueueEntry<'batch>> {
        self.btree
            // O(log n) search for existing entry
            .binary_search_by(move |probe| probe.original_offset.cmp(&offset))
            .ok()
            // O(n) worse case to shift elements
            .map(|index| self.btree.remove(index))
    }
}

impl<'batch, 'ring, I: Iterator<Item = QueueEntry<'batch>>> WriteBatch<'batch, 'ring, I> {
    fn new(ring: &'ring mut IoUring, fd: RawFd, entries: I) -> Self {
        let (submitter, sq, cq) = ring.split();
        Self {
            fd,
            submitter,
            sq,
            cq,
            outstanding: self::drop_guard::DropGuard::new(
                Outstanding::with_capacity(COMPLETION_QUEUE_SIZE as usize),
                Outstanding::is_empty,
                "io-uring write batch terminated with outstanding entries",
            ),
            backlog: std::collections::VecDeque::new(),
            entries,
            written: 0,
        }
    }

    fn is_finished(&self) -> bool {
        // we never take from the iterator without submitting it to the kernel
        // or adding it to the backlog, so we don't need to check the iterator
        self.backlog.is_empty() && self.outstanding.is_empty()
    }

    fn pop_next_entry(&mut self) -> Option<QueueEntry<'batch>> {
        self.backlog.pop_front().or_else(|| self.entries.next())
    }

    fn add_entry_to_backlog(&mut self, entry: QueueEntry<'batch>) {
        if entry.offset == entry.original_offset {
            // new entry, queue at the back
            self.backlog.push_back(entry);
        } else {
            // partially written entry, re-queue at the front for priority
            self.backlog.push_front(entry);
        }
    }

    fn sync_completion_queue(&mut self) -> io::Result<()> {
        trace!("syncing io-uring completion queue");
        // `submit_and_wait` blocks until at least `want` (1) completion events
        // are available in the completion queue. If there are already `want`
        // events in the cq, this returns immediately.
        //
        // The return value is simply the number of SQEs copied to the kernel
        // since the last time we called `submit` or `submit_and_wait` and is
        // not particularly useful here.
        //
        // The kernel will spurrously interrupt this call if this thread needs
        // to be woken up for an unrelated reason; we ignore those interrupts.
        // The kernel will also return EBUSY if its internal submission queue is
        // full, in which case we break out to process completions. If the CQ
        // is empty, we may spin here until the syscall blocks.
        if let Err(err) = self.submitter.submit_and_wait(1)
            && !ignore_poll_error(&err)
        {
            return Err(err);
        }

        self.cq.sync();

        assert_eq!(
            0,
            self.cq.overflow(),
            "io-uring completion queue overflowed; we are unable to keep up with completions and cannot safely continue"
        );

        Ok(())
    }

    fn enqueue_single_op(&mut self) -> EnqueueResult {
        let Some(entry) = self.pop_next_entry() else {
            return EnqueueResult::Empty;
        };

        let sqe = entry.build_submission_queue_entry(self.fd);
        #[expect(unsafe_code)]
        // SAFETY: rust borrowing rules ensure the buffer lives for `'batch`
        // lifetime which must outlive the `write_batch` method that created
        // this `WriteBatch` instance. We also ensure that the method does
        // not return until all entries have been processed by the kernel.
        if unsafe { self.sq.push(&sqe) }.is_err() {
            self.add_entry_to_backlog(entry);
            trace!("io-uring submission queue is full");
            firewood_increment!(crate::registry::ring::FULL, 1);
            EnqueueResult::Full
        } else {
            let submission_count = entry.submission_count.wrapping_add(1);
            trace!(
                "enqueued io-uring write at offset {} ({}) of length {} (attempt #{submission_count})",
                entry.original_offset,
                entry.offset,
                entry.buffer.len(),
            );
            self.outstanding.insert(QueueEntry {
                submission_count,
                ..entry
            });
            EnqueueResult::Enqueued
        }
    }

    fn seed_submission_queue(&mut self) {
        while let EnqueueResult::Enqueued = self.enqueue_single_op() {}
        self.sq.sync();
    }

    fn flush_submission_queue(&mut self) -> io::Result<()> {
        trace!("flushing io-uring submission queue");
        loop {
            if self.sq.is_full() {
                // Because we use SQPOLL, this is a no-op if the polling thread
                // is awake. However, if asleep, this will make a syscall to
                // wake it up. We avoid calling `needs_wakeup` for metrics here
                // to prevent hitting the same expensive atomic barrier twice.
                if let Err(err) = self.submitter.submit() {
                    return if ignore_poll_error(&err) {
                        trace!("io-uring submit returned EBUSY or EINTR");
                        // kernel is busy, move onto completions
                        Ok(())
                    } else {
                        Err(err)
                    };
                }

                trace!("io-uring submission queue is full, waiting for space");
                firewood_increment!(crate::registry::ring::SQ_WAIT, 1);
                // this is our only mechanism to wait for the kernel to
                // update the SQ once it is full. `submit_and_wait` does not
                // provide the correct synchronization semantics in order for
                // us to see updates made by the SQPOLL thread.
                if let Err(err) = self.submitter.squeue_wait() {
                    return if ignore_poll_error(&err) {
                        trace!("io-uring squeue_wait returned EBUSY or EINTR");
                        // kernel is busy, move onto completions
                        Ok(())
                    } else {
                        Err(err)
                    };
                }
            }

            // synchronize the SQ after submitting/waiting to see kernel updates
            self.sq.sync();

            let mut needs_sync = false;
            loop {
                match self.enqueue_single_op() {
                    // nothing more to do
                    EnqueueResult::Empty => {
                        if needs_sync {
                            self.sq.sync();
                        }
                        return Ok(());
                    }
                    // submit and try again
                    EnqueueResult::Full => break,
                    // keep going
                    EnqueueResult::Enqueued => needs_sync = true,
                }
            }

            // synchronize the SQ after adding entries
            self.sq.sync();

            // synchronize the CQ after filling the SQ to check if we have
            // completions to process before adding more entries
            self.cq.sync();

            if !self.cq.is_empty() {
                // we have completions to process, break out to handle them
                return Ok(());
            }
        }
    }

    fn flush_completion_queue(&mut self, errors: &mut Vec<BatchError>) {
        trace!("flushing io-uring completion queue");
        // copy the entries in batches for efficiency
        let mut cqes =
            [const { std::mem::MaybeUninit::uninit() }; SUBMISSION_QUEUE_SIZE as usize / 2];

        loop {
            let cqes = self.cq.fill(&mut cqes);
            if cqes.is_empty() {
                break;
            }

            // synchronize the CQ after taking entries to let the kernel know
            // we have processed them
            self.cq.sync();

            for entry in cqes {
                self.handle_completion_event(errors, entry);
            }
        }
    }

    fn handle_completion_event(
        &mut self,
        errors: &mut Vec<BatchError>,
        entry: &io_uring::cqueue::Entry,
    ) {
        let offset = entry.user_data();
        let Some(mut write_entry) = self.outstanding.remove(offset) else {
            errors.push(BatchError {
                batch_offset: offset,
                error_offset: offset,
                err: io::Error::other("completion event for unknown offset"),
                incurable: false,
            });
            return;
        };
        match write_entry.handle_result(entry.result()) {
            Ok(written) => {
                // copy offset off the entry before adding it back to the backlog
                let error_offset = write_entry.offset;
                if !write_entry.buffer.is_empty() {
                    // not fully written, re-queue
                    self.add_entry_to_backlog(write_entry);
                    if written != 0 {
                        // if zero, we would have already logged EAGAIN above
                        trace!("io-uring write at offset {offset} partially completed, re-queuing");
                        firewood_increment!(crate::registry::ring::PARTIAL_WRITE_RETRY, 1);
                    }
                }
                let carry;
                (self.written, carry) = self.written.overflowing_add(written);
                if carry {
                    errors.push(BatchError {
                        batch_offset: offset,
                        error_offset,
                        err: io::Error::other(format!(
                            "overflow after adding {written} bytes wrapping to {}",
                            self.written
                        )),
                        incurable: false,
                    });
                }
            }
            Err(err) => {
                errors.push(err);
            }
        }
    }
}

fn ignore_poll_error(err: &io::Error) -> bool {
    matches!(
        err.kind(),
        // EINTR happens on intentionally blocking syscalls when the thread
        // needs to be woken up for another reason (usually signal handlers),
        // we can safely ignore those and retry as the signal handler will have
        // completed by the time control returns to us.
        // EBUSY happens when the kernel submission queue is full and cannot
        // accept new entries; in that case we should break out to process
        // completions.
        std::io::ErrorKind::Interrupted | std::io::ErrorKind::ResourceBusy
    )
}

mod errors {
    use std::{fmt, io, path::PathBuf};

    use crate::FileIoError;

    /// A collection of errors encountered during an io-uring write batch.
    ///
    /// A least one error occured for this to be returned. Incurable errors can be
    /// observed via [`BatchErrors::incurable_error`].
    #[must_use]
    pub struct BatchErrors {
        pub(super) errors: Vec<BatchError>,
    }

    /// An error encountered during an io-uring write operation.
    pub struct BatchError {
        /// The offset of the original write operation that caused the error.
        ///
        /// This value is meaningful only if `incurable` is false.
        pub batch_offset: u64,

        /// The offset at which the error occurred, which may be different from
        /// `batch_offset` if a prior write operation partially succeeded.
        ///
        /// This value is meaningful only if `incurable` is false.
        pub error_offset: u64,

        /// The underlying I/O error.
        pub err: io::Error,

        /// Incurable errors are those that prevent us from communicating with the
        /// kernel via io-uring and force us to abort the entire batch immediately.
        ///
        /// The data is likely corrupted and should not be trusted. These errors
        /// likely should abort the process instead of being handled gracefully;
        /// however, we return them here so the abort can happen at a higher level.
        ///
        /// Curable errors are those that affect only a single write operation and
        /// may be recoverable, depending on the operation and context.
        pub(super) incurable: bool,
    }

    impl BatchError {
        /// If true, the error is incurable and prevents further communication
        /// with the kernel preventing further processing of the batch.
        pub const fn is_incurable(&self) -> bool {
            self.incurable
        }
    }

    impl fmt::Display for BatchError {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            if self.batch_offset == self.error_offset {
                write!(f, "{} at offset {}", self.err, self.batch_offset)
            } else {
                write!(
                    f,
                    "{} at offset {} (after writing up to offset {})",
                    self.err, self.error_offset, self.batch_offset
                )
            }
        }
    }

    impl BatchErrors {
        pub fn into_file_io_error(self, path: Option<PathBuf>) -> FileIoError {
            if self.errors.len() == 1 {
                let only = self.errors.into_iter().next().expect("just checked length");
                FileIoError::new(
                    only.err,
                    path,
                    only.error_offset,
                    Some(
                        if only.incurable {
                            "io_uring incurable write error"
                        } else {
                            "io_uring write error"
                        }
                        .to_owned(),
                    ),
                )
            } else {
                FileIoError::new(
                    io::Error::other(self),
                    path,
                    0,
                    Some("multiple io_uring write errors".to_string()),
                )
            }
        }

        pub fn errors(&self) -> &[BatchError] {
            &self.errors
        }

        pub fn incurable_error(&self) -> Option<&io::Error> {
            // if we have an incurable error, it will always be the last one added
            self.errors()
                .last()
                .and_then(|e| if e.is_incurable() { Some(&e.err) } else { None })
        }
    }

    impl fmt::Debug for BatchErrors {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            fmt::Display::fmt(self, f)
        }
    }

    impl fmt::Display for BatchErrors {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            use std::error::Error;
            match self.errors() {
                // zero-errors; we should never try to display these
                [] => write!(f, "no errors (this is unexpected)"),

                // a single error, forward to its Display impl
                [only] => fmt::Display::fmt(&only, f),

                // `#` formatting and we have an incurable error at the end,
                // print it first with its causes, then the rest
                [
                    rest @ ..,
                    incurable @ BatchError {
                        incurable: true, ..
                    },
                ] if f.alternate() => {
                    writeln!(
                        f,
                        "Encountered an incurable error after other errors: {incurable}"
                    )?;
                    for err in ErrorChain(incurable.err.source()) {
                        writeln!(f, "  Caused by: {err}")?;
                    }
                    writeln!(f, "Other errors:")?;
                    for (i, err) in rest.iter().enumerate() {
                        writeln!(f, "  {}: {err}", i.wrapping_add(1))?;
                        for err in ErrorChain(err.err.source()) {
                            writeln!(f, "    Caused by: {err}")?;
                        }
                    }
                    Ok(())
                }

                // regular formatting and we have an incurable error at the end,
                // print it last with a count of other errors
                [
                    rest @ ..,
                    incurable @ BatchError {
                        incurable: true, ..
                    },
                ] => {
                    write!(
                        f,
                        "Encountered an incurable error after {} other errors: {incurable}",
                        rest.len()
                    )
                }

                // `#` formatting and multiple errors, print them all with causes
                many @ [_, ..] if f.alternate() => {
                    writeln!(f, "Multiple errors encountered:")?;
                    for (i, err) in many.iter().enumerate() {
                        writeln!(f, "  {}: {err}", i.wrapping_add(1))?;
                        for err in ErrorChain(err.err.source()) {
                            writeln!(f, "    Caused by: {err}")?;
                        }
                    }
                    Ok(())
                }

                // regular formatting and multiple errors, print the first with
                // a count of the rest
                [first, rest @ ..] => {
                    write!(f, "{first} (and {} more errors)", rest.len())
                }
            }
        }
    }

    impl std::error::Error for BatchErrors {
        fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
            // if we have an incurable error, prefer that as the source
            self.incurable_error().map(|e| e as &_).or_else(|| {
                // Source only lets us return one error, so return the first.
                self.errors.first().map(|e| &e.err as &_)
            })
        }
    }

    struct ErrorChain<'a>(Option<&'a (dyn std::error::Error + 'static)>);

    impl<'a> Iterator for ErrorChain<'a> {
        type Item = &'a (dyn std::error::Error + 'static);

        fn next(&mut self) -> Option<Self::Item> {
            self.0.take().inspect(|err| self.0 = err.source())
        }
    }
}

/// The drop guard ensures a predicate is false when dropped.
///
/// This is inside its own module to avoid accidentally exposing the inner
/// `value`.
mod drop_guard {
    use std::{
        fmt::Debug,
        mem::ManuallyDrop,
        ops::{Deref, DerefMut},
    };

    pub(super) struct DropGuard<T: Debug, F: FnOnce(&T) -> bool = fn(&T) -> bool> {
        value: ManuallyDrop<T>,
        /// The predicate that must be false when dropped. If `true`, this indicates
        /// a logic error in the code using the guard and will cause a panic. If
        /// this happens during a panic unwind, the process will abort (which is
        /// desirable to avoid further corruption).
        predicate: ManuallyDrop<F>,
        message: &'static str,
    }

    impl<T: Debug, F: FnOnce(&T) -> bool> DropGuard<T, F> {
        pub(super) const fn new(value: T, predicate: F, message: &'static str) -> Self {
            Self {
                value: ManuallyDrop::new(value),
                predicate: ManuallyDrop::new(predicate),
                message,
            }
        }

        /// Forget the guard without evaluating the predicate; and return the
        /// inner value.
        ///
        /// This must take ownership of `self` to ensure the guard is forgotten
        /// and cannot be used afterward.
        pub(super) fn forget(self) -> T {
            let mut this = ManuallyDrop::new(self);
            #[expect(unsafe_code)]
            // SAFETY: we are forgetting the guard; therefore, we must drop the
            // predicate and move the value without running the `Drop` handler
            // on the guard. `ManuallyDrop(self)` ensures that.
            unsafe {
                ManuallyDrop::drop(&mut this.predicate);
                ManuallyDrop::take(&mut this.value)
            }
        }
    }

    impl<T: Debug, F: FnOnce(&T) -> bool> Drop for DropGuard<T, F> {
        fn drop(&mut self) {
            #[expect(unsafe_code)]
            // SAFETY: we are in the `Drop` handler; therefore, it is safe to
            // take ownership of the predicate to call it. `Drop` ensures this
            // is only called once and the values within the manually dropped
            // are not used afterward.
            let (value, predicate) = unsafe {
                (
                    ManuallyDrop::take(&mut self.value),
                    ManuallyDrop::take(&mut self.predicate),
                )
            };
            assert!(!(predicate)(&value), "{value:?}: {}", self.message);
        }
    }

    impl<T: Debug, F: FnOnce(&T) -> bool> Deref for DropGuard<T, F> {
        type Target = T;

        fn deref(&self) -> &Self::Target {
            &self.value
        }
    }

    impl<T: Debug, F: FnOnce(&T) -> bool> DerefMut for DropGuard<T, F> {
        fn deref_mut(&mut self) -> &mut Self::Target {
            &mut self.value
        }
    }
}
