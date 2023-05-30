// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::collections::VecDeque;
use std::error::Error;
use std::fmt;
use std::io::{Cursor, Write};
use std::path::Path;
use std::rc::Rc;
use std::sync::Arc;
use std::thread::JoinHandle;

use bytemuck::{cast_slice, AnyBitPattern};
use metered::{metered, HitCount};
use parking_lot::{Mutex, RwLock};
#[cfg(feature = "eth")]
use primitive_types::U256;
use shale::ShaleError;
use shale::{compact::CompactSpaceHeader, CachedStore, ObjPtr, SpaceID, Storable, StoredView};
use typed_builder::TypedBuilder;

#[cfg(feature = "eth")]
use crate::account::{Account, AccountRlp, Blob, BlobStash};
use crate::file;
use crate::merkle::{Hash, IdTrans, Merkle, MerkleError, Node};
use crate::proof::{Proof, ProofError};
use crate::storage::buffer::{BufferWrite, DiskBuffer, DiskBufferRequester};
pub use crate::storage::{buffer::DiskBufferConfig, WalConfig};
use crate::storage::{
    AshRecord, CachedSpace, MemStoreR, SpaceWrite, StoreConfig, StoreDelta, StoreRevMut,
    StoreRevShared, PAGE_SIZE_NBIT,
};

const MERKLE_META_SPACE: SpaceID = 0x0;
const MERKLE_PAYLOAD_SPACE: SpaceID = 0x1;
const BLOB_META_SPACE: SpaceID = 0x2;
const BLOB_PAYLOAD_SPACE: SpaceID = 0x3;
const SPACE_RESERVED: u64 = 0x1000;

const MAGIC_STR: &[u8; 13] = b"firewood v0.1";

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
}

/// Config for accessing a version of the DB.
#[derive(TypedBuilder, Clone, Debug)]
pub struct DbRevConfig {
    /// Maximum cached Trie objects.
    #[builder(default = 1 << 20)]
    pub merkle_ncached_objs: usize,
    /// Maximum cached Blob (currently just `Account`) objects.
    #[builder(default = 4096)]
    pub blob_ncached_objs: usize,
}

/// Database configuration.
#[derive(TypedBuilder, Debug)]
pub struct DbConfig {
    /// Maximum cached pages for the free list of the item stash.
    #[builder(default = 16384)] // 64M total size by default
    pub meta_ncached_pages: usize,
    /// Maximum cached file descriptors for the free list of the item stash.
    #[builder(default = 1024)] // 1K fds by default
    pub meta_ncached_files: usize,
    /// Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    /// the power of 2 for the file size.
    #[builder(default = 22)] // 4MB file by default
    pub meta_file_nbit: u64,
    /// Maximum cached pages for the item stash. This is the low-level cache used by the linear
    /// space that holds Trie nodes and account objects.
    #[builder(default = 262144)] // 1G total size by default
    pub payload_ncached_pages: usize,
    /// Maximum cached file descriptors for the item stash.
    #[builder(default = 1024)] // 1K fds by default
    pub payload_ncached_files: usize,
    /// Number of low-bits in the 64-bit address to determine the file ID. It is the exponent to
    /// the power of 2 for the file size.
    #[builder(default = 22)] // 4MB file by default
    pub payload_file_nbit: u64,
    /// Maximum steps of walk to recycle a freed item.
    #[builder(default = 10)]
    pub payload_max_walk: u64,
    /// Region size in bits (should be not greater than `payload_file_nbit`). One file is
    /// partitioned into multiple regions. Just use the default value.
    #[builder(default = 22)]
    pub payload_regn_nbit: u64,
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    /// Config for accessing a version of the DB.
    #[builder(default = DbRevConfig::builder().build())]
    pub rev: DbRevConfig,
    /// Config for the disk buffer.
    #[builder(default = DiskBufferConfig::builder().build())]
    pub buffer: DiskBufferConfig,
    /// Config for Wal.
    #[builder(default = WalConfig::builder().build())]
    pub wal: WalConfig,
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
    fn to_mem_store_r(&self) -> SubUniverse<Rc<dyn MemStoreR>> {
        SubUniverse {
            meta: self.meta.inner().clone(),
            payload: self.payload.inner().clone(),
        }
    }
}

impl SubUniverse<Rc<dyn MemStoreR>> {
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

impl SubUniverse<Rc<CachedSpace>> {
    fn to_mem_store_r(&self) -> SubUniverse<Rc<dyn MemStoreR>> {
        SubUniverse {
            meta: self.meta.clone(),
            payload: self.payload.clone(),
        }
    }
}

fn get_sub_universe_from_deltas(
    sub_universe: &SubUniverse<Rc<CachedSpace>>,
    meta_delta: StoreDelta,
    payload_delta: StoreDelta,
) -> SubUniverse<StoreRevShared> {
    SubUniverse::new(
        StoreRevShared::from_delta(sub_universe.meta.clone(), meta_delta),
        StoreRevShared::from_delta(sub_universe.payload.clone(), payload_delta),
    )
}

fn get_sub_universe_from_empty_delta(
    sub_universe: &SubUniverse<Rc<CachedSpace>>,
) -> SubUniverse<StoreRevShared> {
    get_sub_universe_from_deltas(sub_universe, StoreDelta::default(), StoreDelta::default())
}

/// DB-wide metadata, it keeps track of the roots of the top-level tries.
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
    fn to_mem_store_r(&self) -> Universe<Rc<dyn MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
            blob: self.blob.to_mem_store_r(),
        }
    }
}

impl Universe<Rc<CachedSpace>> {
    fn to_mem_store_r(&self) -> Universe<Rc<dyn MemStoreR>> {
        Universe {
            merkle: self.merkle.to_mem_store_r(),
            blob: self.blob.to_mem_store_r(),
        }
    }
}

impl Universe<Rc<dyn MemStoreR>> {
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
pub struct DbRev {
    header: shale::Obj<DbHeader>,
    merkle: Merkle,
    #[cfg(feature = "eth")]
    blob: BlobStash,
}

impl DbRev {
    fn flush_dirty(&mut self) -> Option<()> {
        self.header.flush_dirty();
        self.merkle.flush_dirty()?;
        #[cfg(feature = "eth")]
        self.blob.flush_dirty()?;
        Some(())
    }

    #[cfg(feature = "eth")]
    fn borrow_split(&mut self) -> (&mut shale::Obj<DbHeader>, &mut Merkle, &mut BlobStash) {
        (&mut self.header, &mut self.merkle, &mut self.blob)
    }
    #[cfg(not(feature = "eth"))]
    fn borrow_split(&mut self) -> (&mut shale::Obj<DbHeader>, &mut Merkle) {
        (&mut self.header, &mut self.merkle)
    }

    /// Get root hash of the generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<Hash, DbError> {
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
impl DbRev {
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
    pub fn root_hash(&self) -> Result<Hash, DbError> {
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

struct DbInner {
    latest: DbRev,
    disk_requester: DiskBufferRequester,
    disk_thread: Option<JoinHandle<()>>,
    staging: Universe<Rc<StoreRevMut>>,
    cached: Universe<Rc<CachedSpace>>,
}

impl Drop for DbInner {
    fn drop(&mut self) {
        self.disk_requester.shutdown();
        self.disk_thread.take().map(JoinHandle::join);
    }
}

/// Firewood database handle.
pub struct Db {
    inner: Arc<RwLock<DbInner>>,
    revisions: Arc<Mutex<DbRevInner>>,
    payload_regn_nbit: u64,
    rev_cfg: DbRevConfig,
    metrics: Arc<DbMetrics>,
}

pub struct DbRevInner {
    inner: VecDeque<Universe<StoreRevShared>>,
    max_revisions: usize,
    base: Universe<StoreRevShared>,
}

#[metered(registry = DbMetrics, visibility = pub)]
impl Db {
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
            };
            nix::sys::uio::pwrite(fd0, &shale::util::get_raw_bytes(&header), 0)
                .map_err(DbError::System)?;
        }

        // read DbParams
        let mut header_bytes = [0; std::mem::size_of::<DbParams>()];
        nix::sys::uio::pread(fd0, &mut header_bytes, 0).map_err(DbError::System)?;
        drop(file0);
        let mut offset = header_bytes.len() as u64;
        let header: DbParams = cast_slice(&header_bytes)[0];

        // setup disk buffer
        let cached = Universe {
            merkle: SubUniverse::new(
                Rc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.meta_ncached_pages)
                            .ncached_files(cfg.meta_ncached_files)
                            .space_id(MERKLE_META_SPACE)
                            .file_nbit(header.meta_file_nbit)
                            .rootdir(merkle_meta_path)
                            .build(),
                    )
                    .unwrap(),
                ),
                Rc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.payload_ncached_pages)
                            .ncached_files(cfg.payload_ncached_files)
                            .space_id(MERKLE_PAYLOAD_SPACE)
                            .file_nbit(header.payload_file_nbit)
                            .rootdir(merkle_payload_path)
                            .build(),
                    )
                    .unwrap(),
                ),
            ),
            blob: SubUniverse::new(
                Rc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.meta_ncached_pages)
                            .ncached_files(cfg.meta_ncached_files)
                            .space_id(BLOB_META_SPACE)
                            .file_nbit(header.meta_file_nbit)
                            .rootdir(blob_meta_path)
                            .build(),
                    )
                    .unwrap(),
                ),
                Rc::new(
                    CachedSpace::new(
                        &StoreConfig::builder()
                            .ncached_pages(cfg.payload_ncached_pages)
                            .ncached_files(cfg.payload_ncached_files)
                            .space_id(BLOB_PAYLOAD_SPACE)
                            .file_nbit(header.payload_file_nbit)
                            .rootdir(blob_payload_path)
                            .build(),
                    )
                    .unwrap(),
                ),
            ),
        };

        let wal = WalConfig::builder()
            .file_nbit(header.wal_file_nbit)
            .block_nbit(header.wal_block_nbit)
            .max_revisions(cfg.wal.max_revisions)
            .build();
        let (sender, inbound) = tokio::sync::mpsc::channel(cfg.buffer.max_buffered);
        let disk_requester = DiskBufferRequester::new(sender);
        let buffer = cfg.buffer.clone();
        let disk_thread = Some(std::thread::spawn(move || {
            let disk_buffer = DiskBuffer::new(inbound, &buffer, &wal).unwrap();
            disk_buffer.run()
        }));

        disk_requester.reg_cached_space(cached.merkle.meta.as_ref());
        disk_requester.reg_cached_space(cached.merkle.payload.as_ref());
        disk_requester.reg_cached_space(cached.blob.meta.as_ref());
        disk_requester.reg_cached_space(cached.blob.payload.as_ref());

        let mut staging = Universe {
            merkle: SubUniverse::new(
                Rc::new(StoreRevMut::new(
                    cached.merkle.meta.clone() as Rc<dyn MemStoreR>
                )),
                Rc::new(StoreRevMut::new(
                    cached.merkle.payload.clone() as Rc<dyn MemStoreR>
                )),
            ),
            blob: SubUniverse::new(
                Rc::new(StoreRevMut::new(
                    cached.blob.meta.clone() as Rc<dyn MemStoreR>
                )),
                Rc::new(StoreRevMut::new(
                    cached.blob.payload.clone() as Rc<dyn MemStoreR>
                )),
            ),
        };

        // recover from Wal
        disk_requester.init_wal("wal", db_path);

        // set up the storage layout

        let db_header: ObjPtr<DbHeader> = ObjPtr::new_from_addr(offset);
        offset += DbHeader::MSIZE;
        let merkle_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(offset);
        offset += CompactSpaceHeader::MSIZE;
        assert!(offset <= SPACE_RESERVED);
        let blob_payload_header: ObjPtr<CompactSpaceHeader> = ObjPtr::new_from_addr(0);

        if reset {
            // initialize space headers
            let initializer = Rc::<StoreRevMut>::make_mut(&mut staging.merkle.meta);
            initializer.write(
                merkle_payload_header.addr(),
                &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                    SPACE_RESERVED,
                    SPACE_RESERVED,
                ))?,
            );
            initializer.write(
                db_header.addr(),
                &shale::to_dehydrated(&DbHeader::new_empty())?,
            );
            let initializer = Rc::<StoreRevMut>::make_mut(&mut staging.blob.meta);
            initializer.write(
                blob_payload_header.addr(),
                &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                    SPACE_RESERVED,
                    SPACE_RESERVED,
                ))?,
            );
        }

        let (mut db_header_ref, merkle_payload_header_ref, _blob_payload_header_ref) = {
            let merkle_meta_ref = staging.merkle.meta.as_ref();
            let blob_meta_ref = staging.blob.meta.as_ref();

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
            staging.merkle.meta.clone(),
            staging.merkle.payload.clone(),
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.rev.merkle_ncached_objs),
            cfg.payload_max_walk,
            header.payload_regn_nbit,
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
                        Merkle::init_root(&mut r.acc_root, &merkle_space)?;
                        Merkle::init_root(&mut r.kv_root, &merkle_space)
                    })();
                })
                .unwrap();
            err.map_err(DbError::Merkle)?
        }

        let mut latest = DbRev {
            header: db_header_ref,
            merkle: Merkle::new(Box::new(merkle_space)),
            #[cfg(feature = "eth")]
            blob: BlobStash::new(Box::new(blob_space)),
        };
        latest.flush_dirty().unwrap();

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&cached.merkle),
            blob: get_sub_universe_from_empty_delta(&cached.blob),
        };

        Ok(Self {
            inner: Arc::new(RwLock::new(DbInner {
                latest,
                disk_thread,
                disk_requester,
                staging,
                cached,
            })),
            revisions: Arc::new(Mutex::new(DbRevInner {
                inner: VecDeque::new(),
                max_revisions: cfg.wal.max_revisions as usize,
                base,
            })),
            payload_regn_nbit: header.payload_regn_nbit,
            rev_cfg: cfg.rev.clone(),
            metrics: Arc::new(DbMetrics::default()),
        })
    }

    /// Create a write batch.
    pub fn new_writebatch(&self) -> WriteBatch {
        WriteBatch {
            m: Arc::clone(&self.inner),
            r: Arc::clone(&self.revisions),
            root_hash_recalc: true,
            committed: false,
        }
    }

    /// Dump the Trie of the latest generic key-value storage.
    pub fn kv_dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.kv_dump(w)
    }
    /// Get root hash of the latest generic key-value storage.
    pub fn kv_root_hash(&self) -> Result<Hash, DbError> {
        self.inner.read().latest.kv_root_hash()
    }

    /// Get a value in the kv store associated with a particular key.
    #[measure([HitCount])]
    pub fn kv_get<K: AsRef<[u8]>>(&self, key: K) -> Result<Vec<u8>, DbError> {
        self.inner
            .read()
            .latest
            .kv_get(key)
            .ok_or(DbError::KeyNotFound)
    }
    /// Get a handle that grants the access to any committed state of the entire DB.
    ///
    /// The latest revision (nback) starts from 0, which is the current state.
    /// If nback equals is above the configured maximum number of revisions, this function returns None.
    /// Returns `None` if `nback` is greater than the configured maximum amount of revisions.
    #[measure([HitCount])]
    pub fn get_revision(&self, nback: usize, cfg: Option<DbRevConfig>) -> Option<Revision> {
        let mut revisions = self.revisions.lock();
        let inner = self.inner.read();

        let rlen = revisions.inner.len();
        if nback > revisions.max_revisions {
            return None;
        }
        if rlen < nback {
            // TODO: Remove unwrap
            let ashes = inner.disk_requester.collect_ash(nback).ok().unwrap();
            for mut ash in ashes.into_iter().skip(rlen) {
                for (_, a) in ash.0.iter_mut() {
                    a.undo.reverse()
                }

                let u = match revisions.inner.back() {
                    Some(u) => u.to_mem_store_r(),
                    None => inner.cached.to_mem_store_r(),
                };
                revisions.inner.push_back(u.rewind(
                    &ash.0[&MERKLE_META_SPACE].undo,
                    &ash.0[&MERKLE_PAYLOAD_SPACE].undo,
                    &ash.0[&BLOB_META_SPACE].undo,
                    &ash.0[&BLOB_PAYLOAD_SPACE].undo,
                ));
            }
        }
        if revisions.inner.len() < nback {
            return None;
        }
        // set up the storage layout

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
        drop(inner);

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
            Rc::new(space.merkle.meta.clone()),
            Rc::new(space.merkle.payload.clone()),
            merkle_payload_header_ref,
            shale::ObjCache::new(cfg.as_ref().unwrap_or(&self.rev_cfg).merkle_ncached_objs),
            0,
            self.payload_regn_nbit,
        )
        .unwrap();

        #[cfg(feature = "eth")]
        let blob_space = shale::compact::CompactSpace::new(
            Rc::new(space.blob.meta.clone()),
            Rc::new(space.blob.payload.clone()),
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
    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}
#[cfg(feature = "eth")]
impl Db {
    /// Dump the Trie of the latest entire account model storage.
    pub fn dump(&self, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.dump(w)
    }

    /// Dump the Trie of the latest state storage under an account.
    pub fn dump_account<K: AsRef<[u8]>>(&self, key: K, w: &mut dyn Write) -> Result<(), DbError> {
        self.inner.read().latest.dump_account(key, w)
    }

    /// Get root hash of the latest world state of all accounts.
    pub fn root_hash(&self) -> Result<Hash, DbError> {
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
pub struct Revision {
    rev: DbRev,
}

impl std::ops::Deref for Revision {
    type Target = DbRev;
    fn deref(&self) -> &DbRev {
        &self.rev
    }
}

/// An atomic batch of changes made to the DB. Each operation on a [WriteBatch] will move itself
/// because when an error occurs, the write batch will be automatically aborted so that the DB
/// remains clean.
pub struct WriteBatch {
    m: Arc<RwLock<DbInner>>,
    r: Arc<Mutex<DbRevInner>>,
    root_hash_recalc: bool,
    committed: bool,
}

impl WriteBatch {
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
    /// Do not rehash merkle roots upon commit. This will leave the recalculation of the dirty root
    /// hashes to future invocation of `root_hash`, `kv_root_hash` or batch commits.
    pub fn no_root_hash(mut self) -> Self {
        self.root_hash_recalc = false;
        self
    }

    /// Persist all changes to the DB. The atomicity of the [WriteBatch] guarantees all changes are
    /// either retained on disk or lost together during a crash.
    pub fn commit(mut self) {
        let mut rev_inner = self.m.write();
        if self.root_hash_recalc {
            #[cfg(feature = "eth")]
            rev_inner.latest.root_hash().ok();
            rev_inner.latest.kv_root_hash().ok();
        }
        // clear the staging layer and apply changes to the CachedSpace
        rev_inner.latest.flush_dirty().unwrap();
        let (merkle_payload_pages, merkle_payload_plain) =
            rev_inner.staging.merkle.payload.take_delta();
        let (merkle_meta_pages, merkle_meta_plain) = rev_inner.staging.merkle.meta.take_delta();
        let (blob_payload_pages, blob_payload_plain) = rev_inner.staging.blob.payload.take_delta();
        let (blob_meta_pages, blob_meta_plain) = rev_inner.staging.blob.meta.take_delta();

        let old_merkle_meta_delta = rev_inner
            .cached
            .merkle
            .meta
            .update(&merkle_meta_pages)
            .unwrap();
        let old_merkle_payload_delta = rev_inner
            .cached
            .merkle
            .payload
            .update(&merkle_payload_pages)
            .unwrap();
        let old_blob_meta_delta = rev_inner.cached.blob.meta.update(&blob_meta_pages).unwrap();
        let old_blob_payload_delta = rev_inner
            .cached
            .blob
            .payload
            .update(&blob_payload_pages)
            .unwrap();

        // update the rolling window of past revisions
        let new_base = Universe {
            merkle: get_sub_universe_from_deltas(
                &rev_inner.cached.merkle,
                old_merkle_meta_delta,
                old_merkle_payload_delta,
            ),
            blob: get_sub_universe_from_deltas(
                &rev_inner.cached.blob,
                old_blob_meta_delta,
                old_blob_payload_delta,
            ),
        };

        let mut revisions = self.r.lock();
        if let Some(rev) = revisions.inner.front_mut() {
            rev.merkle
                .meta
                .set_prev(new_base.merkle.meta.inner().clone());
            rev.merkle
                .payload
                .set_prev(new_base.merkle.payload.inner().clone());
            rev.blob.meta.set_prev(new_base.blob.meta.inner().clone());
            rev.blob
                .payload
                .set_prev(new_base.blob.payload.inner().clone());
        }
        revisions.inner.push_front(new_base);
        while revisions.inner.len() > revisions.max_revisions {
            revisions.inner.pop_back();
        }

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&rev_inner.cached.merkle),
            blob: get_sub_universe_from_empty_delta(&rev_inner.cached.blob),
        };
        revisions.base = base;

        self.committed = true;

        // schedule writes to the disk
        rev_inner.disk_requester.write(
            vec![
                BufferWrite {
                    space_id: rev_inner.staging.merkle.payload.id(),
                    delta: merkle_payload_pages,
                },
                BufferWrite {
                    space_id: rev_inner.staging.merkle.meta.id(),
                    delta: merkle_meta_pages,
                },
                BufferWrite {
                    space_id: rev_inner.staging.blob.payload.id(),
                    delta: blob_payload_pages,
                },
                BufferWrite {
                    space_id: rev_inner.staging.blob.meta.id(),
                    delta: blob_meta_pages,
                },
            ],
            AshRecord(
                [
                    (MERKLE_META_SPACE, merkle_meta_plain),
                    (MERKLE_PAYLOAD_SPACE, merkle_payload_plain),
                    (BLOB_META_SPACE, blob_meta_plain),
                    (BLOB_PAYLOAD_SPACE, blob_payload_plain),
                ]
                .into(),
            ),
        );
    }
}

#[cfg(feature = "eth")]
impl WriteBatch {
    fn change_account(
        &mut self,
        key: &[u8],
        modify: impl FnOnce(&mut Account, &mut BlobStash) -> Result<(), DbError>,
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
                Hash(sha3::Keccak256::digest(code).into()),
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

impl Drop for WriteBatch {
    fn drop(&mut self) {
        if !self.committed {
            // drop the staging changes
            self.m.read().staging.merkle.payload.take_delta();
            self.m.read().staging.merkle.meta.take_delta();
            self.m.read().staging.blob.payload.take_delta();
            self.m.read().staging.blob.meta.take_delta();
        }
    }
}
