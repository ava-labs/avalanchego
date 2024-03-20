// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::logger::trace;
use crate::merkle::Node;
use crate::shale::ObjCache;
use crate::storage::{StoreRevMut, StoreRevShared};

use super::disk_address::DiskAddress;
use super::{CachedStore, Obj, ObjRef, ShaleError, Storable, StoredView};
use bytemuck::{Pod, Zeroable};
use std::fmt::Debug;
use std::io::{Cursor, Write};
use std::num::NonZeroUsize;
use std::sync::RwLock;

type PayLoadSize = u64;

#[derive(Debug)]
pub struct CompactHeader {
    payload_size: PayLoadSize,
    is_freed: bool,
    desc_addr: DiskAddress,
}

impl CompactHeader {
    pub const SERIALIZED_LEN: u64 = 17;

    pub const fn is_freed(&self) -> bool {
        self.is_freed
    }

    pub const fn payload_size(&self) -> u64 {
        self.payload_size
    }
}

impl Storable for CompactHeader {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let payload_size =
            u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        #[allow(clippy::indexing_slicing)]
        let is_freed = raw.as_deref()[8] != 0;
        #[allow(clippy::indexing_slicing)]
        let desc_addr =
            usize::from_le_bytes(raw.as_deref()[9..17].try_into().expect("invalid slice"));
        Ok(Self {
            payload_size,
            is_freed,
            desc_addr: DiskAddress(NonZeroUsize::new(desc_addr)),
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes())?;
        cur.write_all(&[if self.is_freed { 1 } else { 0 }])?;
        cur.write_all(&self.desc_addr.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Debug)]
struct CompactFooter {
    payload_size: PayLoadSize,
}

impl CompactFooter {
    const SERIALIZED_LEN: u64 = std::mem::size_of::<PayLoadSize>() as u64;
}

impl Storable for CompactFooter {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::unwrap_used)]
        let payload_size = u64::from_le_bytes(raw.as_deref().try_into().unwrap());
        Ok(Self { payload_size })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.payload_size.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Clone, Copy, Debug)]
struct CompactDescriptor {
    payload_size: PayLoadSize,
    haddr: usize, // disk address of the free space
}

impl CompactDescriptor {
    const SERIALIZED_LEN: u64 = 16;
}

impl Storable for CompactDescriptor {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let payload_size =
            u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        #[allow(clippy::indexing_slicing)]
        let haddr = usize::from_le_bytes(raw.as_deref()[8..].try_into().expect("invalid slice"));
        Ok(Self {
            payload_size,
            haddr,
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes())?;
        cur.write_all(&self.haddr.to_le_bytes())?;
        Ok(())
    }
}

#[repr(C)]
#[derive(Copy, Clone, Debug, Pod, Zeroable)]
pub struct CompactSpaceHeader {
    meta_space_tail: DiskAddress,
    data_space_tail: DiskAddress,
    base_addr: DiskAddress,
    alloc_addr: DiskAddress,
}

#[derive(Debug)]
struct CompactSpaceHeaderSliced {
    meta_space_tail: Obj<DiskAddress>,
    data_space_tail: Obj<DiskAddress>,
    base_addr: Obj<DiskAddress>,
    alloc_addr: Obj<DiskAddress>,
}

impl CompactSpaceHeaderSliced {
    fn flush_dirty(&mut self) {
        self.meta_space_tail.flush_dirty();
        self.data_space_tail.flush_dirty();
        self.base_addr.flush_dirty();
        self.alloc_addr.flush_dirty();
    }
}

impl CompactSpaceHeader {
    pub const SERIALIZED_LEN: u64 = 4 * DiskAddress::SERIALIZED_LEN;
    const META_SPACE_TAIL_OFFSET: usize = 0;
    const DATA_SPACE_TAIL_OFFSET: usize = DiskAddress::SERIALIZED_LEN as usize;
    const BASE_ADDR_OFFSET: usize = 2 * DiskAddress::SERIALIZED_LEN as usize;
    const ALLOC_ADDR_OFFSET: usize = 3 * DiskAddress::SERIALIZED_LEN as usize;

    pub const fn new(meta_base: NonZeroUsize, compact_base: NonZeroUsize) -> Self {
        Self {
            meta_space_tail: DiskAddress::new(meta_base),
            data_space_tail: DiskAddress::new(compact_base),
            base_addr: DiskAddress::new(meta_base),
            alloc_addr: DiskAddress::new(meta_base),
        }
    }

    fn into_fields(r: Obj<Self>) -> Result<CompactSpaceHeaderSliced, ShaleError> {
        Ok(CompactSpaceHeaderSliced {
            meta_space_tail: StoredView::slice(
                &r,
                Self::META_SPACE_TAIL_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.meta_space_tail,
            )?,
            data_space_tail: StoredView::slice(
                &r,
                Self::DATA_SPACE_TAIL_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.data_space_tail,
            )?,
            base_addr: StoredView::slice(
                &r,
                Self::BASE_ADDR_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.base_addr,
            )?,
            alloc_addr: StoredView::slice(
                &r,
                Self::ALLOC_ADDR_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.alloc_addr,
            )?,
        })
    }
}

impl Storable for CompactSpaceHeader {
    fn deserialize<T: CachedStore + Debug>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let meta_space_tail = raw.as_deref()[..Self::DATA_SPACE_TAIL_OFFSET]
            .try_into()
            .expect("Self::MSIZE = 4 * DiskAddress::MSIZE");
        #[allow(clippy::indexing_slicing)]
        let data_space_tail = raw.as_deref()[Self::DATA_SPACE_TAIL_OFFSET..Self::BASE_ADDR_OFFSET]
            .try_into()
            .expect("Self::MSIZE = 4 * DiskAddress::MSIZE");
        #[allow(clippy::indexing_slicing)]
        let base_addr = raw.as_deref()[Self::BASE_ADDR_OFFSET..Self::ALLOC_ADDR_OFFSET]
            .try_into()
            .expect("Self::MSIZE = 4 * DiskAddress::MSIZE");
        #[allow(clippy::indexing_slicing)]
        let alloc_addr = raw.as_deref()[Self::ALLOC_ADDR_OFFSET..]
            .try_into()
            .expect("Self::MSIZE = 4 * DiskAddress::MSIZE");
        Ok(Self {
            meta_space_tail,
            data_space_tail,
            base_addr,
            alloc_addr,
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.meta_space_tail.to_le_bytes())?;
        cur.write_all(&self.data_space_tail.to_le_bytes())?;
        cur.write_all(&self.base_addr.to_le_bytes())?;
        cur.write_all(&self.alloc_addr.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Debug)]
struct CompactSpaceInner<M> {
    meta_space: M,
    data_space: M,
    header: CompactSpaceHeaderSliced,
    alloc_max_walk: u64,
    regn_nbit: u64,
}

impl From<CompactSpaceInner<StoreRevMut>> for CompactSpaceInner<StoreRevShared> {
    fn from(value: CompactSpaceInner<StoreRevMut>) -> CompactSpaceInner<StoreRevShared> {
        CompactSpaceInner {
            meta_space: value.meta_space.into(),
            data_space: value.data_space.into(),
            header: value.header,
            alloc_max_walk: value.alloc_max_walk,
            regn_nbit: value.regn_nbit,
        }
    }
}

impl<M: CachedStore> CompactSpaceInner<M> {
    fn get_descriptor(&self, ptr: DiskAddress) -> Result<Obj<CompactDescriptor>, ShaleError> {
        StoredView::ptr_to_obj(&self.meta_space, ptr, CompactDescriptor::SERIALIZED_LEN)
    }

    fn get_data_ref<U: Storable + 'static>(
        &self,
        ptr: DiskAddress,
        len_limit: u64,
    ) -> Result<Obj<U>, ShaleError> {
        StoredView::ptr_to_obj(&self.data_space, ptr, len_limit)
    }

    fn get_header(&self, ptr: DiskAddress) -> Result<Obj<CompactHeader>, ShaleError> {
        self.get_data_ref::<CompactHeader>(ptr, CompactHeader::SERIALIZED_LEN)
    }

    fn get_footer(&self, ptr: DiskAddress) -> Result<Obj<CompactFooter>, ShaleError> {
        self.get_data_ref::<CompactFooter>(ptr, CompactFooter::SERIALIZED_LEN)
    }

    fn del_desc(&mut self, desc_addr: DiskAddress) -> Result<(), ShaleError> {
        let desc_size = CompactDescriptor::SERIALIZED_LEN;
        // TODO: subtracting two disk addresses is only used here, probably can rewrite this
        // debug_assert!((desc_addr.0 - self.header.base_addr.value.into()) % desc_size == 0);
        #[allow(clippy::unwrap_used)]
        self.header
            .meta_space_tail
            .modify(|r| *r -= desc_size as usize)
            .unwrap();

        if desc_addr != DiskAddress(**self.header.meta_space_tail) {
            let desc_last = self.get_descriptor(*self.header.meta_space_tail.value)?;
            let mut desc = self.get_descriptor(desc_addr)?;
            #[allow(clippy::unwrap_used)]
            desc.modify(|r| *r = *desc_last).unwrap();
            let mut header = self.get_header(desc.haddr.into())?;
            #[allow(clippy::unwrap_used)]
            header.modify(|h| h.desc_addr = desc_addr).unwrap();
        }

        Ok(())
    }

    fn new_desc(&mut self) -> Result<DiskAddress, ShaleError> {
        let addr = **self.header.meta_space_tail;
        #[allow(clippy::unwrap_used)]
        self.header
            .meta_space_tail
            .modify(|r| *r += CompactDescriptor::SERIALIZED_LEN as usize)
            .unwrap();

        Ok(DiskAddress(addr))
    }

    fn free(&mut self, addr: u64) -> Result<(), ShaleError> {
        let hsize = CompactHeader::SERIALIZED_LEN;
        let fsize = CompactFooter::SERIALIZED_LEN;
        let regn_size = 1 << self.regn_nbit;

        let mut offset = addr - hsize;
        let header_payload_size = {
            let header = self.get_header(DiskAddress::from(offset as usize))?;
            assert!(!header.is_freed);
            header.payload_size
        };
        let mut h = offset;
        let mut payload_size = header_payload_size;

        if offset & (regn_size - 1) > 0 {
            // merge with lower data segment
            offset -= fsize;
            let (pheader_is_freed, pheader_payload_size, pheader_desc_addr) = {
                let pfooter = self.get_footer(DiskAddress::from(offset as usize))?;
                offset -= pfooter.payload_size + hsize;
                let pheader = self.get_header(DiskAddress::from(offset as usize))?;
                (pheader.is_freed, pheader.payload_size, pheader.desc_addr)
            };
            if pheader_is_freed {
                h = offset;
                payload_size += hsize + fsize + pheader_payload_size;
                self.del_desc(pheader_desc_addr)?;
            }
        }

        offset = addr + header_payload_size;
        let mut f = offset;

        #[allow(clippy::unwrap_used)]
        if offset + fsize < self.header.data_space_tail.unwrap().get() as u64
            && (regn_size - (offset & (regn_size - 1))) >= fsize + hsize
        {
            // merge with higher data segment
            offset += fsize;
            let (nheader_is_freed, nheader_payload_size, nheader_desc_addr) = {
                let nheader = self.get_header(DiskAddress::from(offset as usize))?;
                (nheader.is_freed, nheader.payload_size, nheader.desc_addr)
            };
            if nheader_is_freed {
                offset += hsize + nheader_payload_size;
                f = offset;
                {
                    let nfooter = self.get_footer(DiskAddress::from(offset as usize))?;
                    assert!(nheader_payload_size == nfooter.payload_size);
                }
                payload_size += hsize + fsize + nheader_payload_size;
                self.del_desc(nheader_desc_addr)?;
            }
        }

        let desc_addr = self.new_desc()?;
        {
            let mut desc = self.get_descriptor(desc_addr)?;
            #[allow(clippy::unwrap_used)]
            desc.modify(|d| {
                d.payload_size = payload_size;
                d.haddr = h as usize;
            })
            .unwrap();
        }
        let mut h = self.get_header(DiskAddress::from(h as usize))?;
        let mut f = self.get_footer(DiskAddress::from(f as usize))?;
        #[allow(clippy::unwrap_used)]
        h.modify(|h| {
            h.payload_size = payload_size;
            h.is_freed = true;
            h.desc_addr = desc_addr;
        })
        .unwrap();
        #[allow(clippy::unwrap_used)]
        f.modify(|f| f.payload_size = payload_size).unwrap();

        Ok(())
    }

    fn alloc_from_freed(&mut self, length: u64) -> Result<Option<u64>, ShaleError> {
        let tail = *self.header.meta_space_tail;
        if tail == *self.header.base_addr {
            return Ok(None);
        }

        let hsize = CompactHeader::SERIALIZED_LEN as usize;
        let fsize = CompactFooter::SERIALIZED_LEN as usize;
        let dsize = CompactDescriptor::SERIALIZED_LEN as usize;

        let mut old_alloc_addr = *self.header.alloc_addr;

        if old_alloc_addr >= tail {
            old_alloc_addr = *self.header.base_addr;
        }

        let mut ptr = old_alloc_addr;
        let mut res: Option<u64> = None;
        for _ in 0..self.alloc_max_walk {
            assert!(ptr < tail);
            let (desc_payload_size, desc_haddr) = {
                let desc = self.get_descriptor(ptr)?;
                (desc.payload_size as usize, desc.haddr)
            };
            let exit = if desc_payload_size == length as usize {
                // perfect match
                {
                    let mut header = self.get_header(DiskAddress::from(desc_haddr))?;
                    assert_eq!(header.payload_size as usize, desc_payload_size);
                    assert!(header.is_freed);
                    #[allow(clippy::unwrap_used)]
                    header.modify(|h| h.is_freed = false).unwrap();
                }
                self.del_desc(ptr)?;
                true
            } else if desc_payload_size > length as usize + hsize + fsize {
                // able to split
                {
                    let mut lheader = self.get_header(DiskAddress::from(desc_haddr))?;
                    assert_eq!(lheader.payload_size as usize, desc_payload_size);
                    assert!(lheader.is_freed);
                    #[allow(clippy::unwrap_used)]
                    lheader
                        .modify(|h| {
                            h.is_freed = false;
                            h.payload_size = length;
                        })
                        .unwrap();
                }
                {
                    let mut lfooter =
                        self.get_footer(DiskAddress::from(desc_haddr + hsize + length as usize))?;
                    //assert!(lfooter.payload_size == desc_payload_size);
                    #[allow(clippy::unwrap_used)]
                    lfooter.modify(|f| f.payload_size = length).unwrap();
                }

                let offset = desc_haddr + hsize + length as usize + fsize;
                let rpayload_size = desc_payload_size - length as usize - fsize - hsize;
                let rdesc_addr = self.new_desc()?;
                {
                    let mut rdesc = self.get_descriptor(rdesc_addr)?;
                    #[allow(clippy::unwrap_used)]
                    rdesc
                        .modify(|rd| {
                            rd.payload_size = rpayload_size as u64;
                            rd.haddr = offset;
                        })
                        .unwrap();
                }
                {
                    let mut rheader = self.get_header(DiskAddress::from(offset))?;
                    #[allow(clippy::unwrap_used)]
                    rheader
                        .modify(|rh| {
                            rh.is_freed = true;
                            rh.payload_size = rpayload_size as u64;
                            rh.desc_addr = rdesc_addr;
                        })
                        .unwrap();
                }
                {
                    let mut rfooter =
                        self.get_footer(DiskAddress::from(offset + hsize + rpayload_size))?;
                    #[allow(clippy::unwrap_used)]
                    rfooter
                        .modify(|f| f.payload_size = rpayload_size as u64)
                        .unwrap();
                }
                self.del_desc(ptr)?;
                true
            } else {
                false
            };
            #[allow(clippy::unwrap_used)]
            if exit {
                self.header.alloc_addr.modify(|r| *r = ptr).unwrap();
                res = Some((desc_haddr + hsize) as u64);
                break;
            }
            ptr += dsize;
            if ptr >= tail {
                ptr = *self.header.base_addr;
            }
            if ptr == old_alloc_addr {
                break;
            }
        }
        Ok(res)
    }

    fn alloc_new(&mut self, length: u64) -> Result<u64, ShaleError> {
        let regn_size = 1 << self.regn_nbit;
        let total_length = CompactHeader::SERIALIZED_LEN + length + CompactFooter::SERIALIZED_LEN;
        let mut offset = *self.header.data_space_tail;
        #[allow(clippy::unwrap_used)]
        self.header
            .data_space_tail
            .modify(|r| {
                // an item is always fully in one region
                let rem = regn_size - (offset & (regn_size - 1)).get();
                if rem < total_length as usize {
                    offset += rem;
                    *r += rem;
                }
                *r += total_length as usize
            })
            .unwrap();
        let mut h = self.get_header(offset)?;
        let mut f =
            self.get_footer(offset + CompactHeader::SERIALIZED_LEN as usize + length as usize)?;
        #[allow(clippy::unwrap_used)]
        h.modify(|h| {
            h.payload_size = length;
            h.is_freed = false;
            h.desc_addr = DiskAddress::null();
        })
        .unwrap();
        #[allow(clippy::unwrap_used)]
        f.modify(|f| f.payload_size = length).unwrap();
        #[allow(clippy::unwrap_used)]
        Ok((offset + CompactHeader::SERIALIZED_LEN as usize)
            .0
            .unwrap()
            .get() as u64)
    }

    fn alloc(&mut self, length: u64) -> Result<u64, ShaleError> {
        self.alloc_from_freed(length).and_then(|addr| {
            if let Some(addr) = addr {
                Ok(addr)
            } else {
                self.alloc_new(length)
            }
        })
    }
}

#[derive(Debug)]
pub struct CompactSpace<T: Storable, M> {
    inner: RwLock<CompactSpaceInner<M>>,
    obj_cache: ObjCache<T>,
}

impl<T: Storable, M: CachedStore> CompactSpace<T, M> {
    pub fn new(
        meta_space: M,
        data_space: M,
        header: Obj<CompactSpaceHeader>,
        obj_cache: super::ObjCache<T>,
        alloc_max_walk: u64,
        regn_nbit: u64,
    ) -> Result<Self, ShaleError> {
        let cs = CompactSpace {
            inner: RwLock::new(CompactSpaceInner {
                meta_space,
                data_space,
                header: CompactSpaceHeader::into_fields(header)?,
                alloc_max_walk,
                regn_nbit,
            }),
            obj_cache,
        };
        Ok(cs)
    }
}

impl From<CompactSpace<Node, StoreRevMut>> for CompactSpace<Node, StoreRevShared> {
    #[allow(clippy::unwrap_used)]
    fn from(value: CompactSpace<Node, StoreRevMut>) -> Self {
        let inner = value.inner.into_inner().unwrap();
        CompactSpace {
            inner: RwLock::new(inner.into()),
            obj_cache: value.obj_cache,
        }
    }
}

impl<T: Storable + Debug + 'static, M: CachedStore> CompactSpace<T, M> {
    pub(crate) fn put_item(&self, item: T, extra: u64) -> Result<ObjRef<'_, T>, ShaleError> {
        let size = item.serialized_len() + extra;
        #[allow(clippy::unwrap_used)]
        let addr = self.inner.write().unwrap().alloc(size)?;

        trace!("{self:p} put_item at {addr} size {size}");

        #[allow(clippy::unwrap_used)]
        let obj = {
            let inner = self.inner.read().unwrap();
            let data_space = &inner.data_space;
            #[allow(clippy::unwrap_used)]
            let view = StoredView::item_to_obj(data_space, addr.try_into().unwrap(), size, item)?;

            self.obj_cache.put(view)
        };

        let cache = &self.obj_cache;

        let mut obj_ref = ObjRef::new(obj, cache);

        // should this use a `?` instead of `unwrap`?
        #[allow(clippy::unwrap_used)]
        obj_ref.write(|_| {}).unwrap();

        Ok(obj_ref)
    }

    #[allow(clippy::unwrap_used)]
    pub(crate) fn free_item(&mut self, ptr: DiskAddress) -> Result<(), ShaleError> {
        let mut inner = self.inner.write().unwrap();
        self.obj_cache.pop(ptr);
        #[allow(clippy::unwrap_used)]
        inner.free(ptr.unwrap().get() as u64)
    }

    pub(crate) fn get_item(&self, ptr: DiskAddress) -> Result<ObjRef<'_, T>, ShaleError> {
        let obj = self.obj_cache.get(ptr)?;

        #[allow(clippy::unwrap_used)]
        let inner = self.inner.read().unwrap();
        let cache = &self.obj_cache;

        if let Some(obj) = obj {
            return Ok(ObjRef::new(obj, cache));
        }

        #[allow(clippy::unwrap_used)]
        if ptr < DiskAddress::from(CompactSpaceHeader::SERIALIZED_LEN as usize) {
            return Err(ShaleError::InvalidAddressLength {
                expected: CompactSpaceHeader::SERIALIZED_LEN,
                found: ptr.0.map(|inner| inner.get()).unwrap_or_default() as u64,
            });
        }

        let payload_size = inner
            .get_header(ptr - CompactHeader::SERIALIZED_LEN as usize)?
            .payload_size;
        let obj = self.obj_cache.put(inner.get_data_ref(ptr, payload_size)?);
        let cache = &self.obj_cache;

        Ok(ObjRef::new(obj, cache))
    }

    #[allow(clippy::unwrap_used)]
    pub(crate) fn flush_dirty(&self) -> Option<()> {
        let mut inner = self.inner.write().unwrap();
        inner.header.flush_dirty();
        // hold the write lock to ensure that both cache and header are flushed in-sync
        self.obj_cache.flush_dirty()
    }
}

#[cfg(test)]
#[allow(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use sha3::Digest;

    use crate::shale::{self, cached::InMemLinearStore};

    use super::*;

    const HASH_SIZE: usize = 32;
    const ZERO_HASH: Hash = Hash([0u8; HASH_SIZE]);

    #[derive(PartialEq, Eq, Debug, Clone)]
    pub struct Hash(pub [u8; HASH_SIZE]);

    impl Hash {
        const SERIALIZED_LEN: u64 = 32;
    }

    impl std::ops::Deref for Hash {
        type Target = [u8; HASH_SIZE];
        fn deref(&self) -> &[u8; HASH_SIZE] {
            &self.0
        }
    }

    impl Storable for Hash {
        fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
            let raw =
                mem.get_view(addr, Self::SERIALIZED_LEN)
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr,
                        size: Self::SERIALIZED_LEN,
                    })?;
            Ok(Self(
                raw.as_deref()[..Self::SERIALIZED_LEN as usize]
                    .try_into()
                    .expect("invalid slice"),
            ))
        }

        fn serialized_len(&self) -> u64 {
            Self::SERIALIZED_LEN
        }

        fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
            let mut cur = to;
            cur.write_all(&self.0)?;
            Ok(())
        }
    }

    #[test]
    fn test_space_item() {
        let meta_size: NonZeroUsize = NonZeroUsize::new(0x10000).unwrap();
        let compact_size: NonZeroUsize = NonZeroUsize::new(0x10000).unwrap();
        let reserved: DiskAddress = 0x1000.into();

        let mut dm = InMemLinearStore::new(meta_size.get() as u64, 0x0);

        // initialize compact space
        let compact_header = DiskAddress::from(0x1);
        dm.write(
            compact_header.unwrap().get(),
            &shale::to_dehydrated(&CompactSpaceHeader::new(
                reserved.0.unwrap(),
                reserved.0.unwrap(),
            ))
            .unwrap(),
        )
        .unwrap();
        let compact_header =
            StoredView::ptr_to_obj(&dm, compact_header, CompactHeader::SERIALIZED_LEN).unwrap();
        let mem_meta = dm;
        let mem_payload = InMemLinearStore::new(compact_size.get() as u64, 0x1);

        let cache: ObjCache<Hash> = ObjCache::new(1);
        let space =
            CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16).unwrap();

        // initial write
        let data = b"hello world";
        let hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(data).into();
        let obj_ref = space.put_item(Hash(hash), 0).unwrap();
        assert_eq!(obj_ref.as_ptr(), DiskAddress::from(4113));
        // create hash ptr from address and attempt to read dirty write.
        let hash_ref = space.get_item(DiskAddress::from(4113)).unwrap();
        // read before flush results in zeroed hash
        assert_eq!(hash_ref.as_ref(), ZERO_HASH.as_ref());
        // not cached
        assert!(obj_ref
            .cache
            .lock()
            .cached
            .get(&DiskAddress::from(4113))
            .is_none());
        // pinned
        assert!(obj_ref
            .cache
            .lock()
            .pinned
            .contains_key(&DiskAddress::from(4113)));
        // dirty
        assert!(obj_ref
            .cache
            .lock()
            .dirty
            .contains(&DiskAddress::from(4113)));
        drop(obj_ref);
        // write is visible
        assert_eq!(
            space.get_item(DiskAddress::from(4113)).unwrap().as_ref(),
            hash
        );
    }
}
