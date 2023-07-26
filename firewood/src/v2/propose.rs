use std::{collections::BTreeMap, fmt::Debug, sync::Arc};

use async_trait::async_trait;

use crate::v2::api;

use super::api::{KeyType, ValueType};

#[derive(Clone, Debug)]
pub(crate) enum KeyOp<V: ValueType> {
    Put(V),
    Delete,
}

#[derive(Debug)]
pub(crate) enum ProposalBase<T> {
    Proposal(Arc<Proposal<T>>),
    View(Arc<T>),
}

// Implement Clone because T doesn't need to be Clone
// so an automatically derived Clone won't work
impl<T: api::DbView> Clone for ProposalBase<T> {
    fn clone(&self) -> Self {
        match self {
            Self::Proposal(arg0) => Self::Proposal(arg0.clone()),
            Self::View(arg0) => Self::View(arg0.clone()),
        }
    }
}

#[derive(Debug)]
pub struct Proposal<T> {
    pub(crate) base: ProposalBase<T>,
    pub(crate) delta: BTreeMap<Vec<u8>, KeyOp<Vec<u8>>>,
}

// Implement Clone because T doesn't need to be Clone
// so an automatically derived Clone won't work
impl<T: api::DbView> Clone for Proposal<T> {
    fn clone(&self) -> Self {
        Self {
            base: self.base.clone(),
            delta: self.delta.clone(),
        }
    }
}

impl<T> Proposal<T> {
    pub(crate) fn new<K: KeyType, V: ValueType>(
        base: ProposalBase<T>,
        batch: api::Batch<K, V>,
    ) -> Self {
        let delta = batch
            .into_iter()
            .map(|op| match op {
                api::BatchOp::Put { key, value } => {
                    (key.as_ref().to_vec(), KeyOp::Put(value.as_ref().to_vec()))
                }
                api::BatchOp::Delete { key } => (key.as_ref().to_vec(), KeyOp::Delete),
            })
            .collect();

        Self { base, delta }
    }
}

#[async_trait]
impl<T: api::DbView + Send + Sync> api::DbView for Proposal<T> {
    async fn hash(&self) -> Result<api::HashKey, api::Error> {
        todo!()
    }

    async fn val<K: KeyType>(&self, key: K) -> Result<Vec<u8>, api::Error> {
        // see if this key is in this proposal
        match self.delta.get(key.as_ref()) {
            Some(change) => match change {
                // key in proposal, check for Put or Delete
                KeyOp::Put(val) => Ok(val.clone()),
                KeyOp::Delete => Err(api::Error::KeyNotFound), // key was deleted in this proposal
            },
            None => match &self.base {
                // key not in this proposal, so delegate to base
                ProposalBase::Proposal(p) => p.val(key).await,
                ProposalBase::View(view) => view.val(key).await,
            },
        }
    }

    async fn single_key_proof<K: KeyType, V: ValueType>(
        &self,
        _key: K,
    ) -> Result<api::Proof<V>, api::Error> {
        todo!()
    }

    async fn range_proof<KT: KeyType, VT: ValueType>(
        &self,
        _first_key: Option<KT>,
        _last_key: Option<KT>,
        _limit: usize,
    ) -> Result<api::RangeProof<KT, VT>, api::Error> {
        todo!()
    }
}

#[async_trait]
impl<T: api::DbView + Send + Sync> api::Proposal<T> for Proposal<T> {
    type Proposal = Proposal<T>;

    async fn propose<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        data: api::Batch<K, V>,
    ) -> Result<Self::Proposal, api::Error> {
        // find the Arc for this base proposal from the parent
        Ok(Proposal::new(ProposalBase::Proposal(self), data))
    }

    async fn commit(self: Arc<Self>) -> Result<Arc<T>, api::Error> {
        // TODO: commit should modify the db; this will only work for
        // emptydb at the moment
        match &self.base {
            ProposalBase::Proposal(base) => base.clone().commit().await,
            ProposalBase::View(v) => Ok(v.clone()),
        }
    }
}

impl<T: api::DbView> std::ops::Add for Proposal<T> {
    type Output = Arc<Proposal<T>>;

    fn add(self, rhs: Self) -> Self::Output {
        let mut delta = self.delta.clone();

        delta.extend(rhs.delta);

        let proposal = Proposal {
            base: self.base,
            delta,
        };

        Arc::new(proposal)
    }
}

impl<T: api::DbView> std::ops::Add for &Proposal<T> {
    type Output = Arc<Proposal<T>>;

    fn add(self, rhs: Self) -> Self::Output {
        let mut delta = self.delta.clone();

        delta.extend(rhs.delta.clone());

        let proposal = Proposal {
            base: self.base.clone(),
            delta,
        };

        Arc::new(proposal)
    }
}
