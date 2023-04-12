// TODO: try to get rid of the use `RefCell` in this file

use std::cell::{RefCell, RefMut};
use std::collections::HashMap;
use std::fmt;
use std::num::NonZeroUsize;
use std::ops::Deref;
use std::rc::Rc;
use std::sync::Arc;

use aiofut::{AIOBuilder, AIOManager};
use growthring::{
    wal::{RecoverPolicy, WALLoader, WALWriter},
    WALStoreAIO,
};
use nix::fcntl::{flock, FlockArg};
use shale::{MemStore, MemView, SpaceID};
use tokio::sync::{mpsc, oneshot, Mutex, Semaphore};
use typed_builder::TypedBuilder;

use crate::file::{Fd, File};

pub(crate) const PAGE_SIZE_NBIT: u64 = 12;
pub(crate) const PAGE_SIZE: u64 = 1 << PAGE_SIZE_NBIT;
pub(crate) const PAGE_MASK: u64 = PAGE_SIZE - 1;

pub trait MemStoreR {
    fn get_slice(&self, offset: u64, length: u64) -> Option<Vec<u8>>;
    fn id(&self) -> SpaceID;
}

type Page = [u8; PAGE_SIZE as usize];

#[derive(Debug)]
pub struct SpaceWrite {
    offset: u64,
    data: Box<[u8]>,
}

#[derive(Debug)]
pub struct Ash {
    pub old: Vec<SpaceWrite>,
    pub new: Vec<Box<[u8]>>,
}

impl Ash {
    fn new() -> Self {
        Self {
            old: Vec::new(),
            new: Vec::new(),
        }
    }
}

#[derive(Debug)]
pub struct AshRecord(pub HashMap<SpaceID, Ash>);

impl growthring::wal::Record for AshRecord {
    fn serialize(&self) -> growthring::wal::WALBytes {
        let mut bytes = Vec::new();
        bytes.extend((self.0.len() as u64).to_le_bytes());
        for (space_id, w) in self.0.iter() {
            bytes.extend((*space_id as u8).to_le_bytes());
            bytes.extend((w.old.len() as u32).to_le_bytes());
            for (sw_old, sw_new) in w.old.iter().zip(w.new.iter()) {
                bytes.extend(sw_old.offset.to_le_bytes());
                bytes.extend((sw_old.data.len() as u64).to_le_bytes());
                bytes.extend(&*sw_old.data);
                bytes.extend(&**sw_new);
            }
        }
        bytes.into()
    }
}

impl AshRecord {
    fn deserialize(raw: growthring::wal::WALBytes) -> Self {
        let mut r = &raw[..];
        let len = u64::from_le_bytes(r[..8].try_into().unwrap());
        r = &r[8..];
        let writes = (0..len)
            .map(|_| {
                let space_id = u8::from_le_bytes(r[..1].try_into().unwrap());
                let wlen = u32::from_le_bytes(r[1..5].try_into().unwrap());
                r = &r[5..];
                let mut old = Vec::new();
                let mut new = Vec::new();
                for _ in 0..wlen {
                    let offset = u64::from_le_bytes(r[..8].try_into().unwrap());
                    let data_len = u64::from_le_bytes(r[8..16].try_into().unwrap());
                    r = &r[16..];
                    let old_write = SpaceWrite {
                        offset,
                        data: r[..data_len as usize].into(),
                    };
                    r = &r[data_len as usize..];
                    let new_data: Box<[u8]> = r[..data_len as usize].into();
                    r = &r[data_len as usize..];
                    old.push(old_write);
                    new.push(new_data);
                }
                (space_id, Ash { old, new })
            })
            .collect();
        Self(writes)
    }
}

/// Basic copy-on-write item in the linear storage space for multi-versioning.
pub struct DeltaPage(u64, Box<Page>);

impl DeltaPage {
    #[inline(always)]
    fn offset(&self) -> u64 {
        self.0 << PAGE_SIZE_NBIT
    }

    #[inline(always)]
    fn data(&self) -> &[u8] {
        self.1.as_ref()
    }

    #[inline(always)]
    fn data_mut(&mut self) -> &mut [u8] {
        self.1.as_mut()
    }
}

#[derive(Default)]
pub struct StoreDelta(Vec<DeltaPage>);

impl fmt::Debug for StoreDelta {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "<StoreDelta>")
    }
}

impl Deref for StoreDelta {
    type Target = [DeltaPage];
    fn deref(&self) -> &[DeltaPage] {
        &self.0
    }
}

impl StoreDelta {
    pub fn new(src: &dyn MemStoreR, writes: &[SpaceWrite]) -> Self {
        let mut deltas = Vec::new();
        let mut widx: Vec<_> = (0..writes.len()).filter(|i| writes[*i].data.len() > 0).collect();
        if widx.is_empty() {
            // the writes are all empty
            return Self(deltas)
        }

        // sort by the starting point
        widx.sort_by_key(|i| writes[*i].offset);

        let mut witer = widx.into_iter();
        let w0 = &writes[witer.next().unwrap()];
        let mut head = w0.offset >> PAGE_SIZE_NBIT;
        let mut tail = (w0.offset + w0.data.len() as u64 - 1) >> PAGE_SIZE_NBIT;

        macro_rules! create_dirty_pages {
            ($l: expr, $r: expr) => {
                for p in $l..=$r {
                    let off = p << PAGE_SIZE_NBIT;
                    deltas.push(DeltaPage(
                        p,
                        Box::new(src.get_slice(off, PAGE_SIZE).unwrap().try_into().unwrap()),
                    ));
                }
            };
        }

        for i in witer {
            let w = &writes[i];
            let ep = (w.offset + w.data.len() as u64 - 1) >> PAGE_SIZE_NBIT;
            let wp = w.offset >> PAGE_SIZE_NBIT;
            if wp > tail {
                // all following writes won't go back past w.offset, so the previous continous
                // write area is determined
                create_dirty_pages!(head, tail);
                head = wp;
            }
            tail = std::cmp::max(tail, ep)
        }
        create_dirty_pages!(head, tail);

        let psize = PAGE_SIZE as usize;
        for w in writes.into_iter() {
            let mut l = 0;
            let mut r = deltas.len();
            while r - l > 1 {
                let mid = (l + r) >> 1;
                (*if w.offset < deltas[mid].offset() {
                    &mut r
                } else {
                    &mut l
                }) = mid;
            }
            let off = (w.offset - deltas[l].offset()) as usize;
            let len = std::cmp::min(psize - off, w.data.len());
            deltas[l].data_mut()[off..off + len].copy_from_slice(&w.data[..len]);
            let mut data = &w.data[len..];
            while data.len() >= psize {
                l += 1;
                deltas[l].data_mut().copy_from_slice(&data[..psize]);
                data = &data[psize..];
            }
            if data.len() > 0 {
                l += 1;
                deltas[l].data_mut()[..data.len()].copy_from_slice(&data);
            }
        }
        Self(deltas)
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }
}

pub struct StoreRev {
    prev: RefCell<Rc<dyn MemStoreR>>,
    delta: StoreDelta,
}

impl fmt::Debug for StoreRev {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "<StoreRev")?;
        for d in self.delta.iter() {
            write!(f, " 0x{:x}", d.0)?;
        }
        write!(f, ">\n")
    }
}

impl MemStoreR for StoreRev {
    fn get_slice(&self, offset: u64, length: u64) -> Option<Vec<u8>> {
        let prev = self.prev.borrow();
        let mut start = offset;
        let end = start + length;
        let delta = &self.delta;
        let mut l = 0;
        let mut r = delta.len();
        // no dirty page, before or after all dirty pages
        if r == 0 {
            return prev.get_slice(start, end - start)
        }
        // otherwise, some dirty pages are covered by the range
        while r - l > 1 {
            let mid = (l + r) >> 1;
            (*if start < delta[mid].offset() { &mut r } else { &mut l }) = mid;
        }
        if start >= delta[l].offset() + PAGE_SIZE {
            l += 1
        }
        if l >= delta.len() || end < delta[l].offset() {
            return prev.get_slice(start, end - start)
        }
        let mut data = Vec::new();
        let p_off = std::cmp::min(end - delta[l].offset(), PAGE_SIZE);
        if start < delta[l].offset() {
            data.extend(prev.get_slice(start, delta[l].offset() - start)?);
            data.extend(&delta[l].data()[..p_off as usize]);
        } else {
            data.extend(&delta[l].data()[(start - delta[l].offset()) as usize..p_off as usize]);
        };
        start = delta[l].offset() + p_off;
        while start < end {
            l += 1;
            if l >= delta.len() || end < delta[l].offset() {
                data.extend(prev.get_slice(start, end - start)?);
                break
            }
            if delta[l].offset() > start {
                data.extend(prev.get_slice(start, delta[l].offset() - start)?);
            }
            if end < delta[l].offset() + PAGE_SIZE {
                data.extend(&delta[l].data()[..(end - delta[l].offset()) as usize]);
                break
            }
            data.extend(delta[l].data());
            start = delta[l].offset() + PAGE_SIZE;
        }
        assert!(data.len() == length as usize);
        Some(data)
    }

    fn id(&self) -> SpaceID {
        self.prev.borrow().id()
    }
}

#[derive(Clone, Debug)]
pub struct StoreRevShared(Rc<StoreRev>);

impl StoreRevShared {
    pub fn from_ash(prev: Rc<dyn MemStoreR>, writes: &[SpaceWrite]) -> Self {
        let delta = StoreDelta::new(prev.as_ref(), writes);
        let prev = RefCell::new(prev);
        Self(Rc::new(StoreRev { prev, delta }))
    }

    pub fn from_delta(prev: Rc<dyn MemStoreR>, delta: StoreDelta) -> Self {
        let prev = RefCell::new(prev);
        Self(Rc::new(StoreRev { prev, delta }))
    }

    pub fn set_prev(&mut self, prev: Rc<dyn MemStoreR>) {
        *self.0.prev.borrow_mut() = prev
    }

    pub fn inner(&self) -> &Rc<StoreRev> {
        &self.0
    }
}

impl MemStore for StoreRevShared {
    fn get_view(&self, offset: u64, length: u64) -> Option<Box<dyn MemView>> {
        let data = self.0.get_slice(offset, length)?;
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Option<Box<dyn Deref<Target = dyn MemStore>>> {
        Some(Box::new(StoreShared(self.clone())))
    }

    fn write(&self, _offset: u64, _change: &[u8]) {
        // StoreRevShared is a read-only view version of MemStore
        // Writes could be induced by lazy hashing and we can just ignore those
    }

    fn id(&self) -> SpaceID {
        <StoreRev as MemStoreR>::id(&self.0)
    }
}

struct StoreRef {
    data: Vec<u8>,
}

impl Deref for StoreRef {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.data
    }
}

impl MemView for StoreRef {}

struct StoreShared<S: Clone + MemStore>(S);

impl<S: Clone + MemStore + 'static> Deref for StoreShared<S> {
    type Target = dyn MemStore;
    fn deref(&self) -> &(dyn MemStore + 'static) {
        &self.0
    }
}

struct StoreRevMutDelta {
    pages: HashMap<u64, Box<Page>>,
    plain: Ash,
}

#[derive(Clone)]
pub struct StoreRevMut {
    prev: Rc<dyn MemStoreR>,
    deltas: Rc<RefCell<StoreRevMutDelta>>,
}

impl StoreRevMut {
    pub fn new(prev: Rc<dyn MemStoreR>) -> Self {
        Self {
            prev,
            deltas: Rc::new(RefCell::new(StoreRevMutDelta {
                pages: HashMap::new(),
                plain: Ash::new(),
            })),
        }
    }

    fn get_page_mut(&self, pid: u64) -> RefMut<[u8]> {
        let mut deltas = self.deltas.borrow_mut();
        if deltas.pages.get(&pid).is_none() {
            let page = Box::new(
                self.prev
                    .get_slice(pid << PAGE_SIZE_NBIT, PAGE_SIZE)
                    .unwrap()
                    .try_into()
                    .unwrap(),
            );
            deltas.pages.insert(pid, page);
        }
        RefMut::map(deltas, |e| &mut e.pages.get_mut(&pid).unwrap()[..])
    }

    pub fn take_delta(&self) -> (StoreDelta, Ash) {
        let mut pages = Vec::new();
        let deltas = std::mem::replace(
            &mut *self.deltas.borrow_mut(),
            StoreRevMutDelta {
                pages: HashMap::new(),
                plain: Ash::new(),
            },
        );
        for (pid, page) in deltas.pages.into_iter() {
            pages.push(DeltaPage(pid, page));
        }
        pages.sort_by_key(|p| p.0);
        (StoreDelta(pages), deltas.plain)
    }
}

impl MemStore for StoreRevMut {
    fn get_view(&self, offset: u64, length: u64) -> Option<Box<dyn MemView>> {
        let data = if length == 0 {
            Vec::new()
        } else {
            let end = offset + length - 1;
            let s_pid = offset >> PAGE_SIZE_NBIT;
            let s_off = (offset & PAGE_MASK) as usize;
            let e_pid = end >> PAGE_SIZE_NBIT;
            let e_off = (end & PAGE_MASK) as usize;
            let deltas = &self.deltas.borrow().pages;
            if s_pid == e_pid {
                match deltas.get(&s_pid) {
                    Some(p) => p[s_off..e_off + 1].to_vec(),
                    None => self.prev.get_slice(offset, length)?,
                }
            } else {
                let mut data = match deltas.get(&s_pid) {
                    Some(p) => p[s_off..].to_vec(),
                    None => self.prev.get_slice(offset, PAGE_SIZE - s_off as u64)?,
                };
                for p in s_pid + 1..e_pid {
                    match deltas.get(&p) {
                        Some(p) => data.extend(&**p),
                        None => data.extend(&self.prev.get_slice(p << PAGE_SIZE_NBIT, PAGE_SIZE)?),
                    };
                }
                match deltas.get(&e_pid) {
                    Some(p) => data.extend(&p[..e_off + 1]),
                    None => data.extend(self.prev.get_slice(e_pid << PAGE_SIZE_NBIT, e_off as u64 + 1)?),
                }
                data
            }
        };
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Option<Box<dyn Deref<Target = dyn MemStore>>> {
        Some(Box::new(StoreShared(self.clone())))
    }

    fn write(&self, offset: u64, mut change: &[u8]) {
        let length = change.len() as u64;
        let end = offset + length - 1;
        let s_pid = offset >> PAGE_SIZE_NBIT;
        let s_off = (offset & PAGE_MASK) as usize;
        let e_pid = end >> PAGE_SIZE_NBIT;
        let e_off = (end & PAGE_MASK) as usize;
        let mut old: Vec<u8> = Vec::new();
        let new: Box<[u8]> = change.into();
        if s_pid == e_pid {
            let slice = &mut self.get_page_mut(s_pid)[s_off..e_off + 1];
            old.extend(&*slice);
            slice.copy_from_slice(change)
        } else {
            let len = PAGE_SIZE as usize - s_off;
            {
                let slice = &mut self.get_page_mut(s_pid)[s_off..];
                old.extend(&*slice);
                slice.copy_from_slice(&change[..len]);
            }
            change = &change[len..];
            for p in s_pid + 1..e_pid {
                let mut slice = self.get_page_mut(p);
                old.extend(&*slice);
                slice.copy_from_slice(&change[..PAGE_SIZE as usize]);
                change = &change[PAGE_SIZE as usize..];
            }
            let slice = &mut self.get_page_mut(e_pid)[..e_off + 1];
            old.extend(&*slice);
            slice.copy_from_slice(change);
        }
        let plain = &mut self.deltas.borrow_mut().plain;
        assert!(old.len() == new.len());
        plain.old.push(SpaceWrite {
            offset,
            data: old.into(),
        });
        plain.new.push(new);
    }

    fn id(&self) -> SpaceID {
        self.prev.id()
    }
}

#[cfg(test)]
#[derive(Clone)]
pub struct ZeroStore(Rc<()>);

#[cfg(test)]
impl ZeroStore {
    pub fn new() -> Self {
        Self(Rc::new(()))
    }
}

#[cfg(test)]
impl MemStoreR for ZeroStore {
    fn get_slice(&self, _: u64, length: u64) -> Option<Vec<u8>> {
        Some(vec![0; length as usize])
    }

    fn id(&self) -> SpaceID {
        shale::INVALID_SPACE_ID
    }
}

#[test]
fn test_from_ash() {
    use rand::{rngs::StdRng, Rng, SeedableRng};
    let mut rng = StdRng::seed_from_u64(42);
    let min = rng.gen_range(0..2 * PAGE_SIZE);
    let max = rng.gen_range(min + 1 * PAGE_SIZE..min + 100 * PAGE_SIZE);
    for _ in 0..2000 {
        let n = 20;
        let mut canvas = Vec::new();
        canvas.resize((max - min) as usize, 0);
        let mut writes: Vec<_> = Vec::new();
        for _ in 0..n {
            let l = rng.gen_range(min..max);
            let r = rng.gen_range(l + 1..std::cmp::min(l + 3 * PAGE_SIZE, max));
            let data: Box<[u8]> = (l..r).map(|_| rng.gen()).collect();
            for (idx, byte) in (l..r).zip(data.iter()) {
                canvas[(idx - min) as usize] = *byte;
            }
            println!("[0x{:x}, 0x{:x})", l, r);
            writes.push(SpaceWrite { offset: l, data });
        }
        let z = Rc::new(ZeroStore::new());
        let rev = StoreRevShared::from_ash(z, &writes);
        println!("{:?}", rev);
        assert_eq!(&**rev.get_view(min, max - min).unwrap(), &canvas);
        for _ in 0..2 * n {
            let l = rng.gen_range(min..max);
            let r = rng.gen_range(l + 1..max);
            assert_eq!(
                &**rev.get_view(l, r - l).unwrap(),
                &canvas[(l - min) as usize..(r - min) as usize]
            );
        }
    }
}

#[derive(TypedBuilder)]
pub struct StoreConfig {
    ncached_pages: usize,
    ncached_files: usize,
    #[builder(default = 22)] // 4MB file by default
    file_nbit: u64,
    space_id: SpaceID,
    rootfd: Fd,
}

struct CachedSpaceInner {
    cached_pages: lru::LruCache<u64, Box<Page>>,
    pinned_pages: HashMap<u64, (usize, Box<Page>)>,
    files: Arc<FilePool>,
    disk_buffer: DiskBufferRequester,
}

#[derive(Clone)]
pub struct CachedSpace {
    inner: Rc<RefCell<CachedSpaceInner>>,
    space_id: SpaceID,
}

impl CachedSpace {
    pub fn new(cfg: &StoreConfig) -> Result<Self, StoreError> {
        let space_id = cfg.space_id;
        let files = Arc::new(FilePool::new(cfg)?);
        Ok(Self {
            inner: Rc::new(RefCell::new(CachedSpaceInner {
                cached_pages: lru::LruCache::new(NonZeroUsize::new(cfg.ncached_pages).expect("non-zero cache size")),
                pinned_pages: HashMap::new(),
                files,
                disk_buffer: DiskBufferRequester::default(),
            })),
            space_id,
        })
    }

    /// Apply `delta` to the store and return the StoreDelta that can undo this change.
    pub fn update(&self, delta: &StoreDelta) -> Option<StoreDelta> {
        let mut pages = Vec::new();
        for DeltaPage(pid, page) in &delta.0 {
            let data = self.inner.borrow_mut().pin_page(self.space_id, *pid).ok()?;
            // save the original data
            pages.push(DeltaPage(*pid, Box::new(data.try_into().unwrap())));
            // apply the change
            data.copy_from_slice(page.as_ref());
        }
        Some(StoreDelta(pages))
    }
}

impl CachedSpaceInner {
    fn fetch_page(&mut self, space_id: SpaceID, pid: u64) -> Result<Box<Page>, StoreError> {
        if let Some(p) = self.disk_buffer.get_page(space_id, pid) {
            return Ok(Box::new((*p).clone()))
        }
        let file_nbit = self.files.get_file_nbit();
        let file_size = 1 << file_nbit;
        let poff = pid << PAGE_SIZE_NBIT;
        let file = self.files.get_file(poff >> file_nbit)?;
        let mut page: Page = [0; PAGE_SIZE as usize];
        nix::sys::uio::pread(file.get_fd(), &mut page, (poff & (file_size - 1)) as nix::libc::off_t)
            .map_err(StoreError::System)?;
        Ok(Box::new(page))
    }

    fn pin_page(&mut self, space_id: SpaceID, pid: u64) -> Result<&'static mut [u8], StoreError> {
        let base = match self.pinned_pages.get_mut(&pid) {
            Some(mut e) => {
                e.0 += 1;
                e.1.as_mut_ptr()
            }
            None => {
                let mut page = match self.cached_pages.pop(&pid) {
                    Some(p) => p,
                    None => self.fetch_page(space_id, pid)?,
                };
                let ptr = page.as_mut_ptr();
                self.pinned_pages.insert(pid, (1, page));
                ptr
            }
        };
        Ok(unsafe { std::slice::from_raw_parts_mut(base, PAGE_SIZE as usize) })
    }

    fn unpin_page(&mut self, pid: u64) {
        use std::collections::hash_map::Entry::*;
        let page = match self.pinned_pages.entry(pid) {
            Occupied(mut e) => {
                let cnt = &mut e.get_mut().0;
                assert!(*cnt > 0);
                *cnt -= 1;
                if *cnt == 0 {
                    e.remove().1
                } else {
                    return
                }
            }
            _ => unreachable!(),
        };
        self.cached_pages.put(pid, page);
    }
}

struct PageRef {
    pid: u64,
    data: &'static mut [u8],
    store: CachedSpace,
}

impl<'a> std::ops::Deref for PageRef {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        self.data
    }
}

impl<'a> std::ops::DerefMut for PageRef {
    fn deref_mut(&mut self) -> &mut [u8] {
        self.data
    }
}

impl PageRef {
    fn new(pid: u64, store: &CachedSpace) -> Option<Self> {
        Some(Self {
            pid,
            data: store.inner.borrow_mut().pin_page(store.space_id, pid).ok()?,
            store: store.clone(),
        })
    }
}

impl Drop for PageRef {
    fn drop(&mut self) {
        self.store.inner.borrow_mut().unpin_page(self.pid);
    }
}

impl MemStoreR for CachedSpace {
    fn get_slice(&self, offset: u64, length: u64) -> Option<Vec<u8>> {
        if length == 0 {
            return Some(Default::default())
        }
        let end = offset + length - 1;
        let s_pid = offset >> PAGE_SIZE_NBIT;
        let s_off = (offset & PAGE_MASK) as usize;
        let e_pid = end >> PAGE_SIZE_NBIT;
        let e_off = (end & PAGE_MASK) as usize;
        if s_pid == e_pid {
            return PageRef::new(s_pid, self).map(|e| e[s_off..e_off + 1].to_vec())
        }
        let mut data: Vec<u8> = Vec::new();
        {
            data.extend(&PageRef::new(s_pid, self)?[s_off..]);
            for p in s_pid + 1..e_pid {
                data.extend(&PageRef::new(p, self)?[..]);
            }
            data.extend(&PageRef::new(e_pid, self)?[..e_off + 1]);
        }
        Some(data)
    }

    fn id(&self) -> SpaceID {
        self.space_id
    }
}

pub struct FilePool {
    files: parking_lot::Mutex<lru::LruCache<u64, Arc<File>>>,
    file_nbit: u64,
    rootfd: Fd,
}

impl FilePool {
    fn new(cfg: &StoreConfig) -> Result<Self, StoreError> {
        let rootfd = cfg.rootfd;
        let file_nbit = cfg.file_nbit;
        let s = Self {
            files: parking_lot::Mutex::new(lru::LruCache::new(
                NonZeroUsize::new(cfg.ncached_files).expect("non-zero file num"),
            )),
            file_nbit,
            rootfd,
        };
        let f0 = s.get_file(0)?;
        if let Err(_) = flock(f0.get_fd(), FlockArg::LockExclusiveNonblock) {
            return Err(StoreError::InitError("the store is busy".into()))
        }
        Ok(s)
    }

    fn get_file(&self, fid: u64) -> Result<Arc<File>, StoreError> {
        let mut files = self.files.lock();
        let file_size = 1 << self.file_nbit;
        Ok(match files.get(&fid) {
            Some(f) => f.clone(),
            None => {
                files.put(
                    fid,
                    Arc::new(File::new(fid, file_size, self.rootfd).map_err(StoreError::System)?),
                );
                files.peek(&fid).unwrap().clone()
            }
        })
    }

    fn get_file_nbit(&self) -> u64 {
        self.file_nbit
    }
}

impl Drop for FilePool {
    fn drop(&mut self) {
        let f0 = self.get_file(0).unwrap();
        flock(f0.get_fd(), FlockArg::UnlockNonblock).ok();
        nix::unistd::close(self.rootfd).ok();
    }
}

#[derive(Debug)]
pub struct BufferWrite {
    pub space_id: SpaceID,
    pub delta: StoreDelta,
}

pub enum BufferCmd {
    InitWAL(Fd, String),
    WriteBatch(Vec<BufferWrite>, AshRecord),
    GetPage((SpaceID, u64), oneshot::Sender<Option<Arc<Page>>>),
    CollectAsh(usize, oneshot::Sender<Vec<AshRecord>>),
    RegCachedSpace(SpaceID, Arc<FilePool>),
    Shutdown,
}

#[derive(TypedBuilder, Clone)]
pub struct WALConfig {
    #[builder(default = 22)] // 4MB WAL logs
    pub(crate) file_nbit: u64,
    #[builder(default = 15)] // 32KB
    pub(crate) block_nbit: u64,
    #[builder(default = 100)] // preserve a rolling window of 100 past commits
    pub(crate) max_revisions: u32,
}

/// Config for the disk buffer.
#[derive(TypedBuilder, Clone)]
pub struct DiskBufferConfig {
    /// Maximum buffered disk buffer commands.
    #[builder(default = 4096)]
    pub(crate) max_buffered: usize,
    /// Maximum number of pending pages.
    #[builder(default = 65536)] // 256MB total size by default
    max_pending: usize,
    /// Maximum number of concurrent async I/O requests.
    #[builder(default = 1024)]
    max_aio_requests: u32,
    /// Maximum number of async I/O responses that it polls for at a time.
    #[builder(default = 128)]
    max_aio_response: u16,
    /// Maximum number of async I/O requests per submission.
    #[builder(default = 128)]
    max_aio_submit: usize,
    /// Maximum number of concurrent async I/O requests in WAL.
    #[builder(default = 256)]
    wal_max_aio_requests: usize,
    /// Maximum buffered WAL records.
    #[builder(default = 1024)]
    wal_max_buffered: usize,
    /// Maximum batched WAL records per write.
    #[builder(default = 4096)]
    wal_max_batch: usize,
}

struct PendingPage {
    staging_data: Arc<Page>,
    file_nbit: u64,
    staging_notifiers: Vec<Rc<Semaphore>>,
    writing_notifiers: Vec<Rc<Semaphore>>,
}

pub struct DiskBuffer {
    pending: HashMap<(SpaceID, u64), PendingPage>,
    inbound: mpsc::Receiver<BufferCmd>,
    fc_notifier: Option<oneshot::Sender<()>>,
    fc_blocker: Option<oneshot::Receiver<()>>,
    file_pools: [Option<Arc<FilePool>>; 255],
    aiomgr: AIOManager,
    local_pool: Rc<tokio::task::LocalSet>,
    task_id: u64,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    wal: Option<Rc<Mutex<WALWriter<WALStoreAIO>>>>,
    cfg: DiskBufferConfig,
    wal_cfg: WALConfig,
}

impl DiskBuffer {
    pub fn new(inbound: mpsc::Receiver<BufferCmd>, cfg: &DiskBufferConfig, wal: &WALConfig) -> Option<Self> {
        const INIT: Option<Arc<FilePool>> = None;
        let aiomgr = AIOBuilder::default()
            .max_events(cfg.max_aio_requests)
            .max_nwait(cfg.max_aio_response)
            .max_nbatched(cfg.max_aio_submit)
            .build()
            .ok()?;

        Some(Self {
            pending: HashMap::new(),
            cfg: cfg.clone(),
            inbound,
            fc_notifier: None,
            fc_blocker: None,
            file_pools: [INIT; 255],
            aiomgr,
            local_pool: Rc::new(tokio::task::LocalSet::new()),
            task_id: 0,
            tasks: Rc::new(RefCell::new(HashMap::new())),
            wal: None,
            wal_cfg: wal.clone(),
        })
    }

    unsafe fn get_longlive_self(&mut self) -> &'static mut Self {
        std::mem::transmute::<&mut Self, &'static mut Self>(self)
    }

    fn schedule_write(&mut self, page_key: (SpaceID, u64)) {
        let p = self.pending.get(&page_key).unwrap();
        let offset = page_key.1 << PAGE_SIZE_NBIT;
        let fid = offset >> p.file_nbit;
        let fmask = (1 << p.file_nbit) - 1;
        let file = self.file_pools[page_key.0 as usize]
            .as_ref()
            .unwrap()
            .get_file(fid)
            .unwrap();
        let fut = self
            .aiomgr
            .write(file.get_fd(), offset & fmask, Box::new(*p.staging_data), None);
        let s = unsafe { self.get_longlive_self() };
        self.start_task(async move {
            let (res, _) = fut.await;
            res.unwrap();
            s.finish_write(page_key);
        });
    }

    fn finish_write(&mut self, page_key: (SpaceID, u64)) {
        use std::collections::hash_map::Entry::*;
        match self.pending.entry(page_key) {
            Occupied(mut e) => {
                let slot = e.get_mut();
                for notifier in std::mem::replace(&mut slot.writing_notifiers, Vec::new()) {
                    notifier.add_permits(1)
                }
                if slot.staging_notifiers.is_empty() {
                    e.remove();
                    if self.pending.len() < self.cfg.max_pending {
                        if let Some(notifier) = self.fc_notifier.take() {
                            notifier.send(()).unwrap();
                        }
                    }
                } else {
                    assert!(slot.writing_notifiers.is_empty());
                    std::mem::swap(&mut slot.writing_notifiers, &mut slot.staging_notifiers);
                    // write again
                    self.schedule_write(page_key);
                }
            }
            _ => unreachable!(),
        }
    }

    async fn init_wal(&mut self, rootfd: Fd, waldir: String) -> Result<(), ()> {
        let mut aiobuilder = AIOBuilder::default();
        aiobuilder.max_events(self.cfg.wal_max_aio_requests as u32);
        let aiomgr = aiobuilder.build().map_err(|_| ())?;
        let store = WALStoreAIO::new(&waldir, false, Some(rootfd), Some(aiomgr)).map_err(|_| ())?;
        let mut loader = WALLoader::new();
        loader
            .file_nbit(self.wal_cfg.file_nbit)
            .block_nbit(self.wal_cfg.block_nbit)
            .recover_policy(RecoverPolicy::Strict);
        if self.wal.is_some() {
            // already initialized
            return Ok(())
        }
        let wal = loader
            .load(
                store,
                |raw, _| {
                    let batch = AshRecord::deserialize(raw);
                    for (space_id, Ash { old, new }) in batch.0 {
                        for (old, data) in old.into_iter().zip(new.into_iter()) {
                            let offset = old.offset;
                            let file_pool = self.file_pools[space_id as usize].as_ref().unwrap();
                            let file_nbit = file_pool.get_file_nbit();
                            let file_mask = (1 << file_nbit) - 1;
                            let fid = offset >> file_nbit;
                            nix::sys::uio::pwrite(
                                file_pool.get_file(fid).map_err(|_| ())?.get_fd(),
                                &*data,
                                (offset & file_mask) as nix::libc::off_t,
                            )
                            .map_err(|_| ())?;
                        }
                    }
                    Ok(())
                },
                self.wal_cfg.max_revisions,
            )
            .await?;
        self.wal = Some(Rc::new(Mutex::new(wal)));
        Ok(())
    }

    async fn run_wal_queue(&mut self, mut writes: mpsc::Receiver<(Vec<BufferWrite>, AshRecord)>) {
        use std::collections::hash_map::Entry::*;
        loop {
            let mut bwrites = Vec::new();
            let mut records = Vec::new();

            if let Some((bw, ac)) = writes.recv().await {
                records.push(ac);
                bwrites.extend(bw);
            } else {
                break
            }
            while let Ok((bw, ac)) = writes.try_recv() {
                records.push(ac);
                bwrites.extend(bw);
                if records.len() >= self.cfg.wal_max_batch {
                    break
                }
            }
            // first write to WAL
            let ring_ids: Vec<_> = futures::future::join_all(self.wal.as_ref().unwrap().lock().await.grow(records))
                .await
                .into_iter()
                .map(|ring| ring.map_err(|_| "WAL Error while writing").unwrap().1)
                .collect();
            let sem = Rc::new(tokio::sync::Semaphore::new(0));
            let mut npermit = 0;
            for BufferWrite { space_id, delta } in bwrites {
                for w in delta.0 {
                    let page_key = (space_id, w.0);
                    match self.pending.entry(page_key) {
                        Occupied(mut e) => {
                            let e = e.get_mut();
                            e.staging_data = w.1.into();
                            e.staging_notifiers.push(sem.clone());
                            npermit += 1;
                        }
                        Vacant(e) => {
                            let file_nbit = self.file_pools[page_key.0 as usize].as_ref().unwrap().file_nbit;
                            e.insert(PendingPage {
                                staging_data: w.1.into(),
                                file_nbit,
                                staging_notifiers: Vec::new(),
                                writing_notifiers: vec![sem.clone()],
                            });
                            npermit += 1;
                            self.schedule_write(page_key);
                        }
                    }
                }
            }
            let wal = self.wal.as_ref().unwrap().clone();
            let max_revisions = self.wal_cfg.max_revisions;
            self.start_task(async move {
                let _ = sem.acquire_many(npermit).await.unwrap();
                wal.lock()
                    .await
                    .peel(ring_ids, max_revisions)
                    .await
                    .map_err(|_| "WAL errore while pruning")
                    .unwrap();
            });
            if self.pending.len() >= self.cfg.max_pending {
                let (tx, rx) = oneshot::channel();
                self.fc_notifier = Some(tx);
                self.fc_blocker = Some(rx);
            }
        }
    }

    async fn process(&mut self, req: BufferCmd, wal_in: &mpsc::Sender<(Vec<BufferWrite>, AshRecord)>) -> bool {
        match req {
            BufferCmd::Shutdown => return false,
            BufferCmd::InitWAL(rootfd, waldir) => {
                if let Err(_) = self.init_wal(rootfd, waldir).await {
                    panic!("cannot initialize from WAL")
                }
            }
            BufferCmd::GetPage(page_key, tx) => tx
                .send(self.pending.get(&page_key).map(|e| e.staging_data.clone()))
                .unwrap(),
            BufferCmd::WriteBatch(writes, wal_writes) => {
                wal_in.send((writes, wal_writes)).await.unwrap();
            }
            BufferCmd::CollectAsh(nrecords, tx) => {
                // wait to ensure writes are paused for WAL
                let ash = self
                    .wal
                    .as_ref()
                    .unwrap()
                    .clone()
                    .lock()
                    .await
                    .read_recent_records(nrecords, &RecoverPolicy::Strict)
                    .await
                    .unwrap()
                    .into_iter()
                    .map(AshRecord::deserialize)
                    .collect();
                tx.send(ash).unwrap();
            }
            BufferCmd::RegCachedSpace(space_id, files) => self.file_pools[space_id as usize] = Some(files),
        }
        true
    }

    fn start_task<F: std::future::Future<Output = ()> + 'static>(&mut self, fut: F) {
        let task_id = self.task_id;
        self.task_id += 1;
        let tasks = self.tasks.clone();
        self.tasks.borrow_mut().insert(
            task_id,
            Some(self.local_pool.spawn_local(async move {
                fut.await;
                tasks.borrow_mut().remove(&task_id);
            })),
        );
    }

    #[tokio::main(flavor = "current_thread")]
    pub async fn run(mut self) {
        let wal_in = {
            let (tx, rx) = mpsc::channel(self.cfg.wal_max_buffered);
            let s = unsafe { self.get_longlive_self() };
            self.start_task(s.run_wal_queue(rx));
            tx
        };
        self.local_pool
            .clone()
            .run_until(async {
                loop {
                    if let Some(fc) = self.fc_blocker.take() {
                        // flow control, wait until ready
                        fc.await.unwrap();
                    }
                    let req = self.inbound.recv().await.unwrap();
                    if !self.process(req, &wal_in).await {
                        break
                    }
                }
                drop(wal_in);
                let handles: Vec<_> = self
                    .tasks
                    .borrow_mut()
                    .iter_mut()
                    .map(|(_, task)| task.take().unwrap())
                    .collect();
                for h in handles {
                    h.await.unwrap();
                }
            })
            .await;
    }
}

#[derive(Clone)]
pub struct DiskBufferRequester {
    sender: mpsc::Sender<BufferCmd>,
}

impl Default for DiskBufferRequester {
    fn default() -> Self {
        Self {
            sender: mpsc::channel(1).0,
        }
    }
}

impl DiskBufferRequester {
    pub fn new(sender: mpsc::Sender<BufferCmd>) -> Self {
        Self { sender }
    }

    pub fn get_page(&self, space_id: SpaceID, pid: u64) -> Option<Arc<Page>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .blocking_send(BufferCmd::GetPage((space_id, pid), resp_tx))
            .ok()
            .unwrap();
        resp_rx.blocking_recv().unwrap()
    }

    pub fn write(&self, page_batch: Vec<BufferWrite>, write_batch: AshRecord) {
        self.sender
            .blocking_send(BufferCmd::WriteBatch(page_batch, write_batch))
            .ok()
            .unwrap()
    }

    pub fn shutdown(&self) {
        self.sender.blocking_send(BufferCmd::Shutdown).ok().unwrap()
    }

    pub fn init_wal(&self, waldir: &str, rootfd: Fd) {
        self.sender
            .blocking_send(BufferCmd::InitWAL(rootfd, waldir.to_string()))
            .ok()
            .unwrap()
    }

    pub fn collect_ash(&self, nrecords: usize) -> Vec<AshRecord> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .blocking_send(BufferCmd::CollectAsh(nrecords, resp_tx))
            .ok()
            .unwrap();
        resp_rx.blocking_recv().unwrap()
    }

    pub fn reg_cached_space(&self, space: &CachedSpace) {
        let mut inner = space.inner.borrow_mut();
        inner.disk_buffer = self.clone();
        self.sender
            .blocking_send(BufferCmd::RegCachedSpace(space.id(), inner.files.clone()))
            .ok()
            .unwrap()
    }
}

#[derive(Debug, PartialEq)]
pub enum StoreError {
    System(nix::Error),
    InitError(String),
    // TODO: more error report from the DiskBuffer
    //WriterError,
}

impl From<nix::Error> for StoreError {
    fn from(e: nix::Error) -> Self {
        StoreError::System(e)
    }
}
