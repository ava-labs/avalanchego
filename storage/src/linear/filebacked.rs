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
#![expect(
    clippy::missing_fields_in_debug,
    reason = "Found 1 occurrences after enabling the lint."
)]

use std::fs::{File, OpenOptions};
use std::io::Read;
use std::num::NonZero;
#[cfg(unix)]
use std::os::unix::fs::FileExt;
#[cfg(windows)]
use std::os::windows::fs::FileExt;
use std::path::PathBuf;
use std::sync::Mutex;

use lru::LruCache;
use metrics::counter;

use crate::{CacheReadStrategy, LinearAddress, MaybePersistedNode, SharedNode};

use super::{FileIoError, OffsetReader, ReadableStorage, WritableStorage};

/// A [`ReadableStorage`] and [`WritableStorage`] backed by a file
pub struct FileBacked {
    fd: File,
    filename: PathBuf,
    cache: Mutex<LruCache<LinearAddress, SharedNode>>,
    free_list_cache: Mutex<LruCache<LinearAddress, Option<LinearAddress>>>,
    cache_read_strategy: CacheReadStrategy,
    #[cfg(feature = "io-uring")]
    pub(crate) ring: Mutex<io_uring::IoUring>,
}

// Manual implementation since ring doesn't implement Debug :(
impl std::fmt::Debug for FileBacked {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("FileBacked")
            .field("fd", &self.fd)
            .field("cache", &self.cache)
            .field("free_list_cache", &self.free_list_cache)
            .field("cache_read_strategy", &self.cache_read_strategy)
            .finish()
    }
}

impl FileBacked {
    /// Make a write operation from a raw data buffer for this file
    #[cfg(feature = "io-uring")]
    pub(crate) fn make_op(&self, data: &[u8]) -> io_uring::opcode::Write {
        use std::os::fd::AsRawFd as _;

        use io_uring::opcode::Write;
        use io_uring::types;

        Write::new(
            types::Fd(self.fd.as_raw_fd()),
            data.as_ptr(),
            data.len() as _,
        )
    }

    #[cfg(feature = "io-uring")]
    // The size of the kernel ring buffer. This buffer will control how many writes we do with
    // a single system call.
    // TODO: make this configurable
    pub(crate) const RINGSIZE: u32 = 32;

    /// Create or open a file at a given path
    pub fn new(
        path: PathBuf,
        node_cache_size: NonZero<usize>,
        free_list_cache_size: NonZero<usize>,
        truncate: bool,
        cache_read_strategy: CacheReadStrategy,
    ) -> Result<Self, FileIoError> {
        let fd = OpenOptions::new()
            .read(true)
            .write(true)
            .truncate(truncate)
            .create(true)
            .open(&path)
            .map_err(|e| FileIoError {
                inner: e,
                filename: Some(path.clone()),
                offset: 0,
                context: Some("file open".to_string()),
            })?;

        #[cfg(feature = "io-uring")]
        let ring = {
            // The kernel will stop the worker thread in this many ms if there is no work to do
            const IDLETIME_MS: u32 = 1000;

            io_uring::IoUring::builder()
                // we promise not to fork and we are the only issuer of writes to this ring
                .dontfork()
                .setup_single_issuer()
                // completion queue should be larger than the request queue, we allocate double
                .setup_cqsize(FileBacked::RINGSIZE * 2)
                // start a kernel thread to do the IO
                .setup_sqpoll(IDLETIME_MS)
                .build(FileBacked::RINGSIZE)
                .map_err(|e| FileIoError {
                    inner: e,
                    filename: Some(path.clone()),
                    offset: 0,
                    context: Some("IO-uring setup".to_string()),
                })?
        };

        Ok(Self {
            fd,
            cache: Mutex::new(LruCache::new(node_cache_size)),
            free_list_cache: Mutex::new(LruCache::new(free_list_cache_size)),
            cache_read_strategy,
            filename: path,
            #[cfg(feature = "io-uring")]
            ring: ring.into(),
        })
    }
}

impl ReadableStorage for FileBacked {
    fn stream_from(&self, addr: u64) -> Result<Box<dyn OffsetReader + '_>, FileIoError> {
        counter!("firewood.read_node", "from" => "file").increment(1);
        Ok(Box::new(PredictiveReader::new(self, addr)))
    }

    fn size(&self) -> Result<u64, FileIoError> {
        Ok(self
            .fd
            .metadata()
            .map_err(|e| self.file_io_error(e, 0, Some("size".to_string())))?
            .len())
    }

    fn read_cached_node(&self, addr: LinearAddress, mode: &'static str) -> Option<SharedNode> {
        let mut guard = self.cache.lock().expect("poisoned lock");
        let cached = guard.get(&addr).cloned();
        counter!("firewood.cache.node", "mode" => mode, "type" => if cached.is_some() { "hit" } else { "miss" })
            .increment(1);
        cached
    }

    fn free_list_cache(&self, addr: LinearAddress) -> Option<Option<LinearAddress>> {
        let mut guard = self.free_list_cache.lock().expect("poisoned lock");
        let cached = guard.pop(&addr);
        counter!("firewood.cache.freelist", "type" => if cached.is_some() { "hit" } else { "miss" }).increment(1);
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
                let mut guard = self.cache.lock().expect("poisoned lock");
                guard.put(addr, node);
            }
            CacheReadStrategy::BranchReads => {
                if !node.is_leaf() {
                    let mut guard = self.cache.lock().expect("poisoned lock");
                    guard.put(addr, node);
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
        #[cfg(unix)]
        {
            self.fd
                .write_at(object, offset)
                .map_err(|e| self.file_io_error(e, offset, Some("write".to_string())))
        }
        #[cfg(windows)]
        {
            self.fd
                .seek_write(object, offset)
                .map_err(|e| self.file_io_error(e, offset, Some("write".to_string())))
        }
    }

    fn write_cached_nodes(
        &self,
        nodes: impl IntoIterator<Item = (LinearAddress, SharedNode)>,
    ) -> Result<(), FileIoError> {
        let mut guard = self.cache.lock().expect("poisoned lock");
        for (addr, node) in nodes {
            guard.put(addr, node);
        }
        Ok(())
    }

    fn invalidate_cached_nodes<'a>(&self, nodes: impl Iterator<Item = &'a MaybePersistedNode>) {
        let mut guard = self.cache.lock().expect("poisoned lock");
        for addr in nodes.filter_map(MaybePersistedNode::as_linear_address) {
            guard.pop(&addr);
        }
    }

    fn add_to_free_list_cache(&self, addr: LinearAddress, next: Option<LinearAddress>) {
        let mut guard = self.free_list_cache.lock().expect("poisoned lock");
        guard.put(addr, next);
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
        counter!("firewood.io.read_ms").increment(elapsed.as_millis());
        counter!("firewood.io.read").increment(1);
    }
}

impl Read for PredictiveReader<'_> {
    fn read(&mut self, buf: &mut [u8]) -> Result<usize, std::io::Error> {
        if self.len == self.pos {
            let bytes_left_in_page = PREDICTIVE_READ_BUFFER_SIZE
                - (self.offset % PREDICTIVE_READ_BUFFER_SIZE as u64) as usize;
            #[cfg(unix)]
            let read = self
                .fd
                .read_at(&mut self.buffer[..bytes_left_in_page], self.offset)?;
            #[cfg(windows)]
            let read = self
                .fd
                .seek_read(&mut self.buffer[..bytes_left_in_page], self.offset)?;
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

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]

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
            CacheReadStrategy::WritesOnly,
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
            CacheReadStrategy::WritesOnly,
        )
        .unwrap();

        let mut reader = fb.stream_from(0).unwrap();
        let mut buf: String = String::new();
        assert_eq!(reader.read_to_string(&mut buf).unwrap(), 11000);
        assert_eq!(buf.len(), 11000);
    }
}
