// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

pub use crate::{
    config::{DbConfig, DbRevConfig},
    storage::{buffer::DiskBufferConfig, WalConfig},
    v2::api::{Batch, BatchOp, Proposal},
};
use crate::{
    file,
    merkle::{
        Bincode, Key, Merkle, MerkleError, MerkleKeyValueStream, Node, Proof, ProofError, TrieHash,
        TRIE_HASH_LEN,
    },
    storage::{
        buffer::{DiskBuffer, DiskBufferRequester},
        CachedSpace, MemStoreR, SpaceWrite, StoreConfig, StoreDelta, StoreRevMut, StoreRevShared,
        ZeroStore, PAGE_SIZE_NBIT,
    },
    v2::api::{self, HashKey, KeyType, ValueType},
};
use crate::{
    merkle,
    shale::{
        self,
        compact::{CompactSpace, CompactSpaceHeader},
        disk_address::DiskAddress,
        CachedStore, Obj, ShaleError, ShaleStore, SpaceId, Storable, StoredView,
    },
};
use async_trait::async_trait;
use bytemuck::{cast_slice, Pod, Zeroable};

use metered::metered;
use parking_lot::{Mutex, RwLock};
use std::{
    collections::VecDeque,
    error::Error,
    fmt,
    io::{Cursor, ErrorKind, Write},
    mem::size_of,
    num::NonZeroUsize,
    ops::Deref,
    os::fd::{AsFd, BorrowedFd},
    path::Path,
    sync::Arc,
    thread::JoinHandle,
};
use tokio::task::block_in_place;

mod proposal;

use self::proposal::ProposalBase;

const MERKLE_META_SPACE: SpaceId = 0x0;
const MERKLE_PAYLOAD_SPACE: SpaceId = 0x1;
const ROOT_HASH_SPACE: SpaceId = 0x2;
const SPACE_RESERVED: u64 = 0x1000;

const MAGIC_STR: &[u8; 16] = b"firewood v0.1\0\0\0";

pub type MutStore = CompactSpace<Node, StoreRevMut>;
pub type SharedStore = CompactSpace<Node, StoreRevShared>;

#[derive(Debug)]
#[non_exhaustive]
pub enum DbError {
    InvalidParams,
    Merkle(MerkleError),
    System(nix::Error),
    KeyNotFound,
    CreateError,
    Shale(ShaleError),
    IO(std::io::Error),
    InvalidProposal,
}

impl fmt::Display for DbError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            DbError::InvalidParams => write!(f, "invalid parameters provided"),
            DbError::Merkle(e) => write!(f, "merkle error: {e:?}"),
            DbError::System(e) => write!(f, "system error: {e:?}"),
            DbError::KeyNotFound => write!(f, "not found"),
            DbError::CreateError => write!(f, "database create error"),
            DbError::IO(e) => write!(f, "I/O error: {e:?}"),
            DbError::Shale(e) => write!(f, "shale error: {e:?}"),
            DbError::InvalidProposal => write!(f, "invalid proposal"),
        }
    }
}

impl From<std::io::Error> for DbError {
    fn from(e: std::io::Error) -> Self {
        DbError::IO(e)
    }
}

impl From<ShaleError> for DbError {
    fn from(e: ShaleError) -> Self {
        DbError::Shale(e)
    }
}

impl Error for DbError {}

/// DbParams contains the constants that are fixed upon the creation of the DB, this ensures the
/// correct parameters are used when the DB is opened later (the parameters here will override the
/// parameters in [DbConfig] if the DB already exists).
#[repr(C)]
#[derive(Debug, Clone, Copy, Pod, Zeroable)]
struct DbParams {
    magic: [u8; 16],
    meta_file_nbit: u64,
    payload_file_nbit: u64,
    payload_regn_nbit: u64,
    wal_file_nbit: u64,
    wal_block_nbit: u64,
    root_hash_file_nbit: u64,
}

#[derive(Clone, Debug)]
/// Necessary linear space instances bundled for a `CompactSpace`.
struct SubUniverse<T> {
    meta: T,
    payload: T,
}

impl<T> SubUniverse<T> {
    const fn new(meta: T, payload: T) -> Self {
        Self { meta, payload }
    }
}

impl SubUniverse<StoreRevShared> {
    fn to_mem_store_r(&self) -> SubUniverse<Arc<impl MemStoreR>> {
        SubUniverse {
            meta: self.meta.inner().clone(),
            payload: self.payload.inner().clone(),
        }
    }
}

impl SubUniverse<StoreRevMut> {
    fn new_from_other(&self) -> SubUniverse<StoreRevMut> {
        SubUniverse {
            meta: StoreRevMut::new_from_other(&self.meta),
            payload: StoreRevMut::new_from_other(&self.payload),
        }
    }
}

impl<T: MemStoreR + 'static> SubUniverse<Arc<T>> {
    fn rewind(
        &self,
        meta_writes: &[SpaceWrite],
        payload_writes: &[SpaceWrite],
    ) -> SubUniverse<StoreRevShared> {
        SubUniverse::new(
            StoreRevShared::from_ash(self.meta.clone(), meta_writes),
            StoreRevShared::from_ash(self.payload.clone(), payload_writes),
        )
    }
}

impl SubUniverse<Arc<CachedSpace>> {
    fn to_mem_store_r(&self) -> SubUniverse<Arc<impl MemStoreR>> {
        SubUniverse {
            meta: self.meta.clone(),
            payload: self.payload.clone(),
        }
    }
}

fn get_sub_universe_from_deltas(
    sub_universe: &SubUniverse<Arc<CachedSpace>>,
    meta_delta: StoreDelta,
    payload_delta: StoreDelta,
) -> SubUniverse<StoreRevShared> {
    SubUniverse::new(
        StoreRevShared::from_delta(sub_universe.meta.clone(), meta_delta),
        StoreRevShared::from_delta(sub_universe.payload.clone(), payload_delta),
    )
}

fn get_sub_universe_from_empty_delta(
    sub_universe: &SubUniverse<Arc<CachedSpace>>,
) -> SubUniverse<StoreRevShared> {
    get_sub_universe_from_deltas(sub_universe, StoreDelta::default(), StoreDelta::default())
}

/// mutable DB-wide metadata, it keeps track of the root of the top-level trie.
#[repr(C)]
#[derive(Copy, Clone, Debug, Pod, Zeroable)]
struct DbHeader {
    kv_root: DiskAddress,
}

impl DbHeader {
    pub const MSIZE: u64 = std::mem::size_of::<Self>() as u64;

    pub const fn new_empty() -> Self {
        Self {
            kv_root: DiskAddress::null(),
        }
    }
}

impl Storable for DbHeader {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, shale::ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        Ok(Self {
            kv_root: raw.as_deref().as_slice().into(),
        })
    }

    fn serialized_len(&self) -> u64 {
        Self::MSIZE
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.kv_root.to_le_bytes())?;
        Ok(())
    }
}

#[derive(Clone, Debug)]
/// Necessary linear space instances bundled for the state of the entire DB.
struct Universe<T> {
    merkle: SubUniverse<T>,
}

impl Universe<StoreRevShared> {
    fn to_mem_store_r(&self) -> Universe<Arc<impl MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
        }
    }
}

impl Universe<StoreRevMut> {
    fn new_from_other(&self) -> Universe<StoreRevMut> {
        Universe {
            merkle: self.merkle.new_from_other(),
        }
    }
}

impl Universe<Arc<CachedSpace>> {
    fn to_mem_store_r(&self) -> Universe<Arc<impl MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
        }
    }
}

impl<T: MemStoreR + 'static> Universe<Arc<T>> {
    fn rewind(
        &self,
        merkle_meta_writes: &[SpaceWrite],
        merkle_payload_writes: &[SpaceWrite],
    ) -> Universe<StoreRevShared> {
        Universe {
            merkle: self
                .merkle
                .rewind(merkle_meta_writes, merkle_payload_writes),
        }
    }
}

/// Some readable version of the DB.
#[derive(Debug)]
pub struct DbRev<S> {
    header: shale::Obj<DbHeader>,
    merkle: Merkle<S, Bincode>,
}

#[async_trait]
impl<S: ShaleStore<Node> + Send + Sync> api::DbView for DbRev<S> {
    type Stream<'a> = MerkleKeyValueStream<'a, S, Bincode> where Self: 'a;

    async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
        self.merkle
            .root_hash(self.header.kv_root)
            .map(|h| *h)
            .map_err(|e| api::Error::IO(std::io::Error::new(ErrorKind::Other, e)))
    }

    async fn val<K: api::KeyType>(&self, key: K) -> Result<Option<Vec<u8>>, api::Error> {
        let obj_ref = self.merkle.get(key, self.header.kv_root);
        match obj_ref {
            Err(e) => Err(api::Error::IO(std::io::Error::new(ErrorKind::Other, e))),
            Ok(obj) => Ok(obj.map(|inner| inner.deref().to_owned())),
        }
    }

    async fn single_key_proof<K: api::KeyType>(
        &self,
        key: K,
    ) -> Result<Option<Proof<Vec<u8>>>, api::Error> {
        self.merkle
            .prove(key, self.header.kv_root)
            .map(Some)
            .map_err(|e| api::Error::IO(std::io::Error::new(ErrorKind::Other, e)))
    }

    async fn range_proof<K: api::KeyType, V>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<usize>,
    ) -> Result<Option<api::RangeProof<Vec<u8>, Vec<u8>>>, api::Error> {
        self.merkle
            .range_proof(self.header.kv_root, first_key, last_key, limit)
            .await
            .map_err(|e| api::Error::InternalError(Box::new(e)))
    }

    fn iter_option<K: KeyType>(
        &self,
        first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        Ok(match first_key {
            None => self.merkle.key_value_iter(self.header.kv_root),
            Some(key) => self
                .merkle
                .key_value_iter_from_key(self.header.kv_root, key.as_ref().into()),
        })
    }
}

impl<S: ShaleStore<Node> + Send + Sync> DbRev<S> {
    pub fn stream(&self) -> merkle::MerkleKeyValueStream<'_, S, Bincode> {
        self.merkle.key_value_iter(self.header.kv_root)
    }

    pub fn stream_from(&self, start_key: Key) -> merkle::MerkleKeyValueStream<'_, S, Bincode> {
        self.merkle
            .key_value_iter_from_key(self.header.kv_root, start_key)
    }

    fn flush_dirty(&mut self) -> Option<()> {
        self.header.flush_dirty();
        self.merkle.flush_dirty()?;
        Some(())
    }

    fn borrow_split(&mut self) -> (&mut shale::Obj<DbHeader>, &mut Merkle<S, Bincode>) {
        (&mut self.header, &mut self.merkle)
    }

    /// Get root hash of the generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        self.merkle
            .root_hash(self.header.kv_root)
            .map_err(DbError::Merkle)
    }

    /// Get a value associated with a key.
    pub fn kv_get<K: AsRef<[u8]>>(&self, key: K) -> Option<Vec<u8>> {
        let obj_ref = self.merkle.get(key, self.header.kv_root);
        match obj_ref {
            Err(_) => None,
            Ok(obj) => obj.map(|o| o.to_vec()),
        }
    }

    /// Dump the Trie of the generic key-value storage.
    pub fn kv_dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.merkle
            .dump(self.header.kv_root, w)
            .map_err(DbError::Merkle)
    }

    pub fn prove<K: AsRef<[u8]>>(&self, key: K) -> Result<Proof<Vec<u8>>, MerkleError> {
        self.merkle.prove::<K>(key, self.header.kv_root)
    }

    /// Verifies a range proof is valid for a set of keys.
    pub fn verify_range_proof<N: AsRef<[u8]> + Send, K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        proof: Proof<N>,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        values: Vec<V>,
    ) -> Result<bool, ProofError> {
        let hash: [u8; 32] = *self.kv_root_hash()?;
        let valid = proof.verify_range_proof(hash, first_key, last_key, keys, values)?;
        Ok(valid)
    }
}

impl From<DbRev<MutStore>> for DbRev<SharedStore> {
    fn from(value: DbRev<MutStore>) -> Self {
        DbRev {
            header: value.header,
            merkle: value.merkle.into(),
        }
    }
}

#[derive(Debug)]
struct DbInner {
    disk_requester: DiskBufferRequester,
    disk_thread: Option<JoinHandle<()>>,
    cached_space: Universe<Arc<CachedSpace>>,
    // Whether to reset the store headers when creating a new store on top of the cached space.
    reset_store_headers: bool,
    root_hash_staging: StoreRevMut,
}

impl Drop for DbInner {
    fn drop(&mut self) {
        self.disk_requester.shutdown();
        self.disk_thread.take().map(JoinHandle::join);
    }
}

#[async_trait]
impl api::Db for Db {
    type Historical = DbRev<SharedStore>;

    type Proposal = proposal::Proposal;

    async fn revision(&self, root_hash: HashKey) -> Result<Arc<Self::Historical>, api::Error> {
        let rev = self.get_revision(&TrieHash(root_hash));
        if let Some(rev) = rev {
            Ok(Arc::new(rev))
        } else {
            Err(api::Error::HashNotFound {
                provided: root_hash,
            })
        }
    }

    async fn root_hash(&self) -> Result<HashKey, api::Error> {
        self.kv_root_hash().map(|hash| hash.0).map_err(Into::into)
    }

    async fn propose<K: KeyType, V: ValueType>(
        &self,
        batch: api::Batch<K, V>,
    ) -> Result<Self::Proposal, api::Error> {
        self.new_proposal(batch).map_err(Into::into)
    }
}

#[derive(Debug)]
pub struct DbRevInner<T> {
    inner: VecDeque<Universe<StoreRevShared>>,
    root_hashes: VecDeque<TrieHash>,
    max_revisions: usize,
    base: Universe<StoreRevShared>,
    base_revision: Arc<DbRev<T>>,
}

/// Firewood database handle.
#[derive(Debug)]
pub struct Db {
    inner: Arc<RwLock<DbInner>>,
    revisions: Arc<Mutex<DbRevInner<SharedStore>>>,
    payload_regn_nbit: u64,
    metrics: Arc<DbMetrics>,
    cfg: DbConfig,
}

#[metered(registry = DbMetrics, visibility = pub)]
impl Db {
    const PARAM_SIZE: u64 = size_of::<DbParams>() as u64;

    pub async fn new<P: AsRef<Path>>(db_path: P, cfg: &DbConfig) -> Result<Self, api::Error> {
        if cfg.truncate {
            let _ = tokio::fs::remove_dir_all(db_path.as_ref()).await;
        }

        #[cfg(feature = "logger")]
        // initialize the logger, but ignore if this fails. This could fail because the calling
        // library already initialized the logger or if you're opening a second database
        let _ = env_logger::try_init();

        block_in_place(|| Db::new_internal(db_path, cfg.clone()))
            .map_err(|e| api::Error::InternalError(Box::new(e)))
    }

    /// Open a database.
    fn new_internal<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, DbError> {
        let open_options = if cfg.truncate {
            file::Options::Truncate
        } else {
            file::Options::NoTruncate
        };

        let (db_path, reset) = file::open_dir(db_path, open_options)?;

        let merkle_path = file::touch_dir("merkle", &db_path)?;
        let merkle_meta_path = file::touch_dir("meta", &merkle_path)?;
        let merkle_payload_path = file::touch_dir("compact", &merkle_path)?;

        let root_hash_path = file::touch_dir("root_hash", &db_path)?;

        let file0 = crate::file::File::new(0, SPACE_RESERVED, &merkle_meta_path)?;
        let fd0 = file0.as_fd();

        if reset {
            // initialize dbparams
            if cfg.payload_file_nbit < cfg.payload_regn_nbit
                || cfg.payload_regn_nbit < PAGE_SIZE_NBIT
            {
                return Err(DbError::InvalidParams);
            }
            Self::initialize_header_on_disk(&cfg, fd0)?;
        }

        // read DbParams
        let mut header_bytes = [0; size_of::<DbParams>()];
        nix::sys::uio::pread(fd0, &mut header_bytes, 0).map_err(DbError::System)?;
        drop(file0);
        #[allow(clippy::indexing_slicing)]
        let params: DbParams = cast_slice(&header_bytes)[0];

        let wal = WalConfig::builder()
            .file_nbit(params.wal_file_nbit)
            .block_nbit(params.wal_block_nbit)
            .max_revisions(cfg.wal.max_revisions)
            .build();
        let (sender, inbound) = tokio::sync::mpsc::unbounded_channel();
        let disk_requester = DiskBufferRequester::new(sender);
        let buffer = cfg.buffer.clone();
        #[allow(clippy::unwrap_used)]
        let disk_buffer = DiskBuffer::new(inbound, &buffer, &wal).expect("DiskBuffer::new");

        let disk_thread = Some(
            std::thread::Builder::new()
                .name("DiskBuffer".to_string())
                .spawn(move || disk_buffer.run())
                .expect("thread spawn should succeed"),
        );

        #[allow(clippy::unwrap_used)]
        let root_hash_cache: Arc<CachedSpace> = CachedSpace::new(
            &StoreConfig::builder()
                .ncached_pages(cfg.root_hash_ncached_pages)
                .ncached_files(cfg.root_hash_ncached_files)
                .space_id(ROOT_HASH_SPACE)
                .file_nbit(params.root_hash_file_nbit)
                .rootdir(root_hash_path)
                .build(),
            disk_requester.clone(),
        )
        .unwrap()
        .into();

        // setup disk buffer
        #[allow(clippy::unwrap_used)]
        let data_cache = Universe {
            merkle: SubUniverse::<Arc<CachedSpace>>::new(
                CachedSpace::new(
                    &StoreConfig::builder()
                        .ncached_pages(cfg.meta_ncached_pages)
                        .ncached_files(cfg.meta_ncached_files)
                        .space_id(MERKLE_META_SPACE)
                        .file_nbit(params.meta_file_nbit)
                        .rootdir(merkle_meta_path)
                        .build(),
                    disk_requester.clone(),
                )
                .unwrap()
                .into(),
                CachedSpace::new(
                    &StoreConfig::builder()
                        .ncached_pages(cfg.payload_ncached_pages)
                        .ncached_files(cfg.payload_ncached_files)
                        .space_id(MERKLE_PAYLOAD_SPACE)
                        .file_nbit(params.payload_file_nbit)
                        .rootdir(merkle_payload_path)
                        .build(),
                    disk_requester.clone(),
                )
                .unwrap()
                .into(),
            ),
        };

        [
            data_cache.merkle.meta.as_ref(),
            data_cache.merkle.payload.as_ref(),
            root_hash_cache.as_ref(),
        ]
        .into_iter()
        .for_each(|cached_space| {
            disk_requester.reg_cached_space(cached_space.id(), cached_space.clone_files());
        });

        // recover from Wal
        disk_requester.init_wal("wal", &db_path);

        let root_hash_staging = StoreRevMut::new(root_hash_cache);
        let reset_headers = reset;

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&data_cache.merkle),
        };

        let db_header_ref = Db::get_db_header_ref(&base.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&base.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

        let header_refs = (db_header_ref, merkle_payload_header_ref);

        let base_revision = Db::new_revision(
            header_refs,
            (base.merkle.meta.clone(), base.merkle.payload.clone()),
            params.payload_regn_nbit,
            cfg.payload_max_walk,
            &cfg.rev,
        )?;

        Ok(Self {
            inner: Arc::new(RwLock::new(DbInner {
                disk_thread,
                disk_requester,
                cached_space: data_cache,
                reset_store_headers: reset_headers,
                root_hash_staging,
            })),
            revisions: Arc::new(Mutex::new(DbRevInner {
                inner: VecDeque::new(),
                root_hashes: VecDeque::new(),
                max_revisions: cfg.wal.max_revisions as usize,
                base,
                base_revision: Arc::new(base_revision),
            })),
            payload_regn_nbit: params.payload_regn_nbit,
            metrics: Arc::new(DbMetrics::default()),
            cfg: cfg.clone(),
        })
    }

    fn initialize_header_on_disk(cfg: &DbConfig, fd0: BorrowedFd) -> Result<(), DbError> {
        // The header consists of three parts:
        // DbParams
        // DbHeader (just a pointer to the sentinel)
        // CompactSpaceHeader for future allocations
        let (params, hdr, csh);
        #[allow(clippy::unwrap_used)]
        let header_bytes: Vec<u8> = {
            params = DbParams {
                magic: *MAGIC_STR,
                meta_file_nbit: cfg.meta_file_nbit,
                payload_file_nbit: cfg.payload_file_nbit,
                payload_regn_nbit: cfg.payload_regn_nbit,
                wal_file_nbit: cfg.wal.file_nbit,
                wal_block_nbit: cfg.wal.block_nbit,
                root_hash_file_nbit: cfg.root_hash_file_nbit,
            };
            let bytes = bytemuck::bytes_of(&params);
            bytes.iter()
        }
        .chain({
            // compute the DbHeader as bytes
            hdr = DbHeader::new_empty();
            bytemuck::bytes_of(&hdr)
        })
        .chain({
            // write out the CompactSpaceHeader
            csh = CompactSpaceHeader::new(
                NonZeroUsize::new(SPACE_RESERVED as usize).unwrap(),
                #[allow(clippy::unwrap_used)]
                NonZeroUsize::new(SPACE_RESERVED as usize).unwrap(),
            );
            bytemuck::bytes_of(&csh)
        })
        .copied()
        .collect();

        nix::sys::uio::pwrite(fd0, &header_bytes, 0).map_err(DbError::System)?;
        Ok(())
    }

    /// Create a new mutable store and an alterable revision of the DB on top.
    fn new_store(
        &self,
        cached_space: &Universe<Arc<CachedSpace>>,
        reset_store_headers: bool,
    ) -> Result<(Universe<StoreRevMut>, DbRev<MutStore>), DbError> {
        let mut offset = Db::PARAM_SIZE as usize;
        let db_header: DiskAddress = DiskAddress::from(offset);
        offset += DbHeader::MSIZE as usize;
        let merkle_payload_header: DiskAddress = DiskAddress::from(offset);
        offset += CompactSpaceHeader::MSIZE as usize;
        assert!(offset <= SPACE_RESERVED as usize);

        let mut merkle_meta_store = StoreRevMut::new(cached_space.merkle.meta.clone());

        if reset_store_headers {
            // initialize store headers
            #[allow(clippy::unwrap_used)]
            merkle_meta_store.write(
                merkle_payload_header.into(),
                &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                    NonZeroUsize::new(SPACE_RESERVED as usize).unwrap(),
                    #[allow(clippy::unwrap_used)]
                    NonZeroUsize::new(SPACE_RESERVED as usize).unwrap(),
                ))?,
            );
            merkle_meta_store.write(
                db_header.into(),
                &shale::to_dehydrated(&DbHeader::new_empty())?,
            );
        }

        let store = Universe {
            merkle: SubUniverse::new(
                merkle_meta_store,
                StoreRevMut::new(cached_space.merkle.payload.clone()),
            ),
        };

        let db_header_ref = Db::get_db_header_ref(&store.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&store.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

        let header_refs = (db_header_ref, merkle_payload_header_ref);

        let mut rev: DbRev<CompactSpace<Node, StoreRevMut>> = Db::new_revision(
            header_refs,
            (store.merkle.meta.clone(), store.merkle.payload.clone()),
            self.payload_regn_nbit,
            self.cfg.payload_max_walk,
            &self.cfg.rev,
        )?;
        #[allow(clippy::unwrap_used)]
        rev.flush_dirty().unwrap();

        Ok((store, rev))
    }

    fn get_payload_header_ref<K: CachedStore>(
        meta_ref: &K,
        header_offset: u64,
    ) -> Result<Obj<CompactSpaceHeader>, DbError> {
        let payload_header = DiskAddress::from(header_offset as usize);
        StoredView::ptr_to_obj(
            meta_ref,
            payload_header,
            shale::compact::CompactHeader::MSIZE,
        )
        .map_err(Into::into)
    }

    fn get_db_header_ref<K: CachedStore>(meta_ref: &K) -> Result<Obj<DbHeader>, DbError> {
        let db_header = DiskAddress::from(Db::PARAM_SIZE as usize);
        StoredView::ptr_to_obj(meta_ref, db_header, DbHeader::MSIZE).map_err(Into::into)
    }

    fn new_revision<K: CachedStore, T: Into<K>>(
        header_refs: (Obj<DbHeader>, Obj<CompactSpaceHeader>),
        merkle: (T, T),
        payload_regn_nbit: u64,
        payload_max_walk: u64,
        cfg: &DbRevConfig,
    ) -> Result<DbRev<CompactSpace<Node, K>>, DbError> {
        // TODO: This should be a compile time check
        const DB_OFFSET: u64 = Db::PARAM_SIZE;
        let merkle_offset = DB_OFFSET + DbHeader::MSIZE;
        assert!(merkle_offset + CompactSpaceHeader::MSIZE <= SPACE_RESERVED);

        let mut db_header_ref = header_refs.0;
        let merkle_payload_header_ref = header_refs.1;

        let merkle_meta = merkle.0.into();
        let merkle_payload = merkle.1.into();

        #[allow(clippy::unwrap_used)]
        let merkle_space = shale::compact::CompactSpace::new(
            merkle_meta,
            merkle_payload,
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.merkle_ncached_objs),
            payload_max_walk,
            payload_regn_nbit,
        )
        .unwrap();

        let merkle = Merkle::new(Box::new(merkle_space));

        if db_header_ref.kv_root.is_null() {
            let mut err = Ok(());
            // create the sentinel node
            #[allow(clippy::unwrap_used)]
            db_header_ref
                .write(|r| {
                    err = (|| {
                        r.kv_root = merkle.init_root()?;
                        Ok(())
                    })();
                })
                .unwrap();
            err.map_err(DbError::Merkle)?
        }

        Ok(DbRev {
            header: db_header_ref,
            merkle,
        })
    }

    /// Create a proposal.
    pub(crate) fn new_proposal<K: KeyType, V: ValueType>(
        &self,
        data: Batch<K, V>,
    ) -> Result<proposal::Proposal, DbError> {
        let mut inner = self.inner.write();
        let reset_store_headers = inner.reset_store_headers;
        let (store, mut rev) = self.new_store(&inner.cached_space, reset_store_headers)?;

        // Flip the reset flag after resetting the store headers.
        if reset_store_headers {
            inner.reset_store_headers = false;
        }

        data.into_iter().try_for_each(|op| -> Result<(), DbError> {
            match op {
                BatchOp::Put { key, value } => {
                    let (header, merkle) = rev.borrow_split();
                    merkle
                        .insert(key, value.as_ref().to_vec(), header.kv_root)
                        .map_err(DbError::Merkle)?;
                    Ok(())
                }
                BatchOp::Delete { key } => {
                    let (header, merkle) = rev.borrow_split();
                    merkle
                        .remove(key, header.kv_root)
                        .map_err(DbError::Merkle)?;
                    Ok(())
                }
            }
        })?;

        // Calculated the root hash before flushing so it can be persisted.
        let root_hash = rev.kv_root_hash()?;
        #[allow(clippy::unwrap_used)]
        rev.flush_dirty().unwrap();

        let parent = ProposalBase::View(Arc::clone(&self.revisions.lock().base_revision));
        Ok(proposal::Proposal {
            m: Arc::clone(&self.inner),
            r: Arc::clone(&self.revisions),
            cfg: self.cfg.clone(),
            rev,
            store,
            committed: Arc::new(Mutex::new(false)),
            root_hash,
            parent,
        })
    }

    /// Get a handle that grants the access to any committed state of the entire DB,
    /// with a given root hash. If the given root hash matches with more than one
    /// revisions, we use the most recent one as the trie are the same.
    ///
    /// If no revision with matching root hash found, returns None.
    // #[measure([HitCount])]
    pub fn get_revision(&self, root_hash: &TrieHash) -> Option<DbRev<SharedStore>> {
        let mut revisions = self.revisions.lock();
        let inner_lock = self.inner.read();

        // Find the revision index with the given root hash.
        let mut nback = revisions.root_hashes.iter().position(|r| r == root_hash);
        let rlen = revisions.root_hashes.len();

        #[allow(clippy::unwrap_used)]
        if nback.is_none() && rlen < revisions.max_revisions {
            let ashes = inner_lock
                .disk_requester
                .collect_ash(revisions.max_revisions)
                .ok()
                .unwrap();

            #[allow(clippy::indexing_slicing)]
            (nback = ashes
                .iter()
                .skip(rlen)
                .map(|ash| {
                    StoreRevShared::from_ash(
                        Arc::new(ZeroStore::default()),
                        #[allow(clippy::indexing_slicing)]
                        &ash.0[&ROOT_HASH_SPACE].redo,
                    )
                })
                .map(|root_hash_store| {
                    root_hash_store
                        .get_view(0, TRIE_HASH_LEN as u64)
                        .expect("get view failed")
                        .as_deref()
                })
                .map(|data| TrieHash(data[..TRIE_HASH_LEN].try_into().unwrap()))
                .position(|trie_hash| &trie_hash == root_hash));
        }

        let nback = nback?;

        let rlen = revisions.inner.len();
        if rlen < nback {
            // TODO: Remove unwrap
            #[allow(clippy::unwrap_used)]
            let ashes = inner_lock.disk_requester.collect_ash(nback).ok().unwrap();
            for mut ash in ashes.into_iter().skip(rlen) {
                for (_, a) in ash.0.iter_mut() {
                    a.undo.reverse()
                }

                let u = match revisions.inner.back() {
                    Some(u) => u.to_mem_store_r().rewind(
                        #[allow(clippy::indexing_slicing)]
                        &ash.0[&MERKLE_META_SPACE].undo,
                        #[allow(clippy::indexing_slicing)]
                        &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                    ),
                    None => inner_lock.cached_space.to_mem_store_r().rewind(
                        #[allow(clippy::indexing_slicing)]
                        &ash.0[&MERKLE_META_SPACE].undo,
                        #[allow(clippy::indexing_slicing)]
                        &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                    ),
                };
                revisions.inner.push_back(u);
            }
        }

        let space = if nback == 0 {
            &revisions.base
        } else {
            #[allow(clippy::indexing_slicing)]
            &revisions.inner[nback - 1]
        };
        // Release the lock after we find the revision
        drop(inner_lock);

        #[allow(clippy::unwrap_used)]
        let db_header_ref = Db::get_db_header_ref(&space.merkle.meta).unwrap();

        #[allow(clippy::unwrap_used)]
        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&space.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)
                .unwrap();

        let header_refs = (db_header_ref, merkle_payload_header_ref);

        #[allow(clippy::unwrap_used)]
        Db::new_revision(
            header_refs,
            (space.merkle.meta.clone(), space.merkle.payload.clone()),
            self.payload_regn_nbit,
            0,
            &self.cfg.rev,
        )
        .unwrap()
        .into()
    }

    /// Dump the Trie of the latest generic key-value storage.
    pub fn kv_dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.revisions.lock().base_revision.kv_dump(w)
    }
    /// Get root hash of the latest generic key-value storage.
    pub(crate) fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        self.revisions.lock().base_revision.kv_root_hash()
    }

    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}
