// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::collections::BTreeMap;
use std::fmt::Debug;
use std::num::NonZeroUsize;
use std::sync::Arc;

use async_trait::async_trait;
use futures::stream::Empty;

use super::api::{self, FrozenProof, FrozenRangeProof, KeyType, ValueType};
use crate::merkle::{Key, Value};

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

/// A proposal is created either from the [[`crate::v2::api::Db`]] object
/// or from another proposal. Proposals are owned by the
/// caller. A proposal can only be committed if it has a
/// base of the current revision of the [[`crate::v2::api::Db`]].
#[cfg_attr(doc, aquamarine::aquamarine)]
/// ```mermaid
/// graph LR
///   subgraph historical
///     direction BT
///     PH1 --> R1((R1))
///     PH2 --> R1
///     PH3 --> PH2
///   end
///   R1 ~~~|"proposals on R1<br>may not be committed"| R1
///   subgraph committed_head
///     direction BT
///     R2 ~~~|"proposals on R2<br>may be committed"| R2
///     PC4 --> R2((R2))
///     PC6 --> PC5
///     PC5 --> R2
///     PC6 ~~~|"Committing PC6<br>creates two revisions"| PC6
///   end
///   subgraph new_committing
///     direction BT
///     PN --> R3((R3))
///     R3 ~~~|"R3 does not yet exist"| R3
///     PN ~~~|"this proposal<br>is committing"<br>--<br>could be<br>PC4 or PC5| PN
///   end
///   historical ==> committed_head
///   committed_head ==> new_committing
/// ```
#[derive(Debug)]
pub struct Proposal<T> {
    pub(crate) base: ProposalBase<T>,
    pub(crate) delta: BTreeMap<Key, KeyOp<Value>>,
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
    ) -> Arc<Self> {
        let delta = batch
            .into_iter()
            .map(|op| match op {
                api::BatchOp::Put { key, value } => (
                    key.as_ref().to_vec().into_boxed_slice(),
                    KeyOp::Put(value.as_ref().to_vec().into_boxed_slice()),
                ),
                api::BatchOp::Delete { key } => {
                    (key.as_ref().to_vec().into_boxed_slice(), KeyOp::Delete)
                }
                api::BatchOp::DeleteRange { prefix } => {
                    (prefix.as_ref().to_vec().into_boxed_slice(), KeyOp::Delete)
                }
            })
            .collect::<BTreeMap<_, _>>();

        Arc::new(Self { base, delta })
    }
}

#[async_trait]
impl<T: api::DbView + Send + Sync> api::DbView for Proposal<T> {
    // TODO: Replace with the correct stream type for an in-memory proposal implementation
    type Stream<'a>
        = Empty<Result<(Key, Value), api::Error>>
    where
        T: 'a;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        todo!();
    }

    async fn val<K: KeyType>(&self, key: K) -> Result<Option<Value>, api::Error> {
        // see if this key is in this proposal
        let key = key.as_ref();
        let mut this = self;
        // avoid recursion problems by looping over the proposal chain
        loop {
            match this.delta.get(key) {
                Some(change) => {
                    // key is in `this` proposal, check for Put or Delete
                    break match change {
                        KeyOp::Put(val) => Ok(Some(val.clone())),
                        KeyOp::Delete => Ok(None), // key was deleted in this proposal
                    };
                }
                None => match &this.base {
                    // key not in this proposal, so delegate to base
                    ProposalBase::Proposal(p) => this = p,
                    ProposalBase::View(view) => break view.val(key).await,
                },
            }
        }
    }

    async fn single_key_proof<K: KeyType>(&self, _key: K) -> Result<FrozenProof, api::Error> {
        todo!();
    }

    async fn range_proof<KT: KeyType>(
        &self,
        _first_key: Option<KT>,
        _last_key: Option<KT>,
        _limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        todo!();
    }

    fn iter_option<K: KeyType>(
        &self,
        _first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        todo!();
    }
}

#[async_trait]
impl<T: api::DbView + Send + Sync> api::Proposal for Proposal<T> {
    type Proposal = Proposal<T>;

    async fn propose<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        data: api::Batch<K, V>,
    ) -> Result<Arc<Self::Proposal>, api::Error> {
        // find the Arc for this base proposal from the parent
        Ok(Proposal::new(ProposalBase::Proposal(self), data))
    }

    async fn commit(self: Arc<Self>) -> Result<(), api::Error> {
        match &self.base {
            ProposalBase::Proposal(base) => base.clone().commit().await,
            ProposalBase::View(_) => Ok(()),
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
