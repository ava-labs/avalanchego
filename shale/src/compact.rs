use super::{CachedStore, Obj, ObjPtr, ObjRef, ShaleError, ShaleStore, Storable, StoredView};
use std::fmt::Debug;
use std::io::{Cursor, Write};
use std::ops::DerefMut;
use std::sync::{Arc, RwLock};

#[derive(Debug)]
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

impl Storable for CompactHeader {
    fn hydrate<T: CachedStore>(addr: u64, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let payload_size =
            u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        let is_freed = raw.as_deref()[8] != 0;
        let desc_addr =
            u64::from_le_bytes(raw.as_deref()[9..17].try_into().expect("invalid slice"));
        Ok(Self {
            payload_size,
            is_freed,
            desc_addr: ObjPtr::new(desc_addr),
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes())?;
        cur.write_all(&[if self.is_freed { 1 } else { 0 }])?;
        cur.write_all(&self.desc_addr.addr().to_le_bytes())?;
        Ok(())
    }
}

#[derive(Debug)]
struct CompactFooter {
    payload_size: u64,
}

impl CompactFooter {
    const MSIZE: u64 = 8;
}

impl Storable for CompactFooter {
    fn hydrate<T: CachedStore>(addr: u64, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let payload_size = u64::from_le_bytes(raw.as_deref().try_into().unwrap());
        Ok(Self { payload_size })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.payload_size.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Clone, Copy, Debug)]
struct CompactDescriptor {
    payload_size: u64,
    haddr: u64, // pointer to the payload of freed space
}

impl CompactDescriptor {
    const MSIZE: u64 = 16;
}

impl Storable for CompactDescriptor {
    fn hydrate<T: CachedStore>(addr: u64, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let payload_size =
            u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        let haddr = u64::from_le_bytes(raw.as_deref()[8..].try_into().expect("invalid slice"));
        Ok(Self {
            payload_size,
            haddr,
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.payload_size.to_le_bytes())?;
        cur.write_all(&self.haddr.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Debug)]
pub struct CompactSpaceHeader {
    meta_space_tail: u64,
    compact_space_tail: u64,
    base_addr: ObjPtr<CompactDescriptor>,
    alloc_addr: ObjPtr<CompactDescriptor>,
}

#[derive(Debug)]
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
            meta_space_tail: StoredView::slice(&r, 0, 8, U64Field(r.meta_space_tail))?,
            compact_space_tail: StoredView::slice(&r, 8, 8, U64Field(r.compact_space_tail))?,
            base_addr: StoredView::slice(&r, 16, 8, r.base_addr)?,
            alloc_addr: StoredView::slice(&r, 24, 8, r.alloc_addr)?,
        })
    }
}

impl Storable for CompactSpaceHeader {
    fn hydrate<T: CachedStore + Debug>(addr: u64, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let meta_space_tail =
            u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        let compact_space_tail =
            u64::from_le_bytes(raw.as_deref()[8..16].try_into().expect("invalid slice"));
        let base_addr =
            u64::from_le_bytes(raw.as_deref()[16..24].try_into().expect("invalid slice"));
        let alloc_addr =
            u64::from_le_bytes(raw.as_deref()[24..].try_into().expect("invalid slice"));
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

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.meta_space_tail.to_le_bytes())?;
        cur.write_all(&self.compact_space_tail.to_le_bytes())?;
        cur.write_all(&self.base_addr.addr().to_le_bytes())?;
        cur.write_all(&self.alloc_addr.addr().to_le_bytes())?;
        Ok(())
    }
}

struct ObjPtrField<T>(ObjPtr<T>);

impl<T> ObjPtrField<T> {
    const MSIZE: u64 = 8;
}

impl<T> Storable for ObjPtrField<T> {
    fn hydrate<U: CachedStore>(addr: u64, mem: &U) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let obj_ptr = ObjPtr::new_from_addr(u64::from_le_bytes(
            <[u8; 8]>::try_from(&raw.as_deref()[0..8]).expect("invalid slice"),
        ));
        Ok(Self(obj_ptr))
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.0.addr().to_le_bytes())?;
        Ok(())
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

#[derive(Debug)]
struct U64Field(u64);

impl U64Field {
    const MSIZE: u64 = 8;
}

impl Storable for U64Field {
    fn hydrate<U: CachedStore>(addr: u64, mem: &U) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        Ok(Self(u64::from_le_bytes(raw.as_deref().try_into().unwrap())))
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.0.to_le_bytes())?;
        Ok(())
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

#[derive(Debug)]
struct CompactSpaceInner<T: Send + Sync, M> {
    meta_space: Arc<M>,
    compact_space: Arc<M>,
    header: CompactSpaceHeaderSliced,
    obj_cache: super::ObjCache<T>,
    alloc_max_walk: u64,
    regn_nbit: u64,
}

impl<T: Storable + Send + Sync, M: CachedStore> CompactSpaceInner<T, M> {
    fn get_descriptor(
        &self,
        ptr: ObjPtr<CompactDescriptor>,
    ) -> Result<Obj<CompactDescriptor>, ShaleError> {
        StoredView::ptr_to_obj(self.meta_space.as_ref(), ptr, CompactDescriptor::MSIZE)
    }

    fn get_data_ref<U: Storable + Debug + Send + Sync + 'static>(
        &self,
        ptr: ObjPtr<U>,
        len_limit: u64,
    ) -> Result<Obj<U>, ShaleError> {
        StoredView::ptr_to_obj(self.compact_space.as_ref(), ptr, len_limit)
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
        self.header
            .meta_space_tail
            .write(|r| **r -= desc_size)
            .unwrap();

        if desc_addr.addr != **self.header.meta_space_tail {
            let desc_last =
                self.get_descriptor(ObjPtr::new_from_addr(**self.header.meta_space_tail))?;
            let mut desc = self.get_descriptor(ObjPtr::new_from_addr(desc_addr.addr))?;
            desc.write(|r| *r = *desc_last).unwrap();
            let mut header = self.get_header(ObjPtr::new(desc.haddr))?;
            header.write(|h| h.desc_addr = desc_addr).unwrap();
        }

        Ok(())
    }

    fn new_desc(&mut self) -> Result<ObjPtr<CompactDescriptor>, ShaleError> {
        let addr = **self.header.meta_space_tail;
        self.header
            .meta_space_tail
            .write(|r| **r += CompactDescriptor::MSIZE)
            .unwrap();

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
            })
            .unwrap();
        }
        let mut h = self.get_header(ObjPtr::new(h))?;
        let mut f = self.get_footer(ObjPtr::new(f))?;
        h.write(|h| {
            h.payload_size = payload_size;
            h.is_freed = true;
            h.desc_addr = desc_addr;
        })
        .unwrap();
        f.write(|f| f.payload_size = payload_size).unwrap();

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
                    header.write(|h| h.is_freed = false).unwrap();
                }
                self.del_desc(ptr)?;
                true
            } else if desc_payload_size > length + hsize + fsize {
                // able to split
                {
                    let mut lheader = self.get_header(ObjPtr::new(desc_haddr))?;
                    assert_eq!(lheader.payload_size, desc_payload_size);
                    assert!(lheader.is_freed);
                    lheader
                        .write(|h| {
                            h.is_freed = false;
                            h.payload_size = length;
                        })
                        .unwrap();
                }
                {
                    let mut lfooter = self.get_footer(ObjPtr::new(desc_haddr + hsize + length))?;
                    //assert!(lfooter.payload_size == desc_payload_size);
                    lfooter.write(|f| f.payload_size = length).unwrap();
                }

                let offset = desc_haddr + hsize + length + fsize;
                let rpayload_size = desc_payload_size - length - fsize - hsize;
                let rdesc_addr = self.new_desc()?;
                {
                    let mut rdesc = self.get_descriptor(rdesc_addr)?;
                    rdesc
                        .write(|rd| {
                            rd.payload_size = rpayload_size;
                            rd.haddr = offset;
                        })
                        .unwrap();
                }
                {
                    let mut rheader = self.get_header(ObjPtr::new(offset))?;
                    rheader
                        .write(|rh| {
                            rh.is_freed = true;
                            rh.payload_size = rpayload_size;
                            rh.desc_addr = rdesc_addr;
                        })
                        .unwrap();
                }
                {
                    let mut rfooter =
                        self.get_footer(ObjPtr::new(offset + hsize + rpayload_size))?;
                    rfooter.write(|f| f.payload_size = rpayload_size).unwrap();
                }
                self.del_desc(ptr)?;
                true
            } else {
                false
            };
            if exit {
                self.header.alloc_addr.write(|r| *r = ptr).unwrap();
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
        self.header
            .compact_space_tail
            .write(|r| {
                // an item is always fully in one region
                let rem = regn_size - (offset & (regn_size - 1));
                if rem < total_length {
                    offset += rem;
                    **r += rem;
                }
                **r += total_length
            })
            .unwrap();
        let mut h = self.get_header(ObjPtr::new(offset))?;
        let mut f = self.get_footer(ObjPtr::new(offset + CompactHeader::MSIZE + length))?;
        h.write(|h| {
            h.payload_size = length;
            h.is_freed = false;
            h.desc_addr = ObjPtr::new(0);
        })
        .unwrap();
        f.write(|f| f.payload_size = length).unwrap();
        Ok(offset + CompactHeader::MSIZE)
    }
}

#[derive(Debug)]
pub struct CompactSpace<T: Send + Sync, M> {
    inner: RwLock<CompactSpaceInner<T, M>>,
}

impl<T: Storable + Send + Sync, M: CachedStore> CompactSpace<T, M> {
    pub fn new(
        meta_space: Arc<M>,
        compact_space: Arc<M>,
        header: Obj<CompactSpaceHeader>,
        obj_cache: super::ObjCache<T>,
        alloc_max_walk: u64,
        regn_nbit: u64,
    ) -> Result<Self, ShaleError> {
        let cs = CompactSpace {
            inner: RwLock::new(CompactSpaceInner {
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

impl<T: Storable + Send + Sync + Debug + 'static, M: CachedStore + Send + Sync> ShaleStore<T>
    for CompactSpace<T, M>
{
    fn put_item<'a>(&'a self, item: T, extra: u64) -> Result<ObjRef<'a, T>, ShaleError> {
        let size = item.dehydrated_len() + extra;
        let ptr: ObjPtr<T> = {
            let mut inner = self.inner.write().unwrap();
            let addr = if let Some(addr) = inner.alloc_from_freed(size)? {
                addr
            } else {
                inner.alloc_new(size)?
            };

            ObjPtr::new_from_addr(addr)
        };
        let inner = self.inner.read().unwrap();

        let mut u: ObjRef<'a, T> = inner.obj_cache.put(StoredView::item_to_obj(
            inner.compact_space.as_ref(),
            ptr.addr(),
            size,
            item,
        )?);

        u.write(|_| {}).unwrap();

        Ok(u)
    }

    fn free_item(&mut self, ptr: ObjPtr<T>) -> Result<(), ShaleError> {
        let mut inner = self.inner.write().unwrap();
        inner.obj_cache.pop(ptr);
        inner.free(ptr.addr())
    }

    fn get_item(&self, ptr: ObjPtr<T>) -> Result<ObjRef<'_, T>, ShaleError> {
        let inner = {
            let inner = self.inner.read().unwrap();
            let obj_ref = inner
                .obj_cache
                .get(inner.obj_cache.lock().deref_mut(), ptr)?;

            if let Some(obj_ref) = obj_ref {
                return Ok(obj_ref);
            }

            inner
        };

        if ptr.addr() < CompactSpaceHeader::MSIZE {
            return Err(ShaleError::InvalidAddressLength {
                expected: CompactSpaceHeader::MSIZE,
                found: ptr.addr(),
            });
        }

        let payload_size = inner
            .get_header(ObjPtr::new(ptr.addr() - CompactHeader::MSIZE))?
            .payload_size;

        Ok(inner.obj_cache.put(inner.get_data_ref(ptr, payload_size)?))
    }

    fn flush_dirty(&self) -> Option<()> {
        let mut inner = self.inner.write().unwrap();
        inner.header.flush_dirty();
        inner.obj_cache.flush_dirty()
    }
}

#[cfg(test)]
mod tests {
    use sha3::Digest;

    use crate::{cached::DynamicMem, ObjCache};

    use super::*;

    const HASH_SIZE: usize = 32;
    const ZERO_HASH: Hash = Hash([0u8; HASH_SIZE]);

    #[derive(PartialEq, Eq, Debug, Clone)]
    pub struct Hash(pub [u8; HASH_SIZE]);

    impl Hash {
        const MSIZE: u64 = 32;
    }

    impl std::ops::Deref for Hash {
        type Target = [u8; HASH_SIZE];
        fn deref(&self) -> &[u8; HASH_SIZE] {
            &self.0
        }
    }

    impl Storable for Hash {
        fn hydrate<T: CachedStore>(addr: u64, mem: &T) -> Result<Self, ShaleError> {
            let raw = mem
                .get_view(addr, Self::MSIZE)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: Self::MSIZE,
                })?;
            Ok(Self(
                raw.as_deref()[..Self::MSIZE as usize]
                    .try_into()
                    .expect("invalid slice"),
            ))
        }

        fn dehydrated_len(&self) -> u64 {
            Self::MSIZE
        }

        fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
            let mut cur = to;
            cur.write_all(&self.0)?;
            Ok(())
        }
    }

    #[test]
    fn test_space_item() {
        const META_SIZE: u64 = 0x10000;
        const COMPACT_SIZE: u64 = 0x10000;
        const RESERVED: u64 = 0x1000;

        let mut dm = DynamicMem::new(META_SIZE, 0x0);

        // initialize compact space
        let compact_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0x1);
        dm.write(
            compact_header.addr(),
            &crate::to_dehydrated(&CompactSpaceHeader::new(RESERVED, RESERVED)).unwrap(),
        );
        let compact_header =
            StoredView::ptr_to_obj(&dm, compact_header, CompactHeader::MSIZE).unwrap();
        let mem_meta = Arc::new(dm);
        let mem_payload = Arc::new(DynamicMem::new(COMPACT_SIZE, 0x1));

        let cache: ObjCache<Hash> = ObjCache::new(1);
        let space =
            CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16).unwrap();

        // initial write
        let data = b"hello world";
        let hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(data).into();
        let obj_ref = space.put_item(Hash(hash), 0).unwrap();
        assert_eq!(obj_ref.as_ptr().addr(), 4113);
        // create hash ptr from address and attempt to read dirty write.
        let hash_ref = space.get_item(ObjPtr::new_from_addr(4113)).unwrap();
        // read before flush results in zeroed hash
        assert_eq!(hash_ref.as_ref(), ZERO_HASH.as_ref());
        // not cached
        assert!(obj_ref
            .cache
            .lock()
            .cached
            .get(&ObjPtr::new_from_addr(4113))
            .is_none());
        // pinned
        assert!(obj_ref
            .cache
            .lock()
            .pinned
            .get(&ObjPtr::new_from_addr(4113))
            .is_some());
        // dirty
        assert!(obj_ref
            .cache
            .lock()
            .dirty
            .get(&ObjPtr::new_from_addr(4113))
            .is_some());
        drop(obj_ref);
        // write is visible
        assert_eq!(
            space
                .get_item(ObjPtr::new_from_addr(4113))
                .unwrap()
                .as_ref(),
            hash
        );
    }
}
