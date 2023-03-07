use std::cell::UnsafeCell;
use std::collections::{HashMap, HashSet};
use std::fmt;
use std::marker::PhantomData;
use std::num::NonZeroUsize;
use std::ops::Deref;
use std::rc::Rc;

pub mod compact;
pub mod util;

#[derive(Debug)]
pub enum ShaleError {
    LinearMemStoreError,
    DecodeError,
    ObjRefAlreadyInUse,
    ObjPtrInvalid,
    SliceError,
}

pub type SpaceID = u8;
pub const INVALID_SPACE_ID: SpaceID = 0xff;

pub struct DiskWrite {
    pub space_id: SpaceID,
    pub space_off: u64,
    pub data: Box<[u8]>,
}

impl std::fmt::Debug for DiskWrite {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> Result<(), std::fmt::Error> {
        write!(
            f,
            "[Disk space=0x{:02x} offset=0x{:04x} data=0x{}",
            self.space_id,
            self.space_off,
            hex::encode(&self.data)
        )
    }
}

/// A handle that pins and provides a readable access to a portion of the linear memory image.
pub trait MemView: Deref<Target = [u8]> {}

/// In-memory store that offers access to intervals from a linear byte space, which is usually
/// backed by a cached/memory-mapped pool of the accessed intervals from the underlying linear
/// persistent store. Reads could trigger disk reads to bring data into memory, but writes will
/// *only* be visible in memory (it does not write back to the disk).
pub trait MemStore {
    /// Returns a handle that pins the `length` of bytes starting from `offset` and makes them
    /// directly accessible.
    fn get_view(&self, offset: u64, length: u64) -> Option<Box<dyn MemView>>;
    /// Returns a handle that allows shared access to the store.
    fn get_shared(&self) -> Option<Box<dyn Deref<Target = dyn MemStore>>>;
    /// Write the `change` to the portion of the linear space starting at `offset`. The change
    /// should be immediately visible to all `MemView` associated to this linear space.
    fn write(&self, offset: u64, change: &[u8]);
    /// Returns the identifier of this storage space.
    fn id(&self) -> SpaceID;
}

/// Opaque typed pointer in the 64-bit virtual addressable space.
#[repr(C)]
pub struct ObjPtr<T: ?Sized> {
    pub(crate) addr: u64,
    phantom: PhantomData<T>,
}

impl<T> std::cmp::PartialEq for ObjPtr<T> {
    fn eq(&self, other: &ObjPtr<T>) -> bool {
        self.addr == other.addr
    }
}

impl<T> Eq for ObjPtr<T> {}
impl<T> Copy for ObjPtr<T> {}
impl<T> Clone for ObjPtr<T> {
    fn clone(&self) -> Self {
        *self
    }
}

impl<T> std::hash::Hash for ObjPtr<T> {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.addr.hash(state)
    }
}

impl<T: ?Sized> fmt::Display for ObjPtr<T> {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "[ObjPtr addr={:08x}]", self.addr)
    }
}

impl<T: ?Sized> ObjPtr<T> {
    pub fn null() -> Self {
        Self::new(0)
    }
    pub fn is_null(&self) -> bool {
        self.addr == 0
    }
    pub fn addr(&self) -> u64 {
        self.addr
    }

    #[inline(always)]
    pub(crate) fn new(addr: u64) -> Self {
        ObjPtr {
            addr,
            phantom: PhantomData,
        }
    }

    #[inline(always)]
    pub fn new_from_addr(addr: u64) -> Self {
        Self::new(addr)
    }
}

/// A addressed, typed, and read-writable handle for the stored item in [ShaleStore]. The object
/// represents the decoded/mapped data. The implementation of [ShaleStore] could use [ObjCache] to
/// turn a `TypedView` into an [ObjRef].
pub trait TypedView<T: ?Sized>: Deref<Target = T> {
    /// Get the offset of the initial byte in the linear space.
    fn get_offset(&self) -> u64;
    /// Access it as a [MemStore] object.
    fn get_mem_store(&self) -> &dyn MemStore;
    /// Estimate the serialized length of the current type content. It should not be smaller than
    /// the actually length.
    fn estimate_mem_image(&self) -> Option<u64>;
    /// Serialize the type content to the memory image. It defines how the current in-memory object
    /// of `T` should be represented in the linear storage space.
    fn write_mem_image(&self, mem_image: &mut [u8]);
    /// Gain mutable access to the typed content. By changing it, its serialized bytes (and length)
    /// could change.
    fn write(&mut self) -> &mut T;
    /// Returns if the typed content is memory-mapped (i.e., all changes through `write` are auto
    /// reflected in the underlying [MemStore]).
    fn is_mem_mapped(&self) -> bool;
}

/// A wrapper of `TypedView` to enable writes. The direct construction (by [Obj::from_typed_view]
/// or [MummyObj::ptr_to_obj]) could be useful for some unsafe access to a low-level item (e.g.
/// headers/metadata at bootstrap or part of [ShaleStore] implementation) stored at a given [ObjPtr]
/// . Users of [ShaleStore] implementation, however, will only use [ObjRef] for safeguarded access.
pub struct Obj<T: ?Sized> {
    value: Box<dyn TypedView<T>>,
    dirty: Option<u64>,
}

impl<T: ?Sized> Obj<T> {
    #[inline(always)]
    pub fn as_ptr(&self) -> ObjPtr<T> {
        ObjPtr::<T>::new(self.value.get_offset())
    }
}

impl<T: ?Sized> Obj<T> {
    /// Write to the underlying object. Returns `Some(())` on success.
    #[inline]
    pub fn write(&mut self, modify: impl FnOnce(&mut T)) -> Option<()> {
        modify(self.value.write());
        // if `estimate_mem_image` gives overflow, the object will not be written
        self.dirty = Some(self.value.estimate_mem_image()?);
        Some(())
    }

    #[inline(always)]
    pub fn get_space_id(&self) -> SpaceID {
        self.value.get_mem_store().id()
    }

    #[inline(always)]
    pub fn from_typed_view(value: Box<dyn TypedView<T>>) -> Self {
        Obj { value, dirty: None }
    }

    pub fn flush_dirty(&mut self) {
        if !self.value.is_mem_mapped() {
            if let Some(new_value_len) = self.dirty.take() {
                let mut new_value = vec![0; new_value_len as usize];
                self.value.write_mem_image(&mut new_value);
                self.value
                    .get_mem_store()
                    .write(self.value.get_offset(), &new_value);
            }
        }
    }
}

impl<T: ?Sized> Drop for Obj<T> {
    fn drop(&mut self) {
        self.flush_dirty()
    }
}

impl<T> Deref for Obj<T> {
    type Target = T;
    fn deref(&self) -> &T {
        &self.value
    }
}

/// User handle that offers read & write access to the stored [ShaleStore] item.
pub struct ObjRef<'a, T> {
    inner: Option<Obj<T>>,
    cache: ObjCache<T>,
    _life: PhantomData<&'a mut ()>,
}

impl<'a, T> ObjRef<'a, T> {
    pub fn to_longlive(mut self) -> ObjRef<'static, T> {
        ObjRef {
            inner: self.inner.take(),
            cache: ObjCache(self.cache.0.clone()),
            _life: PhantomData,
        }
    }

    #[inline]
    pub fn write(&mut self, modify: impl FnOnce(&mut T)) -> Option<()> {
        let inner = self.inner.as_mut().unwrap();
        inner.write(modify)?;
        self.cache.get_inner_mut().dirty.insert(inner.as_ptr());
        Some(())
    }
}

impl<'a, T> Deref for ObjRef<'a, T> {
    type Target = Obj<T>;
    fn deref(&self) -> &Obj<T> {
        self.inner.as_ref().unwrap()
    }
}

impl<'a, T> Drop for ObjRef<'a, T> {
    fn drop(&mut self) {
        let mut inner = self.inner.take().unwrap();
        let ptr = inner.as_ptr();
        let cache = self.cache.get_inner_mut();
        if cache.pinned.remove(&ptr).unwrap() {
            inner.dirty = None;
        } else {
            cache.cached.put(ptr, inner);
        }
    }
}

/// A persistent item storage backed by linear logical space. New items can be created and old
/// items could be retrieved or dropped.
pub trait ShaleStore<T> {
    /// Dereference [ObjPtr] to a unique handle that allows direct access to the item in memory.
    fn get_item(&'_ self, ptr: ObjPtr<T>) -> Result<ObjRef<'_, T>, ShaleError>;
    /// Allocate a new item.
    fn put_item(&'_ self, item: T, extra: u64) -> Result<ObjRef<'_, T>, ShaleError>;
    /// Free an item and recycle its space when applicable.
    fn free_item(&mut self, item: ObjPtr<T>) -> Result<(), ShaleError>;
    /// Flush all dirty writes.
    fn flush_dirty(&self) -> Option<()>;
}

/// A stored item type that can be decoded from or encoded to on-disk raw bytes. An efficient
/// implementation could be directly transmuting to/from a POD struct. But sometimes necessary
/// compression/decompression is needed to reduce disk I/O and facilitate faster in-memory access.
pub trait MummyItem {
    fn dehydrated_len(&self) -> u64;
    fn dehydrate(&self, to: &mut [u8]);
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError>
    where
        Self: Sized;
    fn is_mem_mapped(&self) -> bool {
        false
    }
}

pub fn to_dehydrated(item: &dyn MummyItem) -> Vec<u8> {
    let mut buff = vec![0; item.dehydrated_len() as usize];
    item.dehydrate(&mut buff);
    buff
}

/// Reference implementation of [TypedView]. It takes any type that implements [MummyItem] and
/// should be useful for most applications.
pub struct MummyObj<T> {
    decoded: T,
    mem: Box<dyn Deref<Target = dyn MemStore>>,
    offset: u64,
    len_limit: u64,
}

impl<T> Deref for MummyObj<T> {
    type Target = T;
    fn deref(&self) -> &T {
        &self.decoded
    }
}

impl<T: MummyItem> TypedView<T> for MummyObj<T> {
    fn get_offset(&self) -> u64 {
        self.offset
    }

    fn get_mem_store(&self) -> &dyn MemStore {
        &**self.mem
    }

    fn estimate_mem_image(&self) -> Option<u64> {
        let len = self.decoded.dehydrated_len();
        if len > self.len_limit {
            None
        } else {
            Some(len)
        }
    }

    fn write_mem_image(&self, mem_image: &mut [u8]) {
        self.decoded.dehydrate(mem_image)
    }

    fn write(&mut self) -> &mut T {
        &mut self.decoded
    }
    fn is_mem_mapped(&self) -> bool {
        self.decoded.is_mem_mapped()
    }
}

impl<T: MummyItem + 'static> MummyObj<T> {
    #[inline(always)]
    fn new(offset: u64, len_limit: u64, space: &dyn MemStore) -> Result<Self, ShaleError> {
        let decoded = T::hydrate(offset, space)?;
        Ok(Self {
            offset,
            decoded,
            mem: space.get_shared().ok_or(ShaleError::LinearMemStoreError)?,
            len_limit,
        })
    }

    #[inline(always)]
    fn from_hydrated(
        offset: u64,
        len_limit: u64,
        decoded: T,
        space: &dyn MemStore,
    ) -> Result<Self, ShaleError> {
        Ok(Self {
            offset,
            decoded,
            mem: space.get_shared().ok_or(ShaleError::LinearMemStoreError)?,
            len_limit,
        })
    }

    #[inline(always)]
    pub fn ptr_to_obj(
        store: &dyn MemStore,
        ptr: ObjPtr<T>,
        len_limit: u64,
    ) -> Result<Obj<T>, ShaleError> {
        Ok(Obj::from_typed_view(Box::new(Self::new(
            ptr.addr(),
            len_limit,
            store,
        )?)))
    }

    #[inline(always)]
    pub fn item_to_obj(
        store: &dyn MemStore,
        addr: u64,
        len_limit: u64,
        decoded: T,
    ) -> Result<Obj<T>, ShaleError> {
        Ok(Obj::from_typed_view(Box::new(Self::from_hydrated(
            addr, len_limit, decoded, store,
        )?)))
    }
}

impl<T> MummyObj<T> {
    fn new_from_slice(
        offset: u64,
        len_limit: u64,
        decoded: T,
        space: &dyn MemStore,
    ) -> Result<Self, ShaleError> {
        Ok(Self {
            offset,
            decoded,
            mem: space.get_shared().ok_or(ShaleError::LinearMemStoreError)?,
            len_limit,
        })
    }

    pub fn slice<U: MummyItem + 'static>(
        s: &Obj<T>,
        offset: u64,
        length: u64,
        decoded: U,
    ) -> Result<Obj<U>, ShaleError> {
        let addr_ = s.value.get_offset() + offset;
        if s.dirty.is_some() {
            return Err(ShaleError::SliceError);
        }
        let r = Box::new(MummyObj::new_from_slice(
            addr_,
            length,
            decoded,
            s.value.get_mem_store(),
        )?);
        Ok(Obj {
            value: r,
            dirty: None,
        })
    }
}

impl<T> ObjPtr<T> {
    const MSIZE: u64 = 8;
}

impl<T> MummyItem for ObjPtr<T> {
    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        use std::io::{Cursor, Write};
        Cursor::new(to)
            .write_all(&self.addr().to_le_bytes())
            .unwrap();
    }

    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::LinearMemStoreError)?;
        Ok(Self::new_from_addr(u64::from_le_bytes(
            (**raw).try_into().unwrap(),
        )))
    }
}

/// Purely volatile, vector-based implementation for [MemStore]. This is good for testing or trying
/// out stuff (persistent data structures) built on [ShaleStore] in memory, without having to write
/// your own [MemStore] implementation.
pub struct PlainMem {
    space: Rc<UnsafeCell<Vec<u8>>>,
    id: SpaceID,
}

impl PlainMem {
    pub fn new(size: u64, id: SpaceID) -> Self {
        let mut space = Vec::new();
        space.resize(size as usize, 0);
        let space = Rc::new(UnsafeCell::new(space));
        Self { space, id }
    }

    fn get_space_mut(&self) -> &mut Vec<u8> {
        unsafe { &mut *self.space.get() }
    }
}

impl MemStore for PlainMem {
    fn get_view(&self, offset: u64, length: u64) -> Option<Box<dyn MemView>> {
        let offset = offset as usize;
        let length = length as usize;
        if offset + length > self.get_space_mut().len() {
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

    fn get_shared(&self) -> Option<Box<dyn Deref<Target = dyn MemStore>>> {
        Some(Box::new(PlainMemShared(Self {
            space: self.space.clone(),
            id: self.id,
        })))
    }

    fn write(&self, offset: u64, change: &[u8]) {
        let offset = offset as usize;
        let length = change.len();
        self.get_space_mut()[offset..offset + length].copy_from_slice(change)
    }

    fn id(&self) -> SpaceID {
        self.id
    }
}

struct PlainMemView {
    offset: usize,
    length: usize,
    mem: PlainMem,
}

struct PlainMemShared(PlainMem);

impl Deref for PlainMemView {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.mem.get_space_mut()[self.offset..self.offset + self.length]
    }
}

impl Deref for PlainMemShared {
    type Target = dyn MemStore;
    fn deref(&self) -> &(dyn MemStore + 'static) {
        &self.0
    }
}

impl MemView for PlainMemView {}

struct ObjCacheInner<T: ?Sized> {
    cached: lru::LruCache<ObjPtr<T>, Obj<T>>,
    pinned: HashMap<ObjPtr<T>, bool>,
    dirty: HashSet<ObjPtr<T>>,
}

/// [ObjRef] pool that is used by [ShaleStore] implementation to construct [ObjRef]s.
pub struct ObjCache<T: ?Sized>(Rc<UnsafeCell<ObjCacheInner<T>>>);

impl<T> ObjCache<T> {
    pub fn new(capacity: usize) -> Self {
        Self(Rc::new(UnsafeCell::new(ObjCacheInner {
            cached: lru::LruCache::new(NonZeroUsize::new(capacity).expect("non-zero cache size")),
            pinned: HashMap::new(),
            dirty: HashSet::new(),
        })))
    }

    #[inline(always)]
    fn get_inner_mut(&self) -> &mut ObjCacheInner<T> {
        unsafe { &mut *self.0.get() }
    }

    #[inline(always)]
    pub fn get(&'_ self, ptr: ObjPtr<T>) -> Result<Option<ObjRef<'_, T>>, ShaleError> {
        let inner = &mut self.get_inner_mut();
        if let Some(r) = inner.cached.pop(&ptr) {
            if inner.pinned.insert(ptr, false).is_some() {
                return Err(ShaleError::ObjRefAlreadyInUse);
            }
            return Ok(Some(ObjRef {
                inner: Some(r),
                cache: Self(self.0.clone()),
                _life: PhantomData,
            }));
        }
        Ok(None)
    }

    #[inline(always)]
    pub fn put(&'_ self, inner: Obj<T>) -> ObjRef<'_, T> {
        let ptr = inner.as_ptr();
        self.get_inner_mut().pinned.insert(ptr, false);
        ObjRef {
            inner: Some(inner),
            cache: Self(self.0.clone()),
            _life: PhantomData,
        }
    }

    #[inline(always)]
    pub fn pop(&self, ptr: ObjPtr<T>) {
        let inner = self.get_inner_mut();
        if let Some(f) = inner.pinned.get_mut(&ptr) {
            *f = true
        }
        if let Some(mut r) = inner.cached.pop(&ptr) {
            r.dirty = None
        }
        inner.dirty.remove(&ptr);
    }

    pub fn flush_dirty(&self) -> Option<()> {
        let inner = self.get_inner_mut();
        if !inner.pinned.is_empty() {
            return None;
        }
        for ptr in std::mem::take(&mut inner.dirty) {
            if let Some(r) = inner.cached.peek_mut(&ptr) {
                r.flush_dirty()
            }
        }
        Some(())
    }
}
