// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{
    borrow::BorrowMut,
    ops::{Deref, DerefMut},
    sync::{Arc, RwLock},
};

use super::{CachedStore, CachedView, SendSyncDerefMut, ShaleError, SpaceId};

/// in-memory vector-based implementation for [CachedStore] for testing
// built on [ShaleStore](super::ShaleStore) in memory, without having to write
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

    fn write(&mut self, offset: usize, change: &[u8]) -> Result<(), ShaleError> {
        let length = change.len();
        #[allow(clippy::unwrap_used)]
        let mut vect = self.space.deref().write().unwrap();
        #[allow(clippy::indexing_slicing)]
        vect.as_mut_slice()[offset..offset + length].copy_from_slice(change);
        Ok(())
    }

    fn id(&self) -> SpaceId {
        self.id
    }

    fn is_writeable(&self) -> bool {
        true
    }
}

#[derive(Debug)]
struct PlainMemView {
    offset: usize,
    length: usize,
    mem: PlainMem,
}

pub struct PlainMemShared(pub PlainMem);

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
