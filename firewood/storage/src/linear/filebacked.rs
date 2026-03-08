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

use firewood_metrics::{firewood_increment, firewood_set};
use lru::LruCache as EntryLruCache;
use lru_mem::LruCache as MemLruCache;

use crate::{CacheReadStrategy, CachedNode, LinearAddress, MaybePersistedNode, SharedNode};

use super::{FileIoError, OffsetReader, ReadableStorage, WritableStorage};

/// A [`ReadableStorage`] and [`WritableStorage`] backed by a file
#[derive(Debug)]
pub struct FileBacked {
    filename: PathBuf,
    cache: Mutex<MemLruCache<LinearAddress, CachedNode>>,
    free_list_cache: Mutex<EntryLruCache<LinearAddress, Option<LinearAddress>>>,
    cache_read_strategy: CacheReadStrategy,
    node_hash_algorithm: crate::NodeHashAlgorithm,
    // keep before `fd` so that it is dropped first (fields are dropped in the order they are declared)
    #[cfg(feature = "io-uring")]
    ring: super::io_uring::IoUringProxy,
    fd: UnlockOnDrop,
}

impl FileBacked {
    /// Acquire an advisory lock on the underlying file to prevent multiple processes
    /// from accessing it simultaneously
    pub fn lock(&self) -> Result<(), FileIoError> {
        self.fd.try_lock().map_err(|e| {
            let context =
                "unable to obtain advisory lock: database may be opened by another instance"
                    .to_string();
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
                context: Some("file open".to_string()),
            })?;

        #[cfg(feature = "io-uring")]
        let ring = super::io_uring::IoUringProxy::new().map_err(|err| FileIoError {
            inner: err,
            filename: Some(path.clone()),
            offset: 0,
            context: Some("io_uring setup".to_string()),
        })?;

        Ok(Self {
            cache: Mutex::new(MemLruCache::new(node_cache_memory_limit.get())),
            free_list_cache: Mutex::new(EntryLruCache::new(free_list_cache_size)),
            cache_read_strategy,
            filename: path,
            node_hash_algorithm,
            #[cfg(feature = "io-uring")]
            ring,
            fd: UnlockOnDrop(fd),
        })
    }

    /// Set the length of this file.
    pub fn set_len(&self, size: u64) -> Result<(), FileIoError> {
        self.fd
            .set_len(size)
            .map_err(|e| self.file_io_error(e, 0, Some("set_len".to_string())))
    }
}

impl ReadableStorage for FileBacked {
    fn node_hash_algorithm(&self) -> crate::NodeHashAlgorithm {
        self.node_hash_algorithm
    }

    fn stream_from(&self, addr: u64) -> Result<impl OffsetReader, FileIoError> {
        firewood_increment!(crate::registry::READ_NODE, 1, "from" => "file");
        Ok(PredictiveReader::new(self, addr))
    }

    fn size(&self) -> Result<u64, FileIoError> {
        Ok(self
            .fd
            .metadata()
            .map_err(|e| self.file_io_error(e, 0, Some("size".to_string())))?
            .len())
    }

    fn read_cached_node(&self, addr: LinearAddress, mode: &'static str) -> Option<SharedNode> {
        let mut guard = self.cache.lock();
        let cached = guard.get(&addr).map(|cached_node| cached_node.0.clone());
        firewood_increment!(crate::registry::CACHE_NODE, 1, "mode" => mode, "type" => if cached.is_some() { "hit" } else { "miss" });
        cached
    }

    fn free_list_cache(&self, addr: LinearAddress) -> Option<Option<LinearAddress>> {
        let mut guard = self.free_list_cache.lock();
        let cached = guard.pop(&addr);
        firewood_increment!(crate::registry::CACHE_FREELIST, 1, "type" => if cached.is_some() { "hit" } else { "miss" });
        firewood_set!(crate::registry::FREELIST_CACHE_SIZE, guard.len());
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
                let mut guard = self.cache.lock();
                CachedNode(node).insert_into_cache(&mut guard, addr);
            }
            CacheReadStrategy::BranchReads => {
                if !node.is_leaf() {
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
        self.fd
            .write_all_at(object, offset)
            .map(|()| object.len())
            .map_err(|e| self.file_io_error(e, offset, Some("write".to_string())))
    }

    #[cfg(feature = "io-uring")]
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
        let mut guard = self.cache.lock();
        for addr in nodes.filter_map(MaybePersistedNode::as_linear_address) {
            guard.remove(&addr);
        }
        // Update cache metrics after removals
        CachedNode::update_cache_metrics(&guard);
    }

    fn add_to_free_list_cache(&self, addr: LinearAddress, next: Option<LinearAddress>) {
        let mut guard = self.free_list_cache.lock();
        guard.put(addr, next);
        firewood_set!(crate::registry::FREELIST_CACHE_SIZE, guard.len());
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
    started: coarsetime::Instant,
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
            started: coarsetime::Instant::now(),
        }
    }
}

impl Drop for PredictiveReader<'_> {
    fn drop(&mut self) {
        let elapsed = self.started.elapsed();
        firewood_increment!(crate::registry::IO_READ_MS, elapsed.as_millis());
        firewood_increment!(crate::registry::IO_READ_COUNT, 1);
    }
}

impl Read for PredictiveReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
        if self.len == self.pos {
            let bytes_left_in_page = PREDICTIVE_READ_BUFFER_SIZE
                - (self.offset % PREDICTIVE_READ_BUFFER_SIZE as u64) as usize;
            let read = self
                .fd
                .read_at(&mut self.buffer[..bytes_left_in_page], self.offset)?;
            self.offset += read as u64;
            self.len = read;
            self.pos = 0;
        }
        let max_to_return = std::cmp::min(buf.len(), self.len - self.pos);
        buf[..max_to_return].copy_from_slice(&self.buffer[self.pos..self.pos + max_to_return]);
        self.pos += max_to_return;
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
    #![expect(clippy::unwrap_used)]

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
        assert_eq!(buf, "hello world".to_string());
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
        assert_eq!(buf, "world".to_string());
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
