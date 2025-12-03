// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::CreateIteratorResult;
use firewood::v2::api;
use firewood::v2::api::{ArcDynDbView, BoxKeyValueIter, DbView, HashKey};

#[derive(Debug)]
pub struct RevisionHandle {
    view: ArcDynDbView,
}

impl RevisionHandle {
    /// Creates a new revision handle for the provided database view.
    pub(crate) fn new(view: ArcDynDbView) -> RevisionHandle {
        RevisionHandle { view }
    }

    /// Creates an iterator on the revision starting from the given key.
    #[must_use]
    #[allow(clippy::missing_panics_doc)]
    pub fn iter_from(&self, first_key: Option<&[u8]>) -> CreateIteratorResult<'_> {
        let it = self
            .view
            .iter_option(first_key)
            .expect("infallible; see issue #1329");
        CreateIteratorResult(it.into())
    }
}

impl DbView for RevisionHandle {
    type Iter<'view>
        = BoxKeyValueIter<'view>
    where
        Self: 'view;

    fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        self.view.root_hash()
    }

    fn val<K: api::KeyType>(&self, key: K) -> Result<Option<firewood::merkle::Value>, api::Error> {
        self.view.val(key.as_ref())
    }

    fn single_key_proof<K: api::KeyType>(&self, key: K) -> Result<api::FrozenProof, api::Error> {
        self.view.single_key_proof(key.as_ref())
    }

    fn range_proof<K: api::KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<std::num::NonZeroUsize>,
    ) -> Result<api::FrozenRangeProof, api::Error> {
        self.view.range_proof(
            first_key.as_ref().map(AsRef::as_ref),
            last_key.as_ref().map(AsRef::as_ref),
            limit,
        )
    }

    fn iter_option<K: api::KeyType>(
        &self,
        first_key: Option<K>,
    ) -> Result<Self::Iter<'_>, api::Error> {
        self.view.iter_option(first_key.as_ref().map(AsRef::as_ref))
    }
}

#[derive(Debug)]
pub struct GetRevisionResult {
    pub handle: RevisionHandle,
    pub root_hash: HashKey,
}
