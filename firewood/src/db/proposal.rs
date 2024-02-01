// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{
    get_sub_universe_from_deltas, Db, DbConfig, DbError, DbHeader, DbInner, DbRev, DbRevInner,
    MutStore, SharedStore, Universe, MERKLE_META_SPACE, MERKLE_PAYLOAD_SPACE, ROOT_HASH_SPACE,
};
use crate::merkle::Proof;
use crate::shale::CachedStore;
use crate::{
    merkle::{TrieHash, TRIE_HASH_LEN},
    storage::{buffer::BufferWrite, AshRecord, StoreRevMut},
    v2::api::{self, KeyType, ValueType},
};
use async_trait::async_trait;
use parking_lot::{Mutex, RwLock};
use std::{io::ErrorKind, sync::Arc};
use tokio::task::block_in_place;

pub use crate::v2::api::{Batch, BatchOp};

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
    pub(super) rev: DbRev<MutStore>,
    pub(super) store: Universe<StoreRevMut>,
    pub(super) committed: Arc<Mutex<bool>>,
    pub(super) root_hash: TrieHash,

    pub(super) parent: ProposalBase,
}

pub enum ProposalBase {
    Proposal(Arc<Proposal>),
    View(Arc<DbRev<SharedStore>>),
}

#[async_trait]
impl crate::v2::api::Proposal for Proposal {
    type Proposal = Proposal;

    #[allow(clippy::unwrap_used)]
    async fn commit(self: Arc<Self>) -> Result<(), api::Error> {
        let proposal = Arc::<Proposal>::into_inner(self).unwrap();
        block_in_place(|| proposal.commit_sync().map_err(Into::into))
    }

    async fn propose<K: api::KeyType, V: api::ValueType>(
        self: Arc<Self>,
        data: api::Batch<K, V>,
    ) -> Result<Self::Proposal, api::Error> {
        self.propose_sync(data).map_err(Into::into)
    }
}

impl Proposal {
    // Propose a new proposal from this proposal. The new proposal will be
    // the child of it.
    pub fn propose_sync<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        data: Batch<K, V>,
    ) -> Result<Proposal, DbError> {
        let store = self.store.new_from_other();

        let m = Arc::clone(&self.m);
        let r = Arc::clone(&self.r);
        let cfg = self.cfg.clone();

        let db_header_ref = Db::get_db_header_ref(&store.merkle.meta)?;

        let merkle_payload_header_ref =
            Db::get_payload_header_ref(&store.merkle.meta, Db::PARAM_SIZE + DbHeader::MSIZE)?;

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
        let hash = rev.kv_root_hash()?;
        #[allow(clippy::unwrap_used)]
        rev.flush_dirty().unwrap();

        let parent = ProposalBase::Proposal(self);

        Ok(Proposal {
            m,
            r,
            cfg,
            rev,
            store,
            committed: Arc::new(Mutex::new(false)),
            root_hash: hash,
            parent,
        })
    }

    /// Persist all changes to the DB. The atomicity of the [Proposal] guarantees all changes are
    /// either retained on disk or lost together during a crash.
    pub fn commit_sync(self) -> Result<(), DbError> {
        let Self {
            m,
            r,
            cfg: _,
            rev,
            store,
            committed,
            root_hash: hash,
            parent,
        } = self;

        let mut committed = committed.lock();
        if *committed {
            return Ok(());
        }

        if let ProposalBase::Proposal(_p) = parent {
            // p.commit_sync()?;
            todo!();
        }

        // Check for if it can be committed
        let mut revisions = r.lock();
        let committed_root_hash = revisions.base_revision.kv_root_hash().ok();
        let committed_root_hash =
            committed_root_hash.expect("committed_root_hash should not be none");
        match &parent {
            ProposalBase::Proposal(p) => {
                if p.root_hash != committed_root_hash {
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

        // clear the staging layer and apply changes to the CachedSpace
        let (merkle_payload_redo, merkle_payload_wal) = store.merkle.payload.delta();
        let (merkle_meta_redo, merkle_meta_wal) = store.merkle.meta.delta();

        let mut rev_inner = m.write();
        #[allow(clippy::unwrap_used)]
        let merkle_meta_undo = rev_inner
            .cached_space
            .merkle
            .meta
            .update(&merkle_meta_redo)
            .unwrap();
        #[allow(clippy::unwrap_used)]
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

        revisions.base_revision = Arc::new(rev.into());

        // update the rolling window of root hashes
        revisions.root_hashes.push_front(hash);
        if revisions.root_hashes.len() > max_revisions {
            revisions
                .root_hashes
                .resize(max_revisions, TrieHash([0; TRIE_HASH_LEN]));
        }

        rev_inner.root_hash_staging.write(0, &hash.0);
        let (root_hash_redo, root_hash_wal) = rev_inner.root_hash_staging.delta();

        // schedule writes to the disk
        rev_inner.disk_requester.write(
            vec![
                BufferWrite {
                    space_id: store.merkle.payload.id(),
                    delta: merkle_payload_redo,
                },
                BufferWrite {
                    space_id: store.merkle.meta.id(),
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
    pub const fn get_revision(&self) -> &DbRev<MutStore> {
        &self.rev
    }
}

#[async_trait]
impl api::DbView for Proposal {
    async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
        self.get_revision()
            .kv_root_hash()
            .map(|hash| hash.0)
            .map_err(|e| api::Error::IO(std::io::Error::new(ErrorKind::Other, e)))
    }
    async fn val<K>(&self, key: K) -> Result<Option<Vec<u8>>, api::Error>
    where
        K: api::KeyType,
    {
        // TODO: pass back errors from kv_get
        Ok(self.get_revision().kv_get(key))
    }

    async fn single_key_proof<K>(&self, key: K) -> Result<Option<Proof<Vec<u8>>>, api::Error>
    where
        K: api::KeyType,
    {
        self.get_revision()
            .prove(key)
            .map(Some)
            .map_err(|e| api::Error::IO(std::io::Error::new(ErrorKind::Other, e)))
    }

    async fn range_proof<K, V>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: Option<usize>,
    ) -> Result<Option<api::RangeProof<Vec<u8>, Vec<u8>>>, api::Error>
    where
        K: api::KeyType,
    {
        todo!();
    }
}
