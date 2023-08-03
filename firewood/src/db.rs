// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(feature = "eth")]
use crate::account::{Account, AccountRlp, Blob, BlobStash};
pub use crate::config::{DbConfig, DbRevConfig};
pub use crate::storage::{buffer::DiskBufferConfig, WalConfig};
use crate::storage::{
    AshRecord, CachedSpace, MemStoreR, SpaceWrite, StoreConfig, StoreDelta, StoreRevMut,
    StoreRevShared, ZeroStore, PAGE_SIZE_NBIT,
};
use crate::{
    file,
    merkle::{IdTrans, Merkle, MerkleError, Node, TrieHash, TRIE_HASH_LEN},
    proof::{Proof, ProofError},
    storage::buffer::{BufferWrite, DiskBuffer, DiskBufferRequester},
};
use bytemuck::{cast_slice, AnyBitPattern};
use metered::{metered, HitCount};
use parking_lot::{Mutex, RwLock};
#[cfg(feature = "eth")]
use primitive_types::U256;
use shale::compact::CompactSpace;
use shale::ShaleStore;
use shale::{
    compact::CompactSpaceHeader, CachedStore, ObjPtr, ShaleError, SpaceId, Storable, StoredView,
};
use std::{
    collections::VecDeque,
    error::Error,
    fmt,
    io::{Cursor, Write},
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

const MAGIC_STR: &[u8; 13] = b"firewood v0.1";

type Store = (
    Universe<Arc<StoreRevMut>>,
    DbRev<CompactSpace<Node, StoreRevMut>>,
);

#[derive(Debug)]
#[non_exhaustive]
pub enum DbError {
    InvalidParams,
    Merkle(MerkleError),
    #[cfg(feature = "eth")]
    Blob(crate::account::BlobError),
    System(nix::Error),
    KeyNotFound,
    CreateError,
    Shale(ShaleError),
    IO(std::io::Error),
}

impl fmt::Display for DbError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            DbError::InvalidParams => write!(f, "invalid parameters provided"),
            DbError::Merkle(e) => write!(f, "merkle error: {e:?}"),
            #[cfg(feature = "eth")]
            DbError::Blob(e) => write!(f, "storage error: {e:?}"),
            DbError::System(e) => write!(f, "system error: {e:?}"),
            DbError::KeyNotFound => write!(f, "not found"),
            DbError::CreateError => write!(f, "database create error"),
            DbError::IO(e) => write!(f, "I/O error: {e:?}"),
            DbError::Shale(e) => write!(f, "shale error: {e:?}"),
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
pub struct DbRev<S> {
    header: shale::Obj<DbHeader>,
    merkle: Merkle<S>,
    #[cfg(feature = "eth")]
    blob: BlobStash<S>,
}

impl<S: ShaleStore<Node> + Send + Sync> DbRev<S> {
    fn flush_dirty(&mut self) -> Option<()> {
        self.header.flush_dirty();
        self.merkle.flush_dirty()?;
        #[cfg(feature = "eth")]
        self.blob.flush_dirty()?;
        Some(())
    }

    #[cfg(feature = "eth")]
    fn borrow_split(&mut self) -> (&mut shale::Obj<DbHeader>, &mut Merkle<S>, &mut BlobStash<S>) {
        (&mut self.header, &mut self.merkle, &mut self.blob)
    }
    #[cfg(not(feature = "eth"))]
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

#[cfg(feature = "eth")]
impl<S: ShaleStore<Node> + Send + Sync> DbRev<S> {
    /// Get nonce of the account.
    pub fn get_nonce<K: AsRef<[u8]>>(&self, key: K) -> Result<u64, DbError> {
        Ok(self.get_account(key)?.nonce)
    }

    /// Get the state value indexed by `sub_key` in the account indexed by `key`.
    pub fn get_state<K: AsRef<[u8]>>(&self, key: K, sub_key: K) -> Result<Vec<u8>, DbError> {
        let root = self.get_account(key)?.root;
        if root.is_null() {
            return Ok(Vec::new());
        }
        Ok(match self.merkle.get(sub_key, root) {
            Ok(Some(v)) => v.to_vec(),
            Ok(None) => Vec::new(),
            Err(e) => return Err(DbError::Merkle(e)),
        })
    }

    /// Get root hash of the world state of all accounts.
    pub fn root_hash(&self) -> Result<TrieHash, DbError> {
        self.merkle
            .root_hash::<AccountRlp>(self.header.acc_root)
            .map_err(DbError::Merkle)
    }

    /// Dump the Trie of the entire account model storage.
    pub fn dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.merkle
            .dump(self.header.acc_root, w)
            .map_err(DbError::Merkle)
    }

    fn get_account<K: AsRef<[u8]>>(&self, key: K) -> Result<Account, DbError> {
        Ok(match self.merkle.get(key, self.header.acc_root) {
            Ok(Some(bytes)) => Account::deserialize(&bytes),
            Ok(None) => Account::default(),
            Err(e) => return Err(DbError::Merkle(e)),
        })
    }

    /// Dump the Trie of the state storage under an account.
    pub fn dump_account<K: AsRef<[u8]>>(&self, key: K, w: &mut dyn Write) -> Result<(), DbError> {
        let acc = match self.merkle.get(key, self.header.acc_root) {
            Ok(Some(bytes)) => Account::deserialize(&bytes),
            Ok(None) => Account::default(),
            Err(e) => return Err(DbError::Merkle(e)),
        };
        writeln!(w, "{acc:?}").unwrap();
        if !acc.root.is_null() {
            self.merkle.dump(acc.root, w).map_err(DbError::Merkle)?;
        }
        Ok(())
    }

    /// Get balance of the account.
    pub fn get_balance<K: AsRef<[u8]>>(&self, key: K) -> Result<U256, DbError> {
        Ok(self.get_account(key)?.balance)
    }

    /// Get code of the account.
    pub fn get_code<K: AsRef<[u8]>>(&self, key: K) -> Result<Vec<u8>, DbError> {
        let code = self.get_account(key)?.code;
        if code.is_null() {
            return Ok(Vec::new());
        }
        let b = self.blob.get_blob(code).map_err(DbError::Blob)?;
        Ok(match &**b {
            Blob::Code(code) => code.clone(),
        })
    }
}

struct DbInner<S> {
    latest: DbRev<S>,
    disk_requester: DiskBufferRequester,
    disk_thread: Option<JoinHandle<()>>,
    data_staging: Universe<Arc<StoreRevMut>>,
    data_cache: Universe<Arc<CachedSpace>>,
    root_hash_staging: StoreRevMut,
}

impl<S> Drop for DbInner<S> {
    fn drop(&mut self) {
        self.disk_requester.shutdown();
        self.disk_thread.take().map(JoinHandle::join);
    }
}

/// Firewood database handle.
pub struct Db<S> {
    inner: Arc<RwLock<DbInner<S>>>,
    revisions: Arc<Mutex<DbRevInner>>,
    payload_regn_nbit: u64,
    rev_cfg: DbRevConfig,
    metrics: Arc<DbMetrics>,
}

pub struct DbRevInner {
    inner: VecDeque<Universe<StoreRevShared>>,
    root_hashes: VecDeque<TrieHash>,
    max_revisions: usize,
    base: Universe<StoreRevShared>,
}

// #[metered(registry = DbMetrics, visibility = pub)]
impl Db<CompactSpace<Node, StoreRevMut>> {
    /// Open a database.
    pub fn new<P: AsRef<Path>>(db_path: P, cfg: &DbConfig) -> Result<Self, DbError> {
        // TODO: make sure all fds are released at the end
        if cfg.truncate {
            let _ = std::fs::remove_dir_all(db_path.as_ref());
        }
        let (db_path, reset) = file::open_dir(db_path, cfg.truncate)?;

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
            let mut magic = [0; 16];
            magic[..MAGIC_STR.len()].copy_from_slice(MAGIC_STR);
            let header = DbParams {
                magic,
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
                    std::mem::size_of::<DbParams>(),
                )
                .to_vec()
            };

            nix::sys::uio::pwrite(fd0, &header_bytes, 0).map_err(DbError::System)?;
        }

        // read DbParams
        let mut header_bytes = [0; std::mem::size_of::<DbParams>()];
        nix::sys::uio::pread(fd0, &mut header_bytes, 0).map_err(DbError::System)?;
        drop(file0);
        let offset = header_bytes.len() as u64;
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
        let (data_staging, mut latest) = Db::new_store(&data_cache, reset, offset, cfg, &params)?;
        latest.flush_dirty().unwrap();

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&data_cache.merkle),
            blob: get_sub_universe_from_empty_delta(&data_cache.blob),
        };

        Ok(Self {
            inner: Arc::new(RwLock::new(DbInner {
                latest,
                disk_thread,
                disk_requester,
                data_staging,
                data_cache,
                root_hash_staging,
            })),
            revisions: Arc::new(Mutex::new(DbRevInner {
                inner: VecDeque::new(),
                root_hashes: VecDeque::new(),
                max_revisions: cfg.wal.max_revisions as usize,
                base,
            })),
            payload_regn_nbit: params.payload_regn_nbit,
            rev_cfg: cfg.rev.clone(),
            metrics: Arc::new(DbMetrics::default()),
        })
    }

    /// Create a new mutable store and an alterable revision of the DB on top.
    fn new_store(
        cached_space: &Universe<Arc<CachedSpace>>,
        reset: bool,
        offset: u64,
        cfg: &DbConfig,
        params: &DbParams,
    ) -> Result<Store, DbError> {
        let mut offset = offset;
        let db_header: ObjPtr<DbHeader> = ObjPtr::new_from_addr(offset);
        offset += DbHeader::MSIZE;
        let merkle_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(offset);
        offset += CompactSpaceHeader::MSIZE;
        assert!(offset <= SPACE_RESERVED);
        let blob_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0);

        let mut merkle_meta_store = StoreRevMut::new(cached_space.merkle.meta.clone());
        let mut blob_meta_store = StoreRevMut::new(cached_space.blob.meta.clone());

        if reset {
            // initialize space headers
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

        let (mut db_header_ref, merkle_payload_header_ref, _blob_payload_header_ref) = {
            let merkle_meta_ref = store.merkle.meta.as_ref();
            let blob_meta_ref = store.blob.meta.as_ref();

            (
                StoredView::ptr_to_obj(merkle_meta_ref, db_header, DbHeader::MSIZE).unwrap(),
                StoredView::ptr_to_obj(
                    merkle_meta_ref,
                    merkle_payload_header,
                    shale::compact::CompactHeader::MSIZE,
                )
                .unwrap(),
                StoredView::ptr_to_obj(
                    blob_meta_ref,
                    blob_payload_header,
                    shale::compact::CompactHeader::MSIZE,
                )
                .unwrap(),
            )
        };

        let merkle_space = shale::compact::CompactSpace::new(
            store.merkle.meta.clone(),
            store.merkle.payload.clone(),
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.rev.merkle_ncached_objs),
            cfg.payload_max_walk,
            params.payload_regn_nbit,
        )
        .unwrap();

        #[cfg(feature = "eth")]
        let blob_space = shale::compact::CompactSpace::new(
            staging.blob.meta.clone(),
            staging.blob.payload.clone(),
            blob_payload_header_ref,
            shale::ObjCache::new(cfg.rev.blob_ncached_objs),
            cfg.payload_max_walk,
            header.payload_regn_nbit,
        )
        .unwrap();

        if db_header_ref.acc_root.is_null() {
            let mut err = Ok(());
            // create the sentinel node
            db_header_ref
                .write(|r| {
                    err = (|| {
                        Merkle::<CompactSpace<Node, StoreRevMut>>::init_root(
                            &mut r.acc_root,
                            &merkle_space,
                        )?;
                        Merkle::<CompactSpace<Node, StoreRevMut>>::init_root(
                            &mut r.kv_root,
                            &merkle_space,
                        )
                    })();
                })
                .unwrap();
            err.map_err(DbError::Merkle)?
        }

        let rev = DbRev {
            header: db_header_ref,
            merkle: Merkle::new(Box::new(merkle_space)),
            #[cfg(feature = "eth")]
            blob: BlobStash::new(Box::new(blob_space)),
        };

        Ok((store, rev))
    }

    /// Get a handle that grants the access to any committed state of the entire DB,
    /// with a given root hash. If the given root hash matches with more than one
    /// revisions, we use the most recent one as the trie are the same.
    ///
    /// If no revision with matching root hash found, returns None.
    // #[measure([HitCount])]
    pub fn get_revision(
        &self,
        root_hash: &TrieHash,
        cfg: Option<DbRevConfig>,
    ) -> Option<Revision<CompactSpace<Node, StoreRevShared>>> {
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
                    None => inner_lock.data_cache.to_mem_store_r().rewind(
                        &ash.0[&MERKLE_META_SPACE].undo,
                        &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                        &ash.0[&BLOB_META_SPACE].undo,
                        &ash.0[&BLOB_PAYLOAD_SPACE].undo,
                    ),
                };
                revisions.inner.push_back(u);
            }
        }

        // Set up the storage layout
        let mut offset = std::mem::size_of::<DbParams>() as u64;
        // DbHeader starts after DbParams in merkle meta space
        let db_header: ObjPtr<DbHeader> = ObjPtr::new_from_addr(offset);
        offset += DbHeader::MSIZE;
        // Merkle CompactHeader starts after DbHeader in merkle meta space
        let merkle_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(offset);
        offset += CompactSpaceHeader::MSIZE;
        assert!(offset <= SPACE_RESERVED);
        // Blob CompactSpaceHeader starts right in blob meta space
        let blob_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0);

        let space = if nback == 0 {
            &revisions.base
        } else {
            &revisions.inner[nback - 1]
        };
        // Release the lock after we find the revision
        drop(inner_lock);

        let (db_header_ref, merkle_payload_header_ref, _blob_payload_header_ref) = {
            let merkle_meta_ref = &space.merkle.meta;
            let blob_meta_ref = &space.blob.meta;

            (
                StoredView::ptr_to_obj(merkle_meta_ref, db_header, DbHeader::MSIZE).unwrap(),
                StoredView::ptr_to_obj(
                    merkle_meta_ref,
                    merkle_payload_header,
                    shale::compact::CompactHeader::MSIZE,
                )
                .unwrap(),
                StoredView::ptr_to_obj(
                    blob_meta_ref,
                    blob_payload_header,
                    shale::compact::CompactHeader::MSIZE,
                )
                .unwrap(),
            )
        };

        let merkle_space = shale::compact::CompactSpace::new(
            Arc::new(space.merkle.meta.clone()),
            Arc::new(space.merkle.payload.clone()),
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.as_ref().unwrap_or(&self.rev_cfg).merkle_ncached_objs),
            0,
            self.payload_regn_nbit,
        )
        .unwrap();

        #[cfg(feature = "eth")]
        let blob_space = shale::compact::CompactSpace::new(
            Arc::new(space.blob.meta.clone()),
            Arc::new(space.blob.payload.clone()),
            blob_payload_header_ref,
            shale::ObjCache::new(cfg.as_ref().unwrap_or(&self.rev_cfg).blob_ncached_objs),
            0,
            self.payload_regn_nbit,
        )
        .unwrap();

        Some(Revision {
            rev: DbRev {
                header: db_header_ref,
                merkle: Merkle::new(Box::new(merkle_space)),
                #[cfg(feature = "eth")]
                blob: BlobStash::new(Box::new(blob_space)),
            },
        })
    }
}

#[metered(registry = DbMetrics, visibility = pub)]
impl<S: ShaleStore<Node> + Send + Sync> Db<S> {
    /// Create a write batch.
    pub fn new_writebatch(&self) -> WriteBatch<S> {
        WriteBatch {
            m: Arc::clone(&self.inner),
            r: Arc::clone(&self.revisions),
            committed: false,
        }
    }

    /// Dump the Trie of the latest generic key-value storage.
    pub fn kv_dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.kv_dump(w)
    }
    /// Get root hash of the latest generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        self.inner.read().latest.kv_root_hash()
    }

    /// Get a value in the kv store associated with a particular key.
    #[measure(HitCount)]
    pub fn kv_get<K: AsRef<[u8]>>(&self, key: K) -> Result<Vec<u8>, DbError> {
        self.inner
            .read()
            .latest
            .kv_get(key)
            .ok_or(DbError::KeyNotFound)
    }

    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}
#[cfg(feature = "eth")]
impl<S: ShaleStore<Node> + Send + Sync> Db<S> {
    /// Dump the Trie of the latest entire account model storage.
    pub fn dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.dump(w)
    }

    /// Dump the Trie of the latest state storage under an account.
    pub fn dump_account<K: AsRef<[u8]>>(&self, key: K, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.dump_account(key, w)
    }

    /// Get root hash of the latest world state of all accounts.
    pub fn root_hash(&self) -> Result<TrieHash, DbError> {
        self.inner.read().latest.root_hash()
    }

    /// Get the latest balance of the account.
    pub fn get_balance<K: AsRef<[u8]>>(&self, key: K) -> Result<U256, DbError> {
        self.inner.read().latest.get_balance(key)
    }

    /// Get the latest code of the account.
    pub fn get_code<K: AsRef<[u8]>>(&self, key: K) -> Result<Vec<u8>, DbError> {
        self.inner.read().latest.get_code(key)
    }

    /// Get the latest nonce of the account.
    pub fn get_nonce<K: AsRef<[u8]>>(&self, key: K) -> Result<u64, DbError> {
        self.inner.read().latest.get_nonce(key)
    }

    /// Get the latest state value indexed by `sub_key` in the account indexed by `key`.
    pub fn get_state<K: AsRef<[u8]>>(&self, key: K, sub_key: K) -> Result<Vec<u8>, DbError> {
        self.inner.read().latest.get_state(key, sub_key)
    }

    /// Check if the account exists in the latest world state.
    pub fn exist<K: AsRef<[u8]>>(&self, key: K) -> Result<bool, DbError> {
        self.inner.read().latest.exist(key)
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

/// An atomic batch of changes made to the DB. Each operation on a [WriteBatch] will move itself
/// because when an error occurs, the write batch will be automatically aborted so that the DB
/// remains clean.
pub struct WriteBatch<S> {
    m: Arc<RwLock<DbInner<S>>>,
    r: Arc<Mutex<DbRevInner>>,
    committed: bool,
}

impl<S: ShaleStore<Node> + Send + Sync> WriteBatch<S> {
    /// Insert an item to the generic key-value storage.
    pub fn kv_insert<K: AsRef<[u8]>>(self, key: K, val: Vec<u8>) -> Result<Self, DbError> {
        let mut rev = self.m.write();
        #[cfg(feature = "eth")]
        let (header, merkle, _) = rev.latest.borrow_split();
        #[cfg(not(feature = "eth"))]
        let (header, merkle) = rev.latest.borrow_split();
        merkle
            .insert(key, val, header.kv_root)
            .map_err(DbError::Merkle)?;
        drop(rev);
        Ok(self)
    }

    /// Remove an item from the generic key-value storage. `val` will be set to the value that is
    /// removed from the storage if it exists.
    pub fn kv_remove<K: AsRef<[u8]>>(self, key: K) -> Result<(Self, Option<Vec<u8>>), DbError> {
        let mut rev = self.m.write();
        #[cfg(feature = "eth")]
        let (header, merkle, _) = rev.latest.borrow_split();
        #[cfg(not(feature = "eth"))]
        let (header, merkle) = rev.latest.borrow_split();
        let old_value = merkle
            .remove(key, header.kv_root)
            .map_err(DbError::Merkle)?;
        drop(rev);
        Ok((self, old_value))
    }

    /// Persist all changes to the DB. The atomicity of the [WriteBatch] guarantees all changes are
    /// either retained on disk or lost together during a crash.
    pub fn commit(mut self) {
        let mut rev_inner = self.m.write();

        #[cfg(feature = "eth")]
        rev_inner.latest.root_hash().ok();

        let kv_root_hash = rev_inner.latest.kv_root_hash().ok();
        let kv_root_hash = kv_root_hash.expect("kv_root_hash should not be none");

        // clear the staging layer and apply changes to the CachedSpace
        rev_inner.latest.flush_dirty().unwrap();
        let (merkle_payload_redo, merkle_payload_wal) =
            rev_inner.data_staging.merkle.payload.delta();
        rev_inner.data_staging.merkle.payload.reset_deltas();
        let (merkle_meta_redo, merkle_meta_wal) = rev_inner.data_staging.merkle.meta.delta();
        rev_inner.data_staging.merkle.meta.reset_deltas();
        let (blob_payload_redo, blob_payload_wal) = rev_inner.data_staging.blob.payload.delta();
        rev_inner.data_staging.blob.payload.reset_deltas();
        let (blob_meta_redo, blob_meta_wal) = rev_inner.data_staging.blob.meta.delta();
        rev_inner.data_staging.blob.meta.reset_deltas();
        let merkle_meta_undo = rev_inner
            .data_cache
            .merkle
            .meta
            .update(&merkle_meta_redo)
            .unwrap();
        let merkle_payload_undo = rev_inner
            .data_cache
            .merkle
            .payload
            .update(&merkle_payload_redo)
            .unwrap();
        let blob_meta_undo = rev_inner
            .data_cache
            .blob
            .meta
            .update(&blob_meta_redo)
            .unwrap();
        let blob_payload_undo = rev_inner
            .data_cache
            .blob
            .payload
            .update(&blob_payload_redo)
            .unwrap();

        // update the rolling window of past revisions
        let latest_past = Universe {
            merkle: get_sub_universe_from_deltas(
                &rev_inner.data_cache.merkle,
                merkle_meta_undo,
                merkle_payload_undo,
            ),
            blob: get_sub_universe_from_deltas(
                &rev_inner.data_cache.blob,
                blob_meta_undo,
                blob_payload_undo,
            ),
        };

        let mut revisions = self.r.lock();
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
            merkle: get_sub_universe_from_empty_delta(&rev_inner.data_cache.merkle),
            blob: get_sub_universe_from_empty_delta(&rev_inner.data_cache.blob),
        };
        revisions.base = base;

        // update the rolling window of root hashes
        revisions.root_hashes.push_front(kv_root_hash.clone());
        if revisions.root_hashes.len() > max_revisions {
            revisions
                .root_hashes
                .resize(max_revisions, TrieHash([0; TRIE_HASH_LEN]));
        }

        rev_inner.root_hash_staging.write(0, &kv_root_hash.0);
        let (root_hash_redo, root_hash_wal) = rev_inner.root_hash_staging.delta();

        self.committed = true;

        // schedule writes to the disk
        rev_inner.disk_requester.write(
            vec![
                BufferWrite {
                    space_id: rev_inner.data_staging.merkle.payload.id(),
                    delta: merkle_payload_redo,
                },
                BufferWrite {
                    space_id: rev_inner.data_staging.merkle.meta.id(),
                    delta: merkle_meta_redo,
                },
                BufferWrite {
                    space_id: rev_inner.data_staging.blob.payload.id(),
                    delta: blob_payload_redo,
                },
                BufferWrite {
                    space_id: rev_inner.data_staging.blob.meta.id(),
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
    }
}

#[cfg(feature = "eth")]
impl<S: ShaleStore<Node> + Send + Sync> WriteBatch<S> {
    fn change_account(
        &mut self,
        key: &[u8],
        modify: impl FnOnce(&mut Account, &mut BlobStash<S>) -> Result<(), DbError>,
    ) -> Result<(), DbError> {
        let mut rev = self.m.write();
        let (header, merkle, blob) = rev.latest.borrow_split();
        match merkle.get_mut(key, header.acc_root) {
            Ok(Some(mut bytes)) => {
                let mut ret = Ok(());
                bytes
                    .write(|b| {
                        let mut acc = Account::deserialize(b);
                        ret = modify(&mut acc, blob);
                        if ret.is_err() {
                            return;
                        }
                        *b = acc.serialize();
                    })
                    .map_err(DbError::Merkle)?;
                ret?;
            }
            Ok(None) => {
                let mut acc = Account::default();
                modify(&mut acc, blob)?;
                merkle
                    .insert(key, acc.serialize(), header.acc_root)
                    .map_err(DbError::Merkle)?;
            }
            Err(e) => return Err(DbError::Merkle(e)),
        }
        Ok(())
    }

    /// Set balance of the account.
    pub fn set_balance(mut self, key: &[u8], balance: U256) -> Result<Self, DbError> {
        self.change_account(key, |acc, _| {
            acc.balance = balance;
            Ok(())
        })?;
        Ok(self)
    }

    /// Set code of the account.
    pub fn set_code(mut self, key: &[u8], code: &[u8]) -> Result<Self, DbError> {
        use sha3::Digest;
        self.change_account(key, |acc, blob_stash| {
            if !acc.code.is_null() {
                blob_stash.free_blob(acc.code).map_err(DbError::Blob)?;
            }
            acc.set_code(
                TrieHash(sha3::Keccak256::digest(code).into()),
                blob_stash
                    .new_blob(Blob::Code(code.to_vec()))
                    .map_err(DbError::Blob)?
                    .as_ptr(),
            );
            Ok(())
        })?;
        Ok(self)
    }

    /// Set nonce of the account.
    pub fn set_nonce(mut self, key: &[u8], nonce: u64) -> Result<Self, DbError> {
        self.change_account(key, |acc, _| {
            acc.nonce = nonce;
            Ok(())
        })?;
        Ok(self)
    }

    /// Set the state value indexed by `sub_key` in the account indexed by `key`.
    pub fn set_state(self, key: &[u8], sub_key: &[u8], val: Vec<u8>) -> Result<Self, DbError> {
        let mut rev = self.m.write();
        let (header, merkle, _) = rev.latest.borrow_split();
        let mut acc = match merkle.get(key, header.acc_root) {
            Ok(Some(r)) => Account::deserialize(&r),
            Ok(None) => Account::default(),
            Err(e) => return Err(DbError::Merkle(e)),
        };
        if acc.root.is_null() {
            Merkle::init_root(&mut acc.root, merkle.get_store()).map_err(DbError::Merkle)?;
        }
        merkle
            .insert(sub_key, val, acc.root)
            .map_err(DbError::Merkle)?;
        acc.root_hash = merkle
            .root_hash::<IdTrans>(acc.root)
            .map_err(DbError::Merkle)?;
        merkle
            .insert(key, acc.serialize(), header.acc_root)
            .map_err(DbError::Merkle)?;
        drop(rev);
        Ok(self)
    }

    /// Create an account.
    pub fn create_account(self, key: &[u8]) -> Result<Self, DbError> {
        let mut rev = self.m.write();
        let (header, merkle, _) = rev.latest.borrow_split();
        let old_balance = match merkle.get_mut(key, header.acc_root) {
            Ok(Some(bytes)) => Account::deserialize(&bytes.get()).balance,
            Ok(None) => U256::zero(),
            Err(e) => return Err(DbError::Merkle(e)),
        };
        let acc = Account {
            balance: old_balance,
            ..Default::default()
        };
        merkle
            .insert(key, acc.serialize(), header.acc_root)
            .map_err(DbError::Merkle)?;

        drop(rev);
        Ok(self)
    }

    /// Delete an account.
    pub fn delete_account(self, key: &[u8], acc: &mut Option<Account>) -> Result<Self, DbError> {
        let mut rev = self.m.write();
        let (header, merkle, blob_stash) = rev.latest.borrow_split();
        let mut a = match merkle.remove(key, header.acc_root) {
            Ok(Some(bytes)) => Account::deserialize(&bytes),
            Ok(None) => {
                *acc = None;
                drop(rev);
                return Ok(self);
            }
            Err(e) => return Err(DbError::Merkle(e)),
        };
        if !a.root.is_null() {
            merkle.remove_tree(a.root).map_err(DbError::Merkle)?;
            a.root = ObjPtr::null();
        }
        if !a.code.is_null() {
            blob_stash.free_blob(a.code).map_err(DbError::Blob)?;
            a.code = ObjPtr::null();
        }
        *acc = Some(a);
        drop(rev);
        Ok(self)
    }
}

impl<S> Drop for WriteBatch<S> {
    fn drop(&mut self) {
        if !self.committed {
            // drop the staging changes
            self.m.read().data_staging.merkle.payload.reset_deltas();
            self.m.read().data_staging.merkle.meta.reset_deltas();
            self.m.read().data_staging.blob.payload.reset_deltas();
            self.m.read().data_staging.blob.meta.reset_deltas();
            self.m.read().root_hash_staging.reset_deltas();
        }
    }
}
