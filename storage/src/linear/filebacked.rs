// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// during development only
#![allow(dead_code)]

// This synchronous file layer is a simple implementation of what we
// want to do for I/O. This uses a [Mutex] lock around a simple `File`
// object. Instead, we probably should use an IO system that can perform multiple
// read/write operations at once

use std::fs::{File, OpenOptions};
use std::io::{Error, Read, Seek};
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
    fd: Mutex<File>,
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
            fd: Mutex::new(fd),
            cache: Mutex::new(LruCache::new(node_cache_size)),
            free_list_cache: Mutex::new(LruCache::new(free_list_cache_size)),
        })
    }
}

impl ReadableStorage for FileBacked {
    fn stream_from(&self, addr: u64) -> Result<Box<dyn Read>, Error> {
        let mut fd = self.fd.lock().expect("p");
        fd.seek(std::io::SeekFrom::Start(addr))?;
        Ok(Box::new(fd.try_clone().expect("poisoned lock")))
    }

    fn size(&self) -> Result<u64, Error> {
        self.fd
            .lock()
            .expect("poisoned lock")
            .seek(std::io::SeekFrom::End(0))
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
        self.fd
            .lock()
            .expect("poisoned lock")
            .write_at(object, offset)
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
