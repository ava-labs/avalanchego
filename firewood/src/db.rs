// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

pub use crate::{
    config::{DbConfig, DbRevConfig},
    storage::{buffer::DiskBufferConfig, WalConfig},
};
use crate::{
    file,
    merkle::{IdTrans, Merkle, MerkleError, Node, TrieHash, TRIE_HASH_LEN},
    proof::{Proof, ProofError},
    storage::{
        buffer::{BufferWrite, DiskBuffer, DiskBufferRequester},
        AshRecord, CachedSpace, MemStoreR, SpaceWrite, StoreConfig, StoreDelta, StoreRevMut,
        StoreRevShared, ZeroStore, PAGE_SIZE_NBIT,
    },
};
use bytemuck::{cast_slice, AnyBitPattern};
use metered::{metered, HitCount};
use parking_lot::{Mutex, RwLock};
use shale::{
    compact::{CompactSpace, CompactSpaceHeader},
    CachedStore, Obj, ObjPtr, ShaleError, ShaleStore, SpaceId, Storable, StoredView,
};
use std::{
    collections::VecDeque,
    error::Error,
    fmt,
    io::{Cursor, Write},
    mem::size_of,
    path::Path,
    sync::Arc,
    thread::JoinHandle,
};

const MERKLE_META_SPACE: SpaceId = 0x0;
const MERKLE_PAYLOAD_SPACE: SpaceId = 0x1;
const BLOB_META_SPACE: SpaceId = 0x2;
const BLOB_PAYLOAD_SPACE: SpaceId = 0x3;
const ROOT_HASH_SPACE: SpaceId = 0x4;
const SPACE_RESERVED: u64 = 0x1000;

const MAGIC_STR: &[u8; 16] = b"firewood v0.1\0\0\0";

type Store = CompactSpace<Node, StoreRevMut>;
type SharedStore = CompactSpace<Node, StoreRevShared>;

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
#[derive(Debug, Clone, Copy, AnyBitPattern)]
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
    fn new(meta: T, payload: T) -> Self {
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

impl SubUniverse<Arc<StoreRevMut>> {
    fn new_from_other(&self) -> SubUniverse<Arc<StoreRevMut>> {
        SubUniverse {
            meta: Arc::new(StoreRevMut::new_from_other(self.meta.as_ref())),
            payload: Arc::new(StoreRevMut::new_from_other(self.payload.as_ref())),
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

/// DB-wide metadata, it keeps track of the roots of the top-level tries.
#[derive(Debug)]
struct DbHeader {
    /// The root node of the account model storage. (Where the values are [Account] objects, which
    /// may contain the root for the secondary trie.)
    acc_root: ObjPtr<Node>,
    /// The root node of the generic key-value store.
    kv_root: ObjPtr<Node>,
}

impl DbHeader {
    pub const MSIZE: u64 = 16;

    pub fn new_empty() -> Self {
        Self {
            acc_root: ObjPtr::null(),
            kv_root: ObjPtr::null(),
        }
    }
}

impl Storable for DbHeader {
    fn hydrate<T: CachedStore>(addr: u64, mem: &T) -> Result<Self, shale::ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let acc_root = u64::from_le_bytes(raw.as_deref()[..8].try_into().expect("invalid slice"));
        let kv_root = u64::from_le_bytes(raw.as_deref()[8..].try_into().expect("invalid slice"));
        Ok(Self {
            acc_root: ObjPtr::new_from_addr(acc_root),
            kv_root: ObjPtr::new_from_addr(kv_root),
        })
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);
        cur.write_all(&self.acc_root.addr().to_le_bytes())?;
        cur.write_all(&self.kv_root.addr().to_le_bytes())?;
        Ok(())
    }
}

#[derive(Clone, Debug)]
/// Necessary linear space instances bundled for the state of the entire DB.
struct Universe<T> {
    merkle: SubUniverse<T>,
    blob: SubUniverse<T>,
}

impl Universe<StoreRevShared> {
    fn to_mem_store_r(&self) -> Universe<Arc<impl MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
            blob: self.blob.to_mem_store_r(),
        }
    }
}

impl Universe<Arc<StoreRevMut>> {
    fn new_from_other(&self) -> Universe<Arc<StoreRevMut>> {
        Universe {
            merkle: self.merkle.new_from_other(),
            blob: self.blob.new_from_other(),
        }
    }
}

impl Universe<Arc<CachedSpace>> {
    fn to_mem_store_r(&self) -> Universe<Arc<impl MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
            blob: self.blob.to_mem_store_r(),
        }
    }
}

impl<T: MemStoreR + 'static> Universe<Arc<T>> {
    fn rewind(
        &self,
        merkle_meta_writes: &[SpaceWrite],
        merkle_payload_writes: &[SpaceWrite],
        blob_meta_writes: &[SpaceWrite],
        blob_payload_writes: &[SpaceWrite],
    ) -> Universe<StoreRevShared> {
        Universe {
            merkle: self
                .merkle
                .rewind(merkle_meta_writes, merkle_payload_writes),
            blob: self.blob.rewind(blob_meta_writes, blob_payload_writes),
        }
    }
}

/// Some readable version of the DB.
#[derive(Debug)]
pub struct DbRev<S> {
    header: shale::Obj<DbHeader>,
    merkle: Merkle<S>,
}

impl<S: ShaleStore<Node> + Send + Sync> DbRev<S> {
    fn flush_dirty(&mut self) -> Option<()> {
        self.header.flush_dirty();
        self.merkle.flush_dirty()?;
        Some(())
    }

    fn borrow_split(&mut self) -> (&mut shale::Obj<DbHeader>, &mut Merkle<S>) {
        (&mut self.header, &mut self.merkle)
    }

    /// Get root hash of the generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        self.merkle
            .root_hash::<IdTrans>(self.header.kv_root)
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

    /// Provides a proof that a key is in the Trie.
    pub fn prove<K: AsRef<[u8]>>(&self, key: K) -> Result<Proof, MerkleError> {
        self.merkle
            .prove::<&[u8], IdTrans>(key.as_ref(), self.header.kv_root)
    }

    /// Verifies a range proof is valid for a set of keys.
    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        proof: Proof,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        values: Vec<V>,
    ) -> Result<bool, ProofError> {
        let hash: [u8; 32] = *self.kv_root_hash()?;
        let valid = proof.verify_range_proof(hash, first_key, last_key, keys, values)?;
        Ok(valid)
    }

    /// Check if the account exists.
    pub fn exist<K: AsRef<[u8]>>(&self, key: K) -> Result<bool, DbError> {
        Ok(match self.merkle.get(key, self.header.acc_root) {
            Ok(r) => r.is_some(),
            Err(e) => return Err(DbError::Merkle(e)),
        })
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

// #[metered(registry = DbMetrics, visibility = pub)]
impl Db {
    const PARAM_SIZE: u64 = size_of::<DbParams>() as u64;

    /// Open a database.
    pub fn new<P: AsRef<Path>>(db_path: P, cfg: &DbConfig) -> Result<Self, DbError> {
        // TODO: make sure all fds are released at the end
        if cfg.truncate {
            let _ = std::fs::remove_dir_all(db_path.as_ref());
        }

        let open_options = if cfg.truncate {
            file::Options::Truncate
        } else {
            file::Options::NoTruncate
        };

        let (db_path, reset) = file::open_dir(db_path, open_options)?;

        let merkle_path = file::touch_dir("merkle", &db_path)?;
        let merkle_meta_path = file::touch_dir("meta", &merkle_path)?;
        let merkle_payload_path = file::touch_dir("compact", &merkle_path)?;

        let blob_path = file::touch_dir("blob", &db_path)?;
        let blob_meta_path = file::touch_dir("meta", &blob_path)?;
        let blob_payload_path = file::touch_dir("compact", &blob_path)?;

        let root_hash_path = file::touch_dir("root_hash", &db_path)?;

        let file0 = crate::file::File::new(0, SPACE_RESERVED, &merkle_meta_path)?;
        let fd0 = file0.get_fd();

        if reset {
            // initialize dbparams
            if cfg.payload_file_nbit < cfg.payload_regn_nbit
                || cfg.payload_regn_nbit < PAGE_SIZE_NBIT
            {
                return Err(DbError::InvalidParams);
            }
            nix::unistd::ftruncate(fd0, 0).map_err(DbError::System)?;
            nix::unistd::ftruncate(fd0, 1 << cfg.meta_file_nbit).map_err(DbError::System)?;
            let header = DbParams {
                magic: *MAGIC_STR,
                meta_file_nbit: cfg.meta_file_nbit,
                payload_file_nbit: cfg.payload_file_nbit,
                payload_regn_nbit: cfg.payload_regn_nbit,
                wal_file_nbit: cfg.wal.file_nbit,
                wal_block_nbit: cfg.wal.block_nbit,
                root_hash_file_nbit: cfg.root_hash_file_nbit,
            };

            let header_bytes = unsafe {
                std::slice::from_raw_parts(
                    &header as *const DbParams as *const u8,
                    size_of::<DbParams>(),
                )
                .to_vec()
            };

            nix::sys::uio::pwrite(fd0, &header_bytes, 0).map_err(DbError::System)?;
        }

        // read DbParams
        let mut header_bytes = [0; size_of::<DbParams>()];
        nix::sys::uio::pread(fd0, &mut header_bytes, 0).map_err(DbError::System)?;
        drop(file0);
        let params: DbParams = cast_slice(&header_bytes)[0];

        let wal = WalConfig::builder()
            .file_nbit(params.wal_file_nbit)
            .block_nbit(params.wal_block_nbit)
            .max_revisions(cfg.wal.max_revisions)
            .build();
        let (sender, inbound) = tokio::sync::mpsc::channel(cfg.buffer.max_buffered);
        let disk_requester = DiskBufferRequester::new(sender);
        let buffer = cfg.buffer.clone();
        let disk_thread = Some(std::thread::spawn(move || {
            let disk_buffer = DiskBuffer::new(inbound, &buffer, &wal).unwrap();
            disk_buffer.run()
        }));

        let root_hash_cache = Arc::new(
            CachedSpace::new(
                &StoreConfig::builder()
                    .ncached_pages(cfg.root_hash_ncached_pages)
                    .ncached_files(cfg.root_hash_ncached_files)
                    .space_id(ROOT_HASH_SPACE)
                    .file_nbit(params.root_hash_file_nbit)
                    .rootdir(root_hash_path)
                    .build(),
                disk_requester.clone(),
            )
            .unwrap(),
        );

        // setup disk buffer
        let data_cache = Universe {
            merkle: SubUniverse::new(
                Arc::new(
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
                    .unwrap(),
                ),
                Arc::new(
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
                    .unwrap(),
                ),
            ),
            blob: SubUniverse::new(
                Arc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.meta_ncached_pages)
                            .ncached_files(cfg.meta_ncached_files)
                            .space_id(BLOB_META_SPACE)
                            .file_nbit(params.meta_file_nbit)
                            .rootdir(blob_meta_path)
                            .build(),
                        disk_requester.clone(),
                    )
                    .unwrap(),
                ),
                Arc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.payload_ncached_pages)
                            .ncached_files(cfg.payload_ncached_files)
                            .space_id(BLOB_PAYLOAD_SPACE)
                            .file_nbit(params.payload_file_nbit)
                            .rootdir(blob_payload_path)
                            .build(),
                        disk_requester.clone(),
                    )
                    .unwrap(),
                ),
            ),
        };

        [
            data_cache.merkle.meta.as_ref(),
            data_cache.merkle.payload.as_ref(),
            data_cache.blob.meta.as_ref(),
            data_cache.blob.payload.as_ref(),
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
            blob: get_sub_universe_from_empty_delta(&data_cache.blob),
        };

        let db_header_ref = Db::get_db_header_ref(&base.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&base.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

        let blob_payload_header_ref = Db::get_payload_header_ref(&base.blob.meta, 0)?;

        let header_refs = (
            db_header_ref,
            merkle_payload_header_ref,
            blob_payload_header_ref,
        );

        let base_revision = Db::new_revision(
            header_refs,
            (base.merkle.meta.clone(), base.merkle.payload.clone()),
            (base.blob.meta.clone(), base.blob.payload.clone()),
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

    /// Create a new mutable store and an alterable revision of the DB on top.
    fn new_store(
        cached_space: &Universe<Arc<CachedSpace>>,
        reset_store_headers: bool,
        payload_regn_nbit: u64,
        cfg: &DbConfig,
    ) -> Result<(Universe<Arc<StoreRevMut>>, DbRev<Store>), DbError> {
        let mut offset = Db::PARAM_SIZE;
        let db_header: ObjPtr<DbHeader> = ObjPtr::new_from_addr(offset);
        offset += DbHeader::MSIZE;
        let merkle_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(offset);
        offset += CompactSpaceHeader::MSIZE;
        assert!(offset <= SPACE_RESERVED);
        let blob_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0);

        let mut merkle_meta_store = StoreRevMut::new(cached_space.merkle.meta.clone());
        let mut blob_meta_store = StoreRevMut::new(cached_space.blob.meta.clone());

        if reset_store_headers {
            // initialize store headers
            merkle_meta_store.write(
                merkle_payload_header.addr(),
                &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                    SPACE_RESERVED,
                    SPACE_RESERVED,
                ))?,
            );
            merkle_meta_store.write(
                db_header.addr(),
                &shale::to_dehydrated(&DbHeader::new_empty())?,
            );
            blob_meta_store.write(
                blob_payload_header.addr(),
                &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                    SPACE_RESERVED,
                    SPACE_RESERVED,
                ))?,
            );
        }

        let store = Universe {
            merkle: SubUniverse::new(
                Arc::new(merkle_meta_store),
                Arc::new(StoreRevMut::new(cached_space.merkle.payload.clone())),
            ),
            blob: SubUniverse::new(
                Arc::new(blob_meta_store),
                Arc::new(StoreRevMut::new(cached_space.blob.payload.clone())),
            ),
        };

        let db_header_ref = Db::get_db_header_ref(store.merkle.meta.as_ref())?;

        let merkle_payload_header_ref = Db::get_payload_header_ref(
            store.merkle.meta.as_ref(),
            Db::PARAM_SIZE + DbHeader::MSIZE,
        )?;

        let blob_payload_header_ref = Db::get_payload_header_ref(store.blob.meta.as_ref(), 0)?;

        let header_refs = (
            db_header_ref,
            merkle_payload_header_ref,
            blob_payload_header_ref,
        );

        let mut rev: DbRev<CompactSpace<Node, StoreRevMut>> = Db::new_revision(
            header_refs,
            (store.merkle.meta.clone(), store.merkle.payload.clone()),
            (store.blob.meta.clone(), store.blob.payload.clone()),
            payload_regn_nbit,
            cfg.payload_max_walk,
            &cfg.rev,
        )?;
        rev.flush_dirty().unwrap();

        Ok((store, rev))
    }

    fn get_payload_header_ref<K: CachedStore>(
        meta_ref: &K,
        header_offset: u64,
    ) -> Result<Obj<CompactSpaceHeader>, DbError> {
        let payload_header = ObjPtr::<CompactSpaceHeader>::new_from_addr(header_offset);
        StoredView::ptr_to_obj(
            meta_ref,
            payload_header,
            shale::compact::CompactHeader::MSIZE,
        )
        .map_err(Into::into)
    }

    fn get_db_header_ref<K: CachedStore>(meta_ref: &K) -> Result<Obj<DbHeader>, DbError> {
        let db_header = ObjPtr::<DbHeader>::new_from_addr(Db::PARAM_SIZE);
        StoredView::ptr_to_obj(meta_ref, db_header, DbHeader::MSIZE).map_err(Into::into)
    }

    fn new_revision<K: CachedStore, T: Into<Arc<K>>>(
        header_refs: (
            Obj<DbHeader>,
            Obj<CompactSpaceHeader>,
            Obj<CompactSpaceHeader>,
        ),
        merkle: (T, T),
        _blob: (T, T),
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

        let merkle_space = shale::compact::CompactSpace::new(
            merkle_meta,
            merkle_payload,
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.merkle_ncached_objs),
            payload_max_walk,
            payload_regn_nbit,
        )
        .unwrap();

        if db_header_ref.acc_root.is_null() {
            let mut err = Ok(());
            // create the sentinel node
            db_header_ref
                .write(|r| {
                    err = (|| {
                        Merkle::<Store>::init_root(&mut r.acc_root, &merkle_space)?;
                        Merkle::<Store>::init_root(&mut r.kv_root, &merkle_space)
                    })();
                })
                .unwrap();
            err.map_err(DbError::Merkle)?
        }

        Ok(DbRev {
            header: db_header_ref,
            merkle: Merkle::new(Box::new(merkle_space)),
        })
    }

    /// Create a proposal.
    pub fn new_proposal<K: AsRef<[u8]>>(
        &self,
        data: Batch<K>,
    ) -> Result<Proposal<Store, SharedStore>, DbError> {
        let mut inner = self.inner.write();
        let reset_store_headers = inner.reset_store_headers;
        let (store, mut rev) = Db::new_store(
            &inner.cached_space,
            reset_store_headers,
            self.payload_regn_nbit,
            &self.cfg,
        )?;

        // Flip the reset flag after reseting the store headers.
        if reset_store_headers {
            inner.reset_store_headers = false;
        }

        data.into_iter().try_for_each(|op| -> Result<(), DbError> {
            match op {
                BatchOp::Put { key, value } => {
                    let (header, merkle) = rev.borrow_split();
                    merkle
                        .insert(key, value, header.kv_root)
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
        rev.flush_dirty().unwrap();

        let parent = ProposalBase::View(Arc::clone(&self.revisions.lock().base_revision));
        Ok(Proposal {
            m: Arc::clone(&self.inner),
            r: Arc::clone(&self.revisions),
            cfg: self.cfg.clone(),
            rev,
            store,
            committed: Arc::new(Mutex::new(false)),
            parent,
        })
    }

    /// Get a handle that grants the access to any committed state of the entire DB,
    /// with a given root hash. If the given root hash matches with more than one
    /// revisions, we use the most recent one as the trie are the same.
    ///
    /// If no revision with matching root hash found, returns None.
    // #[measure([HitCount])]
    pub fn get_revision(&self, root_hash: &TrieHash) -> Option<Revision<SharedStore>> {
        let mut revisions = self.revisions.lock();
        let inner_lock = self.inner.read();

        // Find the revision index with the given root hash.
        let mut nback = revisions.root_hashes.iter().position(|r| r == root_hash);
        let rlen = revisions.root_hashes.len();

        if nback.is_none() && rlen < revisions.max_revisions {
            let ashes = inner_lock
                .disk_requester
                .collect_ash(revisions.max_revisions)
                .ok()
                .unwrap();

            nback = ashes
                .iter()
                .skip(rlen)
                .map(|ash| {
                    StoreRevShared::from_ash(
                        Arc::new(ZeroStore::default()),
                        &ash.0[&ROOT_HASH_SPACE].redo,
                    )
                })
                .map(|root_hash_store| {
                    root_hash_store
                        .get_view(0, TRIE_HASH_LEN as u64)
                        .unwrap()
                        .as_deref()
                })
                .map(|data| TrieHash(data[..TRIE_HASH_LEN].try_into().unwrap()))
                .position(|trie_hash| &trie_hash == root_hash);
        }

        let Some(nback) = nback else {
            return None;
        };

        let rlen = revisions.inner.len();
        if rlen < nback {
            // TODO: Remove unwrap
            let ashes = inner_lock.disk_requester.collect_ash(nback).ok().unwrap();
            for mut ash in ashes.into_iter().skip(rlen) {
                for (_, a) in ash.0.iter_mut() {
                    a.undo.reverse()
                }

                let u = match revisions.inner.back() {
                    Some(u) => u.to_mem_store_r().rewind(
                        &ash.0[&MERKLE_META_SPACE].undo,
                        &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                        &ash.0[&BLOB_META_SPACE].undo,
                        &ash.0[&BLOB_PAYLOAD_SPACE].undo,
                    ),
                    None => inner_lock.cached_space.to_mem_store_r().rewind(
                        &ash.0[&MERKLE_META_SPACE].undo,
                        &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                        &ash.0[&BLOB_META_SPACE].undo,
                        &ash.0[&BLOB_PAYLOAD_SPACE].undo,
                    ),
                };
                revisions.inner.push_back(u);
            }
        }

        let space = if nback == 0 {
            &revisions.base
        } else {
            &revisions.inner[nback - 1]
        };
        // Release the lock after we find the revision
        drop(inner_lock);

        let db_header_ref = Db::get_db_header_ref(&space.merkle.meta).unwrap();

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&space.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)
                .unwrap();

        let blob_payload_header_ref = Db::get_payload_header_ref(&space.blob.meta, 0).unwrap();

        let header_refs = (
            db_header_ref,
            merkle_payload_header_ref,
            blob_payload_header_ref,
        );

        Revision {
            rev: Db::new_revision(
                header_refs,
                (space.merkle.meta.clone(), space.merkle.payload.clone()),
                (space.blob.meta.clone(), space.blob.payload.clone()),
                self.payload_regn_nbit,
                0,
                &self.cfg.rev,
            )
            .unwrap(),
        }
        .into()
    }
}

#[metered(registry = DbMetrics, visibility = pub)]
impl Db {
    /// Dump the Trie of the latest generic key-value storage.
    pub fn kv_dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.revisions.lock().base_revision.kv_dump(w)
    }
    /// Get root hash of the latest generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        self.revisions.lock().base_revision.kv_root_hash()
    }

    /// Get a value in the kv store associated with a particular key.
    #[measure(HitCount)]
    pub fn kv_get<K: AsRef<[u8]>>(&self, key: K) -> Result<Vec<u8>, DbError> {
        self.revisions
            .lock()
            .base_revision
            .kv_get(key)
            .ok_or(DbError::KeyNotFound)
    }

    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}

/// Lock protected handle to a readable version of the DB.
pub struct Revision<S> {
    rev: DbRev<S>,
}

impl<S> std::ops::Deref for Revision<S> {
    type Target = DbRev<S>;
    fn deref(&self) -> &DbRev<S> {
        &self.rev
    }
}

/// A key/value pair operation. Only put (upsert) and delete are
/// supported
#[derive(Debug)]
pub enum BatchOp<K> {
    Put { key: K, value: Vec<u8> },
    Delete { key: K },
}

/// A list of operations to consist of a batch that
/// can be proposed
pub type Batch<K> = Vec<BatchOp<K>>;

/// An atomic batch of changes proposed against the latest committed revision,
/// or any existing [Proposal]. Multiple proposals can be created against the
/// latest committed revision at the same time. [Proposal] is immutable meaning
/// the internal batch cannot be altered after creation. Committing a proposal
/// invalidates all other proposals that are not children of the committed one.
pub struct Proposal<S, T> {
    // State of the Db
    m: Arc<RwLock<DbInner>>,
    r: Arc<Mutex<DbRevInner<T>>>,
    cfg: DbConfig,

    // State of the proposal
    rev: DbRev<S>,
    store: Universe<Arc<StoreRevMut>>,
    committed: Arc<Mutex<bool>>,

    parent: ProposalBase<S, T>,
}

pub enum ProposalBase<S, T> {
    Proposal(Arc<Proposal<S, T>>),
    View(Arc<DbRev<T>>),
}

impl Proposal<Store, SharedStore> {
    // Propose a new proposal from this proposal. The new proposal will be
    // the child of it.
    pub fn propose<K: AsRef<[u8]>>(
        self: Arc<Self>,
        data: Batch<K>,
    ) -> Result<Proposal<Store, SharedStore>, DbError> {
        let store = self.store.new_from_other();

        let m = Arc::clone(&self.m);
        let r = Arc::clone(&self.r);
        let cfg = self.cfg.clone();

        let db_header_ref = Db::get_db_header_ref(store.merkle.meta.as_ref())?;

        let merkle_payload_header_ref = Db::get_payload_header_ref(
            store.merkle.meta.as_ref(),
            Db::PARAM_SIZE + DbHeader::MSIZE,
        )?;

        let blob_payload_header_ref = Db::get_payload_header_ref(store.blob.meta.as_ref(), 0)?;

        let header_refs = (
            db_header_ref,
            merkle_payload_header_ref,
            blob_payload_header_ref,
        );

        let mut rev = Db::new_revision(
            header_refs,
            (store.merkle.meta.clone(), store.merkle.payload.clone()),
            (store.blob.meta.clone(), store.blob.payload.clone()),
            cfg.payload_regn_nbit,
            cfg.payload_max_walk,
            &cfg.rev,
        )?;
        data.into_iter().try_for_each(|op| -> Result<(), DbError> {
            match op {
                BatchOp::Put { key, value } => {
                    let (header, merkle) = rev.borrow_split();
                    merkle
                        .insert(key, value, header.kv_root)
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
        rev.flush_dirty().unwrap();

        let parent = ProposalBase::Proposal(self);

        Ok(Proposal {
            m,
            r,
            cfg,
            rev,
            store,
            committed: Arc::new(Mutex::new(false)),
            parent,
        })
    }

    /// Persist all changes to the DB. The atomicity of the [Proposal] guarantees all changes are
    /// either retained on disk or lost together during a crash.
    pub fn commit(&self) -> Result<(), DbError> {
        let mut committed = self.committed.lock();
        if *committed {
            return Ok(());
        }

        if let ProposalBase::Proposal(p) = &self.parent {
            p.commit()?;
        };

        // Check for if it can be committed
        let mut revisions = self.r.lock();
        let committed_root_hash = revisions.base_revision.kv_root_hash().ok();
        let committed_root_hash =
            committed_root_hash.expect("committed_root_hash should not be none");
        match &self.parent {
            ProposalBase::Proposal(p) => {
                let parent_root_hash = p.rev.kv_root_hash().ok();
                let parent_root_hash =
                    parent_root_hash.expect("parent_root_hash should not be none");
                if parent_root_hash != committed_root_hash {
                    return Err(DbError::InvalidProposal);
                }
            }
            ProposalBase::View(p) => {
                let parent_root_hash = p.kv_root_hash().ok();
                let parent_root_hash =
                    parent_root_hash.expect("parent_root_hash should not be none");
                if parent_root_hash != committed_root_hash {
                    return Err(DbError::InvalidProposal);
                }
            }
        };

        let kv_root_hash = self.rev.kv_root_hash().ok();
        let kv_root_hash = kv_root_hash.expect("kv_root_hash should not be none");

        // clear the staging layer and apply changes to the CachedSpace
        let (merkle_payload_redo, merkle_payload_wal) = self.store.merkle.payload.delta();
        let (merkle_meta_redo, merkle_meta_wal) = self.store.merkle.meta.delta();
        let (blob_payload_redo, blob_payload_wal) = self.store.blob.payload.delta();
        let (blob_meta_redo, blob_meta_wal) = self.store.blob.meta.delta();

        let mut rev_inner = self.m.write();
        let merkle_meta_undo = rev_inner
            .cached_space
            .merkle
            .meta
            .update(&merkle_meta_redo)
            .unwrap();
        let merkle_payload_undo = rev_inner
            .cached_space
            .merkle
            .payload
            .update(&merkle_payload_redo)
            .unwrap();
        let blob_meta_undo = rev_inner
            .cached_space
            .blob
            .meta
            .update(&blob_meta_redo)
            .unwrap();
        let blob_payload_undo = rev_inner
            .cached_space
            .blob
            .payload
            .update(&blob_payload_redo)
            .unwrap();

        // update the rolling window of past revisions
        let latest_past = Universe {
            merkle: get_sub_universe_from_deltas(
                &rev_inner.cached_space.merkle,
                merkle_meta_undo,
                merkle_payload_undo,
            ),
            blob: get_sub_universe_from_deltas(
                &rev_inner.cached_space.blob,
                blob_meta_undo,
                blob_payload_undo,
            ),
        };

        let max_revisions = revisions.max_revisions;
        if let Some(rev) = revisions.inner.front_mut() {
            rev.merkle
                .meta
                .set_base_space(latest_past.merkle.meta.inner().clone());
            rev.merkle
                .payload
                .set_base_space(latest_past.merkle.payload.inner().clone());
            rev.blob
                .meta
                .set_base_space(latest_past.blob.meta.inner().clone());
            rev.blob
                .payload
                .set_base_space(latest_past.blob.payload.inner().clone());
        }
        revisions.inner.push_front(latest_past);
        while revisions.inner.len() > max_revisions {
            revisions.inner.pop_back();
        }

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&rev_inner.cached_space.merkle),
            blob: get_sub_universe_from_empty_delta(&rev_inner.cached_space.blob),
        };

        let db_header_ref = Db::get_db_header_ref(&base.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&base.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

        let blob_payload_header_ref = Db::get_payload_header_ref(&base.blob.meta, 0)?;

        let header_refs = (
            db_header_ref,
            merkle_payload_header_ref,
            blob_payload_header_ref,
        );

        let base_revision = Db::new_revision(
            header_refs,
            (base.merkle.meta.clone(), base.merkle.payload.clone()),
            (base.blob.meta.clone(), base.blob.payload.clone()),
            0,
            self.cfg.payload_max_walk,
            &self.cfg.rev,
        )?;
        revisions.base = base;
        revisions.base_revision = Arc::new(base_revision);

        // update the rolling window of root hashes
        revisions.root_hashes.push_front(kv_root_hash.clone());
        if revisions.root_hashes.len() > max_revisions {
            revisions
                .root_hashes
                .resize(max_revisions, TrieHash([0; TRIE_HASH_LEN]));
        }

        rev_inner.root_hash_staging.write(0, &kv_root_hash.0);
        let (root_hash_redo, root_hash_wal) = rev_inner.root_hash_staging.delta();

        // schedule writes to the disk
        rev_inner.disk_requester.write(
            vec![
                BufferWrite {
                    space_id: self.store.merkle.payload.id(),
                    delta: merkle_payload_redo,
                },
                BufferWrite {
                    space_id: self.store.merkle.meta.id(),
                    delta: merkle_meta_redo,
                },
                BufferWrite {
                    space_id: self.store.blob.payload.id(),
                    delta: blob_payload_redo,
                },
                BufferWrite {
                    space_id: self.store.blob.meta.id(),
                    delta: blob_meta_redo,
                },
                BufferWrite {
                    space_id: rev_inner.root_hash_staging.id(),
                    delta: root_hash_redo,
                },
            ],
            AshRecord(
                [
                    (MERKLE_META_SPACE, merkle_meta_wal),
                    (MERKLE_PAYLOAD_SPACE, merkle_payload_wal),
                    (BLOB_META_SPACE, blob_meta_wal),
                    (BLOB_PAYLOAD_SPACE, blob_payload_wal),
                    (ROOT_HASH_SPACE, root_hash_wal),
                ]
                .into(),
            ),
        );
        *committed = true;
        Ok(())
    }
}

impl<S: ShaleStore<Node> + Send + Sync, T: ShaleStore<Node> + Send + Sync> Proposal<S, T> {
    pub fn get_revision(&self) -> &DbRev<S> {
        &self.rev
    }
}

impl<S, T> Drop for Proposal<S, T> {
    fn drop(&mut self) {
        if !*self.committed.lock() {
            // drop the staging changes
            self.store.merkle.payload.reset_deltas();
            self.store.merkle.meta.reset_deltas();
            self.store.blob.payload.reset_deltas();
            self.store.blob.meta.reset_deltas();
            self.m.read().root_hash_staging.reset_deltas();
        }
    }
}
