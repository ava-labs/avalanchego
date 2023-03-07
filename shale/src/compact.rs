use super::{MemStore, MummyItem, MummyObj, Obj, ObjPtr, ObjRef, ShaleError, ShaleStore};
use std::cell::UnsafeCell;
use std::io::{Cursor, Write};
use std::rc::Rc;

pub struct CompactHeader {
    payload_size: u64,
    is_freed: bool,
    desc_addr: ObjPtr<CompactDescriptor>,
}

impl CompactHeader {
    pub const MSIZE: u64 = 17;
    pub fn is_freed(&self) -> bool {
        self.is_freed
    }

    pub fn payload_size(&self) -> u64 {
        self.payload_size
    }
}

impl MummyItem for CompactHeader {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        let payload_size = u64::from_le_bytes(raw.as_deref()[..8].try_into().unwrap());
        let is_freed = raw.as_deref()[8] != 0;
        let desc_addr = u64::from_le_bytes(raw.as_deref()[9..17].try_into().unwrap());
        Ok(Self {
            payload_size,
            is_freed,
            desc_addr: ObjPtr::new(desc_addr),
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes()).unwrap();
        cur.write_all(&[if self.is_freed { 1 } else { 0 }]).unwrap();
        cur.write_all(&self.desc_addr.addr().to_le_bytes()).unwrap();
    }
}

struct CompactFooter {
    payload_size: u64,
}

impl CompactFooter {
    const MSIZE: u64 = 8;
}

impl MummyItem for CompactFooter {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        let payload_size = u64::from_le_bytes(raw.as_deref().try_into().unwrap());
        Ok(Self { payload_size })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        Cursor::new(to)
            .write_all(&self.payload_size.to_le_bytes())
            .unwrap();
    }
}

#[derive(Clone, Copy)]
struct CompactDescriptor {
    payload_size: u64,
    haddr: u64, // pointer to the payload of freed space
}

impl CompactDescriptor {
    const MSIZE: u64 = 16;
}

impl MummyItem for CompactDescriptor {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        let payload_size = u64::from_le_bytes(raw.as_deref()[..8].try_into().unwrap());
        let haddr = u64::from_le_bytes(raw.as_deref()[8..].try_into().unwrap());
        Ok(Self {
            payload_size,
            haddr,
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes()).unwrap();
        cur.write_all(&self.haddr.to_le_bytes()).unwrap();
    }
}

pub struct CompactSpaceHeader {
    meta_space_tail: u64,
    compact_space_tail: u64,
    base_addr: ObjPtr<CompactDescriptor>,
    alloc_addr: ObjPtr<CompactDescriptor>,
}

struct CompactSpaceHeaderSliced {
    meta_space_tail: Obj<U64Field>,
    compact_space_tail: Obj<U64Field>,
    base_addr: Obj<ObjPtr<CompactDescriptor>>,
    alloc_addr: Obj<ObjPtr<CompactDescriptor>>,
}

impl CompactSpaceHeaderSliced {
    fn flush_dirty(&mut self) {
        self.meta_space_tail.flush_dirty();
        self.compact_space_tail.flush_dirty();
        self.base_addr.flush_dirty();
        self.alloc_addr.flush_dirty();
    }
}

impl CompactSpaceHeader {
    pub const MSIZE: u64 = 32;

    pub fn new(meta_base: u64, compact_base: u64) -> Self {
        Self {
            meta_space_tail: meta_base,
            compact_space_tail: compact_base,
            base_addr: ObjPtr::new_from_addr(meta_base),
            alloc_addr: ObjPtr::new_from_addr(meta_base),
        }
    }

    fn into_fields(r: Obj<Self>) -> Result<CompactSpaceHeaderSliced, ShaleError> {
        Ok(CompactSpaceHeaderSliced {
            meta_space_tail: MummyObj::slice(&r, 0, 8, U64Field(r.meta_space_tail))?,
            compact_space_tail: MummyObj::slice(&r, 8, 8, U64Field(r.compact_space_tail))?,
            base_addr: MummyObj::slice(&r, 16, 8, r.base_addr)?,
            alloc_addr: MummyObj::slice(&r, 24, 8, r.alloc_addr)?,
        })
    }
}

impl MummyItem for CompactSpaceHeader {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        let meta_space_tail = u64::from_le_bytes(raw.as_deref()[..8].try_into().unwrap());
        let compact_space_tail = u64::from_le_bytes(raw.as_deref()[8..16].try_into().unwrap());
        let base_addr = u64::from_le_bytes(raw.as_deref()[16..24].try_into().unwrap());
        let alloc_addr = u64::from_le_bytes(raw.as_deref()[24..].try_into().unwrap());
        Ok(Self {
            meta_space_tail,
            compact_space_tail,
            base_addr: ObjPtr::new_from_addr(base_addr),
            alloc_addr: ObjPtr::new_from_addr(alloc_addr),
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.meta_space_tail.to_le_bytes()).unwrap();
        cur.write_all(&self.compact_space_tail.to_le_bytes())
            .unwrap();
        cur.write_all(&self.base_addr.addr().to_le_bytes()).unwrap();
        cur.write_all(&self.alloc_addr.addr().to_le_bytes())
            .unwrap();
    }
}

struct ObjPtrField<T>(ObjPtr<T>);

impl<T> ObjPtrField<T> {
    const MSIZE: u64 = 8;
}

impl<T> MummyItem for ObjPtrField<T> {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        Ok(Self(ObjPtr::new_from_addr(u64::from_le_bytes(
            raw.as_deref().try_into().unwrap(),
        ))))
    }

    fn dehydrate(&self, to: &mut [u8]) {
        Cursor::new(to)
            .write_all(&self.0.addr().to_le_bytes())
            .unwrap()
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }
}

impl<T> std::ops::Deref for ObjPtrField<T> {
    type Target = ObjPtr<T>;
    fn deref(&self) -> &ObjPtr<T> {
        &self.0
    }
}

impl<T> std::ops::DerefMut for ObjPtrField<T> {
    fn deref_mut(&mut self) -> &mut ObjPtr<T> {
        &mut self.0
    }
}

struct U64Field(u64);

impl U64Field {
    const MSIZE: u64 = 8;
}

impl MummyItem for U64Field {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        Ok(Self(u64::from_le_bytes(raw.as_deref().try_into().unwrap())))
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        Cursor::new(to).write_all(&self.0.to_le_bytes()).unwrap()
    }
}

impl std::ops::Deref for U64Field {
    type Target = u64;
    fn deref(&self) -> &u64 {
        &self.0
    }
}

impl std::ops::DerefMut for U64Field {
    fn deref_mut(&mut self) -> &mut u64 {
        &mut self.0
    }
}

struct CompactSpaceInner<T: MummyItem> {
    meta_space: Rc<dyn MemStore>,
    compact_space: Rc<dyn MemStore>,
    header: CompactSpaceHeaderSliced,
    obj_cache: super::ObjCache<T>,
    alloc_max_walk: u64,
    regn_nbit: u64,
}

impl<T: MummyItem> CompactSpaceInner<T> {
    fn get_descriptor(
        &self,
        ptr: ObjPtr<CompactDescriptor>,
    ) -> Result<Obj<CompactDescriptor>, ShaleError> {
        MummyObj::ptr_to_obj(self.meta_space.as_ref(), ptr, CompactDescriptor::MSIZE)
    }

    fn get_data_ref<U: MummyItem + 'static>(
        &self,
        ptr: ObjPtr<U>,
        len_limit: u64,
    ) -> Result<Obj<U>, ShaleError> {
        MummyObj::ptr_to_obj(self.compact_space.as_ref(), ptr, len_limit)
    }

    fn get_header(&self, ptr: ObjPtr<CompactHeader>) -> Result<Obj<CompactHeader>, ShaleError> {
        self.get_data_ref::<CompactHeader>(ptr, CompactHeader::MSIZE)
    }

    fn get_footer(&self, ptr: ObjPtr<CompactFooter>) -> Result<Obj<CompactFooter>, ShaleError> {
        self.get_data_ref::<CompactFooter>(ptr, CompactFooter::MSIZE)
    }

    fn del_desc(&mut self, desc_addr: ObjPtr<CompactDescriptor>) -> Result<(), ShaleError> {
        let desc_size = CompactDescriptor::MSIZE;
        debug_assert!((desc_addr.addr - self.header.base_addr.addr) % desc_size == 0);
        self.header.meta_space_tail.write(|r| **r -= desc_size);
        if desc_addr.addr != **self.header.meta_space_tail {
            let desc_last =
                self.get_descriptor(ObjPtr::new_from_addr(**self.header.meta_space_tail))?;
            let mut desc = self.get_descriptor(ObjPtr::new_from_addr(desc_addr.addr))?;
            desc.write(|r| *r = *desc_last);
            let mut header = self.get_header(ObjPtr::new(desc.haddr))?;
            header.write(|h| h.desc_addr = desc_addr);
        }
        Ok(())
    }

    fn new_desc(&mut self) -> Result<ObjPtr<CompactDescriptor>, ShaleError> {
        let addr = **self.header.meta_space_tail;
        self.header
            .meta_space_tail
            .write(|r| **r += CompactDescriptor::MSIZE);
        Ok(ObjPtr::new_from_addr(addr))
    }

    fn free(&mut self, addr: u64) -> Result<(), ShaleError> {
        let hsize = CompactHeader::MSIZE;
        let fsize = CompactFooter::MSIZE;
        let regn_size = 1 << self.regn_nbit;

        let mut offset = addr - hsize;
        let header_payload_size = {
            let header = self.get_header(ObjPtr::new(offset))?;
            assert!(!header.is_freed);
            header.payload_size
        };
        let mut h = offset;
        let mut payload_size = header_payload_size;

        if offset & (regn_size - 1) > 0 {
            // merge with lower data segment
            offset -= fsize;
            let (pheader_is_freed, pheader_payload_size, pheader_desc_addr) = {
                let pfooter = self.get_footer(ObjPtr::new(offset))?;
                offset -= pfooter.payload_size + hsize;
                let pheader = self.get_header(ObjPtr::new(offset))?;
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

        if offset + fsize < **self.header.compact_space_tail
            && (regn_size - (offset & (regn_size - 1))) >= fsize + hsize
        {
            // merge with higher data segment
            offset += fsize;
            let (nheader_is_freed, nheader_payload_size, nheader_desc_addr) = {
                let nheader = self.get_header(ObjPtr::new(offset))?;
                (nheader.is_freed, nheader.payload_size, nheader.desc_addr)
            };
            if nheader_is_freed {
                offset += hsize + nheader_payload_size;
                f = offset;
                {
                    let nfooter = self.get_footer(ObjPtr::new(offset))?;
                    assert!(nheader_payload_size == nfooter.payload_size);
                }
                payload_size += hsize + fsize + nheader_payload_size;
                self.del_desc(nheader_desc_addr)?;
            }
        }

        let desc_addr = self.new_desc()?;
        {
            let mut desc = self.get_descriptor(desc_addr)?;
            desc.write(|d| {
                d.payload_size = payload_size;
                d.haddr = h;
            });
        }
        let mut h = self.get_header(ObjPtr::new(h))?;
        let mut f = self.get_footer(ObjPtr::new(f))?;
        h.write(|h| {
            h.payload_size = payload_size;
            h.is_freed = true;
            h.desc_addr = desc_addr;
        });
        f.write(|f| f.payload_size = payload_size);
        Ok(())
    }

    fn alloc_from_freed(&mut self, length: u64) -> Result<Option<u64>, ShaleError> {
        let tail = **self.header.meta_space_tail;
        if tail == self.header.base_addr.addr {
            return Ok(None);
        }

        let hsize = CompactHeader::MSIZE;
        let fsize = CompactFooter::MSIZE;
        let dsize = CompactDescriptor::MSIZE;

        let mut old_alloc_addr = *self.header.alloc_addr;

        if old_alloc_addr.addr >= tail {
            old_alloc_addr = *self.header.base_addr;
        }

        let mut ptr = old_alloc_addr;
        let mut res: Option<u64> = None;
        for _ in 0..self.alloc_max_walk {
            assert!(ptr.addr < tail);
            let (desc_payload_size, desc_haddr) = {
                let desc = self.get_descriptor(ptr)?;
                (desc.payload_size, desc.haddr)
            };
            let exit = if desc_payload_size == length {
                // perfect match
                {
                    let mut header = self.get_header(ObjPtr::new(desc_haddr))?;
                    assert_eq!(header.payload_size, desc_payload_size);
                    assert!(header.is_freed);
                    header.write(|h| h.is_freed = false);
                }
                self.del_desc(ptr)?;
                true
            } else if desc_payload_size > length + hsize + fsize {
                // able to split
                {
                    let mut lheader = self.get_header(ObjPtr::new(desc_haddr))?;
                    assert_eq!(lheader.payload_size, desc_payload_size);
                    assert!(lheader.is_freed);
                    lheader.write(|h| {
                        h.is_freed = false;
                        h.payload_size = length;
                    });
                }
                {
                    let mut lfooter = self.get_footer(ObjPtr::new(desc_haddr + hsize + length))?;
                    //assert!(lfooter.payload_size == desc_payload_size);
                    lfooter.write(|f| f.payload_size = length);
                }

                let offset = desc_haddr + hsize + length + fsize;
                let rpayload_size = desc_payload_size - length - fsize - hsize;
                let rdesc_addr = self.new_desc()?;
                {
                    let mut rdesc = self.get_descriptor(rdesc_addr)?;
                    rdesc.write(|rd| {
                        rd.payload_size = rpayload_size;
                        rd.haddr = offset;
                    });
                }
                {
                    let mut rheader = self.get_header(ObjPtr::new(offset))?;
                    rheader.write(|rh| {
                        rh.is_freed = true;
                        rh.payload_size = rpayload_size;
                        rh.desc_addr = rdesc_addr;
                    });
                }
                {
                    let mut rfooter =
                        self.get_footer(ObjPtr::new(offset + hsize + rpayload_size))?;
                    rfooter.write(|f| f.payload_size = rpayload_size);
                }
                self.del_desc(ptr)?;
                true
            } else {
                false
            };
            if exit {
                self.header.alloc_addr.write(|r| *r = ptr);
                res = Some(desc_haddr + hsize);
                break;
            }
            ptr.addr += dsize;
            if ptr.addr >= tail {
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
        let total_length = CompactHeader::MSIZE + length + CompactFooter::MSIZE;
        let mut offset = **self.header.compact_space_tail;
        self.header.compact_space_tail.write(|r| {
            // an item is always fully in one region
            let rem = regn_size - (offset & (regn_size - 1));
            if rem < total_length {
                offset += rem;
                **r += rem;
            }
            **r += total_length
        });
        let mut h = self.get_header(ObjPtr::new(offset))?;
        let mut f = self.get_footer(ObjPtr::new(offset + CompactHeader::MSIZE + length))?;
        h.write(|h| {
            h.payload_size = length;
            h.is_freed = false;
            h.desc_addr = ObjPtr::new(0);
        });
        f.write(|f| f.payload_size = length);
        Ok(offset + CompactHeader::MSIZE)
    }
}

pub struct CompactSpace<T: MummyItem> {
    inner: UnsafeCell<CompactSpaceInner<T>>,
}

impl<T: MummyItem> CompactSpace<T> {
    pub fn new(
        meta_space: Rc<dyn MemStore>,
        compact_space: Rc<dyn MemStore>,
        header: Obj<CompactSpaceHeader>,
        obj_cache: super::ObjCache<T>,
        alloc_max_walk: u64,
        regn_nbit: u64,
    ) -> Result<Self, ShaleError> {
        let cs = CompactSpace {
            inner: UnsafeCell::new(CompactSpaceInner {
                meta_space,
                compact_space,
                header: CompactSpaceHeader::into_fields(header)?,
                obj_cache,
                alloc_max_walk,
                regn_nbit,
            }),
        };
        Ok(cs)
    }
}

impl<T: MummyItem + 'static> ShaleStore<T> for CompactSpace<T> {
    fn put_item(&'_ self, item: T, extra: u64) -> Result<ObjRef<'_, T>, ShaleError> {
        let size = item.dehydrated_len() + extra;
        let inner = unsafe { &mut *self.inner.get() };
        let ptr: ObjPtr<T> =
            ObjPtr::new_from_addr(if let Some(addr) = inner.alloc_from_freed(size)? {
                addr
            } else {
                inner.alloc_new(size)?
            });
        let mut u = inner.obj_cache.put(MummyObj::item_to_obj(
            inner.compact_space.as_ref(),
            ptr.addr(),
            size,
            item,
        )?);
        u.write(|_| {}).unwrap();
        Ok(u)
    }

    fn free_item(&mut self, ptr: ObjPtr<T>) -> Result<(), ShaleError> {
        let inner = self.inner.get_mut();
        inner.obj_cache.pop(ptr);
        inner.free(ptr.addr())
    }

    fn get_item(&'_ self, ptr: ObjPtr<T>) -> Result<ObjRef<'_, T>, ShaleError> {
        let inner = unsafe { &*self.inner.get() };
        if let Some(r) = inner.obj_cache.get(ptr)? {
            return Ok(r);
        }
        if ptr.addr() < CompactSpaceHeader::MSIZE {
            return Err(ShaleError::ObjPtrInvalid);
        }
        let h = inner.get_header(ObjPtr::new(ptr.addr() - CompactHeader::MSIZE))?;
        Ok(inner
            .obj_cache
            .put(inner.get_data_ref(ptr, h.payload_size)?))
    }

    fn flush_dirty(&self) -> Option<()> {
        let inner = unsafe { &mut *self.inner.get() };
        inner.header.flush_dirty();
        inner.obj_cache.flush_dirty()
    }
}
