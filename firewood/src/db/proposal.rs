use super::{
    get_sub_universe_from_deltas, get_sub_universe_from_empty_delta, Db, DbConfig, DbError,
    DbHeader, DbInner, DbRev, DbRevInner, SharedStore, Store, Universe, MERKLE_META_SPACE,
    MERKLE_PAYLOAD_SPACE, ROOT_HASH_SPACE,
};
use crate::{
    merkle::{TrieHash, TRIE_HASH_LEN},
    storage::{buffer::BufferWrite, AshRecord, StoreRevMut},
};
use parking_lot::{Mutex, RwLock};
use shale::CachedStore;
use std::sync::Arc;

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
pub struct Proposal {
    // State of the Db
    pub(super) m: Arc<RwLock<DbInner>>,
    pub(super) r: Arc<Mutex<DbRevInner<SharedStore>>>,
    pub(super) cfg: DbConfig,

    // State of the proposal
    pub(super) rev: DbRev<Store>,
    pub(super) store: Universe<Arc<StoreRevMut>>,
    pub(super) committed: Arc<Mutex<bool>>,

    pub(super) parent: ProposalBase,
}

pub enum ProposalBase {
    Proposal(Arc<Proposal>),
    View(Arc<DbRev<SharedStore>>),
}

impl Proposal {
    // Propose a new proposal from this proposal. The new proposal will be
    // the child of it.
    pub fn propose<K: AsRef<[u8]>>(self: Arc<Self>, data: Batch<K>) -> Result<Proposal, DbError> {
        let store = self.store.new_from_other();

        let m = Arc::clone(&self.m);
        let r = Arc::clone(&self.r);
        let cfg = self.cfg.clone();

        let db_header_ref = Db::get_db_header_ref(store.merkle.meta.as_ref())?;

        let merkle_payload_header_ref = Db::get_payload_header_ref(
            store.merkle.meta.as_ref(),
            Db::PARAM_SIZE + DbHeader::MSIZE,
        )?;

        let header_refs = (db_header_ref, merkle_payload_header_ref);

        let mut rev = Db::new_revision(
            header_refs,
            (store.merkle.meta.clone(), store.merkle.payload.clone()),
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

        // update the rolling window of past revisions
        let latest_past = Universe {
            merkle: get_sub_universe_from_deltas(
                &rev_inner.cached_space.merkle,
                merkle_meta_undo,
                merkle_payload_undo,
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
        }
        revisions.inner.push_front(latest_past);
        while revisions.inner.len() > max_revisions {
            revisions.inner.pop_back();
        }

        let base = Universe {
            merkle: get_sub_universe_from_empty_delta(&rev_inner.cached_space.merkle),
        };

        let db_header_ref = Db::get_db_header_ref(&base.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&base.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

        let header_refs = (db_header_ref, merkle_payload_header_ref);

        let base_revision = Db::new_revision(
            header_refs,
            (base.merkle.meta.clone(), base.merkle.payload.clone()),
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
                    space_id: rev_inner.root_hash_staging.id(),
                    delta: root_hash_redo,
                },
            ],
            AshRecord(
                [
                    (MERKLE_META_SPACE, merkle_meta_wal),
                    (MERKLE_PAYLOAD_SPACE, merkle_payload_wal),
                    (ROOT_HASH_SPACE, root_hash_wal),
                ]
                .into(),
            ),
        );
        *committed = true;
        Ok(())
    }
}

impl Proposal {
    pub fn get_revision(&self) -> &DbRev<Store> {
        &self.rev
    }
}

impl Drop for Proposal {
    fn drop(&mut self) {
        if !*self.committed.lock() {
            // drop the staging changes
            self.store.merkle.payload.reset_deltas();
            self.store.merkle.meta.reset_deltas();
            self.m.read().root_hash_staging.reset_deltas();
        }
    }
}
