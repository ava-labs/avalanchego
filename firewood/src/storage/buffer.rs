//! Disk buffer for staging in memory pages and flushing them to disk.
use std::fmt::Debug;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;
use std::{cell::RefCell, collections::HashMap};

use super::{
    AshRecord, CachedSpace, FilePool, MemStoreR, Page, StoreDelta, StoreError, WalConfig,
    PAGE_SIZE_NBIT,
};

use aiofut::{AioBuilder, AioError, AioManager};
use growthring::WalFileAio;
use growthring::{
    wal::{RecoverPolicy, WalLoader, WalWriter},
    walerror::WalError,
    WalStoreAio,
};
use shale::SpaceID;
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
    GetPage((SpaceID, u64), oneshot::Sender<Option<Arc<Page>>>),
    CollectAsh(usize, oneshot::Sender<Vec<AshRecord>>),
    /// Register a new space and add the files to a memory mapped pool.
    RegCachedSpace(SpaceID, Arc<FilePool>),
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
    pub space_id: SpaceID,
    pub delta: StoreDelta,
}

#[derive(Debug)]
struct PendingPage {
    staging_data: Arc<Page>,
    file_nbit: u64,
    staging_notifiers: Vec<Rc<Semaphore>>,
    writing_notifiers: Vec<Rc<Semaphore>>,
}

/// Responsible for processing [`BufferCmd`]s from the [`DiskBufferRequester`]
/// and managing the persistance of pages.
pub struct DiskBuffer {
    pending: HashMap<(SpaceID, u64), PendingPage>,
    inbound: mpsc::Receiver<BufferCmd>,
    fc_notifier: Option<oneshot::Sender<()>>,
    fc_blocker: Option<oneshot::Receiver<()>>,
    file_pools: [Option<Arc<FilePool>>; 255],
    aiomgr: AioManager,
    local_pool: Rc<tokio::task::LocalSet>,
    task_id: u64,
    tasks: Rc<RefCell<HashMap<u64, Option<tokio::task::JoinHandle<()>>>>>,
    wal: Option<Rc<Mutex<WalWriter<WalFileAio, WalStoreAio>>>>,
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
            pending: HashMap::new(),
            cfg: cfg.clone(),
            inbound,
            fc_notifier: None,
            fc_blocker: None,
            file_pools: std::array::from_fn(|_| None),
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

    /// Add an pending pages to aio manager for processing by the local pool.
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
        let fut = self.aiomgr.write(
            file.get_fd(),
            offset & fmask,
            Box::new(*p.staging_data),
            None,
        );
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
                for notifier in std::mem::take(&mut slot.writing_notifiers) {
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

    /// Initialize the Wal subsystem if it does not exists and attempts to replay the Wal if exists.
    async fn init_wal(&mut self, rootpath: PathBuf, waldir: String) -> Result<(), WalError> {
        let final_path = rootpath.clone().join(waldir.clone());
        let mut aiobuilder = AioBuilder::default();
        aiobuilder.max_events(self.cfg.wal_max_aio_requests as u32);
        let aiomgr = aiobuilder.build()?;
        let store = WalStoreAio::new(final_path.clone(), false, Some(aiomgr))?;
        let mut loader = WalLoader::new();
        loader
            .file_nbit(self.wal_cfg.file_nbit)
            .block_nbit(self.wal_cfg.block_nbit)
            .recover_policy(RecoverPolicy::Strict);
        if self.wal.is_some() {
            // already initialized
            return Ok(());
        }
        let wal = loader
            .load(
                store,
                |raw, _| {
                    let batch = AshRecord::deserialize(raw);
                    for (space_id, ash) in batch.0 {
                        for (undo, redo) in ash.iter() {
                            let offset = undo.offset;
                            let file_pool = self.file_pools[space_id as usize].as_ref().unwrap();
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
                break;
            }
            while let Ok((bw, ac)) = writes.try_recv() {
                records.push(ac);
                bwrites.extend(bw);
                if records.len() >= self.cfg.wal_max_batch {
                    break;
                }
            }
            // first write to Wal
            let ring_ids: Vec<_> =
                futures::future::join_all(self.wal.as_ref().unwrap().lock().await.grow(records))
                    .await
                    .into_iter()
                    .map(|ring| ring.map_err(|_| "Wal Error while writing").unwrap().1)
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
                            let file_nbit = self.file_pools[page_key.0 as usize]
                                .as_ref()
                                .unwrap()
                                .file_nbit;
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
                    .map_err(|_| "Wal errore while pruning")
                    .unwrap();
            });
            if self.pending.len() >= self.cfg.max_pending {
                let (tx, rx) = oneshot::channel();
                self.fc_notifier = Some(tx);
                self.fc_blocker = Some(rx);
            }
        }
    }

    async fn process(
        &mut self,
        req: BufferCmd,
        wal_in: &mpsc::Sender<(Vec<BufferWrite>, AshRecord)>,
    ) -> bool {
        match req {
            BufferCmd::Shutdown => return false,
            BufferCmd::InitWal(root_path, waldir) => {
                self.init_wal(root_path.clone(), waldir.clone())
                    .await
                    .unwrap_or_else(|e| {
                        panic!(
                            "Initialize Wal in dir {:?} failed creating {:?}: {e:?}",
                            root_path, waldir
                        )
                    });
            }
            BufferCmd::GetPage(page_key, tx) => tx
                .send(self.pending.get(&page_key).map(|e| e.staging_data.clone()))
                .unwrap(),
            BufferCmd::WriteBatch(writes, wal_writes) => {
                wal_in.send((writes, wal_writes)).await.unwrap();
            }
            BufferCmd::CollectAsh(nrecords, tx) => {
                // wait to ensure writes are paused for Wal
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
            BufferCmd::RegCachedSpace(space_id, files) => {
                self.file_pools[space_id as usize] = Some(files)
            }
        }
        true
    }

    /// Processes an async AIOFuture request against the local pool.
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
                        break;
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

impl Default for DiskBufferRequester {
    fn default() -> Self {
        Self {
            sender: mpsc::channel(1).0,
        }
    }
}

impl DiskBufferRequester {
    /// Create a new requester.
    pub fn new(sender: mpsc::Sender<BufferCmd>) -> Self {
        Self { sender }
    }

    /// Get a page from the buffer.
    pub fn get_page(&self, space_id: SpaceID, pid: u64) -> Option<Arc<Page>> {
        let (resp_tx, resp_rx) = oneshot::channel();
        self.sender
            .blocking_send(BufferCmd::GetPage((space_id, pid), resp_tx))
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
    pub fn reg_cached_space(&self, space: &CachedSpace) {
        let mut inner = space.inner.borrow_mut();
        inner.disk_buffer = self.clone();
        self.sender
            .blocking_send(BufferCmd::RegCachedSpace(space.id(), inner.files.clone()))
            .map_err(StoreError::Send)
            .ok();
    }
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use super::*;
    use crate::{
        file,
        storage::{Ash, DeltaPage, StoreConfig, StoreRevMut, StoreRevMutDelta},
    };
    use shale::CachedStore;

    const STATE_SPACE: SpaceID = 0x0;
    #[test]
    #[ignore = "ref: https://github.com/ava-labs/firewood/issues/45"]
    fn test_buffer() {
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
            )
            .unwrap(),
        );

        // add an in memory cached space. this will allow us to write to the
        // disk buffer then later persist the change to disk.
        disk_requester.reg_cached_space(state_cache.as_ref());

        // memory mapped store
        let mut mut_store = StoreRevMut::new(state_cache as Rc<dyn MemStoreR>);

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
