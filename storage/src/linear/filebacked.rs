// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// This synchronous file layer is a simple implementation of what we
// want to do for I/O. This uses a [Mutex] lock around a simple `File`
// object. Instead, we probably should use an IO system that can perform multiple
// read/write operations at once

use std::fs::{File, OpenOptions};
use std::io::{Error, Read};
use std::num::NonZero;
use std::os::unix::fs::FileExt;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use lru::LruCache;
use metrics::counter;

use crate::{LinearAddress, Node};

use super::{ReadableStorage, WritableStorage};

#[derive(Debug)]
/// A [ReadableStorage] backed by a file
pub struct FileBacked {
    fd: File,
    cache: Mutex<LruCache<LinearAddress, Arc<Node>>>,
    free_list_cache: Mutex<LruCache<LinearAddress, Option<LinearAddress>>>,
}

impl FileBacked {
    /// Create or open a file at a given path
    pub fn new(
        path: PathBuf,
        node_cache_size: NonZero<usize>,
        free_list_cache_size: NonZero<usize>,
        truncate: bool,
    ) -> Result<Self, Error> {
        let fd = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(truncate)
            .open(path)?;

        Ok(Self {
            fd,
            cache: Mutex::new(LruCache::new(node_cache_size)),
            free_list_cache: Mutex::new(LruCache::new(free_list_cache_size)),
        })
    }
}

impl ReadableStorage for FileBacked {
    fn stream_from(&self, addr: u64) -> Result<Box<dyn Read + '_>, Error> {
        Ok(Box::new(PredictiveReader::new(self, addr)))
    }

    fn size(&self) -> Result<u64, Error> {
        Ok(self.fd.metadata()?.len())
    }

    fn read_cached_node(&self, addr: LinearAddress) -> Option<Arc<Node>> {
        let mut guard = self.cache.lock().expect("poisoned lock");
        let cached = guard.get(&addr).cloned();
        counter!("firewood.cache.node", "type" => if cached.is_some() { "hit" } else { "miss" })
            .increment(1);
        cached
    }

    fn free_list_cache(&self, addr: LinearAddress) -> Option<Option<LinearAddress>> {
        let mut guard = self.free_list_cache.lock().expect("poisoned lock");
        let cached = guard.pop(&addr);
        counter!("firewood.cache.freelist", "type" => if cached.is_some() { "hit" } else { "miss" }).increment(1);
        cached
    }
}

impl WritableStorage for FileBacked {
    fn write(&self, offset: u64, object: &[u8]) -> Result<usize, Error> {
        self.fd.write_at(object, offset)
    }

    fn write_cached_nodes<'a>(
        &self,
        nodes: impl Iterator<Item = (&'a std::num::NonZero<u64>, &'a std::sync::Arc<crate::Node>)>,
    ) -> Result<(), Error> {
        let mut guard = self.cache.lock().expect("poisoned lock");
        for (addr, node) in nodes {
            guard.put(*addr, node.clone());
        }
        Ok(())
    }

    fn invalidate_cached_nodes<'a>(&self, addresses: impl Iterator<Item = &'a LinearAddress>) {
        let mut guard = self.cache.lock().expect("poisoned lock");
        for addr in addresses {
            guard.pop(addr);
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
        }
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

#[cfg(test)]
mod test {
    use super::*;
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
            NonZero::new(10).unwrap(),
            NonZero::new(10).unwrap(),
            false,
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
            NonZero::new(10).unwrap(),
            NonZero::new(10).unwrap(),
            false,
        )
        .unwrap();
        let mut reader = fb.stream_from(0).unwrap();
        let mut buf: String = String::new();
        assert_eq!(reader.read_to_string(&mut buf).unwrap(), 11000);
        assert_eq!(buf.len(), 11000);
    }
}
