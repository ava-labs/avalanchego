use std::{fmt::Debug, ops::DerefMut, sync::Arc};

use tokio::sync::Mutex;

use async_trait::async_trait;

use crate::v2::api::{self, Batch, KeyType, ValueType};

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

#[async_trait]
impl<T> api::Db for Db<T>
where
    T: api::DbView,
    T: Send + Sync,
    T: Default,
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

#[derive(Debug, Default)]
pub struct DbView;

#[async_trait]
impl api::DbView for DbView {
    async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
        todo!()
    }

    async fn val<K: KeyType>(&self, _key: K) -> Result<Option<&[u8]>, api::Error> {
        todo!()
    }

    async fn single_key_proof<K: KeyType, V: ValueType>(
        &self,
        _key: K,
    ) -> Result<Option<api::Proof<V>>, api::Error> {
        todo!()
    }

    async fn range_proof<K: KeyType, V, N>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: usize,
    ) -> Result<Option<api::RangeProof<K, V, N>>, api::Error> {
        todo!()
    }
}
