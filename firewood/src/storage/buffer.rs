//! Disk buffer for staging in memory pages and flushing them to disk.
use std::fmt::Debug;
use std::ops::IndexMut;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::atomic::AtomicU64;
use std::sync::atomic::Ordering::Relaxed;
use std::sync::Arc;
use std::{cell::RefCell, collections::HashMap};

use crate::storage::DeltaPage;

use super::{AshRecord, FilePool, Page, StoreDelta, StoreError, WalConfig, PAGE_SIZE_NBIT};

use aiofut::{AioBuilder, AioError, AioManager};
use futures::future::join_all;
use growthring::WalFileImpl;
use growthring::{
    wal::{RecoverPolicy, WalLoader, WalWriter},
    walerror::WalError,
    WalStoreImpl,
};
use shale::SpaceId;
use tokio::sync::oneshot::error::RecvError;
use tokio::sync::{mpsc, oneshot, Mutex, Semaphore};
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
    /// Maximum buffered disk buffer commands.
    #[builder(default = 4096)]
    pub max_buffered: usize,
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
/// and managing the persistance of pages.
pub struct DiskBuffer {
    inbound: mpsc::Receiver<BufferCmd>,
    aiomgr: AioManager,
    cfg: DiskBufferConfig,
    wal_cfg: WalConfig,
}

impl DiskBuffer {
    /// Create a new aio managed disk buffer.
    pub fn new(
        inbound: mpsc::Receiver<BufferCmd>,
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
        // TODO: call local_pool.await to make sure that all pending futures finish instead of using the `tasks` HashMap.
        static TASK_ID: AtomicU64 = AtomicU64::new(0);

        let mut inbound = self.inbound;
        let aiomgr = Rc::new(self.aiomgr);
        let cfg = self.cfg;
        let wal_cfg = self.wal_cfg;

        let pending = Rc::new(RefCell::new(HashMap::new()));
        let file_pools = Rc::new(RefCell::new(std::array::from_fn(|_| None)));
        let local_pool = Rc::new(tokio::task::LocalSet::new());
        let tasks = Rc::new(RefCell::new(HashMap::new()));

        let max = WalQueueMax {
            batch: cfg.wal_max_batch,
            revisions: wal_cfg.max_revisions,
            pending: cfg.max_pending,
        };

        let (wal_in, writes) = mpsc::channel(cfg.wal_max_buffered);

        let mut writes = Some(writes);
        let mut wal = None;

        let notifier = Rc::new(tokio::sync::Notify::new());

        local_pool
            .run_until(async {
                loop {
                    let pending_len = pending.borrow().len();
                    if pending_len >= cfg.max_pending {
                        notifier.notified().await;
                    }

                    let req = inbound.recv().await.unwrap();

                    let process_result = process(
                        pending.clone(),
                        notifier.clone(),
                        file_pools.clone(),
                        aiomgr.clone(),
                        local_pool.clone(),
                        tasks.clone(),
                        &mut wal,
                        &cfg,
                        &wal_cfg,
                        req,
                        max,
                        wal_in.clone(),
                        &TASK_ID,
                        &mut writes,
                    )
                    .await;

                    if !process_result {
                        break;
                    }
                }

                drop(wal_in);

                let handles: Vec<_> = tasks
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
#[derive(Clone, Copy)]
struct WalQueueMax {
    batch: usize,
    revisions: u32,
    pending: usize,
}

/// Processes an async AIOFuture request against the local pool.
fn start_task<F: std::future::Future<Output = ()> + 'static>(
    task_id: &AtomicU64,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    local_pool: Rc<tokio::task::LocalSet>,
    fut: F,
) {
    let key = task_id.fetch_add(1, Relaxed);
    let handle = local_pool.spawn_local({
        let tasks = tasks.clone();

        async move {
            fut.await;
            tasks.borrow_mut().remove(&key);
        }
    });

    tasks.borrow_mut().insert(key, handle.into());
}

/// Add an pending pages to aio manager for processing by the local pool.
#[allow(clippy::too_many_arguments)]
fn schedule_write(
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    fc_notifier: Rc<tokio::sync::Notify>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    aiomgr: Rc<AioManager>,
    local_pool: Rc<tokio::task::LocalSet>,
    task_id: &'static AtomicU64,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    max: WalQueueMax,
    page_key: (SpaceId, u64),
) {
    use std::collections::hash_map::Entry::*;

    let fut = {
        let pending = pending.borrow();
        let p = pending.get(&page_key).unwrap();
        let offset = page_key.1 << PAGE_SIZE_NBIT;
        let fid = offset >> p.file_nbit;
        let fmask = (1 << p.file_nbit) - 1;
        let file = file_pools.borrow()[page_key.0 as usize]
            .as_ref()
            .unwrap()
            .get_file(fid)
            .unwrap();
        aiomgr.write(file.get_fd(), offset & fmask, p.staging_data.clone(), None)
    };

    let task = {
        let tasks = tasks.clone();
        let local_pool = local_pool.clone();

        async move {
            let (res, _) = fut.await;
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
                            fc_notifier.notify_waiters();
                        }

                        false
                    } else {
                        // if `staging` is not empty, move all semaphors to `writing` and recurse
                        // to schedule the new writes.
                        slot.notifiers.staging_to_writing();

                        true
                    }
                }
                _ => unreachable!(),
            };

            if write_again {
                schedule_write(
                    pending,
                    fc_notifier,
                    file_pools,
                    aiomgr,
                    local_pool,
                    task_id,
                    tasks,
                    max,
                    page_key,
                );
            }
        }
    };

    start_task(task_id, tasks, local_pool, task)
}

/// Initialize the Wal subsystem if it does not exists and attempts to replay the Wal if exists.
async fn init_wal(
    file_pools: &Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    cfg: &DiskBufferConfig,
    wal_cfg: &WalConfig,
    rootpath: &Path,
    waldir: &str,
) -> Result<Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>, WalError> {
    let final_path = rootpath.join(waldir);
    let mut aiobuilder = AioBuilder::default();
    aiobuilder.max_events(cfg.wal_max_aio_requests as u32);
    let store = WalStoreImpl::new(final_path.clone(), false)?;

    let mut loader = WalLoader::new();
    loader
        .file_nbit(wal_cfg.file_nbit)
        .block_nbit(wal_cfg.block_nbit)
        .recover_policy(RecoverPolicy::Strict);

    let wal = loader
        .load(
            store,
            |raw, _| {
                let batch = AshRecord::deserialize(raw);

                for (space_id, ash) in batch.0 {
                    for (undo, redo) in ash.iter() {
                        let offset = undo.offset;
                        let file_pools = file_pools.borrow();
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
                                .get_fd(),
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
            wal_cfg.max_revisions,
        )
        .await?;

    Ok(Rc::new(Mutex::new(wal)))
}

#[allow(clippy::too_many_arguments)]
async fn run_wal_queue(
    task_id: &'static AtomicU64,
    max: WalQueueMax,
    wal: Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>,
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    local_pool: Rc<tokio::task::LocalSet>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    mut writes: mpsc::Receiver<(Vec<BufferWrite>, AshRecord)>,
    fc_notifier: Rc<tokio::sync::Notify>,
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
                        local_pool.clone(),
                        task_id,
                        tasks.clone(),
                        max,
                        page_key,
                    );
                }

                npermit += 1;
            }
        }

        let task = async move {
            let _ = sem.acquire_many(npermit).await.unwrap();

            wal.lock()
                .await
                .peel(ring_ids, max.revisions)
                .await
                .map_err(|_| "Wal errore while pruning")
                .unwrap()
        };

        start_task(task_id, tasks.clone(), local_pool.clone(), task);
    }
}

#[allow(clippy::too_many_arguments)]
async fn process(
    pending: Rc<RefCell<HashMap<(SpaceId, u64), PendingPage>>>,
    fc_notifier: Rc<tokio::sync::Notify>,
    file_pools: Rc<RefCell<[Option<Arc<FilePool>>; 255]>>,
    aiomgr: Rc<AioManager>,
    local_pool: Rc<tokio::task::LocalSet>,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    wal: &mut Option<Rc<Mutex<WalWriter<WalFileImpl, WalStoreImpl>>>>,
    cfg: &DiskBufferConfig,
    wal_cfg: &WalConfig,
    req: BufferCmd,
    max: WalQueueMax,
    wal_in: mpsc::Sender<(Vec<BufferWrite>, AshRecord)>,
    task_id: &'static AtomicU64,
    writes: &mut Option<mpsc::Receiver<(Vec<BufferWrite>, AshRecord)>>,
) -> bool {
    match req {
        BufferCmd::Shutdown => return false,
        BufferCmd::InitWal(rootpath, waldir) => {
            let initialized_wal = init_wal(&file_pools, cfg, wal_cfg, &rootpath, &waldir)
                .await
                .unwrap_or_else(|e| {
                    panic!(
                        "Initialize Wal in dir {:?} failed creating {:?}: {e:?}",
                        rootpath, waldir
                    )
                });

            let writes = writes.take().unwrap();

            let task = run_wal_queue(
                task_id,
                max,
                initialized_wal.clone(),
                pending,
                tasks.clone(),
                local_pool.clone(),
                file_pools.clone(),
                writes,
                fc_notifier,
                aiomgr,
            );

            start_task(task_id, tasks, local_pool, task);

            wal.replace(initialized_wal);
        }
        BufferCmd::GetPage(page_key, tx) => tx
            .send(
                pending
                    .borrow()
                    .get(&page_key)
                    .map(|e| e.staging_data.clone()),
            )
            .unwrap(),
        BufferCmd::WriteBatch(writes, wal_writes) => {
            wal_in.send((writes, wal_writes)).await.unwrap();
        }
        BufferCmd::CollectAsh(nrecords, tx) => {
            // wait to ensure writes are paused for Wal
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
    sender: mpsc::Sender<BufferCmd>,
}

impl DiskBufferRequester {
    /// Create a new requester.
    pub fn new(sender: mpsc::Sender<BufferCmd>) -> Self {
        Self { sender }
    }

    /// Get a page from the buffer.
    pub fn get_page(&self, space_id: SpaceId, page_id: u64) -> Option<Page> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .blocking_send(BufferCmd::GetPage((space_id, page_id), resp_tx))
            .map_err(StoreError::Send)
            .ok();
        resp_rx.blocking_recv().unwrap()
    }

    /// Sends a batch of writes to the buffer.
    pub fn write(&self, page_batch: Vec<BufferWrite>, write_batch: AshRecord) {
        self.sender
            .blocking_send(BufferCmd::WriteBatch(page_batch, write_batch))
            .map_err(StoreError::Send)
            .ok();
    }

    pub fn shutdown(&self) {
        self.sender.blocking_send(BufferCmd::Shutdown).ok().unwrap()
    }

    /// Initialize the Wal.
    pub fn init_wal(&self, waldir: &str, rootpath: PathBuf) {
        self.sender
            .blocking_send(BufferCmd::InitWal(rootpath, waldir.to_string()))
            .map_err(StoreError::Send)
            .ok();
    }

    /// Collect the last N records from the Wal.
    pub fn collect_ash(&self, nrecords: usize) -> Result<Vec<AshRecord>, StoreError<RecvError>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .blocking_send(BufferCmd::CollectAsh(nrecords, resp_tx))
            .map_err(StoreError::Send)
            .ok();
        resp_rx.blocking_recv().map_err(StoreError::Receive)
    }

    /// Register a cached space to the buffer.
    pub fn reg_cached_space(&self, space_id: SpaceId, files: Arc<FilePool>) {
        self.sender
            .blocking_send(BufferCmd::RegCachedSpace(space_id, files))
            .map_err(StoreError::Send)
            .ok();
    }
}

#[cfg(test)]
mod tests {
    use sha3::Digest;
    use std::path::{Path, PathBuf};
    use tempdir::TempDir;

    use super::*;
    use crate::{
        file,
        storage::{
            Ash, CachedSpace, DeltaPage, MemStoreR, StoreConfig, StoreRevMut, StoreRevMutDelta,
            StoreRevShared, ZeroStore,
        },
    };
    use shale::CachedStore;

    const STATE_SPACE: SpaceId = 0x0;
    const HASH_SIZE: usize = 32;
    #[test]
    #[ignore = "ref: https://github.com/ava-labs/firewood/issues/45"]
    fn test_buffer_with_undo() {
        let tmpdb = [
            &std::env::var("CARGO_TARGET_DIR").unwrap_or("/tmp".to_string()),
            "sender_api_test_db",
        ];

        let buf_cfg = DiskBufferConfig::builder().max_buffered(1).build();
        let wal_cfg = WalConfig::builder().build();
        let disk_requester = init_buffer(buf_cfg, wal_cfg);

        // TODO: Run the test in a separate standalone directory for concurrency reasons
        let path = std::path::PathBuf::from(r"/tmp/firewood");
        let (root_db_path, reset) = crate::file::open_dir(path, true).unwrap();

        // file descriptor of the state directory
        let state_path = file::touch_dir("state", &root_db_path).unwrap();
        assert!(reset);
        // create a new wal directory on top of root_db_fd
        disk_requester.init_wal("wal", root_db_path);

        // create a new state cache which tracks on disk state.
        let state_cache = Rc::new(
            CachedSpace::new(
                &StoreConfig::builder()
                    .ncached_pages(1)
                    .ncached_files(1)
                    .space_id(STATE_SPACE)
                    .file_nbit(1)
                    .rootdir(state_path)
                    .build(),
                disk_requester.clone(),
            )
            .unwrap(),
        );

        // add an in memory cached space. this will allow us to write to the
        // disk buffer then later persist the change to disk.
        disk_requester.reg_cached_space(state_cache.id(), state_cache.inner.borrow().files.clone());

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
        assert!([
            &tmpdb.into_iter().collect::<String>(),
            "wal",
            "00000000.log"
        ]
        .iter()
        .collect::<PathBuf>()
        .exists());

        // verify
        assert_eq!(disk_requester.collect_ash(1).unwrap().len(), 1);
    }

    #[test]
    #[ignore = "ref: https://github.com/ava-labs/firewood/issues/45"]
    fn test_buffer_with_redo() {
        let buf_cfg = DiskBufferConfig::builder().max_buffered(1).build();
        let wal_cfg = WalConfig::builder().build();
        let disk_requester = init_buffer(buf_cfg, wal_cfg);

        // TODO: Run the test in a separate standalone directory for concurrency reasons
        let tmp_dir = TempDir::new("firewood").unwrap();
        let path = get_file_path(tmp_dir.path(), file!(), line!());
        let (root_db_path, reset) = crate::file::open_dir(path, true).unwrap();

        // file descriptor of the state directory
        let state_path = file::touch_dir("state", &root_db_path).unwrap();
        assert!(reset);
        // create a new wal directory on top of root_db_fd
        disk_requester.init_wal("wal", root_db_path);

        // create a new state cache which tracks on disk state.
        let state_cache = Rc::new(
            CachedSpace::new(
                &StoreConfig::builder()
                    .ncached_pages(1)
                    .ncached_files(1)
                    .space_id(STATE_SPACE)
                    .file_nbit(1)
                    .rootdir(state_path)
                    .build(),
                disk_requester.clone(),
            )
            .unwrap(),
        );

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
        let (redo_delta, wal) = mut_store.take_delta();
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
            Rc::new(ZeroStore::default()),
            &ashes[0].0[&STATE_SPACE].redo,
        );
        let view = shared_store.get_view(0, hash.len() as u64).unwrap();
        assert_eq!(view.as_deref(), hash);
    }

    fn get_file_path(path: &Path, file: &str, line: u32) -> PathBuf {
        path.join(format!("{}_{}", file.replace('/', "-"), line))
    }

    fn init_buffer(buf_cfg: DiskBufferConfig, wal_cfg: WalConfig) -> DiskBufferRequester {
        let (sender, inbound) = tokio::sync::mpsc::channel(buf_cfg.max_buffered);
        let disk_requester = DiskBufferRequester::new(sender);
        std::thread::spawn(move || {
            let disk_buffer = DiskBuffer::new(inbound, &buf_cfg, &wal_cfg).unwrap();
            disk_buffer.run()
        });
        disk_requester
    }

    fn create_batches(rev_mut: &StoreRevMut) -> (Vec<BufferWrite>, AshRecord) {
        let deltas = std::mem::replace(
            &mut *rev_mut.deltas.borrow_mut(),
            StoreRevMutDelta {
                pages: HashMap::new(),
                plain: Ash::new(),
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
