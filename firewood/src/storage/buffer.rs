// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Disk buffer for staging in memory pages and flushing them to disk.
use std::fmt::Debug;
use std::ops::IndexMut;
use std::os::fd::{AsFd, AsRawFd};
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::{cell::RefCell, collections::HashMap};

use super::{AshRecord, FilePool, Page, StoreDelta, StoreError, WalConfig, PAGE_SIZE_NBIT};
use crate::shale::SpaceId;
use crate::storage::DeltaPage;
use aiofut::{AioBuilder, AioError, AioManager};
use futures::future::join_all;
use growthring::{
    wal::{RecoverPolicy, WalLoader, WalWriter},
    walerror::WalError,
    WalFileImpl, WalStoreImpl,
};
use tokio::{
    sync::{
        mpsc,
        oneshot::{self, error::RecvError},
        Mutex, Notify, Semaphore,
    },
    task,
};
use typed_builder::TypedBuilder;

#[derive(Debug)]
pub enum BufferCmd {
    /// Initialize the Wal.
    InitWal(PathBuf, String),
    /// Process a write batch against the underlying store.
    WriteBatch(Vec<BufferWrite>, AshRecord),
    /// Get a page from the disk buffer.
    GetPage((SpaceId, u64), oneshot::Sender<Option<Page>>),
    CollectAsh(usize, oneshot::Sender<Vec<AshRecord>>),
    /// Register a new space and add the files to a memory mapped pool.
    RegCachedSpace(SpaceId, Arc<FilePool>),
    /// Returns false if the
    Shutdown,
}

/// Config for the disk buffer.
#[derive(TypedBuilder, Clone, Debug)]
pub struct DiskBufferConfig {
    /// Maximum number of pending pages.
    #[builder(default = 65536)] // 256MB total size by default
    pub max_pending: usize,
    /// Maximum number of concurrent async I/O requests.
    #[builder(default = 1024)]
    pub max_aio_requests: u32,
    /// Maximum number of async I/O responses that it polls for at a time.
    #[builder(default = 128)]
    pub max_aio_response: u16,
    /// Maximum number of async I/O requests per submission.
    #[builder(default = 128)]
    pub max_aio_submit: usize,
    /// Maximum number of concurrent async I/O requests in Wal.
    #[builder(default = 256)]
    pub wal_max_aio_requests: usize,
    /// Maximum buffered Wal records.
    #[builder(default = 1024)]
    pub wal_max_buffered: usize,
    /// Maximum batched Wal records per write.
    #[builder(default = 4096)]
    pub wal_max_batch: usize,
}

/// List of pages to write to disk.
#[derive(Debug)]
pub struct BufferWrite {
    pub space_id: SpaceId,
    pub delta: StoreDelta,
}

#[derive(Debug)]
struct PendingPage {
    staging_data: Page,
    file_nbit: u64,
    notifiers: Notifiers,
}

#[derive(Debug)]
struct Notifiers {
    staging: Vec<Rc<Semaphore>>,
    writing: Vec<Rc<Semaphore>>,
}

impl Notifiers {
    /// adds one permit to earch semaphore clone and leaves an empty `Vec` for `writing`
    fn drain_writing(&mut self) {
        std::mem::take(&mut self.writing)
            .into_iter()
            .for_each(|notifier| notifier.add_permits(1))
    }

    /// takes all staging semaphores and moves them to writing, leaving an `Vec` for `staging`
    fn staging_to_writing(&mut self) {
        self.writing = std::mem::take(&mut self.staging);
    }
}

/// Responsible for processing [`BufferCmd`]s from the [`DiskBufferRequester`]
/// and managing the persistence of pages.
pub struct DiskBuffer {
    inbound: mpsc::UnboundedReceiver<BufferCmd>,
    aiomgr: AioManager,
    cfg: DiskBufferConfig,
    wal_cfg: WalConfig,
}

impl DiskBuffer {
    /// Create a new aio managed disk buffer.
    pub fn new(
        inbound: mpsc::UnboundedReceiver<BufferCmd>,
        cfg: &DiskBufferConfig,
        wal: &WalConfig,
    ) -> Result<Self, AioError> {
        let aiomgr = AioBuilder::default()
            .max_events(cfg.max_aio_requests)
            .max_nwait(cfg.max_aio_response)
            .max_nbatched(cfg.max_aio_submit)
            .build()
            .map_err(|_| AioError::OtherError)?;

        Ok(Self {
            cfg: cfg.clone(),
            inbound,
            aiomgr,
            wal_cfg: wal.clone(),
        })
    }

    #[tokio::main(flavor = "current_thread")]
    pub async fn run(self) {
        let mut inbound = self.inbound;
        let aiomgr = Rc::new(self.aiomgr);
        let cfg = self.cfg;
        let wal_cfg = self.wal_cfg;

        let pending_writes = Rc::new(RefCell::new(HashMap::new()));
        let file_pools = Rc::new(RefCell::new(std::array::from_fn(|_| None)));
        let local_pool = tokio::task::LocalSet::new();

        let max = WalQueueMax {
            batch: cfg.wal_max_batch,
            revisions: wal_cfg.max_revisions,
            pending: cfg.max_pending,
        };

        let (wal_in, writes) = mpsc::channel(cfg.wal_max_buffered);

        let mut writes = Some(writes);
        let mut wal = None;

        let notifier = Rc::new(Notify::new());

        local_pool
            // everything needs to be moved into this future in order to be properly dropped
            .run_until(async move {
                loop {
                    // can't hold the borrowed `pending_writes` across the .await point inside the if-statement
                    let pending_len = pending_writes.borrow().len();

                    if pending_len >= cfg.max_pending {
                        notifier.notified().await;
                    }

                    // process the the request
                    #[allow(clippy::unwrap_used)]
                    let process_result = process(
                        pending_writes.clone(),
                        notifier.clone(),
                        file_pools.clone(),
                        aiomgr.clone(),
                        &mut wal,
                        &wal_cfg,
                        inbound.recv().await.unwrap(),
                        max,
                        wal_in.clone(),
                        &mut writes,
                    )
                    .await;

                    // stop handling new requests and exit the loop
                    if !process_result {
                        break;
                    }
                }
            })
            .await;

        // when finished process all requests, wait for any pending-futures to complete
        local_pool.await;
    }
}

#[derive(Clone, Copy)]
struct WalQueueMax {
    batch: usize,
    revisions: u32,
    pending: usize,
}

/// Add an pending pages to aio manager for processing by the local pool.
fn schedule_write(
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    fc_notifier: Rc<Notify>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    aiomgr: Rc<AioManager>,
    max: WalQueueMax,
    page_key: (SpaceId, u64),
) {
    use std::collections::hash_map::Entry::*;

    let fut = {
        let pending = pending.borrow();
        #[allow(clippy::unwrap_used)]
        let p = pending.get(&page_key).unwrap();
        let offset = page_key.1 << PAGE_SIZE_NBIT;
        let fid = offset >> p.file_nbit;
        let fmask = (1 << p.file_nbit) - 1;
        #[allow(clippy::unwrap_used)]
        #[allow(clippy::indexing_slicing)]
        let file = file_pools.borrow()[page_key.0 as usize]
            .as_ref()
            .unwrap()
            .get_file(fid)
            .unwrap();
        aiomgr.write(
            file.as_raw_fd(),
            offset & fmask,
            p.staging_data.clone(),
            None,
        )
    };

    let task = {
        async move {
            let (res, _) = fut.await;
            #[allow(clippy::unwrap_used)]
            res.unwrap();

            let pending_len = pending.borrow().len();

            let write_again = match pending.borrow_mut().entry(page_key) {
                Occupied(mut e) => {
                    let slot = e.get_mut();

                    slot.notifiers.drain_writing();

                    // if staging is empty, all we need to do is notify any potential waiters
                    if slot.notifiers.staging.is_empty() {
                        e.remove();

                        if pending_len < max.pending {
                            fc_notifier.notify_one();
                        }

                        false
                    } else {
                        // if `staging` is not empty, move all semaphores to `writing` and recurse
                        // to schedule the new writes.
                        slot.notifiers.staging_to_writing();

                        true
                    }
                }
                _ => unreachable!(),
            };

            if write_again {
                schedule_write(pending, fc_notifier, file_pools, aiomgr, max, page_key);
            }
        }
    };

    task::spawn_local(task);
}

/// Initialize the Wal subsystem if it does not exists and attempts to replay the Wal if exists.
async fn init_wal(
    file_pools: &Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    store: WalStoreImpl,
    loader: WalLoader,
    max_revisions: u32,
    final_path: &Path,
) -> Result<Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>, WalError> {
    let wal = loader
        .load(
            store,
            |raw, _| {
                let batch = AshRecord::deserialize(raw);

                for (space_id, ash) in batch.0 {
                    for (undo, redo) in ash.iter() {
                        let offset = undo.offset;
                        let file_pools = file_pools.borrow();
                        #[allow(clippy::unwrap_used)]
                        #[allow(clippy::indexing_slicing)]
                        let file_pool = file_pools[space_id as usize].as_ref().unwrap();
                        let file_nbit = file_pool.get_file_nbit();
                        let file_mask = (1 << file_nbit) - 1;
                        let fid = offset >> file_nbit;

                        nix::sys::uio::pwrite(
                            file_pool
                                .get_file(fid)
                                .map_err(|e| {
                                    WalError::Other(format!(
                                        "file pool error: {:?} - final path {:?}",
                                        e, final_path
                                    ))
                                })?
                                .as_fd(),
                            &redo.data,
                            (offset & file_mask) as nix::libc::off_t,
                        )
                        .map_err(|e| {
                            WalError::Other(format!(
                                "wal loader error: {:?} - final path {:?}",
                                e, final_path
                            ))
                        })?;
                    }
                }

                Ok(())
            },
            max_revisions,
        )
        .await?;

    Ok(Rc::new(Mutex::new(wal)))
}

async fn run_wal_queue(
    max: WalQueueMax,
    wal: Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>,
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    mut writes: mpsc::Receiver<(Vec<BufferWrite>, AshRecord)>,
    fc_notifier: Rc<Notify>,
    aiomgr: Rc<AioManager>,
) {
    use std::collections::hash_map::Entry::*;

    loop {
        let mut bwrites = Vec::new();
        let mut records = Vec::new();
        let wal = wal.clone();

        if let Some((bw, ac)) = writes.recv().await {
            records.push(ac);
            bwrites.extend(bw);
        } else {
            break;
        }

        while let Ok((bw, ac)) = writes.try_recv() {
            records.push(ac);
            bwrites.extend(bw);

            if records.len() >= max.batch {
                break;
            }
        }

        // first write to Wal
        #[allow(clippy::unwrap_used)]
        let ring_ids = join_all(wal.clone().lock().await.grow(records))
            .await
            .into_iter()
            .map(|ring| ring.map_err(|_| "Wal Error while writing").unwrap().1)
            .collect::<Vec<_>>();
        let sem = Rc::new(tokio::sync::Semaphore::new(0));
        let mut npermit = 0;

        for BufferWrite { space_id, delta } in bwrites {
            for DeltaPage(page_id, page) in delta.0 {
                let page_key = (space_id, page_id);

                let should_write = match pending.borrow_mut().entry(page_key) {
                    Occupied(mut e) => {
                        let e = e.get_mut();
                        e.staging_data = page;
                        e.notifiers.staging.push(sem.clone());

                        false
                    }
                    Vacant(e) => {
                        #[allow(clippy::unwrap_used)]
                        #[allow(clippy::indexing_slicing)]
                        let file_nbit = file_pools.borrow()[page_key.0 as usize]
                            .as_ref()
                            .unwrap()
                            .file_nbit;

                        e.insert(PendingPage {
                            staging_data: page,
                            file_nbit,
                            notifiers: {
                                let semaphore = sem.clone();
                                Notifiers {
                                    staging: Vec::new(),
                                    writing: vec![semaphore],
                                }
                            },
                        });

                        true
                    }
                };

                if should_write {
                    schedule_write(
                        pending.clone(),
                        fc_notifier.clone(),
                        file_pools.clone(),
                        aiomgr.clone(),
                        max,
                        page_key,
                    );
                }

                npermit += 1;
            }
        }

        let task = async move {
            #[allow(clippy::unwrap_used)]
            let _ = sem.acquire_many(npermit).await.unwrap();

            #[allow(clippy::unwrap_used)]
            wal.lock()
                .await
                .peel(ring_ids, max.revisions)
                .await
                .map_err(|_| "Wal errored while pruning")
                .unwrap()
        };

        task::spawn_local(task);
    }

    // if this function breaks for any reason, make sure there is no one waiting for staged writes.
    fc_notifier.notify_one();
}

fn panic_on_intialization_failure_with<'a, T>(
    rootpath: &'a Path,
    waldir: &'a str,
) -> impl Fn(WalError) -> T + 'a {
    move |e| panic!("Initialize Wal in dir {rootpath:?} failed creating {waldir:?}: {e:?}")
}

#[allow(clippy::too_many_arguments)]
async fn process(
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    fc_notifier: Rc<Notify>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    aiomgr: Rc<AioManager>,
    wal: &mut Option<Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>>,
    wal_cfg: &WalConfig,
    req: BufferCmd,
    max: WalQueueMax,
    wal_in: mpsc::Sender<(Vec<BufferWrite>, AshRecord)>,
    writes: &mut Option<mpsc::Receiver<(Vec<BufferWrite>, AshRecord)>>,
) -> bool {
    match req {
        BufferCmd::Shutdown => return false,
        BufferCmd::InitWal(rootpath, waldir) => {
            let final_path = rootpath.join(&waldir);

            let store = WalStoreImpl::new(final_path.clone(), false)
                .unwrap_or_else(panic_on_intialization_failure_with(&rootpath, &waldir));

            let mut loader = WalLoader::new();
            loader
                .file_nbit(wal_cfg.file_nbit)
                .block_nbit(wal_cfg.block_nbit)
                .recover_policy(RecoverPolicy::Strict);

            let initialized_wal = init_wal(
                &file_pools,
                store,
                loader,
                wal_cfg.max_revisions,
                final_path.as_path(),
            )
            .await
            .unwrap_or_else(panic_on_intialization_failure_with(&rootpath, &waldir));

            wal.replace(initialized_wal.clone());

            #[allow(clippy::unwrap_used)]
            let writes = writes.take().unwrap();

            let task = run_wal_queue(
                max,
                initialized_wal,
                pending,
                file_pools.clone(),
                writes,
                fc_notifier,
                aiomgr,
            );

            task::spawn_local(task);
        }

        #[allow(clippy::unwrap_used)]
        BufferCmd::GetPage(page_key, tx) => tx
            .send(
                pending
                    .borrow()
                    .get(&page_key)
                    .map(|e| e.staging_data.clone()),
            )
            .unwrap(),
        BufferCmd::WriteBatch(writes, wal_writes) => {
            #[allow(clippy::unwrap_used)]
            wal_in.send((writes, wal_writes)).await.unwrap();
        }
        BufferCmd::CollectAsh(nrecords, tx) => {
            // wait to ensure writes are paused for Wal
            #[allow(clippy::unwrap_used)]
            let ash = wal
                .as_ref()
                .unwrap()
                .lock()
                .await
                .read_recent_records(nrecords, &RecoverPolicy::Strict)
                .await
                .unwrap()
                .into_iter()
                .map(AshRecord::deserialize)
                .collect();
            #[allow(clippy::unwrap_used)]
            tx.send(ash).unwrap();
        }
        BufferCmd::RegCachedSpace(space_id, files) => {
            file_pools
                .borrow_mut()
                .as_mut_slice()
                .index_mut(space_id as usize)
                .replace(files);
        }
    }

    true
}

/// Communicates with the [`DiskBuffer`] over channels.
#[cfg_attr(doc, aquamarine::aquamarine)]
/// ```mermaid
/// graph LR
///     s([Caller]) --> a[[DiskBufferRequester]]
///     r[[DiskBuffer]] --> f([Disk])
///     subgraph rustc[Thread]
///         r
///     end
///     subgraph rustc[Thread]
///     a -. BufferCmd .-> r
///     end
/// ```
#[derive(Clone, Debug)]
pub struct DiskBufferRequester {
    sender: mpsc::UnboundedSender<BufferCmd>,
}

impl DiskBufferRequester {
    /// Create a new requester.
    pub const fn new(sender: mpsc::UnboundedSender<BufferCmd>) -> Self {
        Self { sender }
    }

    /// Get a page from the buffer.
    pub fn get_page(&self, space_id: SpaceId, page_id: u64) -> Option<Page> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .send(BufferCmd::GetPage((space_id, page_id), resp_tx))
            .map_err(StoreError::Send)
            .ok();
        #[allow(clippy::unwrap_used)]
        resp_rx.blocking_recv().unwrap()
    }

    /// Sends a batch of writes to the buffer.
    pub fn write(&self, page_batch: Vec<BufferWrite>, write_batch: AshRecord) {
        self.sender
            .send(BufferCmd::WriteBatch(page_batch, write_batch))
            .map_err(StoreError::Send)
            .ok();
    }

    pub fn shutdown(&self) {
        #[allow(clippy::unwrap_used)]
        self.sender.send(BufferCmd::Shutdown).ok().unwrap()
    }

    /// Initialize the Wal.
    pub fn init_wal(&self, waldir: &str, rootpath: &Path) {
        self.sender
            .send(BufferCmd::InitWal(
                rootpath.to_path_buf(),
                waldir.to_string(),
            ))
            .map_err(StoreError::Send)
            .ok();
    }

    /// Collect the last N records from the Wal.
    pub fn collect_ash(&self, nrecords: usize) -> Result<Vec<AshRecord>, StoreError<RecvError>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .send(BufferCmd::CollectAsh(nrecords, resp_tx))
            .map_err(StoreError::Send)
            .ok();
        resp_rx.blocking_recv().map_err(StoreError::Receive)
    }

    /// Register a cached space to the buffer.
    pub fn reg_cached_space(&self, space_id: SpaceId, files: Arc<FilePool>) {
        self.sender
            .send(BufferCmd::RegCachedSpace(space_id, files))
            .map_err(StoreError::Send)
            .ok();
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
#[allow(clippy::indexing_slicing)]
mod tests {
    use sha3::Digest;
    use std::path::{Path, PathBuf};
    use tokio::task::block_in_place;

    use super::*;
    use crate::shale::CachedStore;
    use crate::{
        file,
        storage::{
            Ash, CachedSpace, DeltaPage, MemStoreR, StoreConfig, StoreRevMut, StoreRevMutDelta,
            StoreRevShared, ZeroStore,
        },
    };

    const STATE_SPACE: SpaceId = 0x0;
    const HASH_SIZE: usize = 32;

    fn get_tmp_dir() -> PathBuf {
        option_env!("CARGO_TARGET_TMPDIR")
            .map(Into::into)
            .unwrap_or(std::env::temp_dir())
            .join("firewood")
    }

    fn new_cached_space_for_test(
        state_path: PathBuf,
        disk_requester: DiskBufferRequester,
    ) -> Arc<CachedSpace> {
        CachedSpace::new(
            &StoreConfig::builder()
                .ncached_pages(1)
                .ncached_files(1)
                .space_id(STATE_SPACE)
                .file_nbit(1)
                .rootdir(state_path)
                .build(),
            disk_requester,
        )
        .unwrap()
        .into()
    }

    #[tokio::test]
    #[ignore = "ref: https://github.com/ava-labs/firewood/issues/45"]
    async fn test_buffer_with_undo() {
        let temp_dir = get_tmp_dir();

        let buf_cfg = DiskBufferConfig::builder().build();
        let wal_cfg = WalConfig::builder().build();
        let disk_requester = init_buffer(buf_cfg, wal_cfg);

        // TODO: Run the test in a separate standalone directory for concurrency reasons
        let (root_db_path, reset) =
            file::open_dir(temp_dir.as_path(), file::Options::Truncate).unwrap();

        // file descriptor of the state directory
        let state_path = file::touch_dir("state", &root_db_path).unwrap();
        assert!(reset);
        // create a new wal directory on top of root_db_fd
        disk_requester.init_wal("wal", &root_db_path);

        // create a new state cache which tracks on disk state.
        let state_cache = new_cached_space_for_test(state_path, disk_requester.clone());

        // add an in memory cached space. this will allow us to write to the
        // disk buffer then later persist the change to disk.
        disk_requester.reg_cached_space(state_cache.id(), state_cache.inner.read().files.clone());

        // memory mapped store
        let mut mut_store = StoreRevMut::new(state_cache);

        let change = b"this is a test";

        // write to the in memory buffer not to disk
        mut_store.write(0, change);
        assert_eq!(mut_store.id(), STATE_SPACE);

        // mutate the in memory buffer.
        let change = b"this is another test";

        // write to the in memory buffer (ash) not yet to disk
        mut_store.write(0, change);
        assert_eq!(mut_store.id(), STATE_SPACE);

        // wal should have no records.
        assert!(disk_requester.collect_ash(1).unwrap().is_empty());

        // get RO view of the buffer from the beginning.
        let view = mut_store.get_view(0, change.len() as u64).unwrap();
        assert_eq!(view.as_deref(), change);

        let (page_batch, write_batch) = create_batches(&mut_store);

        // create a mutation request to the disk buffer by passing the page and write batch.
        let d1 = disk_requester.clone();
        let write_thread_handle = std::thread::spawn(move || {
            // wal is empty
            assert!(d1.collect_ash(1).unwrap().is_empty());
            // page is not yet persisted to disk.
            assert!(d1.get_page(STATE_SPACE, 0).is_none());
            d1.write(page_batch, write_batch);
        });
        // wait for the write to complete.
        write_thread_handle.join().unwrap();
        // This is not ACID compliant, write should not return before Wal log
        // is written to disk.

        let log_file = temp_dir
            .parent()
            .unwrap()
            .join("sender_api_test_db")
            .join("wal")
            .join("00000000.log");
        assert!(log_file.exists());

        // verify
        assert_eq!(disk_requester.collect_ash(1).unwrap().len(), 1);
    }

    #[tokio::test]
    #[ignore = "ref: https://github.com/ava-labs/firewood/issues/45"]
    async fn test_buffer_with_redo() {
        let buf_cfg = DiskBufferConfig::builder().build();
        let wal_cfg = WalConfig::builder().build();
        let disk_requester = init_buffer(buf_cfg, wal_cfg);

        // TODO: Run the test in a separate standalone directory for concurrency reasons
        let tmp_dir = get_tmp_dir();
        let path = get_file_path(tmp_dir.as_path(), file!(), line!());
        let (root_db_path, reset) = file::open_dir(path, file::Options::Truncate).unwrap();

        // file descriptor of the state directory
        let state_path = file::touch_dir("state", &root_db_path).unwrap();
        assert!(reset);
        // create a new wal directory on top of root_db_fd
        disk_requester.init_wal("wal", &root_db_path);

        // create a new state cache which tracks on disk state.
        let state_cache = new_cached_space_for_test(state_path, disk_requester.clone());

        // add an in memory cached space. this will allow us to write to the
        // disk buffer then later persist the change to disk.
        disk_requester.reg_cached_space(state_cache.id(), state_cache.clone_files());

        // memory mapped store
        let mut mut_store = StoreRevMut::new(state_cache.clone());

        // mutate the in memory buffer.
        let data = b"this is another test";
        let hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(data).into();

        // write to the in memory buffer (ash) not yet to disk
        mut_store.write(0, &hash);
        assert_eq!(mut_store.id(), STATE_SPACE);

        // wal should have no records.
        assert!(disk_requester.collect_ash(1).unwrap().is_empty());

        // get RO view of the buffer from the beginning.
        let view = mut_store.get_view(0, hash.len() as u64).unwrap();
        assert_eq!(view.as_deref(), hash);

        // Commit the change. Take the delta from cached store,
        // then apply changes to the CachedSpace.
        let (redo_delta, wal) = mut_store.delta();
        state_cache.update(&redo_delta).unwrap();

        // create a mutation request to the disk buffer by passing the page and write batch.
        // wal is empty
        assert!(disk_requester.collect_ash(1).unwrap().is_empty());
        // page is not yet persisted to disk.
        assert!(disk_requester.get_page(STATE_SPACE, 0).is_none());
        disk_requester.write(
            vec![BufferWrite {
                space_id: STATE_SPACE,
                delta: redo_delta,
            }],
            AshRecord([(STATE_SPACE, wal)].into()),
        );

        // verify
        assert_eq!(disk_requester.collect_ash(1).unwrap().len(), 1);
        let ashes = disk_requester.collect_ash(1).unwrap();

        // replay the redo from the wal
        let shared_store = StoreRevShared::from_ash(
            Arc::new(ZeroStore::default()),
            &ashes[0].0[&STATE_SPACE].redo,
        );
        let view = shared_store.get_view(0, hash.len() as u64).unwrap();
        assert_eq!(view.as_deref(), hash);
    }

    #[tokio::test(flavor = "multi_thread")]
    async fn test_multi_stores() {
        let buf_cfg = DiskBufferConfig::builder().build();
        let wal_cfg = WalConfig::builder().build();
        let disk_requester = init_buffer(buf_cfg, wal_cfg);

        let tmp_dir = get_tmp_dir();
        let path = get_file_path(tmp_dir.as_path(), file!(), line!());
        std::fs::create_dir_all(&path).unwrap();
        let (root_db_path, reset) = file::open_dir(path, file::Options::Truncate).unwrap();

        // file descriptor of the state directory
        let state_path = file::touch_dir("state", &root_db_path).unwrap();
        assert!(reset);
        // create a new wal directory on top of root_db_fd
        disk_requester.init_wal("wal", &root_db_path);

        // create a new state cache which tracks on disk state.
        let state_cache: Arc<CachedSpace> = CachedSpace::new(
            &StoreConfig::builder()
                .ncached_pages(1)
                .ncached_files(1)
                .space_id(STATE_SPACE)
                .file_nbit(1)
                .rootdir(state_path)
                .build(),
            disk_requester.clone(),
        )
        .unwrap()
        .into();

        // add an in memory cached space. this will allow us to write to the
        // disk buffer then later persist the change to disk.
        disk_requester.reg_cached_space(state_cache.id(), state_cache.clone_files());

        // memory mapped store
        let mut store = StoreRevMut::new(state_cache.clone());

        // mutate the in memory buffer.
        let data = b"this is a test";
        let hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(data).into();
        block_in_place(|| store.write(0, &hash));
        assert_eq!(store.id(), STATE_SPACE);

        let another_data = b"this is another test";
        let another_hash: [u8; HASH_SIZE] = sha3::Keccak256::digest(another_data).into();

        // mutate the in memory buffer in another StoreRev new from the above.
        let mut another_store = StoreRevMut::new_from_other(&store);
        block_in_place(|| another_store.write(32, &another_hash));
        assert_eq!(another_store.id(), STATE_SPACE);

        // wal should have no records.
        assert!(block_in_place(|| disk_requester.collect_ash(1))
            .unwrap()
            .is_empty());

        // get RO view of the buffer from the beginning. Both stores should have the same view.
        let view = store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), hash);

        let view = another_store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), hash);

        // get RO view of the buffer from the second hash. Only the new store should see the value.
        let view = another_store.get_view(32, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), another_hash);
        let empty: [u8; HASH_SIZE] = [0; HASH_SIZE];
        let view = store.get_view(32, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), empty);

        // Overwrite the value from the beginning in the new store.  Only the new store should see the change.
        another_store.write(0, &another_hash);
        let view = another_store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), another_hash);
        let view = store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), hash);

        // Commit the change. Take the delta from both stores.
        let (redo_delta, wal) = store.delta();
        assert_eq!(1, redo_delta.0.len());
        assert_eq!(1, wal.undo.len());

        let (another_redo_delta, another_wal) = another_store.delta();
        assert_eq!(1, another_redo_delta.0.len());
        assert_eq!(2, another_wal.undo.len());

        // Verify after the changes been applied to underlying CachedSpace,
        // the newly created stores should see the previous changes.
        state_cache.update(&redo_delta).unwrap();
        let store = StoreRevMut::new(state_cache.clone());
        let view = store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), hash);

        state_cache.update(&another_redo_delta).unwrap();
        let another_store = StoreRevMut::new(state_cache);
        let view = another_store.get_view(0, HASH_SIZE as u64).unwrap();
        assert_eq!(view.as_deref(), another_hash);
        block_in_place(|| disk_requester.shutdown());
    }

    fn get_file_path(path: &Path, file: &str, line: u32) -> PathBuf {
        path.join(format!("{}_{}", file.replace('/', "-"), line))
    }

    fn init_buffer(buf_cfg: DiskBufferConfig, wal_cfg: WalConfig) -> DiskBufferRequester {
        let (sender, inbound) = tokio::sync::mpsc::unbounded_channel();
        let disk_requester = DiskBufferRequester::new(sender);
        std::thread::spawn(move || {
            let disk_buffer = DiskBuffer::new(inbound, &buf_cfg, &wal_cfg).unwrap();
            disk_buffer.run()
        });
        disk_requester
    }

    fn create_batches(rev_mut: &StoreRevMut) -> (Vec<BufferWrite>, AshRecord) {
        let deltas = std::mem::replace(
            &mut *rev_mut.deltas.write(),
            StoreRevMutDelta {
                pages: HashMap::new(),
                plain: Ash::default(),
            },
        );

        // create a list of delta pages from existing in memory data.
        let mut pages = Vec::new();
        for (pid, page) in deltas.pages.into_iter() {
            pages.push(DeltaPage(pid, page));
        }
        pages.sort_by_key(|p| p.0);

        let page_batch = vec![BufferWrite {
            space_id: STATE_SPACE,
            delta: StoreDelta(pages),
        }];

        let write_batch = AshRecord([(STATE_SPACE, deltas.plain)].into());
        (page_batch, write_batch)
    }
}
