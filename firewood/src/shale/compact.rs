// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::logger::trace;
use crate::merkle::Node;
use crate::shale::ObjCache;
use crate::storage::{StoreRevMut, StoreRevShared};

use super::disk_address::DiskAddress;
use super::{LinearStore, Obj, ObjRef, ShaleError, Storable, StoredView};
use bytemuck::{Pod, Zeroable};
use std::fmt::Debug;
use std::io::{Cursor, Write};
use std::num::NonZeroUsize;
use std::sync::RwLock;

/// Marks the start of a linear chunk of the store.
/// The chunk may be freed or in use.
#[derive(Debug)]
pub struct ChunkHeader {
    chunk_size: u64,
    is_freed: bool,
    desc_addr: DiskAddress,
}

impl ChunkHeader {
    const IS_FREED_OFFSET: usize = std::mem::size_of::<usize>();
    const DESC_ADDR_OFFSET: usize = Self::IS_FREED_OFFSET + 1;
    pub const SERIALIZED_LEN: u64 = (Self::DESC_ADDR_OFFSET + std::mem::size_of::<usize>()) as u64;

    pub const fn is_freed(&self) -> bool {
        self.is_freed
    }

    pub const fn chunk_size(&self) -> u64 {
        self.chunk_size
    }
}

impl Storable for ChunkHeader {
    fn deserialize<T: LinearStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let chunk_size = u64::from_le_bytes(
            raw.as_deref()[..Self::IS_FREED_OFFSET]
                .try_into()
                .expect("invalid slice"),
        );
        #[allow(clippy::indexing_slicing)]
        let is_freed = raw.as_deref()[Self::IS_FREED_OFFSET] != 0;
        #[allow(clippy::indexing_slicing)]
        let desc_addr = usize::from_le_bytes(
            raw.as_deref()[Self::DESC_ADDR_OFFSET..Self::SERIALIZED_LEN as usize]
                .try_into()
                .expect("invalid slice"),
        );
        Ok(Self {
            chunk_size,
            is_freed,
            desc_addr: DiskAddress(NonZeroUsize::new(desc_addr)),
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.chunk_size.to_le_bytes())?;
        cur.write_all(&[if self.is_freed { 1 } else { 0 }])?;
        cur.write_all(&self.desc_addr.to_le_bytes())?;
        Ok(())
    }
}

/// Marks the end of a linear chunk of the store.
#[derive(Debug)]
struct ChunkFooter {
    chunk_size: u64,
}

impl ChunkFooter {
    const SERIALIZED_LEN: u64 = std::mem::size_of::<u64>() as u64;
}

impl Storable for ChunkFooter {
    fn deserialize<T: LinearStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::unwrap_used)]
        let chunk_size = u64::from_le_bytes(raw.as_deref().try_into().unwrap());
        Ok(Self { chunk_size })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.chunk_size.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Clone, Copy, Debug)]
struct ChunkDescriptor {
    chunk_size: u64,
    haddr: usize, // disk address of the free space
}

impl ChunkDescriptor {
    const HADDR_OFFSET: usize = 8;
    const SERIALIZED_LEN: u64 = (Self::HADDR_OFFSET + std::mem::size_of::<usize>()) as u64;
}

impl Storable for ChunkDescriptor {
    fn deserialize<T: LinearStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let chunk_size = u64::from_le_bytes(
            raw.as_deref()[..Self::HADDR_OFFSET]
                .try_into()
                .expect("invalid slice"),
        );
        #[allow(clippy::indexing_slicing)]
        let haddr = usize::from_le_bytes(
            raw.as_deref()[Self::HADDR_OFFSET..]
                .try_into()
                .expect("invalid slice"),
        );
        Ok(Self { chunk_size, haddr })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.chunk_size.to_le_bytes())?;
        cur.write_all(&self.haddr.to_le_bytes())?;
        Ok(())
    }
}

/// A header for a [Store].
#[repr(C)]
#[derive(Copy, Clone, Debug, Pod, Zeroable)]
pub struct StoreHeader {
    meta_store_tail: DiskAddress,
    data_store_tail: DiskAddress,
    base_addr: DiskAddress,
    alloc_addr: DiskAddress,
}

#[derive(Debug)]
struct StoreHeaderSliced {
    meta_store_tail: Obj<DiskAddress>,
    data_store_tail: Obj<DiskAddress>,
    base_addr: Obj<DiskAddress>,
    alloc_addr: Obj<DiskAddress>,
}

impl StoreHeaderSliced {
    fn flush_dirty(&mut self) {
        self.meta_store_tail.flush_dirty();
        self.data_store_tail.flush_dirty();
        self.base_addr.flush_dirty();
        self.alloc_addr.flush_dirty();
    }
}

impl StoreHeader {
    const META_STORE_TAIL_OFFSET: usize = 0;
    const DATA_STORE_TAIL_OFFSET: usize = DiskAddress::SERIALIZED_LEN as usize;
    const BASE_ADDR_OFFSET: usize =
        Self::DATA_STORE_TAIL_OFFSET + DiskAddress::SERIALIZED_LEN as usize;
    const ALLOC_ADDR_OFFSET: usize = Self::BASE_ADDR_OFFSET + DiskAddress::SERIALIZED_LEN as usize;
    pub const SERIALIZED_LEN: u64 = Self::ALLOC_ADDR_OFFSET as u64 + DiskAddress::SERIALIZED_LEN;

    pub const fn new(meta_base: NonZeroUsize, compact_base: NonZeroUsize) -> Self {
        Self {
            meta_store_tail: DiskAddress::new(meta_base),
            data_store_tail: DiskAddress::new(compact_base),
            base_addr: DiskAddress::new(meta_base),
            alloc_addr: DiskAddress::new(meta_base),
        }
    }

    fn into_fields(r: Obj<Self>) -> Result<StoreHeaderSliced, ShaleError> {
        Ok(StoreHeaderSliced {
            meta_store_tail: StoredView::slice(
                &r,
                Self::META_STORE_TAIL_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.meta_store_tail,
            )?,
            data_store_tail: StoredView::slice(
                &r,
                Self::DATA_STORE_TAIL_OFFSET,
                DiskAddress::SERIALIZED_LEN,
                r.data_store_tail,
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

impl Storable for StoreHeader {
    fn deserialize<T: LinearStore + Debug>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::SERIALIZED_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::SERIALIZED_LEN,
            })?;
        #[allow(clippy::indexing_slicing)]
        let meta_store_tail = raw.as_deref()[..Self::DATA_STORE_TAIL_OFFSET]
            .try_into()
            .expect("Self::MSIZE = 4 * DiskAddress::MSIZE");
        #[allow(clippy::indexing_slicing)]
        let data_store_tail = raw.as_deref()[Self::DATA_STORE_TAIL_OFFSET..Self::BASE_ADDR_OFFSET]
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
            meta_store_tail,
            data_store_tail,
            base_addr,
            alloc_addr,
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::SERIALIZED_LEN
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.meta_store_tail.to_le_bytes())?;
        cur.write_all(&self.data_store_tail.to_le_bytes())?;
        cur.write_all(&self.base_addr.to_le_bytes())?;
        cur.write_all(&self.alloc_addr.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Debug)]
struct StoreInner<M> {
    meta_store: M,
    data_store: M,
    header: StoreHeaderSliced,
    alloc_max_walk: u64,
    regn_nbit: u64,
}

impl From<StoreInner<StoreRevMut>> for StoreInner<StoreRevShared> {
    fn from(value: StoreInner<StoreRevMut>) -> StoreInner<StoreRevShared> {
        StoreInner {
            meta_store: value.meta_store.into(),
            data_store: value.data_store.into(),
            header: value.header,
            alloc_max_walk: value.alloc_max_walk,
            regn_nbit: value.regn_nbit,
        }
    }
}

impl<M: LinearStore> StoreInner<M> {
    fn get_data_ref<U: Storable + 'static>(
        &self,
        addr: DiskAddress,
        len_limit: u64,
    ) -> Result<Obj<U>, ShaleError> {
        StoredView::addr_to_obj(&self.data_store, addr, len_limit)
    }

    fn get_header(&self, addr: DiskAddress) -> Result<Obj<ChunkHeader>, ShaleError> {
        self.get_data_ref::<ChunkHeader>(addr, ChunkHeader::SERIALIZED_LEN)
    }

    fn get_footer(&self, addr: DiskAddress) -> Result<Obj<ChunkFooter>, ShaleError> {
        self.get_data_ref::<ChunkFooter>(addr, ChunkFooter::SERIALIZED_LEN)
    }

    fn get_descriptor(&self, addr: DiskAddress) -> Result<Obj<ChunkDescriptor>, ShaleError> {
        StoredView::addr_to_obj(&self.meta_store, addr, ChunkDescriptor::SERIALIZED_LEN)
    }

    fn delete_descriptor(&mut self, desc_addr: DiskAddress) -> Result<(), ShaleError> {
        let desc_size = ChunkDescriptor::SERIALIZED_LEN;
        // TODO: subtracting two disk addresses is only used here, probably can rewrite this
        // debug_assert!((desc_addr.0 - self.header.base_addr.value.into()) % desc_size == 0);

        // Move the last descriptor to the position of the deleted descriptor
        #[allow(clippy::unwrap_used)]
        self.header
            .meta_store_tail
            .modify(|r| *r -= desc_size as usize)
            .unwrap();

        if desc_addr != DiskAddress(**self.header.meta_store_tail) {
            let last_desc = self.get_descriptor(*self.header.meta_store_tail.value)?;

            let mut desc = self.get_descriptor(desc_addr)?;
            #[allow(clippy::unwrap_used)]
            desc.modify(|r| *r = *last_desc).unwrap();

            // `chunk_header` is associated with the deleted descriptor
            let mut chunk_header = self.get_header(desc.haddr.into())?;
            #[allow(clippy::unwrap_used)]
            chunk_header.modify(|h| h.desc_addr = desc_addr).unwrap();
        }

        Ok(())
    }

    fn new_descriptor_address(&mut self) -> Result<DiskAddress, ShaleError> {
        let addr = **self.header.meta_store_tail;
        #[allow(clippy::unwrap_used)]
        self.header
            .meta_store_tail
            .modify(|r| *r += ChunkDescriptor::SERIALIZED_LEN as usize)
            .unwrap();

        Ok(DiskAddress(addr))
    }

    fn free(&mut self, addr: u64) -> Result<(), ShaleError> {
        let region_size = 1 << self.regn_nbit;

        let header_offset = addr - ChunkHeader::SERIALIZED_LEN;
        let header_chunk_size = {
            let header = self.get_header(DiskAddress::from(header_offset as usize))?;
            assert!(!header.is_freed);
            header.chunk_size
        };
        let mut free_header_offset = header_offset;
        let mut free_chunk_size = header_chunk_size;

        if header_offset & (region_size - 1) > 0 {
            // TODO danlaine: document what this condition means.
            // merge with previous chunk if it's freed.
            let prev_footer_offset = header_offset - ChunkFooter::SERIALIZED_LEN;
            let prev_footer = self.get_footer(DiskAddress::from(prev_footer_offset as usize))?;

            let prev_header_offset =
                prev_footer_offset - prev_footer.chunk_size - ChunkHeader::SERIALIZED_LEN;
            let prev_header = self.get_header(DiskAddress::from(prev_header_offset as usize))?;

            if prev_header.is_freed {
                free_header_offset = prev_header_offset;
                free_chunk_size += ChunkHeader::SERIALIZED_LEN
                    + ChunkFooter::SERIALIZED_LEN
                    + prev_header.chunk_size;
                self.delete_descriptor(prev_header.desc_addr)?;
            }
        }

        let footer_offset = addr + header_chunk_size;
        let mut free_footer_offset = footer_offset;

        #[allow(clippy::unwrap_used)]
        if footer_offset + ChunkFooter::SERIALIZED_LEN
            < self.header.data_store_tail.unwrap().get() as u64
            && (region_size - (footer_offset & (region_size - 1)))
                >= ChunkFooter::SERIALIZED_LEN + ChunkHeader::SERIALIZED_LEN
        {
            // TODO danlaine: document what this condition means.
            // merge with next chunk if it's freed.
            let next_header_offset = footer_offset + ChunkFooter::SERIALIZED_LEN;
            let next_header = self.get_header(DiskAddress::from(next_header_offset as usize))?;
            if next_header.is_freed {
                let next_footer_offset =
                    next_header_offset + ChunkHeader::SERIALIZED_LEN + next_header.chunk_size;
                free_footer_offset = next_footer_offset;
                {
                    let next_footer =
                        self.get_footer(DiskAddress::from(next_footer_offset as usize))?;
                    assert!(next_header.chunk_size == next_footer.chunk_size);
                }
                free_chunk_size += ChunkHeader::SERIALIZED_LEN
                    + ChunkFooter::SERIALIZED_LEN
                    + next_header.chunk_size;
                self.delete_descriptor(next_header.desc_addr)?;
            }
        }

        let desc_addr = self.new_descriptor_address()?;
        {
            let mut desc = self.get_descriptor(desc_addr)?;
            #[allow(clippy::unwrap_used)]
            desc.modify(|d| {
                d.chunk_size = free_chunk_size;
                d.haddr = free_header_offset as usize;
            })
            .unwrap();
        }
        let mut free_header = self.get_header(DiskAddress::from(free_header_offset as usize))?;
        #[allow(clippy::unwrap_used)]
        free_header
            .modify(|h| {
                h.chunk_size = free_chunk_size;
                h.is_freed = true;
                h.desc_addr = desc_addr;
            })
            .unwrap();

        let mut free_footer = self.get_footer(DiskAddress::from(free_footer_offset as usize))?;
        #[allow(clippy::unwrap_used)]
        free_footer
            .modify(|f| f.chunk_size = free_chunk_size)
            .unwrap();

        Ok(())
    }

    fn alloc_from_freed(&mut self, length: u64) -> Result<Option<u64>, ShaleError> {
        const HEADER_SIZE: usize = ChunkHeader::SERIALIZED_LEN as usize;
        const FOOTER_SIZE: usize = ChunkFooter::SERIALIZED_LEN as usize;
        const DESCRIPTOR_SIZE: usize = ChunkDescriptor::SERIALIZED_LEN as usize;

        let tail = *self.header.meta_store_tail;
        if tail == *self.header.base_addr {
            return Ok(None);
        }

        let mut old_alloc_addr = *self.header.alloc_addr;

        if old_alloc_addr >= tail {
            old_alloc_addr = *self.header.base_addr;
        }

        let mut addr = old_alloc_addr;
        let mut res: Option<u64> = None;
        for _ in 0..self.alloc_max_walk {
            assert!(addr < tail);
            let (chunk_size, desc_haddr) = {
                let desc = self.get_descriptor(addr)?;
                (desc.chunk_size as usize, desc.haddr)
            };
            let exit = if chunk_size == length as usize {
                // perfect match
                {
                    let mut header = self.get_header(DiskAddress::from(desc_haddr))?;
                    assert_eq!(header.chunk_size as usize, chunk_size);
                    assert!(header.is_freed);
                    #[allow(clippy::unwrap_used)]
                    header.modify(|h| h.is_freed = false).unwrap();
                }
                self.delete_descriptor(addr)?;
                true
            } else if chunk_size > length as usize + HEADER_SIZE + FOOTER_SIZE {
                // able to split
                {
                    let mut lheader = self.get_header(DiskAddress::from(desc_haddr))?;
                    assert_eq!(lheader.chunk_size as usize, chunk_size);
                    assert!(lheader.is_freed);
                    #[allow(clippy::unwrap_used)]
                    lheader
                        .modify(|h| {
                            h.is_freed = false;
                            h.chunk_size = length;
                        })
                        .unwrap();
                }
                {
                    let mut lfooter = self.get_footer(DiskAddress::from(
                        desc_haddr + HEADER_SIZE + length as usize,
                    ))?;
                    //assert!(lfooter.chunk_size == chunk_size);
                    #[allow(clippy::unwrap_used)]
                    lfooter.modify(|f| f.chunk_size = length).unwrap();
                }

                let offset = desc_haddr + HEADER_SIZE + length as usize + FOOTER_SIZE;
                let rchunk_size = chunk_size - length as usize - FOOTER_SIZE - HEADER_SIZE;
                let rdesc_addr = self.new_descriptor_address()?;
                {
                    let mut rdesc = self.get_descriptor(rdesc_addr)?;
                    #[allow(clippy::unwrap_used)]
                    rdesc
                        .modify(|rd| {
                            rd.chunk_size = rchunk_size as u64;
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
                            rh.chunk_size = rchunk_size as u64;
                            rh.desc_addr = rdesc_addr;
                        })
                        .unwrap();
                }
                {
                    let mut rfooter =
                        self.get_footer(DiskAddress::from(offset + HEADER_SIZE + rchunk_size))?;
                    #[allow(clippy::unwrap_used)]
                    rfooter
                        .modify(|f| f.chunk_size = rchunk_size as u64)
                        .unwrap();
                }
                self.delete_descriptor(addr)?;
                true
            } else {
                false
            };
            #[allow(clippy::unwrap_used)]
            if exit {
                self.header.alloc_addr.modify(|r| *r = addr).unwrap();
                res = Some((desc_haddr + HEADER_SIZE) as u64);
                break;
            }
            addr += DESCRIPTOR_SIZE;
            if addr >= tail {
                addr = *self.header.base_addr;
            }
            if addr == old_alloc_addr {
                break;
            }
        }
        Ok(res)
    }

    fn alloc_new(&mut self, alloc_size: u64) -> Result<u64, ShaleError> {
        let region_size = 1 << self.regn_nbit;
        let new_chunk_size = ChunkHeader::SERIALIZED_LEN + alloc_size + ChunkFooter::SERIALIZED_LEN;
        let mut free_chunk_header_offset = *self.header.data_store_tail;

        #[allow(clippy::unwrap_used)]
        self.header
            .data_store_tail
            .modify(|data_store_tail| {
                // an item is always fully in one region
                // TODO danlaine: we should document the above better. Where is this guaranteed?
                let remaining_region_size =
                    region_size - (free_chunk_header_offset & (region_size - 1)).get();

                if remaining_region_size < new_chunk_size as usize {
                    // There is not enough space in the current region for this alloc.
                    // Move to the next region.
                    free_chunk_header_offset += remaining_region_size;
                    *data_store_tail += remaining_region_size;
                }

                *data_store_tail += new_chunk_size as usize
            })
            .unwrap();

        let mut free_chunk_header = self.get_header(free_chunk_header_offset)?;

        #[allow(clippy::unwrap_used)]
        free_chunk_header
            .modify(|h| {
                h.chunk_size = alloc_size;
                h.is_freed = false;
                h.desc_addr = DiskAddress::null();
            })
            .unwrap();

        let mut free_chunk_footer = self.get_footer(
            free_chunk_header_offset + ChunkHeader::SERIALIZED_LEN as usize + alloc_size as usize,
        )?;

        #[allow(clippy::unwrap_used)]
        free_chunk_footer
            .modify(|f| f.chunk_size = alloc_size)
            .unwrap();

        #[allow(clippy::unwrap_used)]
        Ok(
            (free_chunk_header_offset + ChunkHeader::SERIALIZED_LEN as usize)
                .0
                .unwrap()
                .get() as u64,
        )
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
pub struct Store<T: Storable, M> {
    inner: RwLock<StoreInner<M>>,
    obj_cache: ObjCache<T>,
}

impl<T: Storable, M: LinearStore> Store<T, M> {
    pub fn new(
        meta_store: M,
        data_store: M,
        header: Obj<StoreHeader>,
        obj_cache: super::ObjCache<T>,
        alloc_max_walk: u64,
        regn_nbit: u64,
    ) -> Result<Self, ShaleError> {
        let cs = Store {
            inner: RwLock::new(StoreInner {
                meta_store,
                data_store,
                header: StoreHeader::into_fields(header)?,
                alloc_max_walk,
                regn_nbit,
            }),
            obj_cache,
        };
        Ok(cs)
    }
}

impl From<Store<Node, StoreRevMut>> for Store<Node, StoreRevShared> {
    #[allow(clippy::unwrap_used)]
    fn from(value: Store<Node, StoreRevMut>) -> Self {
        let inner = value.inner.into_inner().unwrap();
        Store {
            inner: RwLock::new(inner.into()),
            obj_cache: value.obj_cache,
        }
    }
}

impl<T: Storable + Debug + 'static, M: LinearStore> Store<T, M> {
    pub(crate) fn put_item(&self, item: T, extra: u64) -> Result<ObjRef<'_, T>, ShaleError> {
        let size = item.serialized_len() + extra;
        #[allow(clippy::unwrap_used)]
        let addr = self.inner.write().unwrap().alloc(size)?;

        trace!("{self:p} put_item at {addr} size {size}");

        #[allow(clippy::unwrap_used)]
        let obj = {
            let inner = self.inner.read().unwrap();
            let data_store = &inner.data_store;
            #[allow(clippy::unwrap_used)]
            let view = StoredView::item_to_obj(data_store, addr.try_into().unwrap(), size, item)?;

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
    pub(crate) fn free_item(&mut self, addr: DiskAddress) -> Result<(), ShaleError> {
        let mut inner = self.inner.write().unwrap();
        self.obj_cache.pop(addr);
        #[allow(clippy::unwrap_used)]
        inner.free(addr.unwrap().get() as u64)
    }

    pub(crate) fn get_item(&self, addr: DiskAddress) -> Result<ObjRef<'_, T>, ShaleError> {
        let obj = self.obj_cache.get(addr)?;

        #[allow(clippy::unwrap_used)]
        let inner = self.inner.read().unwrap();
        let cache = &self.obj_cache;

        if let Some(obj) = obj {
            return Ok(ObjRef::new(obj, cache));
        }

        #[allow(clippy::unwrap_used)]
        if addr < DiskAddress::from(StoreHeader::SERIALIZED_LEN as usize) {
            return Err(ShaleError::InvalidAddressLength {
                expected: StoreHeader::SERIALIZED_LEN,
                found: addr.0.map(|inner| inner.get()).unwrap_or_default() as u64,
            });
        }

        let chunk_size = inner
            .get_header(addr - ChunkHeader::SERIALIZED_LEN as usize)?
            .chunk_size;
        let obj = self.obj_cache.put(inner.get_data_ref(addr, chunk_size)?);
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

    use crate::shale::{self, in_mem::InMemLinearStore};

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
        fn deserialize<T: LinearStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
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
    fn test_store_item() {
        let meta_size: NonZeroUsize = NonZeroUsize::new(0x10000).unwrap();
        let compact_size: NonZeroUsize = NonZeroUsize::new(0x10000).unwrap();
        let reserved: DiskAddress = 0x1000.into();

        let mut dm = InMemLinearStore::new(meta_size.get() as u64, 0x0);

        // initialize compact store
        let compact_header = DiskAddress::from(0x1);
        dm.write(
            compact_header.unwrap().get(),
            &shale::to_dehydrated(&StoreHeader::new(reserved.0.unwrap(), reserved.0.unwrap()))
                .unwrap(),
        )
        .unwrap();
        let compact_header =
            StoredView::addr_to_obj(&dm, compact_header, ChunkHeader::SERIALIZED_LEN).unwrap();
        let mem_meta = dm;
        let mem_payload = InMemLinearStore::new(compact_size.get() as u64, 0x1);

        let cache: ObjCache<Hash> = ObjCache::new(1);
        let store = Store::new(mem_meta, mem_payload, compact_header, cache, 10, 16).unwrap();

        // initial write
        let data = b"hello world";
        let hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(data).into();
        let obj_ref = store.put_item(Hash(hash), 0).unwrap();
        assert_eq!(obj_ref.as_addr(), DiskAddress::from(4113));
        // create hash addr from address and attempt to read dirty write.
        let hash_ref = store.get_item(DiskAddress::from(4113)).unwrap();
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
            store.get_item(DiskAddress::from(4113)).unwrap().as_ref(),
            hash
        );
    }
}
