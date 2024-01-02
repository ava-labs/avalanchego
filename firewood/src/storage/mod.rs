// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// TODO: try to get rid of the use `RefCell` in this file
use self::buffer::DiskBufferRequester;
use crate::file::File;
use crate::shale::{self, CachedStore, CachedView, SendSyncDerefMut, SpaceId};
use nix::fcntl::{flock, FlockArg};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    fmt::{self, Debug},
    num::NonZeroUsize,
    ops::{Deref, DerefMut},
    os::fd::{AsFd, AsRawFd},
    path::PathBuf,
    sync::Arc,
};
use thiserror::Error;
use tokio::sync::{mpsc::error::SendError, oneshot::error::RecvError};
use typed_builder::TypedBuilder;

pub mod buffer;

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

pub trait MemStoreR: Debug + Send + Sync {
    /// Returns a slice of bytes from memory.
    fn get_slice(&self, offset: u64, length: u64) -> Option<Vec<u8>>;
    fn id(&self) -> SpaceId;
}

// Page should be boxed as to not take up so much stack-space
type Page = Box<[u8; PAGE_SIZE as usize]>;

#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct SpaceWrite {
    offset: u64,
    data: Box<[u8]>,
}

#[derive(Debug, Default, Clone, Serialize, Deserialize)]
/// In memory representation of Write-ahead log with `undo` and `redo`.
pub struct Ash {
    /// Deltas to undo the changes.
    pub undo: Vec<SpaceWrite>,
    /// Deltas to replay the changes.
    pub redo: Vec<SpaceWrite>,
}

impl Ash {
    fn iter(&self) -> impl Iterator<Item = (&SpaceWrite, &SpaceWrite)> {
        self.undo.iter().zip(self.redo.iter())
    }
}

#[derive(Debug, serde::Serialize, serde::Deserialize)]
pub struct AshRecord(pub HashMap<SpaceId, Ash>);

impl growthring::wal::Record for AshRecord {
    fn serialize(&self) -> growthring::wal::WalBytes {
        #[allow(clippy::unwrap_used)]
        bincode::serialize(self).unwrap().into()
    }
}

impl AshRecord {
    #[allow(clippy::boxed_local)]
    fn deserialize(raw: growthring::wal::WalBytes) -> Self {
        #[allow(clippy::unwrap_used)]
        bincode::deserialize(raw.as_ref()).unwrap()
    }
}

/// Basic copy-on-write item in the linear storage space for multi-versioning.
pub struct DeltaPage(u64, Page);

impl DeltaPage {
    #[inline(always)]
    const fn offset(&self) -> u64 {
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
        #[allow(clippy::indexing_slicing)]
        let mut widx: Vec<_> = (0..writes.len())
            .filter(|i| writes[*i].data.len() > 0)
            .collect();
        if widx.is_empty() {
            // the writes are all empty
            return Self(deltas);
        }

        // sort by the starting point
        #[allow(clippy::indexing_slicing)]
        widx.sort_by_key(|i| writes[*i].offset);

        let mut witer = widx.into_iter();
        #[allow(clippy::unwrap_used)]
        #[allow(clippy::indexing_slicing)]
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
            #[allow(clippy::indexing_slicing)]
            let w = &writes[i];
            let ep = (w.offset + w.data.len() as u64 - 1) >> PAGE_SIZE_NBIT;
            let wp = w.offset >> PAGE_SIZE_NBIT;
            if wp > tail {
                // all following writes won't go back past w.offset, so the previous continuous
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
                #[allow(clippy::indexing_slicing)]
                ((*if w.offset < deltas[mid].offset() {
                    &mut r
                } else {
                    &mut l
                }) = mid);
            }
            #[allow(clippy::indexing_slicing)]
            let off = (w.offset - deltas[l].offset()) as usize;
            let len = std::cmp::min(psize - off, w.data.len());
            #[allow(clippy::indexing_slicing)]
            deltas[l].data_mut()[off..off + len].copy_from_slice(&w.data[..len]);
            #[allow(clippy::indexing_slicing)]
            let mut data = &w.data[len..];
            while data.len() >= psize {
                l += 1;
                #[allow(clippy::indexing_slicing)]
                deltas[l].data_mut().copy_from_slice(&data[..psize]);
                #[allow(clippy::indexing_slicing)]
                (data = &data[psize..]);
            }
            if !data.is_empty() {
                l += 1;
                #[allow(clippy::indexing_slicing)]
                #[allow(clippy::indexing_slicing)]
                deltas[l].data_mut()[..data.len()].copy_from_slice(data);
            }
        }
        Self(deltas)
    }
}

pub struct StoreRev {
    base_space: RwLock<Arc<dyn MemStoreR>>,
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
        let base_space = self.base_space.read();
        let mut start = offset;
        let end = start + length;
        let delta = &self.delta;
        let mut l = 0;
        let mut r = delta.len();
        // no dirty page, before or after all dirty pages
        if r == 0 {
            return base_space.get_slice(start, end - start);
        }
        // otherwise, some dirty pages are covered by the range
        while r - l > 1 {
            let mid = (l + r) >> 1;
            #[allow(clippy::indexing_slicing)]
            ((*if start < delta[mid].offset() {
                &mut r
            } else {
                &mut l
            }) = mid);
        }
        #[allow(clippy::indexing_slicing)]
        if start >= delta[l].offset() + PAGE_SIZE {
            l += 1
        }
        #[allow(clippy::indexing_slicing)]
        if l >= delta.len() || end < delta[l].offset() {
            return base_space.get_slice(start, end - start);
        }
        let mut data = Vec::new();
        #[allow(clippy::indexing_slicing)]
        let p_off = std::cmp::min(end - delta[l].offset(), PAGE_SIZE);
        #[allow(clippy::indexing_slicing)]
        if start < delta[l].offset() {
            #[allow(clippy::indexing_slicing)]
            data.extend(base_space.get_slice(start, delta[l].offset() - start)?);
            #[allow(clippy::indexing_slicing)]
            #[allow(clippy::indexing_slicing)]
            data.extend(&delta[l].data()[..p_off as usize]);
        } else {
            #[allow(clippy::indexing_slicing)]
            #[allow(clippy::indexing_slicing)]
            #[allow(clippy::indexing_slicing)]
            data.extend(&delta[l].data()[(start - delta[l].offset()) as usize..p_off as usize]);
        };
        #[allow(clippy::indexing_slicing)]
        (start = delta[l].offset() + p_off);
        while start < end {
            l += 1;
            #[allow(clippy::indexing_slicing)]
            if l >= delta.len() || end < delta[l].offset() {
                data.extend(base_space.get_slice(start, end - start)?);
                break;
            }
            #[allow(clippy::indexing_slicing)]
            if delta[l].offset() > start {
                #[allow(clippy::indexing_slicing)]
                data.extend(base_space.get_slice(start, delta[l].offset() - start)?);
            }
            #[allow(clippy::indexing_slicing)]
            if end < delta[l].offset() + PAGE_SIZE {
                #[allow(clippy::indexing_slicing)]
                #[allow(clippy::indexing_slicing)]
                #[allow(clippy::indexing_slicing)]
                data.extend(&delta[l].data()[..(end - delta[l].offset()) as usize]);
                break;
            }
            #[allow(clippy::indexing_slicing)]
            data.extend(delta[l].data());
            #[allow(clippy::indexing_slicing)]
            (start = delta[l].offset() + PAGE_SIZE);
        }
        assert!(data.len() == length as usize);
        Some(data)
    }

    fn id(&self) -> SpaceId {
        self.base_space.read().id()
    }
}

#[derive(Clone, Debug)]
pub struct StoreRevShared(Arc<StoreRev>);

impl StoreRevShared {
    pub fn from_ash(base_space: Arc<dyn MemStoreR>, writes: &[SpaceWrite]) -> Self {
        let delta = StoreDelta::new(base_space.as_ref(), writes);
        let base_space = RwLock::new(base_space);
        Self(Arc::new(StoreRev { base_space, delta }))
    }

    pub fn from_delta(base_space: Arc<dyn MemStoreR>, delta: StoreDelta) -> Self {
        let base_space = RwLock::new(base_space);
        Self(Arc::new(StoreRev { base_space, delta }))
    }

    pub fn set_base_space(&mut self, base_space: Arc<dyn MemStoreR>) {
        *self.0.base_space.write() = base_space
    }

    pub const fn inner(&self) -> &Arc<StoreRev> {
        &self.0
    }
}

impl CachedStore for StoreRevShared {
    fn get_view(
        &self,
        offset: usize,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let data = self.0.get_slice(offset as u64, length)?;
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Box<dyn SendSyncDerefMut<Target = dyn CachedStore>> {
        Box::new(StoreShared(self.clone()))
    }

    fn write(&mut self, _offset: usize, _change: &[u8]) {
        // StoreRevShared is a read-only view version of CachedStore
        // Writes could be induced by lazy hashing and we can just ignore those
    }

    fn id(&self) -> SpaceId {
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

#[derive(Debug, Default)]
struct StoreRevMutDelta {
    pages: HashMap<u64, Page>,
    plain: Ash,
}

#[derive(Clone, Debug)]
/// A mutable revision of the store. The view is constructed by applying the `deltas` to the
/// `base space`. The `deltas` tracks both `undo` and `redo` to be able to rewind or reapply
/// the changes. `StoreRevMut` supports basing on top of another `StoreRevMut`, by chaining
/// `prev_deltas` (from based `StoreRevMut`) with current `deltas` from itself . In this way,
/// callers can create a new `StoreRevMut` from an existing one without actually committing
/// the mutations to the base space.
pub struct StoreRevMut {
    base_space: Arc<dyn MemStoreR>,
    deltas: Arc<RwLock<StoreRevMutDelta>>,
    prev_deltas: Arc<RwLock<StoreRevMutDelta>>,
}

impl StoreRevMut {
    pub fn new(base_space: Arc<dyn MemStoreR>) -> Self {
        Self {
            base_space,
            deltas: Default::default(),
            prev_deltas: Default::default(),
        }
    }

    pub fn new_from_other(other: &StoreRevMut) -> Self {
        Self {
            base_space: other.base_space.clone(),
            deltas: Default::default(),
            prev_deltas: other.deltas.clone(),
        }
    }

    pub fn into_shared(self) -> StoreRevShared {
        let mut pages = Vec::new();
        let deltas = std::mem::take(&mut *self.deltas.write());
        for (pid, page) in deltas.pages.into_iter() {
            pages.push(DeltaPage(pid, page));
        }
        pages.sort_by_key(|p| p.0);
        let delta = StoreDelta(pages);

        let rev = Arc::new(StoreRev {
            base_space: RwLock::new(self.base_space),
            delta,
        });
        StoreRevShared(rev)
    }

    fn get_page_mut<'a>(
        &self,
        deltas: &'a mut StoreRevMutDelta,
        prev_deltas: &StoreRevMutDelta,
        pid: u64,
    ) -> &'a mut [u8] {
        #[allow(clippy::unwrap_used)]
        let page = deltas
            .pages
            .entry(pid)
            .or_insert_with(|| match prev_deltas.pages.get(&pid) {
                Some(p) => Box::new(*p.as_ref()),
                None => Box::new(
                    self.base_space
                        .get_slice(pid << PAGE_SIZE_NBIT, PAGE_SIZE)
                        .unwrap()
                        .try_into()
                        .unwrap(),
                ),
            });

        page.as_mut()
    }

    #[must_use]
    pub fn delta(&self) -> (StoreDelta, Ash) {
        let guard = self.deltas.read();
        let mut pages: Vec<DeltaPage> = guard
            .pages
            .iter()
            .map(|page| DeltaPage(*page.0, page.1.clone()))
            .collect();
        pages.sort_by_key(|p| p.0);
        (StoreDelta(pages), guard.plain.clone())
    }
    pub fn reset_deltas(&self) {
        let mut guard = self.deltas.write();
        guard.plain = Ash::default();
        guard.pages = HashMap::new();
    }
}

impl CachedStore for StoreRevMut {
    fn get_view(
        &self,
        offset: usize,
        length: u64,
    ) -> Option<Box<dyn CachedView<DerefReturn = Vec<u8>>>> {
        let data = if length == 0 {
            Vec::new()
        } else {
            let end = offset + length as usize - 1;
            let s_pid = (offset >> PAGE_SIZE_NBIT) as u64;
            let s_off = offset & PAGE_MASK as usize;
            let e_pid = (end >> PAGE_SIZE_NBIT) as u64;
            let e_off = end & PAGE_MASK as usize;
            let deltas = &self.deltas.read().pages;
            let prev_deltas = &self.prev_deltas.read().pages;
            if s_pid == e_pid {
                match deltas.get(&s_pid) {
                    #[allow(clippy::indexing_slicing)]
                    Some(p) => p[s_off..e_off + 1].to_vec(),
                    None => match prev_deltas.get(&s_pid) {
                        #[allow(clippy::indexing_slicing)]
                        Some(p) => p[s_off..e_off + 1].to_vec(),
                        None => self.base_space.get_slice(offset as u64, length)?,
                    },
                }
            } else {
                let mut data = match deltas.get(&s_pid) {
                    #[allow(clippy::indexing_slicing)]
                    Some(p) => p[s_off..].to_vec(),
                    None => match prev_deltas.get(&s_pid) {
                        #[allow(clippy::indexing_slicing)]
                        Some(p) => p[s_off..].to_vec(),
                        None => self
                            .base_space
                            .get_slice(offset as u64, PAGE_SIZE - s_off as u64)?,
                    },
                };
                for p in s_pid + 1..e_pid {
                    match deltas.get(&p) {
                        Some(p) => data.extend(**p),
                        None => match prev_deltas.get(&p) {
                            Some(p) => data.extend(**p),
                            None => data.extend(
                                &self.base_space.get_slice(p << PAGE_SIZE_NBIT, PAGE_SIZE)?,
                            ),
                        },
                    };
                }
                match deltas.get(&e_pid) {
                    #[allow(clippy::indexing_slicing)]
                    Some(p) => data.extend(&p[..e_off + 1]),
                    None => match prev_deltas.get(&e_pid) {
                        #[allow(clippy::indexing_slicing)]
                        Some(p) => data.extend(&p[..e_off + 1]),
                        None => data.extend(
                            self.base_space
                                .get_slice(e_pid << PAGE_SIZE_NBIT, e_off as u64 + 1)?,
                        ),
                    },
                }
                data
            }
        };
        Some(Box::new(StoreRef { data }))
    }

    fn get_shared(&self) -> Box<dyn SendSyncDerefMut<Target = dyn CachedStore>> {
        Box::new(StoreShared(self.clone()))
    }

    fn write(&mut self, offset: usize, mut change: &[u8]) {
        let length = change.len() as u64;
        let end = offset + length as usize - 1;
        let s_pid = offset >> PAGE_SIZE_NBIT;
        let s_off = offset & PAGE_MASK as usize;
        let e_pid = end >> PAGE_SIZE_NBIT;
        let e_off = end & PAGE_MASK as usize;
        let mut undo: Vec<u8> = Vec::new();
        let redo: Box<[u8]> = change.into();

        if s_pid == e_pid {
            let mut deltas = self.deltas.write();
            #[allow(clippy::indexing_slicing)]
            let slice = &mut self.get_page_mut(&mut deltas, &self.prev_deltas.read(), s_pid as u64)
                [s_off..e_off + 1];
            undo.extend(&*slice);
            slice.copy_from_slice(change)
        } else {
            let len = PAGE_SIZE as usize - s_off;

            {
                let mut deltas = self.deltas.write();
                #[allow(clippy::indexing_slicing)]
                let slice =
                    &mut self.get_page_mut(&mut deltas, &self.prev_deltas.read(), s_pid as u64)
                        [s_off..];
                undo.extend(&*slice);
                #[allow(clippy::indexing_slicing)]
                slice.copy_from_slice(&change[..len]);
            }

            #[allow(clippy::indexing_slicing)]
            (change = &change[len..]);

            let mut deltas = self.deltas.write();
            for p in s_pid + 1..e_pid {
                let slice = self.get_page_mut(&mut deltas, &self.prev_deltas.read(), p as u64);
                undo.extend(&*slice);
                #[allow(clippy::indexing_slicing)]
                slice.copy_from_slice(&change[..PAGE_SIZE as usize]);
                #[allow(clippy::indexing_slicing)]
                (change = &change[PAGE_SIZE as usize..]);
            }

            #[allow(clippy::indexing_slicing)]
            let slice = &mut self.get_page_mut(&mut deltas, &self.prev_deltas.read(), e_pid as u64)
                [..e_off + 1];
            undo.extend(&*slice);
            slice.copy_from_slice(change);
        }

        let plain = &mut self.deltas.write().plain;
        assert!(undo.len() == redo.len());
        plain.undo.push(SpaceWrite {
            offset: offset as u64,
            data: undo.into(),
        });
        plain.redo.push(SpaceWrite {
            offset: offset as u64,
            data: redo,
        });
    }

    fn id(&self) -> SpaceId {
        self.base_space.id()
    }
}

#[derive(Clone, Debug)]
/// A zero-filled in memory store which can serve as a plain base to overlay deltas on top.
pub struct ZeroStore(Arc<()>);

impl Default for ZeroStore {
    fn default() -> Self {
        Self(Arc::new(()))
    }
}

impl MemStoreR for ZeroStore {
    fn get_slice(&self, _: u64, length: u64) -> Option<Vec<u8>> {
        Some(vec![0; length as usize])
    }

    fn id(&self) -> SpaceId {
        shale::INVALID_SPACE_ID
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
#[allow(clippy::indexing_slicing)]
mod test {
    use super::*;
    #[test]
    fn test_from_ash() {
        use rand::{rngs::StdRng, Rng, SeedableRng};
        let mut rng = StdRng::seed_from_u64(42);
        let min = rng.gen_range(0..2 * PAGE_SIZE);
        let max = rng.gen_range(min + PAGE_SIZE..min + 100 * PAGE_SIZE);
        for _ in 0..20 {
            let n = 20;
            let mut canvas = vec![0; (max - min) as usize];
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
            let z = Arc::new(ZeroStore::default());
            let rev = StoreRevShared::from_ash(z, &writes);
            println!("{rev:?}");
            assert_eq!(
                rev.get_view(min as usize, max - min)
                    .as_deref()
                    .unwrap()
                    .as_deref(),
                canvas
            );
            for _ in 0..2 * n {
                let l = rng.gen_range(min..max);
                let r = rng.gen_range(l + 1..max);
                assert_eq!(
                    rev.get_view(l as usize, r - l)
                        .as_deref()
                        .unwrap()
                        .as_deref(),
                    canvas[(l - min) as usize..(r - min) as usize]
                );
            }
        }
    }
}

#[derive(TypedBuilder)]
pub struct StoreConfig {
    ncached_pages: usize,
    ncached_files: usize,
    #[builder(default = 22)] // 4MB file by default
    file_nbit: u64,
    space_id: SpaceId,
    rootdir: PathBuf,
}

#[derive(Debug)]
struct CachedSpaceInner {
    cached_pages: lru::LruCache<u64, Page>,
    pinned_pages: HashMap<u64, (usize, Page)>,
    files: Arc<FilePool>,
    disk_requester: DiskBufferRequester,
}

#[derive(Clone, Debug)]
pub struct CachedSpace {
    inner: Arc<RwLock<CachedSpaceInner>>,
    space_id: SpaceId,
}

impl CachedSpace {
    pub fn new(
        cfg: &StoreConfig,
        disk_requester: DiskBufferRequester,
    ) -> Result<Self, StoreError<std::io::Error>> {
        let space_id = cfg.space_id;
        let files = Arc::new(FilePool::new(cfg)?);
        Ok(Self {
            inner: Arc::new(RwLock::new(CachedSpaceInner {
                cached_pages: lru::LruCache::new(
                    NonZeroUsize::new(cfg.ncached_pages).expect("non-zero cache size"),
                ),
                pinned_pages: HashMap::new(),
                files,
                disk_requester,
            })),
            space_id,
        })
    }

    pub fn clone_files(&self) -> Arc<FilePool> {
        self.inner.read().files.clone()
    }

    /// Apply `delta` to the store and return the StoreDelta that can undo this change.
    pub fn update(&self, delta: &StoreDelta) -> Option<StoreDelta> {
        let mut pages = Vec::new();
        for DeltaPage(pid, page) in &delta.0 {
            let data = self.inner.write().pin_page(self.space_id, *pid).ok()?;
            // save the original data
            #[allow(clippy::unwrap_used)]
            pages.push(DeltaPage(*pid, Box::new(data.try_into().unwrap())));
            // apply the change
            data.copy_from_slice(page.as_ref());
        }
        Some(StoreDelta(pages))
    }
}

impl CachedSpaceInner {
    fn pin_page(
        &mut self,
        space_id: SpaceId,
        pid: u64,
    ) -> Result<&'static mut [u8], StoreError<std::io::Error>> {
        let base = match self.pinned_pages.get_mut(&pid) {
            Some(e) => {
                e.0 += 1;
                e.1.as_mut_ptr()
            }
            None => {
                let page = self
                    .cached_pages
                    .pop(&pid)
                    .or_else(|| self.disk_requester.get_page(space_id, pid));
                let mut page = match page {
                    Some(page) => page,
                    None => {
                        let file_nbit = self.files.get_file_nbit();
                        let file_size = 1 << file_nbit;
                        let poff = pid << PAGE_SIZE_NBIT;
                        let file = self.files.get_file(poff >> file_nbit)?;
                        let mut page: Page = Page::new([0; PAGE_SIZE as usize]);

                        nix::sys::uio::pread(
                            file.as_fd(),
                            &mut *page,
                            (poff & (file_size - 1)) as nix::libc::off_t,
                        )
                        .map_err(StoreError::System)?;

                        page
                    }
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
            data: store.inner.write().pin_page(store.space_id, pid).ok()?,
            store: store.clone(),
        })
    }
}

impl Drop for PageRef {
    fn drop(&mut self) {
        self.store.inner.write().unpin_page(self.pid);
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
            #[allow(clippy::indexing_slicing)]
            return PageRef::new(s_pid, self).map(|e| e[s_off..e_off + 1].to_vec());
        }
        let mut data: Vec<u8> = Vec::new();
        {
            #[allow(clippy::indexing_slicing)]
            data.extend(&PageRef::new(s_pid, self)?[s_off..]);
            for p in s_pid + 1..e_pid {
                data.extend(&PageRef::new(p, self)?[..]);
            }
            #[allow(clippy::indexing_slicing)]
            data.extend(&PageRef::new(e_pid, self)?[..e_off + 1]);
        }
        Some(data)
    }

    fn id(&self) -> SpaceId {
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
        if flock(f0.as_raw_fd(), FlockArg::LockExclusiveNonblock).is_err() {
            return Err(StoreError::Init("the store is busy".into()));
        }
        Ok(s)
    }

    fn get_file(&self, fid: u64) -> Result<Arc<File>, StoreError<std::io::Error>> {
        let mut files = self.files.lock();

        let file = match files.get(&fid) {
            Some(f) => f.clone(),
            None => {
                let file_size = 1 << self.file_nbit;
                let file = Arc::new(File::new(fid, file_size, &self.rootdir)?);
                files.put(fid, file.clone());
                file
            }
        };

        Ok(file)
    }

    const fn get_file_nbit(&self) -> u64 {
        self.file_nbit
    }
}

impl Drop for FilePool {
    fn drop(&mut self) {
        #[allow(clippy::unwrap_used)]
        let f0 = self.get_file(0).unwrap();
        flock(f0.as_raw_fd(), FlockArg::UnlockNonblock).ok();
    }
}

#[derive(TypedBuilder, Clone, Debug)]
pub struct WalConfig {
    #[builder(default = 22)] // 4MB Wal logs
    pub file_nbit: u64,
    #[builder(default = 15)] // 32KB
    pub block_nbit: u64,
    #[builder(default = 100)] // preserve a rolling window of 100 past commits
    pub max_revisions: u32,
}
