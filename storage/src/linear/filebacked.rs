// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// This synchronous file layer is a simple implementation of what we
// want to do for I/O. This uses a [Mutex] lock around a simple `File`
// object. Instead, we probably should use an IO system that can perform multiple
// read/write operations at once

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 5 occurrences after enabling the lint."
)]
#![expect(
    clippy::indexing_slicing,
    reason = "Found 3 occurrences after enabling the lint."
)]
#![expect(
    clippy::missing_errors_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]

use parking_lot::Mutex;
use std::fs::{File, OpenOptions};
use std::io::Read;
use std::num::NonZero;
use std::os::unix::fs::FileExt;
use std::path::PathBuf;

use firewood_metrics::{GaugeExt, firewood_counter, firewood_gauge, firewood_histogram};
use lru::LruCache as EntryLruCache;
use lru_mem::LruCache as MemLruCache;

use crate::linear::ReadableNodeMode;
use crate::{CacheReadStrategy, CachedNode, LinearAddress, MaybePersistedNode, SharedNode};

use super::{FileIoError, OffsetReader, ReadableStorage, WritableStorage};

/// A [`ReadableStorage`] and [`WritableStorage`] backed by a single on-disk file.
///
/// This is the persistent storage backend for a node store. It owns the open
/// file handle — which can be advisory-locked via [`Self::lock`] to stop another
/// process from opening the same database — along with two in-memory LRU caches
/// that sit in front of it: one for trie nodes and one for free-list links.
///
/// Reads and writes go through positioned (`pread`/`pwrite`) syscalls, so they
/// share no file cursor and take no per-handle lock; the caches absorb most
/// reads so that the hot path rarely touches disk. The only serialization
/// points are the two cache mutexes and, on the io-uring path, the ring's
/// internal lock.
#[derive(Debug)]
pub struct FileBacked {
    /// Path of the on-disk database file.
    filename: PathBuf,
    /// In-memory cache of trie nodes keyed by their on-disk [`LinearAddress`].
    ///
    /// This cache is *memory-bounded* (its capacity is a byte budget, not an
    /// entry count) and sits on the hot read path: every node lookup checks it
    /// before falling back to disk. Reads populate it according to
    /// [`Self::cache_read_strategy`]; writes always populate it.
    cache: Mutex<MemLruCache<LinearAddress, CachedNode>>,
    /// In-memory cache of free-list links, mapping a freed node's
    /// [`LinearAddress`] to the address of the next free node in that list
    /// ([`None`] marks the end of the list).
    ///
    /// This cache is *entry-count bounded* and accelerates node allocation by
    /// avoiding a disk read to walk the free list. A lookup is destructive: a
    /// cache hit pops the entry, since allocating from the free list consumes it.
    free_list_cache: Mutex<EntryLruCache<LinearAddress, Option<LinearAddress>>>,
    /// Policy controlling which reads get inserted into [`Self::cache`]
    /// (no reads, branch reads only, or all reads). Writes are always cached
    /// regardless of this setting.
    cache_read_strategy: CacheReadStrategy,
    /// The node hashing algorithm (MerkleDB or Ethereum) this file was opened
    /// with. Reported via [`ReadableStorage::node_hash_algorithm`] so callers
    /// can detect a mismatch between the file and the running build.
    node_hash_algorithm: crate::NodeHashAlgorithm,
    /// `io_uring` proxy used to submit batched writes to the file
    /// (see [`WritableStorage::write_batch`]). The proxy owns only the
    /// `io_uring` instance, not the descriptor — `fd` is passed by `RawFd` into
    /// each `write_batch` call.
    ///
    /// Present only under `cfg(io_uring)`, that is on Linux with the `io-uring`
    /// feature enabled.
    ///
    /// Declared before `fd` so that it is dropped first (struct fields are
    /// dropped in declaration order).
    #[cfg(io_uring)]
    ring: super::io_uring::IoUringProxy,
    /// The open file handle backing this storage, wrapped so that the advisory
    /// lock taken by [`Self::lock`] is released when the handle is dropped.
    fd: UnlockOnDrop,
}

impl FileBacked {
    /// Acquire an advisory lock on the underlying file to prevent multiple processes
    /// from accessing it simultaneously
    pub fn lock(&self) -> Result<(), FileIoError> {
        self.fd.try_lock().map_err(|e| {
            let context =
                "unable to obtain advisory lock: database may be opened by another instance"
                    .to_owned();
            // Convert TryLockError to a generic IO error for our FileIoError
            let io_error = std::io::Error::new(std::io::ErrorKind::WouldBlock, e);
            self.file_io_error(io_error, 0, Some(context))
        })
    }

    /// Create or open a file at a given path
    pub fn new(
        path: PathBuf,
        node_cache_memory_limit: NonZero<usize>,
        free_list_cache_size: NonZero<usize>,
        truncate: bool,
        create: bool,
        cache_read_strategy: CacheReadStrategy,
        node_hash_algorithm: crate::NodeHashAlgorithm,
    ) -> Result<Self, FileIoError> {
        let fd = OpenOptions::new()
            .read(true)
            .write(true)
            .truncate(truncate)
            .create(create)
            .open(&path)
            .map_err(|e| FileIoError {
                inner: e,
                filename: Some(path.clone()),
                offset: 0,
                context: Some("file open".to_owned()),
            })?;

        #[cfg(io_uring)]
        let ring = super::io_uring::IoUringProxy::new().map_err(|err| FileIoError {
            inner: err,
            filename: Some(path.clone()),
            offset: 0,
            context: Some("io_uring setup".to_owned()),
        })?;

        Ok(Self {
            cache: Mutex::new(MemLruCache::new(node_cache_memory_limit.get())),
            free_list_cache: Mutex::new(EntryLruCache::new(free_list_cache_size)),
            cache_read_strategy,
            filename: path,
            node_hash_algorithm,
            #[cfg(io_uring)]
            ring,
            fd: UnlockOnDrop(fd),
        })
    }

    /// Set the length of this file.
    pub fn set_len(&self, size: u64) -> Result<(), FileIoError> {
        self.fd
            .set_len(size)
            .map_err(|e| self.file_io_error(e, 0, Some("set_len".to_owned())))
    }
}

impl ReadableStorage for FileBacked {
    fn node_hash_algorithm(&self) -> crate::NodeHashAlgorithm {
        self.node_hash_algorithm
    }

    fn stream_from(&self, addr: u64) -> Result<impl OffsetReader, FileIoError> {
        firewood_counter!(READ_NODE, "from" => "file").increment(1);
        firewood_counter!(IO_READ_COUNT).increment(1);
        Ok(PredictiveReader::new(self, addr))
    }

    fn size(&self) -> Result<u64, FileIoError> {
        Ok(self
            .fd
            .metadata()
            .map_err(|e| self.file_io_error(e, 0, Some("size".to_owned())))?
            .len())
    }

    fn read_cached_node(&self, addr: LinearAddress, mode: ReadableNodeMode) -> Option<SharedNode> {
        // BLOCKING: mutex lock on the node LRU cache. This is in the hot read path; every
        // node lookup acquires this lock. Under concurrent readers the lock becomes a serialization
        // point — all trie traversals contend here. Impact scales with reader concurrency.
        let mut guard = self.cache.lock();
        let cached = guard.get(&addr).map(|cached_node| cached_node.0.clone());
        firewood_counter!(CACHE_NODE, "mode" => mode.as_str(), "type" => if cached.is_some() { "hit" } else { "miss" }).increment(1);
        cached
    }

    fn free_list_cache(&self, addr: LinearAddress) -> Option<Option<LinearAddress>> {
        // BLOCKING: mutex lock on the free-list LRU cache. Called during node allocation on
        // every proposal/commit. Contends with writes that also update the free-list cache.
        let mut guard = self.free_list_cache.lock();
        let cached = guard.pop(&addr);
        firewood_counter!(CACHE_FREELIST, "type" => if cached.is_some() { "hit" } else { "miss" })
            .increment(1);
        firewood_gauge!(FREELIST_CACHE_SIZE).set_integer(guard.len());
        cached
    }

    fn cache_read_strategy(&self) -> &CacheReadStrategy {
        &self.cache_read_strategy
    }

    fn cache_node(&self, addr: LinearAddress, node: SharedNode) {
        match self.cache_read_strategy {
            CacheReadStrategy::WritesOnly => {
                // we don't cache reads
            }
            CacheReadStrategy::All => {
                // BLOCKING: cache mutex on read path (only when CacheReadStrategy::All is set).
                let mut guard = self.cache.lock();
                CachedNode(node).insert_into_cache(&mut guard, addr);
            }
            CacheReadStrategy::BranchReads => {
                if !node.is_leaf() {
                    // BLOCKING: cache mutex on branch-read path (CacheReadStrategy::BranchReads).
                    let mut guard = self.cache.lock();
                    CachedNode(node).insert_into_cache(&mut guard, addr);
                }
            }
        }
    }

    fn filename(&self) -> Option<PathBuf> {
        Some(self.filename.clone())
    }
}

impl WritableStorage for FileBacked {
    fn write(&self, offset: u64, object: &[u8]) -> Result<usize, FileIoError> {
        // BLOCKING: `write_all_at` is a blocking pwrite(2) syscall. Duration depends on I/O
        // scheduler, storage device latency, and page-cache pressure. On a cold page cache or
        // slow device this can be tens of milliseconds. Called per-node on the non-io-uring path.
        self.fd
            .write_all_at(object, offset)
            .map(|()| {
                firewood_counter!(IO_WRITE_COUNT).increment(1);
                firewood_counter!(IO_BYTES_WRITTEN).increment(object.len() as u64);
                object.len()
            })
            .map_err(|e| self.file_io_error(e, offset, Some("write".to_owned())))
    }

    /// Overrides the serial [`WritableStorage::write_batch`] default with a
    /// single batched ring submission. Compiled in only under `cfg(io_uring)`;
    /// otherwise the trait default applies.
    #[cfg(io_uring)]
    fn write_batch<'a, I: IntoIterator<Item = (u64, &'a [u8])> + Clone>(
        &self,
        writes: I,
    ) -> Result<usize, FileIoError> {
        use std::os::fd::AsRawFd;
        self.ring
            .write_batch(self.fd.as_raw_fd(), writes)
            .map_err(|err| err.into_file_io_error(Some(self.filename.clone())))
    }

    fn write_cached_nodes(
        &self,
        nodes: impl IntoIterator<Item = MaybePersistedNode>,
    ) -> Result<(), FileIoError> {
        // BLOCKING: cache mutex held while inserting every node in the batch. This is the write
        // path after a persist; the lock is held for the entire batch iteration, blocking all
        // concurrent reads that need the cache. Larger batches mean longer hold times.
        let mut guard = self.cache.lock();
        for maybe_persisted_node in nodes {
            // Since we know the node is in Allocated state, we can get both address and shared node
            let (addr, shared_node) = maybe_persisted_node
                .allocated_info()
                .expect("node should be allocated");

            CachedNode(shared_node).insert_into_cache(&mut guard, addr);
            // The node can now be read from the general cache, so we can delete the local copy
            maybe_persisted_node.persist_at(addr);
        }
        Ok(())
    }

    fn invalidate_cached_nodes<'a>(&self, nodes: impl Iterator<Item = &'a MaybePersistedNode>) {
        // BLOCKING: cache mutex held while evicting all invalidated nodes. Same concern as
        // `write_cached_nodes` — blocks concurrent reads for the duration of the loop.
        let mut guard = self.cache.lock();
        for addr in nodes.filter_map(MaybePersistedNode::as_linear_address) {
            guard.remove(&addr);
        }
        // Update cache metrics after removals
        CachedNode::update_cache_metrics(&guard);
    }

    fn add_to_free_list_cache(&self, addr: LinearAddress, next: Option<LinearAddress>) {
        // BLOCKING: free-list cache mutex. Called once per freed node during reap; contends
        // with concurrent readers calling `free_list_cache()`.
        let mut guard = self.free_list_cache.lock();
        guard.put(addr, next);
        firewood_gauge!(FREELIST_CACHE_SIZE).set_integer(guard.len());
    }
}

const PREDICTIVE_READ_BUFFER_SIZE: usize = 1024;

/// A reader that can predictively read from a file, avoiding reading past boundaries, but reading in 1k chunks
struct PredictiveReader<'a> {
    fd: &'a File,
    buffer: [u8; PREDICTIVE_READ_BUFFER_SIZE],
    offset: u64,
    len: usize,
    pos: usize,
    bytes_read: u64,
    started: std::time::Instant,
}

impl<'a> PredictiveReader<'a> {
    fn new(fb: &'a FileBacked, start: u64) -> Self {
        let fd = &fb.fd;

        Self {
            fd,
            buffer: [0u8; PREDICTIVE_READ_BUFFER_SIZE],
            offset: start,
            len: 0,
            pos: 0,
            bytes_read: 0,
            started: std::time::Instant::now(),
        }
    }
}

impl Drop for PredictiveReader<'_> {
    fn drop(&mut self) {
        firewood_histogram!(cheap: IO_READ_DURATION_SECONDS)
            .record(self.started.elapsed().as_secs_f64());
        firewood_counter!(IO_BYTES_READ).increment(self.bytes_read);
    }
}

impl Read for PredictiveReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
        if self.len == self.pos {
            // BLOCKING: `read_at` is a blocking pread(2) syscall. Every cache miss on the read
            // path goes through here. On warm page cache this is sub-microsecond; on cold cache
            // or slow storage this can be many milliseconds. Called on any trie traversal that
            // encounters a node not in the in-memory cache.
            let read = self.fd.read_at(&mut self.buffer, self.offset)?;
            self.offset += read as u64;
            self.len = read;
            self.pos = 0;
        }
        let max_to_return = std::cmp::min(buf.len(), self.len - self.pos);
        #[expect(
            clippy::disallowed_methods,
            reason = "both slices are exactly max_to_return long"
        )]
        buf[..max_to_return].copy_from_slice(&self.buffer[self.pos..self.pos + max_to_return]);
        self.pos += max_to_return;
        self.bytes_read += max_to_return as u64;
        Ok(max_to_return)
    }
}

impl OffsetReader for PredictiveReader<'_> {
    fn offset(&self) -> u64 {
        self.offset - self.len as u64 + self.pos as u64
    }
}

#[derive(Debug)]
struct UnlockOnDrop(File);

impl Drop for UnlockOnDrop {
    fn drop(&mut self) {
        // ignore the error, we might not have ever called `lock`
        _ = self.0.unlock();
    }
}

impl std::ops::Deref for UnlockOnDrop {
    type Target = File;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl std::ops::DerefMut for UnlockOnDrop {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

#[cfg(test)]
mod test {
    use crate::NodeHashAlgorithm;

    use super::*;
    use nonzero_ext::nonzero;
    use std::io::Write;
    use tempfile::NamedTempFile;

    #[test]
    fn basic_reader_test() {
        let mut tf = NamedTempFile::new().unwrap();
        let path = tf.path().to_path_buf();
        let output = tf.as_file_mut();
        write!(output, "hello world").unwrap();

        // whole thing at once, this is always less than 1K so it should
        // read the whole thing in
        let fb = FileBacked::new(
            path,
            nonzero!(10usize),
            nonzero!(10usize),
            false,
            true,
            CacheReadStrategy::WritesOnly,
            NodeHashAlgorithm::compile_option(),
        )
        .unwrap();

        let mut reader = fb.stream_from(0).unwrap();
        let mut buf: String = String::new();
        assert_eq!(reader.read_to_string(&mut buf).unwrap(), 11);
        assert_eq!(buf, "hello world".to_owned());
        assert_eq!(0, reader.read(&mut [0u8; 1]).unwrap());

        // byte at a time
        let mut reader = fb.stream_from(0).unwrap();
        for ch in b"hello world" {
            let mut buf = [0u8; 1];
            let read = reader.read(&mut buf).unwrap();
            assert_eq!(read, 1);
            assert_eq!(buf[0], *ch);
        }
        assert_eq!(0, reader.read(&mut [0u8; 1]).unwrap());

        // with offset
        let mut reader = fb.stream_from(6).unwrap();
        buf = String::new();
        assert_eq!(reader.read_to_string(&mut buf).unwrap(), 5);
        assert_eq!(buf, "world".to_owned());
    }

    #[test]
    fn big_file() {
        let mut tf = NamedTempFile::new().unwrap();
        let path = tf.path().to_path_buf();
        let output = tf.as_file_mut();
        for _ in 0..1000 {
            write!(output, "hello world").unwrap();
        }

        let fb = FileBacked::new(
            path,
            nonzero!(10usize),
            nonzero!(10usize),
            false,
            true,
            CacheReadStrategy::WritesOnly,
            NodeHashAlgorithm::compile_option(),
        )
        .unwrap();

        let mut reader = fb.stream_from(0).unwrap();
        let mut buf: String = String::new();
        assert_eq!(reader.read_to_string(&mut buf).unwrap(), 11000);
        assert_eq!(buf.len(), 11000);
    }
}
