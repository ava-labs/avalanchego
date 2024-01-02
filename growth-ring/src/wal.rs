// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use async_trait::async_trait;
use bytemuck::{cast_slice, Pod, Zeroable};
use futures::{
    future::{self, FutureExt, TryFutureExt},
    stream::StreamExt,
    Future,
};

use std::convert::{TryFrom, TryInto};
use std::mem::MaybeUninit;
use std::num::NonZeroUsize;
use std::pin::Pin;
use std::{
    cell::{RefCell, UnsafeCell},
    ffi::OsStr,
    path::{Path, PathBuf},
};
use std::{
    collections::{hash_map, BinaryHeap, HashMap, VecDeque},
    marker::PhantomData,
};

pub use crate::walerror::WalError;

enum WalRingType {
    #[allow(dead_code)]
    Null = 0x0,
    Full,
    First,
    Middle,
    Last,
}

#[repr(C, packed)]
#[derive(Copy, Clone, Debug, Pod, Zeroable)]
struct WalRingBlob {
    counter: u32,
    crc32: u32,
    rsize: u32,
    rtype: u8,
    // payload follows
}

impl TryFrom<u8> for WalRingType {
    type Error = ();
    fn try_from(v: u8) -> Result<Self, Self::Error> {
        match v {
            x if x == WalRingType::Null as u8 => Ok(WalRingType::Null),
            x if x == WalRingType::Full as u8 => Ok(WalRingType::Full),
            x if x == WalRingType::First as u8 => Ok(WalRingType::First),
            x if x == WalRingType::Middle as u8 => Ok(WalRingType::Middle),
            x if x == WalRingType::Last as u8 => Ok(WalRingType::Last),
            _ => Err(()),
        }
    }
}

type WalFileId = u64;
pub type WalBytes = Box<[u8]>;
pub type WalPos = u64;

// convert XXXXXX.log into number from the XXXXXX (in hex)
fn get_fid(fname: &Path) -> Result<WalFileId, WalError> {
    let wal_err: WalError = WalError::Other("not a log file".to_string());

    if fname.extension() != Some(OsStr::new("log")) {
        return Err(wal_err);
    }

    u64::from_str_radix(
        fname
            .file_stem()
            .unwrap_or(OsStr::new(""))
            .to_str()
            .unwrap_or(""),
        16,
    )
    .map_err(|_| wal_err)
}

fn get_fname(fid: WalFileId) -> String {
    format!("{:08x}.log", fid)
}

fn sort_fids(file_nbit: u64, mut fids: Vec<u64>) -> Vec<(u8, u64)> {
    let (min, max) = fids.iter().fold((u64::MAX, u64::MIN), |acc, fid| {
        ((*fid).min(acc.0), (*fid).max(acc.1))
    });
    let fid_half = u64::MAX >> (file_nbit + 1);
    if max > min && max - min > fid_half {
        // we treat this as u64 overflow has happened, take proper care here
        let mut aux: Vec<_> = fids
            .into_iter()
            .map(|fid| (if fid < fid_half { 1 } else { 0 }, fid))
            .collect();
        aux.sort();
        aux
    } else {
        fids.sort();
        fids.into_iter().map(|fid| (0, fid)).collect()
    }
}

const fn counter_lt(a: u32, b: u32) -> bool {
    if u32::abs_diff(a, b) > u32::MAX / 2 {
        b < a
    } else {
        a < b
    }
}

#[repr(transparent)]
#[derive(Debug, Clone, Copy, Pod, Zeroable)]
struct Header {
    /// all preceding files (<fid) could be removed if not yet
    recover_fid: u64,
}

const HEADER_SIZE: usize = std::mem::size_of::<Header>();

#[repr(C)]
#[derive(Eq, PartialEq, Copy, Clone, Debug, Hash)]
pub struct WalRingId {
    start: WalPos,
    end: WalPos,
    counter: u32,
}

impl Default for WalRingId {
    fn default() -> Self {
        Self::empty_id()
    }
}

impl WalRingId {
    pub const fn empty_id() -> Self {
        WalRingId {
            start: 0,
            end: 0,
            counter: 0,
        }
    }
    pub const fn get_start(&self) -> WalPos {
        self.start
    }
    pub const fn get_end(&self) -> WalPos {
        self.end
    }
}

impl Ord for WalRingId {
    fn cmp(&self, other: &WalRingId) -> std::cmp::Ordering {
        other
            .start
            .cmp(&self.start)
            .then_with(|| other.end.cmp(&self.end))
    }
}

impl PartialOrd for WalRingId {
    fn partial_cmp(&self, other: &WalRingId) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

pub trait Record {
    fn serialize(&self) -> WalBytes;
}

impl Record for WalBytes {
    fn serialize(&self) -> WalBytes {
        self[..].into()
    }
}

impl Record for String {
    fn serialize(&self) -> WalBytes {
        self.as_bytes().into()
    }
}

impl Record for &str {
    fn serialize(&self) -> WalBytes {
        self.as_bytes().into()
    }
}

/// the state for a WAL writer
struct WalState {
    /// the next position for a record, addressed in the entire Wal space
    next: WalPos,
    /// number of bits for a file
    file_nbit: u64,
    next_complete: WalRingId,
    counter: u32,
    io_complete: BinaryHeap<WalRingId>,
    pending_removal: VecDeque<(WalFileId, u32)>,
}

#[async_trait(?Send)]
pub trait WalFile {
    /// Initialize the file space in [offset, offset + length) to zero.
    async fn allocate(&self, offset: WalPos, length: usize) -> Result<(), WalError>;
    /// Write data with offset. We assume all previous `allocate`/`truncate` invocations are visible
    /// if ordered earlier (should be guaranteed by most OS).  Additionally, the write caused
    /// by each invocation of this function should be _atomic_ (the entire single write should be
    /// all or nothing).
    async fn write(&self, offset: WalPos, data: WalBytes) -> Result<(), WalError>;
    /// Read data with offset. Return `Ok(None)` when it reaches EOF.
    async fn read(&self, offset: WalPos, length: usize) -> Result<Option<WalBytes>, WalError>;
    /// Truncate a file to a specified length.
    async fn truncate(&self, length: usize) -> Result<(), WalError>;
}

#[async_trait(?Send)]
pub trait WalStore<F: WalFile> {
    type FileNameIter: Iterator<Item = PathBuf>;

    /// Open a file given the filename, create the file if not exists when `touch` is `true`.
    async fn open_file(&self, filename: &str, touch: bool) -> Result<F, WalError>;
    /// Unlink a file given the filename.
    async fn remove_file(&self, filename: String) -> Result<(), WalError>;
    /// Enumerate all Wal filenames. It should include all Wal files that are previously opened
    /// (created) but not removed. The list could be unordered.
    #[allow(clippy::result_unit_err)]
    fn enumerate_files(&self) -> Result<Self::FileNameIter, WalError>;
}

struct WalFileHandle<'a, F: WalFile + 'static, S: WalStore<F>> {
    fid: WalFileId,
    handle: &'a dyn WalFile,
    pool: &'a WalFilePool<F, S>,
    wal_file: PhantomData<F>,
}

impl<'a, F: WalFile, S: WalStore<F>> std::ops::Deref for WalFileHandle<'a, F, S> {
    type Target = dyn WalFile + 'a;
    fn deref(&self) -> &Self::Target {
        self.handle
    }
}

impl<'a, F: WalFile + 'static, S: WalStore<F>> Drop for WalFileHandle<'a, F, S> {
    fn drop(&mut self) {
        (self.pool).release_file(self.fid);
    }
}

/// The middle layer that manages WAL file handles and invokes public trait functions to actually
/// manipulate files and their contents.
struct WalFilePool<F: WalFile, S: WalStore<F>> {
    store: S,
    header_file: F,
    handle_cache: RefCell<lru::LruCache<WalFileId, Box<dyn WalFile>>>,
    #[allow(clippy::type_complexity)]
    handle_used: RefCell<HashMap<WalFileId, UnsafeCell<(Box<dyn WalFile>, usize)>>>,
    #[allow(clippy::type_complexity)]
    last_write: UnsafeCell<MaybeUninit<Pin<Box<dyn Future<Output = Result<(), WalError>>>>>>,
    #[allow(clippy::type_complexity)]
    last_peel: UnsafeCell<MaybeUninit<Pin<Box<dyn Future<Output = Result<(), WalError>>>>>>,
    file_nbit: u64,
    file_size: u64,
    block_nbit: u64,
}

impl<F: WalFile + 'static, S: WalStore<F>> WalFilePool<F, S> {
    async fn new(
        store: S,
        file_nbit: u64,
        block_nbit: u64,
        cache_size: NonZeroUsize,
    ) -> Result<Self, WalError> {
        let header_file = store.open_file("HEAD", true).await?;
        header_file.truncate(HEADER_SIZE).await?;
        Ok(WalFilePool {
            store,
            header_file,
            handle_cache: RefCell::new(lru::LruCache::new(cache_size)),
            handle_used: RefCell::new(HashMap::new()),
            last_write: UnsafeCell::new(MaybeUninit::new(Box::pin(future::ready(Ok(()))))),
            last_peel: UnsafeCell::new(MaybeUninit::new(Box::pin(future::ready(Ok(()))))),
            file_nbit,
            file_size: 1 << file_nbit,
            block_nbit,
        })
    }

    async fn read_header(&self) -> Result<Header, WalError> {
        let bytes = self
            .header_file
            .read(0, HEADER_SIZE)
            .await?
            .ok_or(WalError::Other("EOF".to_string()))?;
        let slice = cast_slice::<_, Header>(&bytes);
        slice
            .first()
            .copied()
            .ok_or(WalError::Other("short read".to_string()))
    }

    async fn write_header(&self, header: Header) -> Result<(), WalError> {
        self.header_file
            .write(0, cast_slice(&[header]).into())
            .await?;
        Ok(())
    }

    #[allow(clippy::await_holding_refcell_ref)]
    // TODO: Refactor to remove mutable reference from being awaited.
    async fn get_file(&self, fid: u64, touch: bool) -> Result<WalFileHandle<F, S>, WalError> {
        if let Some(h) = self.handle_cache.borrow_mut().pop(&fid) {
            let handle = match self.handle_used.borrow_mut().entry(fid) {
                #[allow(unsafe_code)]
                hash_map::Entry::Vacant(e) => unsafe {
                    &*(*e.insert(UnsafeCell::new((h, 1))).get()).0
                },
                _ => unreachable!(),
            };
            Ok(WalFileHandle {
                fid,
                handle,
                pool: self,
                wal_file: PhantomData,
            })
        } else {
            #[allow(unsafe_code)]
            let v = unsafe {
                &mut *match self.handle_used.borrow_mut().entry(fid) {
                    hash_map::Entry::Occupied(e) => e.into_mut(),
                    hash_map::Entry::Vacant(e) => {
                        let file = self.store.open_file(&get_fname(fid), touch).await?;
                        e.insert(UnsafeCell::new((Box::new(file), 0)))
                    }
                }
                .get()
            };
            v.1 += 1;
            Ok(WalFileHandle {
                fid,
                handle: &*v.0,
                pool: self,
                wal_file: PhantomData,
            })
        }
    }

    fn release_file(&self, fid: WalFileId) {
        match self.handle_used.borrow_mut().entry(fid) {
            hash_map::Entry::Occupied(e) => {
                #[allow(unsafe_code)]
                let v = unsafe { &mut *e.get().get() };
                v.1 -= 1;
                if v.1 == 0 {
                    self.handle_cache
                        .borrow_mut()
                        .put(fid, e.remove().into_inner().0);
                }
            }
            _ => unreachable!(),
        }
    }

    #[allow(clippy::type_complexity)]
    fn write<'a>(
        &'a mut self,
        writes: Vec<(WalPos, WalBytes)>,
    ) -> Vec<Pin<Box<dyn Future<Output = Result<(), WalError>> + 'a>>> {
        if writes.is_empty() {
            return Vec::new();
        }
        let file_size = self.file_size;
        let file_nbit = self.file_nbit;
        let meta: Vec<(u64, u64)> = writes
            .iter()
            .map(|(off, w)| ((*off) >> file_nbit, w.len() as u64))
            .collect();
        let mut files: Vec<Pin<Box<dyn Future<Output = _> + 'a>>> = Vec::new();
        for &(fid, _) in meta.iter() {
            files.push(Box::pin(self.get_file(fid, true)) as Pin<Box<dyn Future<Output = _> + 'a>>)
        }
        #[allow(clippy::indexing_slicing)]
        let mut fid = writes[0].0 >> file_nbit;
        #[allow(clippy::indexing_slicing)]
        let mut alloc_start = writes[0].0 & (self.file_size - 1);
        #[allow(clippy::indexing_slicing)]
        let mut alloc_end = alloc_start + writes[0].1.len() as u64;
        #[allow(unsafe_code)]
        let last_write = unsafe {
            std::mem::replace(&mut *self.last_write.get(), std::mem::MaybeUninit::uninit())
                .assume_init()
        };
        // pre-allocate the file space
        let alloc = async move {
            last_write.await?;
            let mut last_h: Option<
                Pin<Box<dyn Future<Output = Result<WalFileHandle<'a, F, S>, WalError>> + 'a>>,
            > = None;
            for ((next_fid, wl), h) in meta.into_iter().zip(files) {
                if let Some(lh) = last_h.take() {
                    if next_fid != fid {
                        lh.await?
                            .allocate(alloc_start, (alloc_end - alloc_start) as usize)
                            .await?;
                        last_h = Some(h);
                        alloc_start = 0;
                        alloc_end = alloc_start + wl;
                        fid = next_fid;
                    } else {
                        last_h = Some(lh);
                        alloc_end += wl;
                    }
                } else {
                    last_h = Some(h);
                }
            }
            if let Some(lh) = last_h {
                lh.await?
                    .allocate(alloc_start, (alloc_end - alloc_start) as usize)
                    .await?
            }
            Ok(())
        };

        let mut res = Vec::new();
        let mut prev = Box::pin(alloc) as Pin<Box<dyn Future<Output = _> + 'a>>;

        for (off, w) in writes.into_iter() {
            let f = self.get_file(off >> file_nbit, true);
            let w = (async move {
                prev.await?;
                let f = f.await?;
                f.write(off & (file_size - 1), w).await
            })
            .shared();
            prev = Box::pin(w.clone());
            res.push(Box::pin(w) as Pin<Box<dyn Future<Output = _> + 'a>>)
        }

        #[allow(unsafe_code)]
        unsafe {
            (*self.last_write.get()) = MaybeUninit::new(std::mem::transmute::<
                Pin<Box<dyn Future<Output = _> + 'a>>,
                Pin<Box<dyn Future<Output = _> + 'static>>,
            >(prev))
        }
        res
    }

    #[allow(clippy::type_complexity)]
    fn remove_files<'a>(
        &'a mut self,
        state: &mut WalState,
        keep_nrecords: u32,
    ) -> impl Future<Output = Result<(), WalError>> + 'a {
        #[allow(unsafe_code)]
        let last_peel = unsafe {
            std::mem::replace(&mut *self.last_peel.get(), std::mem::MaybeUninit::uninit())
                .assume_init()
        };

        let mut removes: Vec<Pin<Box<dyn Future<Output = Result<(), WalError>>>>> = Vec::new();

        #[allow(clippy::unwrap_used)]
        while state.pending_removal.len() > 1 {
            let (fid, counter) = state.pending_removal.front().unwrap();

            if counter_lt(counter + keep_nrecords, state.counter) {
                removes.push(self.store.remove_file(get_fname(*fid))
                    as Pin<Box<dyn Future<Output = _> + 'a>>);
                state.pending_removal.pop_front();
            } else {
                break;
            }
        }

        let p = async move {
            last_peel.await.ok();

            for r in removes.into_iter() {
                r.await.ok();
            }

            Ok(())
        }
        .shared();

        #[allow(unsafe_code)]
        unsafe {
            (*self.last_peel.get()) = MaybeUninit::new(std::mem::transmute(
                Box::pin(p.clone()) as Pin<Box<dyn Future<Output = _> + 'a>>
            ))
        }

        p
    }

    fn in_use_len(&self) -> usize {
        self.handle_used.borrow().len()
    }

    fn reset(&mut self) {
        self.handle_cache.borrow_mut().clear();
        self.handle_used.borrow_mut().clear()
    }
}

pub struct WalWriter<F: WalFile, S: WalStore<F>> {
    state: WalState,
    file_pool: WalFilePool<F, S>,
    block_buffer: WalBytes,
    block_size: u32,
    msize: usize,
}

impl<F: WalFile + 'static, S: WalStore<F>> WalWriter<F, S> {
    fn new(state: WalState, file_pool: WalFilePool<F, S>) -> Self {
        let mut b = Vec::new();
        let block_size = 1 << file_pool.block_nbit as u32;
        let msize = std::mem::size_of::<WalRingBlob>();
        b.resize(block_size as usize, 0);
        WalWriter {
            state,
            file_pool,
            block_buffer: b.into_boxed_slice(),
            block_size,
            msize,
        }
    }

    /// Submit a sequence of records to Wal. It returns a vector of futures, each of which
    /// corresponds to one record. When a future resolves to `WalRingId`, it is guaranteed the
    /// record is already logged. Then, after finalizing the changes encoded by that record to
    /// the persistent storage, the caller can recycle the Wal files by invoking the given
    /// `peel` with the given `WalRingId`s. Note: each serialized record should contain at least 1
    /// byte (empty record payload will result in assertion failure).
    pub fn grow<'a, R: Record + 'a>(
        &'a mut self,
        records: Vec<R>,
    ) -> Vec<impl Future<Output = Result<(R, WalRingId), ()>> + 'a> {
        let mut res = Vec::new();
        let mut writes = Vec::new();
        let msize = self.msize as u32;
        // the global offest of the begining of the block
        // the start of the unwritten data
        let mut bbuff_start = self.state.next as u32 & (self.block_size - 1);
        // the end of the unwritten data
        let mut bbuff_cur = bbuff_start;

        for rec in records.iter() {
            let bytes = rec.serialize();
            let mut rec = &bytes[..];
            let mut rsize = rec.len() as u32;
            let mut ring_start = None;
            assert!(rsize > 0);

            while rsize > 0 {
                let remain = self.block_size - bbuff_cur;

                #[allow(clippy::indexing_slicing)] // TODO: remove this to reduce scope
                if remain > msize {
                    let d = remain - msize;
                    let rs0 = self.state.next + (bbuff_cur - bbuff_start) as u64;

                    #[allow(unsafe_code)]
                    let blob = unsafe {
                        #[allow(clippy::indexing_slicing)]
                        &mut *self.block_buffer[bbuff_cur as usize..]
                            .as_mut_ptr()
                            .cast::<WalRingBlob>()
                    };

                    bbuff_cur += msize;

                    if d >= rsize {
                        // the remaining rec fits in the block
                        let payload = rec;
                        blob.counter = self.state.counter;
                        blob.crc32 = CRC32.checksum(payload);
                        blob.rsize = rsize;

                        let (rs, rt) = if let Some(rs) = ring_start.take() {
                            self.state.counter += 1;
                            (rs, WalRingType::Last)
                        } else {
                            self.state.counter += 1;
                            (rs0, WalRingType::Full)
                        };

                        blob.rtype = rt as u8;
                        #[allow(clippy::indexing_slicing)]
                        self.block_buffer[bbuff_cur as usize..bbuff_cur as usize + payload.len()]
                            .copy_from_slice(payload);
                        bbuff_cur += rsize;
                        rsize = 0;
                        let end = self.state.next + (bbuff_cur - bbuff_start) as u64;

                        res.push((
                            WalRingId {
                                start: rs,
                                end,
                                counter: blob.counter,
                            },
                            Vec::new(),
                        ));
                    } else {
                        // the remaining block can only accommodate partial rec
                        #[allow(clippy::indexing_slicing)]
                        let payload = &rec[..d as usize];
                        blob.counter = self.state.counter;
                        blob.crc32 = CRC32.checksum(payload);
                        blob.rsize = d;

                        blob.rtype = if ring_start.is_some() {
                            WalRingType::Middle
                        } else {
                            ring_start = Some(rs0);
                            WalRingType::First
                        } as u8;

                        #[allow(clippy::indexing_slicing)]
                        self.block_buffer[bbuff_cur as usize..bbuff_cur as usize + payload.len()]
                            .copy_from_slice(payload);
                        bbuff_cur += d;
                        rsize -= d;
                        // TODO: not allowed: #[allow(clippy::indexing_slicing)]
                        rec = &rec[d as usize..];
                    }
                } else {
                    // add padding space by moving the point to the end of the block
                    bbuff_cur = self.block_size;
                }

                if bbuff_cur == self.block_size {
                    #[allow(clippy::indexing_slicing)]
                    writes.push((
                        self.state.next,
                        self.block_buffer[bbuff_start as usize..]
                            .to_vec()
                            .into_boxed_slice(),
                    ));
                    self.state.next += (self.block_size - bbuff_start) as u64;
                    bbuff_start = 0;
                    bbuff_cur = 0;
                }
            }
        }

        if bbuff_cur > bbuff_start {
            #[allow(clippy::indexing_slicing)]
            writes.push((
                self.state.next,
                self.block_buffer[bbuff_start as usize..bbuff_cur as usize]
                    .to_vec()
                    .into_boxed_slice(),
            ));

            self.state.next += (bbuff_cur - bbuff_start) as u64;
        }

        // mark the block info for each record
        let mut i = 0;

        'outer: for (j, (off, w)) in writes.iter().enumerate() {
            let blk_s = *off;
            let blk_e = blk_s + w.len() as u64;

            #[allow(clippy::indexing_slicing)]
            while res[i].0.end <= blk_s {
                i += 1;

                if i >= res.len() {
                    break 'outer;
                }
            }

            #[allow(clippy::indexing_slicing)]
            while res[i].0.start < blk_e {
                res[i].1.push(j);

                if res[i].0.end >= blk_e {
                    break;
                }

                i += 1;

                if i >= res.len() {
                    break 'outer;
                }
            }
        }

        let writes: Vec<future::Shared<_>> = self
            .file_pool
            .write(writes)
            .into_iter()
            .map(move |f| f.shared())
            .collect();

        res.into_iter()
            .zip(records)
            .map(|((ringid, blks), rec)| {
                #[allow(clippy::indexing_slicing)]
                future::try_join_all(blks.into_iter().map(|idx| writes[idx].clone()))
                    .or_else(|_| future::ready(Err(())))
                    .and_then(move |_| future::ready(Ok((rec, ringid))))
            })
            .collect()
    }

    /// Inform the `WalWriter` that some data writes are complete so that it could automatically
    /// remove obsolete Wal files. The given list of `WalRingId` does not need to be ordered and
    /// could be of arbitrary length. Use `0` for `keep_nrecords` if all obsolete Wal files
    /// need to removed (the obsolete files do not affect the speed of recovery or correctness).
    pub async fn peel<T: AsRef<[WalRingId]>>(
        &mut self,
        records: T,
        keep_nrecords: u32,
    ) -> Result<(), WalError> {
        let msize = self.msize as u64;
        let block_size = self.block_size as u64;
        let state = &mut self.state;

        for rec in records.as_ref() {
            state.io_complete.push(*rec);
        }

        while let Some(s) = state.io_complete.peek().map(|&e| e.start) {
            if s != state.next_complete.end {
                break;
            }

            #[allow(clippy::unwrap_used)]
            let mut m = state.io_complete.pop().unwrap();
            let block_remain = block_size - (m.end & (block_size - 1));

            if block_remain <= msize {
                m.end += block_remain
            }

            let fid = m.start >> state.file_nbit;

            match state.pending_removal.back_mut() {
                Some(l) => {
                    if l.0 == fid {
                        l.1 = m.counter
                    } else {
                        for i in l.0 + 1..fid + 1 {
                            state.pending_removal.push_back((i, m.counter))
                        }
                    }
                }
                None => state.pending_removal.push_back((fid, m.counter)),
            }

            state.next_complete = m;
        }

        self.file_pool.remove_files(state, keep_nrecords).await.ok();

        Ok(())
    }

    pub fn file_pool_in_use(&self) -> usize {
        self.file_pool.in_use_len()
    }

    #[allow(clippy::unwrap_used)]
    pub async fn read_recent_records<'a>(
        &'a self,
        nrecords: usize,
        recover_policy: &RecoverPolicy,
    ) -> Result<Vec<WalBytes>, WalError> {
        let file_pool = &self.file_pool;
        let file_nbit = file_pool.file_nbit;
        let block_size = 1 << file_pool.block_nbit;
        let msize = std::mem::size_of::<WalRingBlob>();

        let logfiles = sort_fids(
            file_nbit,
            file_pool
                .store
                .enumerate_files()?
                .flat_map(|s| get_fid(&s))
                .collect(),
        );

        let mut chunks: Option<Vec<_>> = None;
        let mut records = Vec::new();
        'outer: for (_, fid) in logfiles.into_iter().rev() {
            let f = file_pool.get_file(fid, false).await?;
            let ring_stream = WalLoader::read_rings(&f, true, file_pool.block_nbit, recover_policy);
            let mut off = fid << file_nbit;
            let mut rings = Vec::new();
            futures::pin_mut!(ring_stream);
            while let Some(ring) = ring_stream.next().await {
                rings.push(ring);
            }
            for ring in rings.into_iter().rev() {
                let ring = ring.map_err(|_| WalError::Other("error mapping ring".to_string()))?;
                let (header, payload) = ring;
                #[allow(clippy::unwrap_used)]
                let payload = payload.unwrap();
                match header.rtype.try_into() {
                    Ok(WalRingType::Full) => {
                        assert!(chunks.is_none());
                        if !WalLoader::verify_checksum_(&payload, header.crc32, recover_policy)? {
                            return Err(WalError::InvalidChecksum);
                        }
                        off += header.rsize as u64;
                        records.push(payload);
                    }
                    Ok(WalRingType::First) => {
                        if !WalLoader::verify_checksum_(&payload, header.crc32, recover_policy)? {
                            return Err(WalError::InvalidChecksum);
                        }
                        if let Some(mut chunks) = chunks.take() {
                            chunks.push(payload);
                            let mut acc = Vec::new();
                            chunks.into_iter().rev().fold(&mut acc, |acc, v| {
                                acc.extend(v.iter());
                                acc
                            });
                            records.push(acc.into());
                        } else {
                            unreachable!()
                        }
                        off += header.rsize as u64;
                    }
                    Ok(WalRingType::Middle) => {
                        if let Some(chunks) = &mut chunks {
                            chunks.push(payload);
                        } else {
                            unreachable!()
                        }
                        off += header.rsize as u64;
                    }
                    Ok(WalRingType::Last) => {
                        assert!(chunks.is_none());
                        chunks = Some(vec![payload]);
                        off += header.rsize as u64;
                    }
                    Ok(WalRingType::Null) => break,
                    Err(_) => match recover_policy {
                        RecoverPolicy::Strict => {
                            return Err(WalError::Other(
                                "invalid ring type - strict recovery requested".to_string(),
                            ))
                        }
                        RecoverPolicy::BestEffort => break 'outer,
                    },
                }
                let block_remain = block_size - (off & (block_size - 1));
                if block_remain <= msize as u64 {
                    off += block_remain;
                }
                if records.len() >= nrecords {
                    break 'outer;
                }
            }
        }
        Ok(records)
    }
}

#[derive(Copy, Clone)]
pub enum RecoverPolicy {
    /// all checksums must be correct, otherwise recovery fails
    Strict,
    /// stop recovering when hitting the first corrupted record
    BestEffort,
}

pub struct WalLoader {
    file_nbit: u64,
    block_nbit: u64,
    cache_size: NonZeroUsize,
    recover_policy: RecoverPolicy,
}

impl Default for WalLoader {
    #[allow(clippy::unwrap_used)]
    fn default() -> Self {
        WalLoader {
            file_nbit: 22,  // 4MB
            block_nbit: 15, // 32KB,
            cache_size: NonZeroUsize::new(16).unwrap(),
            recover_policy: RecoverPolicy::Strict,
        }
    }
}

impl WalLoader {
    pub fn new() -> Self {
        Default::default()
    }

    pub fn file_nbit(&mut self, v: u64) -> &mut Self {
        self.file_nbit = v;
        self
    }

    pub fn block_nbit(&mut self, v: u64) -> &mut Self {
        self.block_nbit = v;
        self
    }

    pub fn cache_size(&mut self, v: NonZeroUsize) -> &mut Self {
        self.cache_size = v;
        self
    }

    pub fn recover_policy(&mut self, p: RecoverPolicy) -> &mut Self {
        self.recover_policy = p;
        self
    }

    fn verify_checksum_(data: &[u8], checksum: u32, p: &RecoverPolicy) -> Result<bool, WalError> {
        if checksum == CRC32.checksum(data) {
            Ok(true)
        } else {
            match p {
                RecoverPolicy::Strict => Err(WalError::Other("invalid checksum".to_string())),
                RecoverPolicy::BestEffort => Ok(false),
            }
        }
    }

    fn verify_checksum(&self, data: &[u8], checksum: u32) -> Result<bool, WalError> {
        Self::verify_checksum_(data, checksum, &self.recover_policy)
    }

    #[allow(clippy::await_holding_refcell_ref)]
    // TODO: Refactor to a more safe solution.
    fn read_rings<'a, F: WalFile + 'static, S: WalStore<F> + 'a>(
        file: &'a WalFileHandle<'a, F, S>,
        read_payload: bool,
        block_nbit: u64,
        recover_policy: &'a RecoverPolicy,
    ) -> impl futures::Stream<Item = Result<(WalRingBlob, Option<WalBytes>), bool>> + 'a {
        let block_size = 1 << block_nbit;
        let msize = std::mem::size_of::<WalRingBlob>();

        struct Vars<'a, F: WalFile + 'static, S: WalStore<F>> {
            done: bool,
            off: u64,
            file: &'a WalFileHandle<'a, F, S>,
        }

        let vars = std::rc::Rc::new(std::cell::RefCell::new(Vars {
            done: false,
            off: 0,
            file,
        }));

        futures::stream::unfold((), move |_| {
            let v = vars.clone();
            async move {
                let mut v = v.borrow_mut();

                macro_rules! check {
                    ($res: expr) => {
                        match $res {
                            Ok(t) => t,
                            Err(_) => die!(),
                        }
                    };
                }

                macro_rules! die {
                    () => {{
                        v.done = true;
                        return Some((Err(true), ()));
                    }};
                }

                macro_rules! _yield {
                    () => {{
                        v.done = true;
                        return None;
                    }};
                    ($v: expr) => {{
                        let v = $v;
                        catch_up!();
                        return Some((Ok(v), ()));
                    }};
                }

                macro_rules! catch_up {
                    () => {{
                        let block_remain = block_size - (v.off & (block_size - 1));
                        if block_remain <= msize as u64 {
                            v.off += block_remain;
                        }
                    }};
                }

                if v.done {
                    return None;
                }
                let header_raw = match check!(v.file.read(v.off, msize).await) {
                    Some(h) => h,
                    None => _yield!(),
                };
                v.off += msize as u64;
                let header: &[WalRingBlob] = cast_slice(&header_raw);
                let header = *header.first()?;

                let payload;
                match header.rtype.try_into() {
                    Ok(WalRingType::Full)
                    | Ok(WalRingType::First)
                    | Ok(WalRingType::Middle)
                    | Ok(WalRingType::Last) => {
                        payload = if read_payload {
                            Some(check!(check!(
                                v.file.read(v.off, header.rsize as usize).await
                            )
                            .ok_or(WalError::Other)))
                        } else {
                            None
                        };
                        v.off += header.rsize as u64;
                    }
                    Ok(WalRingType::Null) => _yield!(),
                    Err(_) => match recover_policy {
                        RecoverPolicy::Strict => die!(),
                        RecoverPolicy::BestEffort => {
                            v.done = true;
                            return Some((Err(false), ()));
                        }
                    },
                }
                _yield!((header, payload))
            }
        })
    }

    #[allow(clippy::await_holding_refcell_ref)]
    fn read_records<'a, F: WalFile + 'static, S: WalStore<F> + 'a>(
        &'a self,
        file: &'a WalFileHandle<'a, F, S>,
        chunks: &'a mut Option<(Vec<WalBytes>, WalPos)>,
    ) -> impl futures::Stream<Item = Result<(WalBytes, WalRingId, u32), bool>> + 'a {
        let fid = file.fid;
        let file_nbit = self.file_nbit;
        let block_size = 1 << self.block_nbit;
        let msize = std::mem::size_of::<WalRingBlob>();

        struct Vars<'a, F: WalFile + 'static, S: WalStore<F>> {
            done: bool,
            chunks: &'a mut Option<(Vec<WalBytes>, WalPos)>,
            off: u64,
            file: &'a WalFileHandle<'a, F, S>,
        }

        let vars = std::rc::Rc::new(std::cell::RefCell::new(Vars {
            done: false,
            off: 0,
            chunks,
            file,
        }));

        futures::stream::unfold((), move |_| {
            let v = vars.clone();
            async move {
                let mut v = v.borrow_mut();

                macro_rules! check {
                    ($res: expr) => {
                        match $res {
                            Ok(t) => t,
                            Err(_) => die!(),
                        }
                    };
                }

                macro_rules! die {
                    () => {{
                        v.done = true;
                        return Some((Err(true), ()));
                    }};
                }

                macro_rules! _yield {
                    () => {{
                        v.done = true;
                        return None;
                    }};
                    ($v: expr) => {{
                        let v = $v;
                        catch_up!();
                        return Some((Ok(v), ()));
                    }};
                }

                macro_rules! catch_up {
                    () => {{
                        let block_remain = block_size - (v.off & (block_size - 1));
                        if block_remain <= msize as u64 {
                            v.off += block_remain;
                        }
                    }};
                }

                if v.done {
                    return None;
                }
                loop {
                    let header_raw = match check!(v.file.read(v.off, msize).await) {
                        Some(h) => h,
                        None => _yield!(),
                    };
                    let ringid_start = (fid << file_nbit) + v.off;
                    v.off += msize as u64;
                    let header: WalRingBlob = *cast_slice(&header_raw).first()?;
                    let rsize = header.rsize;
                    match header.rtype.try_into() {
                        Ok(WalRingType::Full) => {
                            assert!(v.chunks.is_none());
                            let payload = check!(check!(v.file.read(v.off, rsize as usize).await)
                                .ok_or(WalError::Other));
                            // TODO: improve the behavior when CRC32 fails
                            if !check!(self.verify_checksum(&payload, header.crc32)) {
                                die!()
                            }
                            v.off += rsize as u64;
                            _yield!((
                                payload,
                                WalRingId {
                                    start: ringid_start,
                                    end: (fid << file_nbit) + v.off,
                                    counter: header.counter
                                },
                                header.counter
                            ))
                        }
                        Ok(WalRingType::First) => {
                            assert!(v.chunks.is_none());
                            let chunk = check!(check!(v.file.read(v.off, rsize as usize).await)
                                .ok_or(WalError::Other));
                            if !check!(self.verify_checksum(&chunk, header.crc32)) {
                                die!()
                            }
                            *v.chunks = Some((vec![chunk], ringid_start));
                            v.off += rsize as u64;
                        }
                        Ok(WalRingType::Middle) => {
                            let Vars {
                                chunks, off, file, ..
                            } = &mut *v;
                            if let Some((chunks, _)) = chunks {
                                let chunk = check!(check!(file.read(*off, rsize as usize).await)
                                    .ok_or(WalError::Other));
                                if !check!(self.verify_checksum(&chunk, header.crc32)) {
                                    die!()
                                }
                                chunks.push(chunk);
                            } // otherwise ignore the leftover
                            *off += rsize as u64;
                        }
                        Ok(WalRingType::Last) => {
                            let v_off = v.off;
                            v.off += rsize as u64;
                            if let Some((mut chunks, ringid_start)) = v.chunks.take() {
                                let chunk =
                                    check!(check!(v.file.read(v_off, rsize as usize).await)
                                        .ok_or(WalError::Other));
                                if !check!(self.verify_checksum(&chunk, header.crc32)) {
                                    die!()
                                }
                                chunks.push(chunk);
                                let mut payload =
                                    vec![0; chunks.iter().fold(0, |acc, v| acc + v.len())];
                                let mut ps = &mut payload[..];
                                #[allow(clippy::indexing_slicing)]
                                for c in chunks {
                                    ps[..c.len()].copy_from_slice(&c);
                                    ps = &mut ps[c.len()..];
                                }
                                _yield!((
                                    payload.into_boxed_slice(),
                                    WalRingId {
                                        start: ringid_start,
                                        end: (fid << file_nbit) + v.off,
                                        counter: header.counter,
                                    },
                                    header.counter
                                ))
                            }
                        }
                        Ok(WalRingType::Null) => _yield!(),
                        Err(_) => match self.recover_policy {
                            RecoverPolicy::Strict => die!(),
                            RecoverPolicy::BestEffort => {
                                v.done = true;
                                return Some((Err(false), ()));
                            }
                        },
                    }
                    catch_up!()
                }
            }
        })
    }

    /// Recover by reading the Wal files.
    pub async fn load<
        F: WalFile + 'static,
        S: WalStore<F>,
        Func: FnMut(WalBytes, WalRingId) -> Result<(), WalError>,
    >(
        &self,
        store: S,
        mut recover_func: Func,
        keep_nrecords: u32,
    ) -> Result<WalWriter<F, S>, WalError> {
        let msize = std::mem::size_of::<WalRingBlob>();
        assert!(self.file_nbit > self.block_nbit);
        assert!(msize < 1 << self.block_nbit);
        let mut file_pool =
            WalFilePool::new(store, self.file_nbit, self.block_nbit, self.cache_size).await?;
        let logfiles = sort_fids(
            self.file_nbit,
            file_pool
                .store
                .enumerate_files()?
                .flat_map(|s| get_fid(&s))
                .collect(),
        );

        let header = file_pool.read_header().await?;

        let mut chunks = None;
        let mut pre_skip = true;
        let mut scanned: Vec<(String, WalFileHandle<F, S>)> = Vec::new();
        let mut counter = 0;

        // TODO: check for missing logfiles
        'outer: for (_, fid) in logfiles.into_iter() {
            let fname = get_fname(fid);
            let f = file_pool.get_file(fid, false).await?;
            if header.recover_fid == fid {
                pre_skip = false;
            }
            if pre_skip {
                scanned.push((fname, f));
                continue;
            }
            {
                let stream = self.read_records(&f, &mut chunks);
                futures::pin_mut!(stream);
                while let Some(res) = stream.next().await {
                    let (bytes, ring_id, _) = match res {
                        Err(e) => {
                            if e {
                                return Err(WalError::Other(
                                    "error loading from storage".to_string(),
                                ));
                            } else {
                                break 'outer;
                            }
                        }
                        Ok(t) => t,
                    };
                    recover_func(bytes, ring_id)?;
                }
            }
            scanned.push((fname, f));
        }

        'outer: for (_, f) in scanned.iter().rev() {
            let records: Vec<_> = Self::read_rings(f, false, self.block_nbit, &self.recover_policy)
                .collect()
                .await;
            for e in records.into_iter().rev() {
                let (rec, _) =
                    e.map_err(|_| WalError::Other("error decoding WalRingBlob".to_string()))?;
                if rec.rtype == WalRingType::Full as u8 || rec.rtype == WalRingType::Last as u8 {
                    counter = rec.counter + 1;
                    break 'outer;
                }
            }
        }

        let fid_mask = (!0) >> self.file_nbit;
        let mut pending_removal = VecDeque::new();
        let recover_fid = match scanned.last() {
            Some((_, f)) => (f.fid + 1) & fid_mask,
            None => 0,
        };

        file_pool.write_header(Header { recover_fid }).await?;

        let mut skip_remove = false;
        for (fname, f) in scanned.into_iter() {
            let mut last = None;
            let stream = Self::read_rings(&f, false, self.block_nbit, &self.recover_policy);
            futures::pin_mut!(stream);
            while let Some(r) = stream.next().await {
                last =
                    Some(r.map_err(|_| WalError::Other("error decoding WalRingBlob".to_string()))?);
            }
            if let Some((last_rec, _)) = last {
                if !counter_lt(last_rec.counter + keep_nrecords, counter) {
                    skip_remove = true;
                }
                if skip_remove {
                    pending_removal.push_back((f.fid, last_rec.counter));
                }
            }
            if !skip_remove {
                f.truncate(0).await?;
                file_pool.store.remove_file(fname).await?;
            }
        }

        file_pool.reset();

        let next = recover_fid << self.file_nbit;
        let next_complete = WalRingId {
            start: 0,
            end: next,
            counter,
        };
        Ok(WalWriter::new(
            WalState {
                counter,
                next_complete,
                next,
                file_nbit: self.file_nbit,
                io_complete: BinaryHeap::new(),
                pending_removal,
            },
            file_pool,
        ))
    }
}

pub const CRC32: crc::Crc<u32> = crc::Crc::<u32>::new(&crc::CRC_32_CKSUM);

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod test {
    use super::*;
    use test_case::test_case;

    #[test_case("foo", Err("not a log file"); "no log extension")]
    #[test_case("foo.log", Err("not a log file"); "invalid digit found in string")]
    #[test_case("0000001.log", Ok(1); "happy path")]
    #[test_case("1.log", Ok(1); "no leading zeroes")]

    fn test_get_fid(input: &str, expected: Result<u64, &str>) {
        let got = get_fid(Path::new(input));
        match expected {
            Err(has) => {
                let err = got.err().unwrap().to_string();
                assert!(err.contains(has), "{:?}", err)
            }
            Ok(val) => assert_eq!(got.unwrap(), val),
        }
    }
}
