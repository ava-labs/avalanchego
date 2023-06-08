use std::borrow::BorrowMut;
use std::cell::{RefCell, UnsafeCell};
use std::fmt::Debug;
use std::ops::{Deref, DerefMut};
use std::rc::Rc;

use crate::{CachedStore, CachedView, SpaceId};

/// Purely volatile, vector-based implementation for [CachedStore]. This is good for testing or trying
/// out stuff (persistent data structures) built on [ShaleStore] in memory, without having to write
/// your own [CachedStore] implementation.
#[derive(Debug)]
pub struct PlainMem {
    space: Rc<RefCell<Vec<u8>>>,
    id: SpaceId,
}

impl PlainMem {
    pub fn new(size: u64, id: SpaceId) -> Self {
        let mut space: Vec<u8> = Vec::new();
        space.resize(size as usize, 0);
        let space = Rc::new(RefCell::new(space));
        Self { space, id }
    }
}

impl CachedStore for PlainMem {
    fn get_view(
        &self,
        offset: u64,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let offset = offset as usize;
        let length = length as usize;
        if offset + length > self.space.borrow().len() {
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

    fn get_shared(&self) -> Box<dyn DerefMut<Target = dyn CachedStore>> {
        Box::new(PlainMemShared(Self {
            space: self.space.clone(),
            id: self.id,
        }))
    }

    fn write(&mut self, offset: u64, change: &[u8]) {
        let offset = offset as usize;
        let length = change.len();
        let mut vect = self.space.deref().borrow_mut();
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
        self.mem.space.borrow()[self.offset..self.offset + self.length].to_vec()
    }
}

// Purely volatile, dynamically allocated vector-based implementation for [CachedStore]. This is similar to
/// [PlainMem]. The only difference is, when [write] dynamically allocate more space if original space is
/// not enough.
#[derive(Debug)]
pub struct DynamicMem {
    space: Rc<UnsafeCell<Vec<u8>>>,
    id: SpaceId,
}

impl DynamicMem {
    pub fn new(size: u64, id: SpaceId) -> Self {
        let space = Rc::new(UnsafeCell::new(vec![0; size as usize]));
        Self { space, id }
    }

    #[allow(clippy::mut_from_ref)]
    // TODO: Refactor this usage.
    fn get_space_mut(&self) -> &mut Vec<u8> {
        unsafe { &mut *self.space.get() }
    }
}

impl CachedStore for DynamicMem {
    fn get_view(
        &self,
        offset: u64,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let offset = offset as usize;
        let length = length as usize;
        let size = offset + length;
        // Increase the size if the request range exceeds the current limit.
        if size > self.get_space_mut().len() {
            self.get_space_mut().resize(size, 0);
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

    fn get_shared(&self) -> Box<dyn DerefMut<Target = dyn CachedStore>> {
        Box::new(DynamicMemShared(Self {
            space: self.space.clone(),
            id: self.id,
        }))
    }

    fn write(&mut self, offset: u64, change: &[u8]) {
        let offset = offset as usize;
        let length = change.len();
        let size = offset + length;
        // Increase the size if the request range exceeds the current limit.
        if size > self.get_space_mut().len() {
            self.get_space_mut().resize(size, 0);
        }
        self.get_space_mut()[offset..offset + length].copy_from_slice(change)
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

impl Deref for DynamicMemView {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.mem.get_space_mut()[self.offset..self.offset + self.length]
    }
}

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
        self.mem.get_space_mut()[self.offset..self.offset + self.length].to_vec()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_plain_mem() {
        let mut view = PlainMemShared(PlainMem::new(2, 0));
        let mem = view.deref_mut();
        mem.write(0, &[1, 1]);
        mem.write(0, &[1, 2]);
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
        let mem = view.deref_mut();

        // out of range
        mem.write(1, &[7, 8]);
    }

    #[test]
    fn test_dynamic_mem() {
        let mut view = DynamicMemShared(DynamicMem::new(2, 0));
        let mem = view.deref_mut();
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
