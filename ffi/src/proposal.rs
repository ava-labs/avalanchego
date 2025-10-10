// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::v2::api::{self, BoxKeyValueIter, DbView, HashKey, Proposal as _};

use crate::value::KeyValuePair;

use crate::iterator::CreateIteratorResult;
use metrics::counter;

/// An opaque wrapper around a Proposal that also retains a reference to the
/// database handle it was created from.
#[derive(Debug)]
pub struct ProposalHandle<'db> {
    hash_key: Option<HashKey>,
    proposal: firewood::db::Proposal<'db>,
    handle: &'db crate::DatabaseHandle,
}

impl<'db> DbView for ProposalHandle<'db> {
    type Iter<'view>
        = <firewood::db::Proposal<'db> as DbView>::Iter<'view>
    where
        Self: 'view;

    fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        self.proposal.root_hash()
    }

    fn val<K: api::KeyType>(&self, key: K) -> Result<Option<firewood::merkle::Value>, api::Error> {
        self.proposal.val(key)
    }

    fn single_key_proof<K: api::KeyType>(&self, key: K) -> Result<api::FrozenProof, api::Error> {
        self.proposal.single_key_proof(key)
    }

    fn range_proof<K: api::KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<std::num::NonZeroUsize>,
    ) -> Result<api::FrozenRangeProof, api::Error> {
        self.proposal.range_proof(first_key, last_key, limit)
    }

    fn iter_option<K: api::KeyType>(
        &self,
        first_key: Option<K>,
    ) -> Result<Self::Iter<'_>, api::Error> {
        self.proposal.iter_option(first_key)
    }
}

impl ProposalHandle<'_> {
    /// Returns the root hash of the proposal.
    #[must_use]
    pub fn hash_key(&self) -> Option<crate::HashKey> {
        self.hash_key.clone().map(Into::into)
    }

    /// Consume and commit a proposal.
    ///
    /// # Arguments
    ///
    /// - `token`: An callback function that will be called with the duration
    ///   of the commit operation. This will be dropped without being called if
    ///   the commit fails.
    ///
    /// # Errors
    ///
    /// This function will return an error if committing the proposal fails or if the
    /// proposal is empty.
    pub fn commit_proposal(
        self,
        token: impl FnOnce(coarsetime::Duration),
    ) -> Result<Option<HashKey>, api::Error> {
        let ProposalHandle {
            hash_key,
            proposal,
            handle,
        } = self;

        // promote the proposal to the handle's cached view so that it can be used
        // for future reads while the proposal is being committed
        if let Some(ref hash_key) = hash_key {
            _ = handle.get_root(hash_key.clone());
        }

        let start_time = coarsetime::Instant::now();
        proposal.commit()?;
        let commit_time = start_time.elapsed();

        // clear the cached view so that it does not hold onto the proposal view
        handle.clear_cached_view();

        token(commit_time);

        Ok(hash_key)
    }

    /// Creates an iterator on the proposal starting from the given key.
    #[must_use]
    #[allow(clippy::missing_panics_doc)]
    pub fn iter_from(&self, first_key: Option<&[u8]>) -> CreateIteratorResult<'_> {
        let it = self
            .iter_option(first_key)
            .expect("infallible; see issue #1329");
        CreateIteratorResult((Box::new(it) as BoxKeyValueIter<'_>).into())
    }
}
#[derive(Debug)]
pub struct CreateProposalResult<'db> {
    pub handle: ProposalHandle<'db>,
    pub start_time: coarsetime::Instant,
}

/// A trait that abstracts over database handles and proposal handles for creating proposals.
///
/// This trait allows functions to work with both [`DatabaseHandle`] and [`ProposalHandle`]
/// uniformly when creating new proposals. It provides a common interface for:
/// - Getting the underlying database handle
/// - Creating proposals from key-value pairs
/// - Creating proposal handles with timing information
///
/// This abstraction enables proposal chaining (creating proposals on top of other proposals)
/// while maintaining a consistent API.
///
/// [`DatabaseHandle`]: crate::DatabaseHandle
pub trait CView<'db> {
    /// Returns a reference to the database handle that is ultimately used to
    /// create the proposal. For the database handle, this returns itself. For,
    /// a proposal handle, this returns the handle that was used to create the
    /// proposal.
    fn handle(&self) -> &'db crate::DatabaseHandle;

    /// Create a [`firewood::db::Proposal`] with the provided key-value pairs.
    ///
    /// # Errors
    ///
    /// This function will return a database error if the proposal could not be
    /// created.
    fn create_proposal<'kvp>(
        self,
        values: impl AsRef<[KeyValuePair<'kvp>]> + 'kvp,
    ) -> Result<firewood::db::Proposal<'db>, api::Error>;

    /// Create a [`ProposalHandle`] from the values and return it with timing
    /// information.
    ///
    /// # Errors
    ///
    /// This function will return a database error if the proposal could not be
    /// created or if the proposal is empty.
    fn create_proposal_handle<'kvp>(
        self,
        values: impl AsRef<[KeyValuePair<'kvp>]> + 'kvp,
    ) -> Result<CreateProposalResult<'db>, api::Error>
    where
        Self: Sized,
    {
        let handle = self.handle();

        let start_time = coarsetime::Instant::now();
        let proposal = self.create_proposal(values)?;
        let propose_time = start_time.elapsed();
        counter!("firewood.ffi.propose_ms").increment(propose_time.as_millis());
        counter!("firewood.ffi.propose").increment(1);

        let hash_key = proposal.root_hash()?;

        Ok(CreateProposalResult {
            handle: ProposalHandle {
                hash_key,
                proposal,
                handle,
            },
            start_time,
        })
    }
}

impl<'db> CView<'db> for &ProposalHandle<'db> {
    fn handle(&self) -> &'db crate::DatabaseHandle {
        self.handle
    }

    fn create_proposal<'kvp>(
        self,
        values: impl AsRef<[KeyValuePair<'kvp>]> + 'kvp,
    ) -> Result<firewood::db::Proposal<'db>, api::Error> {
        self.proposal.propose(values.as_ref())
    }
}
