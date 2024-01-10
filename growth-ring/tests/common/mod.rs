// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![allow(clippy::indexing_slicing)]
#[cfg(test)]
use async_trait::async_trait;
use futures::executor::block_on;
use growthring::wal::{WalBytes, WalError, WalFile, WalLoader, WalPos, WalRingId, WalStore};
use indexmap::{map::Entry, IndexMap};
use rand::Rng;
use std::cell::RefCell;
use std::collections::VecDeque;
use std::collections::{hash_map, HashMap};
use std::convert::TryInto;
use std::path::PathBuf;
use std::rc::Rc;

pub trait FailGen {
    fn next_fail(&self) -> bool;
}

struct FileContentEmul(RefCell<Vec<u8>>);

impl FileContentEmul {
    pub const fn new() -> Self {
        FileContentEmul(RefCell::new(Vec::new()))
    }
}

impl std::ops::Deref for FileContentEmul {
    type Target = RefCell<Vec<u8>>;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

/// Emulate the a virtual file handle.
pub struct WalFileEmul<G: FailGen> {
    file: Rc<FileContentEmul>,
    fgen: Rc<G>,
}

#[async_trait(?Send)]
impl<G: FailGen> WalFile for WalFileEmul<G> {
    async fn allocate(&self, offset: WalPos, length: usize) -> Result<(), WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other("allocate fgen next fail".to_string()));
        }
        let offset = offset as usize;
        if offset + length > self.file.borrow().len() {
            self.file.borrow_mut().resize(offset + length, 0)
        }
        for v in &mut self.file.borrow_mut()[offset..offset + length] {
            *v = 0
        }
        Ok(())
    }

    async fn truncate(&self, length: usize) -> Result<(), WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other("truncate fgen next fail".to_string()));
        }
        self.file.borrow_mut().resize(length, 0);
        Ok(())
    }

    async fn write(&self, offset: WalPos, data: WalBytes) -> Result<(), WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other("write fgen next fail".to_string()));
        }
        let offset = offset as usize;
        self.file.borrow_mut()[offset..offset + data.len()].copy_from_slice(&data);
        Ok(())
    }

    async fn read(&self, offset: WalPos, length: usize) -> Result<Option<WalBytes>, WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other("read fgen next fail".to_string()));
        }

        let offset = offset as usize;
        let file = self.file.borrow();
        if offset + length > file.len() {
            Ok(None)
        } else {
            Ok(Some(
                file[offset..offset + length].to_vec().into_boxed_slice(),
            ))
        }
    }
}

pub struct WalStoreEmulState {
    files: HashMap<String, Rc<FileContentEmul>>,
}

impl WalStoreEmulState {
    pub fn new() -> Self {
        WalStoreEmulState {
            files: HashMap::new(),
        }
    }
    pub fn clone(&self) -> Self {
        WalStoreEmulState {
            files: self.files.clone(),
        }
    }
}

/// Emulate the persistent storage state.
pub struct WalStoreEmul<'a, G>
where
    G: FailGen,
{
    state: RefCell<&'a mut WalStoreEmulState>,
    fgen: Rc<G>,
}

impl<'a, G: FailGen> WalStoreEmul<'a, G> {
    pub fn new(state: &'a mut WalStoreEmulState, fgen: Rc<G>) -> Self {
        let state = RefCell::new(state);
        WalStoreEmul { state, fgen }
    }
}

#[async_trait(?Send)]
impl<'a, G> WalStore<WalFileEmul<G>> for WalStoreEmul<'a, G>
where
    G: 'static + FailGen,
{
    type FileNameIter = std::vec::IntoIter<PathBuf>;

    async fn open_file(&self, filename: &str, touch: bool) -> Result<WalFileEmul<G>, WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other("open_file fgen next fail".to_string()));
        }
        match self.state.borrow_mut().files.entry(filename.to_string()) {
            hash_map::Entry::Occupied(e) => {
                let file = WalFileEmul {
                    file: e.get().clone(),
                    fgen: self.fgen.clone(),
                };
                Ok(file)
            }
            hash_map::Entry::Vacant(e) => {
                if touch {
                    let file = WalFileEmul {
                        file: e.insert(Rc::new(FileContentEmul::new())).clone(),
                        fgen: self.fgen.clone(),
                    };
                    Ok(file)
                } else {
                    Err(WalError::Other("open_file not found".to_string()))
                }
            }
        }
    }

    async fn remove_file(&self, filename: String) -> Result<(), WalError> {
        //println!("remove_file(filename={})", filename);
        if self.fgen.next_fail() {
            return Err(WalError::Other("remove_file fgen next fail".to_string()));
        }
        self.state
            .borrow_mut()
            .files
            .remove(&filename)
            .ok_or(WalError::Other("remove_file not found".to_string()))
            .map(|_| ())
    }

    fn enumerate_files(&self) -> Result<Self::FileNameIter, WalError> {
        if self.fgen.next_fail() {
            return Err(WalError::Other(
                "enumerate_files fgen next fail".to_string(),
            ));
        }
        let mut logfiles = Vec::new();
        for (fname, _) in self.state.borrow().files.iter() {
            logfiles.push(fname.into())
        }
        Ok(logfiles.into_iter())
    }
}

pub struct SingleFailGen {
    cnt: std::cell::Cell<usize>,
    fail_point: usize,
}

impl SingleFailGen {
    pub const fn new(fail_point: usize) -> Self {
        SingleFailGen {
            cnt: std::cell::Cell::new(0),
            fail_point,
        }
    }
}

impl FailGen for SingleFailGen {
    fn next_fail(&self) -> bool {
        let c = self.cnt.get();
        self.cnt.set(c + 1);
        c == self.fail_point
    }
}

pub struct ZeroFailGen;

impl FailGen for ZeroFailGen {
    fn next_fail(&self) -> bool {
        false
    }
}

pub struct CountFailGen(std::cell::Cell<usize>);

impl CountFailGen {
    pub const fn new() -> Self {
        CountFailGen(std::cell::Cell::new(0))
    }
    pub fn get_count(&self) -> usize {
        self.0.get()
    }
}

impl FailGen for CountFailGen {
    fn next_fail(&self) -> bool {
        self.0.set(self.0.get() + 1);
        false
    }
}

/// An ordered list of intervals: `(begin, end, color)*`.
#[derive(Clone)]
pub struct PaintStrokes(Vec<(u32, u32, u32)>);

impl PaintStrokes {
    pub const fn new() -> Self {
        PaintStrokes(Vec::new())
    }

    pub fn to_bytes(&self) -> WalBytes {
        let mut res: Vec<u8> = Vec::new();
        let is = std::mem::size_of::<u32>();
        let len = self.0.len() as u32;
        res.resize(is * (1 + 3 * self.0.len()), 0);
        let mut rs = &mut res[..];
        rs[..is].copy_from_slice(&len.to_le_bytes());
        rs = &mut rs[is..];
        for (s, e, c) in self.0.iter() {
            rs[..is].copy_from_slice(&s.to_le_bytes());
            rs[is..is * 2].copy_from_slice(&e.to_le_bytes());
            rs[is * 2..is * 3].copy_from_slice(&c.to_le_bytes());
            rs = &mut rs[is * 3..];
        }
        res.into_boxed_slice()
    }

    pub fn from_bytes(raw: &[u8]) -> Self {
        assert!(raw.len() > 4);
        assert!(raw.len() & 3 == 0);
        let is = std::mem::size_of::<u32>();
        let (len_raw, mut rest) = raw.split_at(is);
        #[allow(clippy::unwrap_used)]
        let len = u32::from_le_bytes(len_raw.try_into().unwrap());
        let mut res = Vec::new();
        for _ in 0..len {
            let (s_raw, rest1) = rest.split_at(is);
            let (e_raw, rest2) = rest1.split_at(is);
            let (c_raw, rest3) = rest2.split_at(is);
            #[allow(clippy::unwrap_used)]
            res.push((
                u32::from_le_bytes(s_raw.try_into().unwrap()),
                #[allow(clippy::unwrap_used)]
                u32::from_le_bytes(e_raw.try_into().unwrap()),
                #[allow(clippy::unwrap_used)]
                u32::from_le_bytes(c_raw.try_into().unwrap()),
            ));
            rest = rest3
        }
        PaintStrokes(res)
    }

    pub fn gen_rand<R: rand::Rng>(
        max_pos: u32,
        max_len: u32,
        max_col: u32,
        n: usize,
        rng: &mut R,
    ) -> PaintStrokes {
        assert!(max_pos > 0);
        let mut strokes = Self::new();
        for _ in 0..n {
            let pos = rng.gen_range(0..max_pos);
            let len = rng.gen_range(1..std::cmp::min(max_len, max_pos - pos + 1));
            strokes.stroke(pos, pos + len, rng.gen_range(0..max_col))
        }
        strokes
    }

    pub fn stroke(&mut self, start: u32, end: u32, color: u32) {
        self.0.push((start, end, color))
    }

    pub fn into_vec(self) -> Vec<(u32, u32, u32)> {
        self.0
    }
}

impl growthring::wal::Record for PaintStrokes {
    fn serialize(&self) -> WalBytes {
        self.to_bytes()
    }
}

#[test]
fn test_paint_strokes() {
    let mut p = PaintStrokes::new();
    for i in 0..3 {
        p.stroke(i, i + 3, i + 10)
    }
    let pr = p.to_bytes();
    for ((s, e, c), i) in PaintStrokes::from_bytes(&pr)
        .into_vec()
        .into_iter()
        .zip(0..)
    {
        assert_eq!(s, i);
        assert_eq!(e, i + 3);
        assert_eq!(c, i + 10);
    }
}

pub struct Canvas {
    waiting: HashMap<WalRingId, usize>,
    queue: IndexMap<u32, VecDeque<(u32, WalRingId)>>,
    canvas: Box<[u32]>,
}

impl Canvas {
    pub fn new(size: usize) -> Self {
        let canvas = vec![0; size].into_boxed_slice();
        // fill the background color 0
        Canvas {
            waiting: HashMap::new(),
            queue: IndexMap::new(),
            canvas,
        }
    }

    pub fn new_reference(&self, ops: &[PaintStrokes]) -> Self {
        let mut res = Self::new(self.canvas.len());
        for op in ops {
            for (s, e, c) in op.0.iter() {
                for i in *s..*e {
                    res.canvas[i as usize] = *c
                }
            }
        }
        res
    }

    fn get_waiting(&mut self, rid: WalRingId) -> &mut usize {
        match self.waiting.entry(rid) {
            hash_map::Entry::Occupied(e) => e.into_mut(),
            hash_map::Entry::Vacant(e) => e.insert(0),
        }
    }

    fn get_queued(&mut self, pos: u32) -> &mut VecDeque<(u32, WalRingId)> {
        match self.queue.entry(pos) {
            Entry::Occupied(e) => e.into_mut(),
            Entry::Vacant(e) => e.insert(VecDeque::new()),
        }
    }

    pub fn prepaint(&mut self, strokes: &PaintStrokes, rid: &WalRingId) {
        let rid = *rid;
        let mut nwait = 0;
        for (s, e, c) in strokes.0.iter() {
            for i in *s..*e {
                nwait += 1;
                self.get_queued(i).push_back((*c, rid))
            }
        }
        *self.get_waiting(rid) = nwait
    }

    // TODO: allow customized scheduler
    /// Schedule to paint one position, randomly. It optionally returns a finished batch write
    /// identified by its start position of WalRingId.
    pub fn rand_paint<R: rand::Rng>(&mut self, rng: &mut R) -> Option<(Option<WalRingId>, u32)> {
        if self.is_empty() {
            return None;
        }
        let idx = rng.gen_range(0..self.queue.len());
        #[allow(clippy::unwrap_used)]
        let (pos, _) = self.queue.get_index(idx).unwrap();
        let pos = *pos;
        Some((self.paint(pos), pos))
    }

    pub fn clear_queued(&mut self) {
        self.queue.clear();
        self.waiting.clear();
    }

    #[allow(clippy::unwrap_used)]
    pub fn paint_all(&mut self) {
        for (pos, q) in self.queue.iter() {
            self.canvas[*pos as usize] = q.back().unwrap().0;
        }
        self.clear_queued()
    }

    pub fn is_empty(&self) -> bool {
        self.queue.is_empty()
    }

    #[allow(clippy::unwrap_used)]
    pub fn paint(&mut self, pos: u32) -> Option<WalRingId> {
        let q = self.queue.get_mut(&pos).unwrap();
        #[allow(clippy::unwrap_used)]
        let (c, rid) = q.pop_front().unwrap();
        if q.is_empty() {
            self.queue.remove(&pos);
        }
        self.canvas[pos as usize] = c;
        if let Some(cnt) = self.waiting.get_mut(&rid) {
            *cnt -= 1;
            if *cnt == 0 {
                self.waiting.remove(&rid);
                Some(rid)
            } else {
                None
            }
        } else {
            None
        }
    }

    pub fn is_same(&self, other: &Canvas) -> bool {
        self.canvas.cmp(&other.canvas) == std::cmp::Ordering::Equal
    }

    pub fn print(&self, max_col: usize) {
        println!("# begin canvas");
        for r in self.canvas.chunks(max_col) {
            for c in r.iter() {
                print!("{:02x} ", c & 0xff);
            }
            println!();
        }
        println!("# end canvas");
    }
}

#[test]
fn test_canvas() {
    let mut rng = <rand::rngs::StdRng as rand::SeedableRng>::seed_from_u64(42);
    let mut canvas1 = Canvas::new(100);
    let mut canvas2 = Canvas::new(100);
    let canvas3 = Canvas::new(101);
    let dummy = WalRingId::empty_id();
    let s1 = PaintStrokes::gen_rand(100, 10, 256, 2, &mut rng);
    let s2 = PaintStrokes::gen_rand(100, 10, 256, 2, &mut rng);
    assert!(canvas1.is_same(&canvas2));
    assert!(!canvas2.is_same(&canvas3));
    canvas1.prepaint(&s1, &dummy);
    canvas1.prepaint(&s2, &dummy);
    canvas2.prepaint(&s1, &dummy);
    canvas2.prepaint(&s2, &dummy);
    assert!(canvas1.is_same(&canvas2));
    canvas1.rand_paint(&mut rng);
    assert!(!canvas1.is_same(&canvas2));
    while canvas1.rand_paint(&mut rng).is_some() {}
    while canvas2.rand_paint(&mut rng).is_some() {}
    assert!(canvas1.is_same(&canvas2));
    canvas1.print(10);
}

pub struct PaintingSim {
    pub block_nbit: u64,
    pub file_nbit: u64,
    pub file_cache: usize,
    /// number of PaintStrokes (WriteBatch)
    pub n: usize,
    /// number of strokes per PaintStrokes
    pub m: usize,
    /// number of scheduled ticks per PaintStroke submission
    pub k: usize,
    /// the size of canvas
    pub csize: usize,
    /// max length of a single stroke
    pub stroke_max_len: u32,
    /// max color value
    pub stroke_max_col: u32,
    /// max number of strokes per PaintStroke
    pub stroke_max_n: usize,
    /// random seed
    pub seed: u64,
}

impl PaintingSim {
    pub fn run<G: 'static + FailGen>(
        &self,
        state: &mut WalStoreEmulState,
        canvas: &mut Canvas,
        loader: WalLoader,
        ops: &mut Vec<PaintStrokes>,
        ringid_map: &mut HashMap<WalRingId, usize>,
        fgen: Rc<G>,
    ) -> Result<(), WalError> {
        let mut rng = <rand::rngs::StdRng as rand::SeedableRng>::seed_from_u64(self.seed);
        let mut wal = block_on(loader.load(
            WalStoreEmul::new(state, fgen.clone()),
            |_, _| {
                if fgen.next_fail() {
                    Err(WalError::Other("run fgen fail".to_string()))
                } else {
                    Ok(())
                }
            },
            0,
        ))?;
        for _ in 0..self.n {
            let pss = (0..self.m)
                .map(|_| {
                    PaintStrokes::gen_rand(
                        self.csize as u32,
                        self.stroke_max_len,
                        self.stroke_max_col,
                        rng.gen_range(1..self.stroke_max_n + 1),
                        &mut rng,
                    )
                })
                .collect::<Vec<PaintStrokes>>();
            let pss_ = pss.clone();
            // write ahead
            let rids = wal.grow(pss);
            assert_eq!(rids.len(), self.m);
            let recs = rids
                .into_iter()
                .zip(pss_.into_iter())
                .map(|(r, ps)| -> Result<_, _> {
                    ops.push(ps);
                    let (rec, rid) = futures::executor::block_on(r)
                        .map_err(|_| WalError::Other("paintstrokes executor error".to_string()))?;
                    ringid_map.insert(rid, ops.len() - 1);
                    Ok((rec, rid))
                })
                .collect::<Result<Vec<_>, WalError>>()?;
            // finish appending to Wal
            /*
            for rid in rids.iter() {
                println!("got ringid: {:?}", rid);
            }
            */
            // prepare data writes
            for (ps, rid) in recs.into_iter() {
                canvas.prepaint(&ps, &rid);
            }
            // run k ticks of the fine-grained scheduler
            for _ in 0..rng.gen_range(1..self.k) {
                // storage I/O could fail
                if fgen.next_fail() {
                    return Err(WalError::Other("run fgen fail: storage i/o".to_string()));
                }
                if let Some((fin_rid, _)) = canvas.rand_paint(&mut rng) {
                    if let Some(rid) = fin_rid {
                        futures::executor::block_on(wal.peel(&[rid], 0))?
                    }
                } else {
                    break;
                }
            }
        }
        //canvas.print(40);
        assert_eq!(wal.file_pool_in_use(), 0);
        Ok(())
    }

    pub fn get_walloader(&self) -> WalLoader {
        let mut loader = WalLoader::new();
        #[allow(clippy::unwrap_used)]
        loader
            .file_nbit(self.file_nbit)
            .block_nbit(self.block_nbit)
            .cache_size(std::num::NonZeroUsize::new(self.file_cache).unwrap());
        loader
    }

    pub fn get_nticks(&self, state: &mut WalStoreEmulState) -> usize {
        let mut canvas = Canvas::new(self.csize);
        let mut ops: Vec<PaintStrokes> = Vec::new();
        let mut ringid_map = HashMap::new();
        let fgen = Rc::new(CountFailGen::new());
        #[allow(clippy::unwrap_used)]
        self.run(
            state,
            &mut canvas,
            self.get_walloader(),
            &mut ops,
            &mut ringid_map,
            fgen.clone(),
        )
        .unwrap();
        fgen.get_count()
    }

    pub fn check(
        &self,
        state: &mut WalStoreEmulState,
        canvas: &mut Canvas,
        wal: WalLoader,
        ops: &[PaintStrokes],
        ringid_map: &HashMap<WalRingId, usize>,
    ) -> bool {
        if ops.is_empty() {
            return true;
        }
        let mut last_idx = 0;
        let mut napplied = 0;
        canvas.clear_queued();
        #[allow(clippy::unwrap_used)]
        block_on(wal.load(
            WalStoreEmul::new(state, Rc::new(ZeroFailGen)),
            |payload, ringid| {
                let s = PaintStrokes::from_bytes(&payload);
                canvas.prepaint(&s, &ringid);
                last_idx = *ringid_map.get(&ringid).unwrap() + 1;
                napplied += 1;
                Ok(())
            },
            0,
        ))
        .unwrap();
        println!("last = {}/{}, applied = {}", last_idx, ops.len(), napplied);
        canvas.paint_all();

        // recover complete
        let start_ops = if last_idx > 0 { &ops[..last_idx] } else { &[] };
        let canvas0 = canvas.new_reference(start_ops);

        if canvas.is_same(&canvas0) {
            return true;
        }

        if last_idx > 0 {
            canvas.print(40);
            canvas0.print(40);

            return false;
        }

        let (start_ops, end_ops) = ops.split_at(self.m);
        let mut canvas0 = canvas0.new_reference(start_ops);

        if canvas.is_same(&canvas0) {
            return true;
        }

        for op in end_ops {
            const EMPTY_ID: WalRingId = WalRingId::empty_id();

            canvas0.prepaint(op, &EMPTY_ID);
            canvas0.paint_all();

            if canvas.is_same(&canvas0) {
                return true;
            }
        }

        canvas.print(40);
        canvas0.print(40);

        false
    }

    pub fn new_canvas(&self) -> Canvas {
        Canvas::new(self.csize)
    }
}
