// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::shale::{CachedStore, CachedView, SendSyncDerefMut, SpaceId};
use std::{
    borrow::BorrowMut,
    fmt::Debug,
    ops::{Deref, DerefMut},
    sync::{Arc, RwLock},
};

/// Purely volatile, vector-based implementation for [CachedStore]. This is good for testing or trying
/// out stuff (persistent data structures) built on [ShaleStore](super::ShaleStore) in memory, without having to write
/// your own [CachedStore] implementation.
#[derive(Debug)]
pub struct PlainMem {
    space: Arc<RwLock<Vec<u8>>>,
    id: SpaceId,
}

impl PlainMem {
    pub fn new(size: u64, id: SpaceId) -> Self {
        // TODO: this could cause problems on a 32-bit system
        let space = Arc::new(RwLock::new(vec![0; size as usize]));
        Self { space, id }
    }
}

impl CachedStore for PlainMem {
    fn get_view(
        &self,
        offset: usize,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let length = length as usize;
        #[allow(clippy::unwrap_used)]
        if offset + length > self.space.read().unwrap().len() {
            None
        } else {
            Some(Box::new(PlainMemView {
                offset,
                length,
                mem: Self {
                    space: self.space.clone(),
                    id: self.id,
                },
            }))
        }
    }

    fn get_shared(&self) -> Box<dyn SendSyncDerefMut<Target = dyn CachedStore>> {
        Box::new(PlainMemShared(Self {
            space: self.space.clone(),
            id: self.id,
        }))
    }

    fn write(&mut self, offset: usize, change: &[u8]) {
        let length = change.len();
        #[allow(clippy::unwrap_used)]
        let mut vect = self.space.deref().write().unwrap();
        #[allow(clippy::indexing_slicing)]
        vect.as_mut_slice()[offset..offset + length].copy_from_slice(change);
    }

    fn id(&self) -> SpaceId {
        self.id
    }
}

#[derive(Debug)]
struct PlainMemView {
    offset: usize,
    length: usize,
    mem: PlainMem,
}

struct PlainMemShared(PlainMem);

impl DerefMut for PlainMemShared {
    fn deref_mut(&mut self) -> &mut Self::Target {
        self.0.borrow_mut()
    }
}

impl Deref for PlainMemShared {
    type Target = dyn CachedStore;
    fn deref(&self) -> &(dyn CachedStore + 'static) {
        &self.0
    }
}

impl CachedView for PlainMemView {
    type DerefReturn = Vec<u8>;

    fn as_deref(&self) -> Self::DerefReturn {
        #[allow(clippy::indexing_slicing, clippy::unwrap_used)]
        self.mem.space.read().unwrap()[self.offset..self.offset + self.length].to_vec()
    }
}

// Purely volatile, dynamically allocated vector-based implementation for [CachedStore]. This is similar to
/// [PlainMem]. The only difference is, when [write] dynamically allocate more space if original space is
/// not enough.
#[derive(Debug)]
pub struct DynamicMem {
    space: Arc<RwLock<Vec<u8>>>,
    id: SpaceId,
}

impl DynamicMem {
    pub fn new(size: u64, id: SpaceId) -> Self {
        let space = Arc::new(RwLock::new(vec![0; size as usize]));
        Self { space, id }
    }
}

impl CachedStore for DynamicMem {
    fn get_view(
        &self,
        offset: usize,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let length = length as usize;
        let size = offset + length;
        #[allow(clippy::unwrap_used)]
        let mut space = self.space.write().unwrap();

        // Increase the size if the request range exceeds the current limit.
        if size > space.len() {
            space.resize(size, 0);
        }

        Some(Box::new(DynamicMemView {
            offset,
            length,
            mem: Self {
                space: self.space.clone(),
                id: self.id,
            },
        }))
    }

    fn get_shared(&self) -> Box<dyn SendSyncDerefMut<Target = dyn CachedStore>> {
        Box::new(DynamicMemShared(Self {
            space: self.space.clone(),
            id: self.id,
        }))
    }

    fn write(&mut self, offset: usize, change: &[u8]) {
        let length = change.len();
        let size = offset + length;

        #[allow(clippy::unwrap_used)]
        let mut space = self.space.write().unwrap();

        // Increase the size if the request range exceeds the current limit.
        if size > space.len() {
            space.resize(size, 0);
        }
        #[allow(clippy::indexing_slicing)]
        space[offset..offset + length].copy_from_slice(change)
    }

    fn id(&self) -> SpaceId {
        self.id
    }
}

#[derive(Debug)]
struct DynamicMemView {
    offset: usize,
    length: usize,
    mem: DynamicMem,
}

struct DynamicMemShared(DynamicMem);

impl Deref for DynamicMemShared {
    type Target = dyn CachedStore;
    fn deref(&self) -> &(dyn CachedStore + 'static) {
        &self.0
    }
}

impl DerefMut for DynamicMemShared {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl CachedView for DynamicMemView {
    type DerefReturn = Vec<u8>;

    fn as_deref(&self) -> Self::DerefReturn {
        #[allow(clippy::indexing_slicing, clippy::unwrap_used)]
        self.mem.space.read().unwrap()[self.offset..self.offset + self.length].to_vec()
    }
}

#[cfg(test)]
#[allow(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use super::*;

    #[test]
    fn test_plain_mem() {
        let mut view = PlainMemShared(PlainMem::new(2, 0));
        let mem = &mut *view;
        mem.write(0, &[1, 1]);
        mem.write(0, &[1, 2]);
        #[allow(clippy::unwrap_used)]
        let r = mem.get_view(0, 2).unwrap().as_deref();
        assert_eq!(r, [1, 2]);

        // previous view not mutated by write
        mem.write(0, &[1, 3]);
        assert_eq!(r, [1, 2]);
        let r = mem.get_view(0, 2).unwrap().as_deref();
        assert_eq!(r, [1, 3]);

        // create a view larger than capacity
        assert!(mem.get_view(0, 4).is_none())
    }

    #[test]
    #[should_panic(expected = "index 3 out of range for slice of length 2")]
    fn test_plain_mem_panic() {
        let mut view = PlainMemShared(PlainMem::new(2, 0));
        let mem = &mut *view;

        // out of range
        mem.write(1, &[7, 8]);
    }

    #[test]
    fn test_dynamic_mem() {
        let mut view = DynamicMemShared(DynamicMem::new(2, 0));
        let mem = &mut *view;
        mem.write(0, &[1, 2]);
        mem.write(0, &[3, 4]);
        assert_eq!(mem.get_view(0, 2).unwrap().as_deref(), [3, 4]);
        mem.get_shared().write(0, &[5, 6]);

        // capacity is increased
        mem.write(5, &[0; 10]);

        // get a view larger than recent growth
        assert_eq!(mem.get_view(3, 20).unwrap().as_deref(), [0; 20]);
    }
}
