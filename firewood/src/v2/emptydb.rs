// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::api::{Batch, Db, DbView, Error, HashKey, KeyType, ValueType};
use super::propose::{Proposal, ProposalBase};
use crate::v2::api::{FrozenProof, FrozenRangeProof};
use async_trait::async_trait;
use futures::Stream;
use std::sync::Arc;

/// An `EmptyDb` is a simple implementation of `api::Db`
/// that doesn't store any data. It contains a single
/// `HistoricalImpl` that has no keys or values
#[derive(Debug)]
pub struct EmptyDb;

/// `HistoricalImpl` is always empty, and there is only one,
/// since nothing can be committed to an `EmptyDb`.
#[derive(Debug)]
pub struct HistoricalImpl;

#[async_trait]
impl Db for EmptyDb {
    type Historical = HistoricalImpl;

    type Proposal<'p> = Proposal<HistoricalImpl>;

    async fn revision(&self, hash_key: HashKey) -> Result<Arc<Self::Historical>, Error> {
        Err(Error::HashNotFound { provided: hash_key })
    }

    async fn root_hash(&self) -> Result<Option<HashKey>, Error> {
        Ok(None)
    }

    async fn propose<'p, K, V>(
        &'p self,
        data: Batch<K, V>,
    ) -> Result<Arc<Self::Proposal<'p>>, Error>
    where
        K: KeyType,
        V: ValueType,
    {
        Ok(Proposal::new(
            ProposalBase::View(HistoricalImpl.into()),
            data,
        ))
    }

    async fn all_hashes(&self) -> Result<Vec<HashKey>, Error> {
        Ok(vec![])
    }
}

#[async_trait]
impl DbView for HistoricalImpl {
    type Stream<'a> = EmptyStreamer;

    async fn root_hash(&self) -> Result<Option<HashKey>, Error> {
        Ok(None)
    }

    async fn val<K: KeyType>(&self, _key: K) -> Result<Option<Box<[u8]>>, Error> {
        Ok(None)
    }

    async fn single_key_proof<K: KeyType>(&self, _key: K) -> Result<FrozenProof, Error> {
        Err(Error::RangeProofOnEmptyTrie)
    }

    async fn range_proof<K: KeyType, V>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: Option<usize>,
    ) -> Result<FrozenRangeProof, Error> {
        Err(Error::RangeProofOnEmptyTrie)
    }

    fn iter_option<K: KeyType>(&self, _first_key: Option<K>) -> Result<EmptyStreamer, Error> {
        Ok(EmptyStreamer {})
    }
}

#[derive(Debug)]
/// An empty streamer that doesn't stream any data
pub struct EmptyStreamer;

impl Stream for EmptyStreamer {
    type Item = Result<(Box<[u8]>, Vec<u8>), Error>;

    fn poll_next(
        self: std::pin::Pin<&mut Self>,
        _cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Option<Self::Item>> {
        std::task::Poll::Ready(None)
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::v2::api::{BatchOp, Proposal};

    #[tokio::test]
    async fn basic_proposal() -> Result<(), Error> {
        let db = EmptyDb;

        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal = db.propose(batch).await?;

        assert_eq!(
            proposal.val(b"k").await.unwrap().unwrap(),
            Box::from(b"v".as_slice())
        );

        assert!(proposal.val(b"z").await.unwrap().is_none());

        Ok(())
    }

    #[tokio::test]
    async fn nested_proposal() -> Result<(), Error> {
        let db = EmptyDb;
        // create proposal1 which adds key "k" with value "v" and deletes "z"
        let batch = vec![
            BatchOp::Put {
                key: b"k",
                value: b"v",
            },
            BatchOp::Delete { key: b"z" },
        ];

        let proposal1 = db.propose(batch).await?;

        // create proposal2 which adds key "z" with value "undo"
        let proposal2 = proposal1
            .clone()
            .propose(vec![BatchOp::Put {
                key: b"z",
                value: "undo",
            }])
            .await?;
        // both proposals still have (k,v)
        assert_eq!(proposal1.val(b"k").await.unwrap().unwrap().to_vec(), b"v");
        assert_eq!(proposal2.val(b"k").await.unwrap().unwrap().to_vec(), b"v");
        // only proposal1 doesn't have z
        assert!(proposal1.val(b"z").await.unwrap().is_none());
        // proposal2 has z with value "undo"
        assert_eq!(
            proposal2.val(b"z").await.unwrap().unwrap().to_vec(),
            b"undo"
        );

        // create a proposal3 by adding the two proposals together, keeping the originals
        let proposal3 = proposal1.as_ref() + proposal2.as_ref();
        assert_eq!(proposal3.val(b"k").await.unwrap().unwrap().to_vec(), b"v");
        assert_eq!(
            proposal3.val(b"z").await.unwrap().unwrap().to_vec(),
            b"undo"
        );

        // now consume proposal1 and proposal2
        proposal2.commit().await?;

        Ok(())
    }
}
