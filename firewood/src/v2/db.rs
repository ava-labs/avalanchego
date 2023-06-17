use std::{
    borrow::Borrow,
    collections::BTreeMap,
    fmt::Debug,
    sync::{Arc, Mutex, RwLock, Weak},
};

use async_trait::async_trait;

use crate::v2::api::{self, Batch, KeyType, ValueType};

#[derive(Debug, Default)]
pub struct Db {
    latest_cache: Mutex<Option<Arc<DbView>>>,
}

#[async_trait]
impl api::Db for Db {
    type Historical = DbView;

    type Proposal = Proposal;

    async fn revision(&self, _hash: api::HashKey) -> Result<Weak<Self::Historical>, api::Error> {
        todo!()
    }

    async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
        todo!()
    }

    async fn propose<K: KeyType, V: ValueType>(
        &mut self,
        data: Batch<K, V>,
    ) -> Result<Weak<Proposal>, api::Error> {
        let mut dbview_latest_cache_guard = self.latest_cache.lock().unwrap();

        if dbview_latest_cache_guard.is_none() {
            // TODO: actually get the latest dbview
            *dbview_latest_cache_guard = Some(Arc::new(DbView {
                proposals: RwLock::new(vec![]),
            }));
        };

        let mut proposal_guard = dbview_latest_cache_guard
            .as_ref()
            .unwrap()
            .proposals
            .write()
            .unwrap();

        let proposal = Arc::new(Proposal::new(
            ProposalBase::View(dbview_latest_cache_guard.clone().unwrap()),
            data,
        ));

        proposal_guard.push(proposal.clone());

        Ok(Arc::downgrade(&proposal))
    }
}

#[derive(Debug)]
pub struct DbView {
    proposals: RwLock<Vec<Arc<Proposal>>>,
}

#[async_trait]
impl api::DbView for DbView {
    async fn hash(&self) -> Result<api::HashKey, api::Error> {
        todo!()
    }

    async fn val<K: KeyType>(&self, _key: K) -> Result<Vec<u8>, api::Error> {
        todo!()
    }

    async fn single_key_proof<K: KeyType, V: ValueType>(
        &self,
        _key: K,
    ) -> Result<api::Proof<V>, api::Error> {
        todo!()
    }

    async fn range_proof<K: KeyType, V: ValueType>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: usize,
    ) -> Result<api::RangeProof<K, V>, api::Error> {
        todo!()
    }
}

#[derive(Clone, Debug)]
enum ProposalBase {
    Proposal(Arc<Proposal>),
    View(Arc<DbView>),
}

#[derive(Clone, Debug)]
enum KeyOp<V: ValueType> {
    Put(V),
    Delete,
}

#[derive(Debug)]
pub struct Proposal {
    base: ProposalBase,
    delta: BTreeMap<Vec<u8>, KeyOp<Vec<u8>>>,
    children: RwLock<Vec<Arc<Proposal>>>,
}

impl Clone for Proposal {
    fn clone(&self) -> Self {
        Self {
            base: self.base.clone(),
            delta: self.delta.clone(),
            children: RwLock::new(vec![]),
        }
    }
}

impl Proposal {
    fn new<K: KeyType, V: ValueType>(base: ProposalBase, batch: Batch<K, V>) -> Self {
        let delta = batch
            .into_iter()
            .map(|op| match op {
                api::BatchOp::Put { key, value } => {
                    (key.as_ref().to_vec(), KeyOp::Put(value.as_ref().to_vec()))
                }
                api::BatchOp::Delete { key } => (key.as_ref().to_vec(), KeyOp::Delete),
            })
            .collect();

        Self {
            base,
            delta,
            children: RwLock::new(vec![]),
        }
    }
}

#[async_trait]
impl api::DbView for Proposal {
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
impl api::Proposal<DbView> for Proposal {
    async fn propose<K: KeyType, V: ValueType>(
        &self,
        data: Batch<K, V>,
    ) -> Result<Weak<Self>, api::Error> {
        // find the Arc for this base proposal from the parent
        let children_guard = match &self.base {
            ProposalBase::Proposal(p) => p.children.read().unwrap(),
            ProposalBase::View(v) => v.proposals.read().unwrap(),
        };

        let arc = children_guard
            .iter()
            .find(|&c| std::ptr::eq(c.borrow() as *const _, self as *const _));

        if arc.is_none() {
            return Err(api::Error::InvalidProposal);
        }

        let proposal = Arc::new(Proposal::new(
            ProposalBase::Proposal(arc.unwrap().clone()),
            data,
        ));

        self.children.write().unwrap().push(proposal.clone());

        Ok(Arc::downgrade(&proposal))
    }

    async fn commit(self) -> Result<Weak<DbView>, api::Error> {
        todo!()
    }
}

impl std::ops::Add for Proposal {
    type Output = Arc<Proposal>;

    fn add(self, rhs: Self) -> Self::Output {
        let mut delta = self.delta.clone();

        delta.extend(rhs.delta);

        let proposal = Proposal {
            base: self.base,
            delta,
            children: RwLock::new(Vec::new()),
        };

        Arc::new(proposal)
    }
}

impl std::ops::Add for &Proposal {
    type Output = Arc<Proposal>;

    fn add(self, rhs: Self) -> Self::Output {
        let mut delta = self.delta.clone();

        delta.extend(rhs.delta.clone());

        let proposal = Proposal {
            base: self.base.clone(),
            delta,
            children: RwLock::new(Vec::new()),
        };

        Arc::new(proposal)
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use crate::v2::api::Db as _;
    use crate::v2::api::DbView as _;
    use crate::v2::api::Proposal;
    use api::BatchOp;

    #[tokio::test]
    async fn test_basic_proposal() -> Result<(), crate::v2::api::Error> {
        let mut db = Db::default();

        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal = db.propose(batch).await?.upgrade().unwrap();

        assert_eq!(proposal.val(b"k").await.unwrap(), b"v");

        assert!(matches!(
            proposal.val(b"z").await.unwrap_err(),
            crate::v2::api::Error::KeyNotFound
        ));

        Ok(())
    }

    #[tokio::test]
    async fn test_nested_proposal() -> Result<(), crate::v2::api::Error> {
        let mut db = Db::default();

        // create proposal1 which adds key "k" with value "v" and deletes "z"
        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal1 = db.propose(batch).await?.upgrade().unwrap();

        // create proposal2 which adds key "z" with value "undo"
        let proposal2 = proposal1
            .propose(vec![BatchOp::Put {
                key: b"z",
                value: "undo",
            }])
            .await?
            .upgrade()
            .unwrap();
        // both proposals still have (k,v)
        assert_eq!(proposal1.val(b"k").await.unwrap(), b"v");
        assert_eq!(proposal2.val(b"k").await.unwrap(), b"v");
        // only proposal1 doesn't have z
        assert!(matches!(
            proposal1.val(b"z").await.unwrap_err(),
            crate::v2::api::Error::KeyNotFound
        ));
        // proposal2 has z with value "undo"
        assert_eq!(proposal2.val(b"z").await.unwrap(), b"undo");

        // create a proposal3 by adding the two proposals together, keeping the originals
        let proposal3: Arc<crate::v2::db::Proposal> = proposal1.as_ref() + proposal2.as_ref();
        assert_eq!(proposal3.val(b"k").await.unwrap(), b"v");
        assert_eq!(proposal3.val(b"z").await.unwrap(), b"undo");

        // now consume proposal1 and proposal2

        Ok(())
    }
}
