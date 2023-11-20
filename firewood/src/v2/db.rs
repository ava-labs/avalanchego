// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{fmt::Debug, ops::DerefMut, sync::Arc};

use tokio::sync::Mutex;

use async_trait::async_trait;

use crate::{
    db::DbError,
    v2::api::{self, Batch, KeyType, ValueType},
};

use super::propose;

#[cfg_attr(doc, aquamarine::aquamarine)]
/// ```mermaid
/// graph LR
///     RevRootHash --> DBRevID
///     RevHeight --> DBRevID
///     DBRevID -- Identify --> DbRev
///     Db/Proposal -- propose with batch --> Proposal
///     Proposal -- translate --> DbRev
///     DB -- commit proposal --> DB
/// ```
#[derive(Debug, Default)]
pub struct Db<T> {
    latest_cache: Mutex<Option<Arc<T>>>,
}

impl From<DbError> for api::Error {
    fn from(value: DbError) -> Self {
        match value {
            DbError::InvalidParams => api::Error::InternalError(Box::new(value)),
            DbError::Merkle(e) => api::Error::InternalError(Box::new(e)),
            DbError::System(e) => api::Error::IO(e.into()),
            DbError::KeyNotFound | DbError::CreateError => {
                api::Error::InternalError(Box::new(value))
            }
            DbError::Shale(e) => api::Error::InternalError(Box::new(e)),
            DbError::IO(e) => api::Error::IO(e),
            DbError::InvalidProposal => api::Error::InvalidProposal,
        }
    }
}

#[async_trait]
impl<T> api::Db for Db<T>
where
    T: api::DbView,
    T: Send + Sync,
    T: Default,
    T: 'static,
{
    type Historical = T;

    type Proposal = propose::Proposal<T>;

    async fn revision(&self, _hash: api::HashKey) -> Result<Arc<Self::Historical>, api::Error> {
        todo!()
    }

    async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
        todo!()
    }

    async fn propose<K: KeyType, V: ValueType>(
        &self,
        data: Batch<K, V>,
    ) -> Result<Self::Proposal, api::Error> {
        let mut dbview_latest_cache_guard = self.latest_cache.lock().await;

        let revision = match dbview_latest_cache_guard.deref_mut() {
            Some(revision) => revision.clone(),
            revision => {
                let default_revision = Arc::new(T::default());
                *revision = Some(default_revision.clone());
                default_revision
            }
        };

        let proposal = Self::Proposal::new(propose::ProposalBase::View(revision), data);

        Ok(proposal)
    }
}
