// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{
    api::{Batch, Db, DbView, Error, HashKey, KeyType, RangeProof, ValueType},
    propose::{Proposal, ProposalBase},
};
use crate::merkle::Proof;
use async_trait::async_trait;
use std::sync::Arc;

/// An EmptyDb is a simple implementation of api::Db
/// that doesn't store any data. It contains a single
/// HistoricalImpl that has no keys or values
#[derive(Debug)]
pub struct EmptyDb;

/// HistoricalImpl is always empty, and there is only one,
/// since nothing can be committed to an EmptyDb.
#[derive(Debug)]
pub struct HistoricalImpl;

/// This is the hash of the [EmptyDb] root
const ROOT_HASH: [u8; 32] = [0; 32];

#[async_trait]
impl Db for EmptyDb {
    type Historical = HistoricalImpl;

    type Proposal = Proposal<HistoricalImpl>;

    async fn revision(&self, hash_key: HashKey) -> Result<Arc<Self::Historical>, Error> {
        if hash_key == ROOT_HASH {
            Ok(HistoricalImpl.into())
        } else {
            Err(Error::HashNotFound { provided: hash_key })
        }
    }

    async fn root_hash(&self) -> Result<HashKey, Error> {
        Ok(ROOT_HASH)
    }

    async fn propose<K, V>(&self, data: Batch<K, V>) -> Result<Self::Proposal, Error>
    where
        K: KeyType,
        V: ValueType,
    {
        Ok(Proposal::new(
            ProposalBase::View(HistoricalImpl.into()),
            data,
        ))
    }
}

#[async_trait]
impl DbView for HistoricalImpl {
    async fn root_hash(&self) -> Result<HashKey, Error> {
        Ok(ROOT_HASH)
    }

    async fn val<K: KeyType>(&self, _key: K) -> Result<Option<Vec<u8>>, Error> {
        Ok(None)
    }

    async fn single_key_proof<K: KeyType>(&self, _key: K) -> Result<Option<Proof<Vec<u8>>>, Error> {
        Ok(None)
    }

    async fn range_proof<K: KeyType, V>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: Option<usize>,
    ) -> Result<Option<RangeProof<Vec<u8>, Vec<u8>>>, Error> {
        Ok(None)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::v2::api::{BatchOp, Proposal};

    #[tokio::test]
    async fn basic_proposal() -> Result<(), Error> {
        let db = Arc::new(EmptyDb);

        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal = db.propose(batch).await?;

        assert_eq!(proposal.val(b"k").await.unwrap().unwrap(), b"v");

        assert!(proposal.val(b"z").await.unwrap().is_none());

        Ok(())
    }

    #[tokio::test]
    async fn nested_proposal() -> Result<(), Error> {
        let db = Arc::new(EmptyDb);

        // create proposal1 which adds key "k" with value "v" and deletes "z"
        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal1 = Arc::new(db.propose(batch).await?);

        // create proposal2 which adds key "z" with value "undo"
        let proposal2 = proposal1
            .clone()
            .propose(vec![BatchOp::Put {
                key: b"z",
                value: "undo",
            }])
            .await?;
        let proposal2 = Arc::new(proposal2);
        // both proposals still have (k,v)
        assert_eq!(proposal1.val(b"k").await.unwrap().unwrap(), b"v");
        assert_eq!(proposal2.val(b"k").await.unwrap().unwrap(), b"v");
        // only proposal1 doesn't have z
        assert!(proposal1.val(b"z").await.unwrap().is_none());
        // proposal2 has z with value "undo"
        assert_eq!(proposal2.val(b"z").await.unwrap().unwrap(), b"undo");

        // create a proposal3 by adding the two proposals together, keeping the originals
        let proposal3 = proposal1.as_ref() + proposal2.as_ref();
        assert_eq!(proposal3.val(b"k").await.unwrap().unwrap(), b"v");
        assert_eq!(proposal3.val(b"z").await.unwrap().unwrap(), b"undo");

        // now consume proposal1 and proposal2
        proposal2.commit().await?;

        Ok(())
    }
}
