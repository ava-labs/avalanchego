// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![allow(dead_code)]

use std::io::Error;
use std::path::PathBuf;

use typed_builder::TypedBuilder;

use crate::v2::api::HashKey;

use storage::FileBacked;

#[derive(Clone, Debug, TypedBuilder)]
pub struct RevisionManagerConfig {
    /// The number of historical revisions to keep in memory.
    #[builder(default = 64)]
    max_revisions: usize,
}

#[derive(Debug)]
pub(crate) struct RevisionManager {
    max_revisions: usize,
    filebacked: FileBacked,
    // historical: VecDeque<Arc<Historical>>,
    // proposals: Vec<Arc<ProposedImmutable>>, // TODO: Should be Vec<Weak<ProposedImmutable>>
    // committing_proposals: VecDeque<Arc<ProposedImmutable>>,
    // TODO: by_hash: HashMap<TrieHash, LinearStore>
    // TODO: maintain root hash of the most recent commit
}

impl RevisionManager {
    pub fn new(
        filename: PathBuf,
        truncate: bool,
        config: RevisionManagerConfig,
    ) -> Result<Self, Error> {
        Ok(Self {
            max_revisions: config.max_revisions,
            filebacked: FileBacked::new(filename, truncate)?,
            // historical: Default::default(),
            // proposals: Default::default(),
            // committing_proposals: Default::default(),
        })
    }
}

#[derive(Debug, thiserror::Error)]
pub(crate) enum RevisionManagerError {
    #[error("The proposal cannot be committed since a sibling was committed")]
    SiblingCommitted,
    #[error(
        "The proposal cannot be committed since it is not a direct child of the most recent commit"
    )]
    NotLatest,
    #[error("An IO error occurred during the commit")]
    IO(#[from] std::io::Error),
}

impl RevisionManager {
    // TODO fix this or remove it. It should take in a proposal.
    fn commit(&mut self, _proposal: ()) -> Result<(), RevisionManagerError> {
        todo!()
        // // detach FileBacked from all revisions to make writes safe
        // let new_historical = self.prepare_for_writes(&proposal)?;

        // // append this historical to the list of known historicals
        // self.historical.push_back(new_historical);

        // // forget about older revisions
        // while self.historical.len() > self.max_revisions {
        //     self.historical.pop_front();
        // }

        // // If we do copy on writes for underneath files, since we keep all changes
        // // after bootstrapping, we should be able to read from the changes and the
        // // read only file map to the state at bootstrapping.
        // // We actually doesn't care whether the writes are successful or not
        // // (crash recovery may need to be handled above)
        // for write in proposal.new.iter() {
        //     self.filebacked.write(*write.0, write.1)?;
        // }

        // self.writes_completed(proposal)
    }

    // TODO fix or remove this. It should take in a proposal.
    fn prepare_for_writes(&mut self, _proposal: ()) -> Result<(), RevisionManagerError> {
        todo!()
        // // check to see if we can commit this proposal
        // let parent = proposal.parent();
        // match parent {
        //     LinearStoreParent::FileBacked(_) => {
        //         if !self.committing_proposals.is_empty() {
        //             return Err(RevisionManagerError::NotLatest);
        //         }
        //     }
        //     LinearStoreParent::Proposed(ref parent_proposal) => {
        //         let Some(last_commiting_proposal) = self.committing_proposals.back() else {
        //             return Err(RevisionManagerError::NotLatest);
        //         };
        //         if !Arc::ptr_eq(parent_proposal, last_commiting_proposal) {
        //             return Err(RevisionManagerError::NotLatest);
        //         }
        //     }
        //     _ => return Err(RevisionManagerError::SiblingCommitted),
        // }
        // // checks complete: safe to commit

        // let new_historical = Arc::new(Historical::new(
        //     std::mem::take(&mut proposal.old.clone()), // TODO: remove clone
        //     parent.clone(),
        //     proposal.size()?,
        // ));

        // // reparent the oldest historical to point to the new proposal
        // if let Some(historical) = self.historical.back() {
        //     historical.reparent(new_historical.clone().into());
        // }

        // // for each outstanding proposal, see if their parent is the last committed linear store
        // for candidate in self
        //     .proposals
        //     .iter()
        //     .filter(|&candidate| candidate.has_parent(&parent) && !Arc::ptr_eq(candidate, proposal))
        // {
        //     candidate.reparent(LinearStoreParent::Historical(new_historical.clone()));
        // }

        // // mark this proposal as committing
        // self.committing_proposals.push_back(proposal.clone());

        // Ok(new_historical)
    }

    // TODO fix or remove this. It should take in a proposal.
    fn writes_completed(&mut self, _proposal: ()) -> Result<(), RevisionManagerError> {
        todo!()
        // // now that the committed proposal is on disk, reparent anything that pointed to our proposal,
        // // which is now fully flushed to our parent, as our parent
        // // TODO: This needs work when we support multiple simultaneous commit writes; we should
        // // only do this work when the entire stack below us has been flushed
        // let parent = proposal.parent();
        // let proposal = LinearStoreParent::Proposed(proposal);
        // for candidate in self
        //     .proposals
        //     .iter()
        //     .filter(|&candidate| candidate.has_parent(&proposal))
        // {
        //     candidate.reparent(parent.clone());
        // }

        // // TODO: As of now, this is always what we just pushed, no support for multiple simultaneous
        // // commits yet; the assert verifies this and should be removed when we add support for this
        // let should_be_us = self
        //     .committing_proposals
        //     .pop_front()
        //     .expect("can't be empty");
        // assert!(
        //     matches!(proposal, LinearStoreParent::Proposed(us) if Arc::ptr_eq(&us, &should_be_us))
        // );

        // // TODO: we should reparent fileback as the parent of this committed proposal??

        // Ok(())
    }
}

pub type NewProposalError = (); // TODO implement

impl RevisionManager {
    // TODO fix this or remove it. It should take in a proposal.
    pub fn add_proposal(&mut self, _proposal: ()) {
        todo!()
        // self.proposals.push(proposal);
    }

    pub fn revision(&self, _root_hash: HashKey) -> Result<(), RevisionManagerError> {
        todo!()
    }

    pub fn root_hash(&self) -> Result<HashKey, RevisionManagerError> {
        todo!()
    }
}

#[cfg(test)]
mod tests {
    // TODO
}
