// TODO: try to get rid of the use `RefCell` in this file
pub mod buffer;

use std::cell::{RefCell, RefMut};
use std::collections::HashMap;
use std::fmt::{self, Debug};
use std::io;
use std::num::NonZeroUsize;
use std::ops::{Deref, DerefMut};
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;

use shale::{CachedStore, CachedView, SpaceID};

use nix::fcntl::{flock, FlockArg};
use thiserror::Error;
use tokio::sync::mpsc::error::SendError;
use tokio::sync::oneshot::error::RecvError;
use typed_builder::TypedBuilder;

use crate::file::File;

use self::buffer::DiskBufferRequester;

pub(crate) const PAGE_SIZE_NBIT: u64 = 12;
pub(crate) const PAGE_SIZE: u64 = 1 << PAGE_SIZE_NBIT;
pub(crate) const PAGE_MASK: u64 = PAGE_SIZE - 1;

#[derive(Debug, Error)]
pub enum StoreError<T> {
    #[error("system error: `{0}`")]
    System(#[from] nix::Error),
    #[error("io error: `{0}`")]
    Io(Box<std::io::Error>),
    #[error("init error: `{0}`")]
    Init(String),
    // TODO: more error report from the DiskBuffer
    //WriterError,
    #[error("error sending data: `{0}`")]
    Send(#[from] SendError<T>),
    #[error("error receiving data: `{0}")]
    Receive(#[from] RecvError),
}

impl<T> From<std::io::Error> for StoreError<T> {
    fn from(e: std::io::Error) -> Self {
        StoreError::Io(Box::new(e))
    }
}

pub trait MemStoreR: Debug {
    /// Returns a slice of bytes from memory.
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
            bytes.extend((*space_id).to_le_bytes());
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
    #[allow(clippy::boxed_local)]
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
        let mut widx: Vec<_> = (0..writes.len())
            .filter(|i| writes[*i].data.len() > 0)
            .collect();
        if widx.is_empty() {
            // the writes are all empty
            return Self(deltas);
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
        for w in writes.iter() {
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
            if !data.is_empty() {
                l += 1;
                deltas[l].data_mut()[..data.len()].copy_from_slice(data);
            }
        }
        Self(deltas)
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
        writeln!(f, ">")
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
            return prev.get_slice(start, end - start);
        }
        // otherwise, some dirty pages are covered by the range
        while r - l > 1 {
            let mid = (l + r) >> 1;
            (*if start < delta[mid].offset() {
                &mut r
            } else {
                &mut l
            }) = mid;
        }
        if start >= delta[l].offset() + PAGE_SIZE {
            l += 1
        }
        if l >= delta.len() || end < delta[l].offset() {
            return prev.get_slice(start, end - start);
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
                break;
            }
            if delta[l].offset() > start {
                data.extend(prev.get_slice(start, delta[l].offset() - start)?);
            }
            if end < delta[l].offset() + PAGE_SIZE {
                data.extend(&delta[l].data()[..(end - delta[l].offset()) as usize]);
                break;
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

impl CachedStore for StoreRevShared {
    fn get_view(
        &self,
        offset: u64,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let data = self.0.get_slice(offset, length)?;
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Box<dyn DerefMut<Target = dyn CachedStore>> {
        Box::new(StoreShared(self.clone()))
    }

    fn write(&mut self, _offset: u64, _change: &[u8]) {
        // StoreRevShared is a read-only view version of CachedStore
        // Writes could be induced by lazy hashing and we can just ignore those
    }

    fn id(&self) -> SpaceID {
        <StoreRev as MemStoreR>::id(&self.0)
    }
}

#[derive(Debug)]
struct StoreRef {
    data: Vec<u8>,
}

impl Deref for StoreRef {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.data
    }
}

impl CachedView for StoreRef {
    type DerefReturn = Vec<u8>;

    fn as_deref(&self) -> Self::DerefReturn {
        self.deref().to_vec()
    }
}

struct StoreShared<S: Clone + CachedStore>(S);

impl<S: Clone + CachedStore + 'static> Deref for StoreShared<S> {
    type Target = dyn CachedStore;
    fn deref(&self) -> &(dyn CachedStore + 'static) {
        &self.0
    }
}

impl<S: Clone + CachedStore + 'static> DerefMut for StoreShared<S> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

#[derive(Debug)]
struct StoreRevMutDelta {
    pages: HashMap<u64, Box<Page>>,
    plain: Ash,
}

#[derive(Clone, Debug)]
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

impl CachedStore for StoreRevMut {
    fn get_view(
        &self,
        offset: u64,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
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
                        Some(p) => data.extend(**p),
                        None => data.extend(&self.prev.get_slice(p << PAGE_SIZE_NBIT, PAGE_SIZE)?),
                    };
                }
                match deltas.get(&e_pid) {
                    Some(p) => data.extend(&p[..e_off + 1]),
                    None => data.extend(
                        self.prev
                            .get_slice(e_pid << PAGE_SIZE_NBIT, e_off as u64 + 1)?,
                    ),
                }
                data
            }
        };
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Box<dyn DerefMut<Target = dyn CachedStore>> {
        Box::new(StoreShared(self.clone()))
    }

    fn write(&mut self, offset: u64, mut change: &[u8]) {
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
#[derive(Clone, Debug)]
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
    let max = rng.gen_range(min + PAGE_SIZE..min + 100 * PAGE_SIZE);
    for _ in 0..20 {
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
            println!("[0x{l:x}, 0x{r:x})");
            writes.push(SpaceWrite { offset: l, data });
        }
        let z = Rc::new(ZeroStore::new());
        let rev = StoreRevShared::from_ash(z, &writes);
        println!("{rev:?}");
        assert_eq!(
            rev.get_view(min, max - min).as_deref().unwrap().as_deref(),
            canvas
        );
        for _ in 0..2 * n {
            let l = rng.gen_range(min..max);
            let r = rng.gen_range(l + 1..max);
            assert_eq!(
                rev.get_view(l, r - l).as_deref().unwrap().as_deref(),
                canvas[(l - min) as usize..(r - min) as usize]
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
    rootdir: PathBuf,
}

#[derive(Debug)]
struct CachedSpaceInner {
    cached_pages: lru::LruCache<u64, Box<Page>>,
    pinned_pages: HashMap<u64, (usize, Box<Page>)>,
    files: Arc<FilePool>,
    disk_buffer: DiskBufferRequester,
}

#[derive(Clone, Debug)]
pub struct CachedSpace {
    inner: Rc<RefCell<CachedSpaceInner>>,
    space_id: SpaceID,
}

impl CachedSpace {
    pub fn new(cfg: &StoreConfig) -> Result<Self, StoreError<std::io::Error>> {
        let space_id = cfg.space_id;
        let files = Arc::new(FilePool::new(cfg)?);
        Ok(Self {
            inner: Rc::new(RefCell::new(CachedSpaceInner {
                cached_pages: lru::LruCache::new(
                    NonZeroUsize::new(cfg.ncached_pages).expect("non-zero cache size"),
                ),
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
    fn fetch_page(
        &mut self,
        space_id: SpaceID,
        pid: u64,
    ) -> Result<Box<Page>, StoreError<io::Error>> {
        if let Some(p) = self.disk_buffer.get_page(space_id, pid) {
            return Ok(Box::new(*p));
        }
        let file_nbit = self.files.get_file_nbit();
        let file_size = 1 << file_nbit;
        let poff = pid << PAGE_SIZE_NBIT;
        let file = self.files.get_file(poff >> file_nbit)?;
        let mut page: Page = [0; PAGE_SIZE as usize];
        nix::sys::uio::pread(
            file.get_fd(),
            &mut page,
            (poff & (file_size - 1)) as nix::libc::off_t,
        )
        .map_err(StoreError::System)?;
        Ok(Box::new(page))
    }

    fn pin_page(
        &mut self,
        space_id: SpaceID,
        pid: u64,
    ) -> Result<&'static mut [u8], StoreError<std::io::Error>> {
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
                    return;
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

impl std::ops::Deref for PageRef {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        self.data
    }
}

impl std::ops::DerefMut for PageRef {
    fn deref_mut(&mut self) -> &mut [u8] {
        self.data
    }
}

impl PageRef {
    fn new(pid: u64, store: &CachedSpace) -> Option<Self> {
        Some(Self {
            pid,
            data: store
                .inner
                .borrow_mut()
                .pin_page(store.space_id, pid)
                .ok()?,
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
            return Some(Default::default());
        }
        let end = offset + length - 1;
        let s_pid = offset >> PAGE_SIZE_NBIT;
        let s_off = (offset & PAGE_MASK) as usize;
        let e_pid = end >> PAGE_SIZE_NBIT;
        let e_off = (end & PAGE_MASK) as usize;
        if s_pid == e_pid {
            return PageRef::new(s_pid, self).map(|e| e[s_off..e_off + 1].to_vec());
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

#[derive(Debug)]
pub struct FilePool {
    files: parking_lot::Mutex<lru::LruCache<u64, Arc<File>>>,
    file_nbit: u64,
    rootdir: PathBuf,
}

impl FilePool {
    fn new(cfg: &StoreConfig) -> Result<Self, StoreError<std::io::Error>> {
        let rootdir = &cfg.rootdir;
        let file_nbit = cfg.file_nbit;
        let s = Self {
            files: parking_lot::Mutex::new(lru::LruCache::new(
                NonZeroUsize::new(cfg.ncached_files).expect("non-zero file num"),
            )),
            file_nbit,
            rootdir: rootdir.to_path_buf(),
        };
        let f0 = s.get_file(0)?;
        if flock(f0.get_fd(), FlockArg::LockExclusiveNonblock).is_err() {
            return Err(StoreError::Init("the store is busy".into()));
        }
        Ok(s)
    }

    fn get_file(&self, fid: u64) -> Result<Arc<File>, StoreError<std::io::Error>> {
        let mut files = self.files.lock();
        let file_size = 1 << self.file_nbit;
        Ok(match files.get(&fid) {
            Some(f) => f.clone(),
            None => {
                files.put(
                    fid,
                    Arc::new(File::new(fid, file_size, self.rootdir.clone())?),
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
    }
}

#[derive(TypedBuilder, Clone, Debug)]
pub struct WALConfig {
    #[builder(default = 22)] // 4MB WAL logs
    pub file_nbit: u64,
    #[builder(default = 15)] // 32KB
    pub block_nbit: u64,
    #[builder(default = 100)] // preserve a rolling window of 100 past commits
    pub max_revisions: u32,
}
