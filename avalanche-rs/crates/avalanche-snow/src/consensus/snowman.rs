//! Snowman consensus implementation.
//!
//! Snowman is a linear chain consensus protocol built on Snowball.
//! It achieves consensus on a single preferred chain of blocks.

use std::collections::{HashMap, HashSet};

use avalanche_ids::Id;
use chrono::{DateTime, Utc};

use super::{Bag, Consensus, Status};
use crate::{ConsensusError, Parameters, Result};

/// A block in the Snowman chain.
pub trait Block: Send + Sync {
    /// Returns the block's unique identifier.
    fn id(&self) -> Id;

    /// Returns the parent block's ID.
    fn parent(&self) -> Id;

    /// Returns the block's height.
    fn height(&self) -> u64;

    /// Returns the block's timestamp.
    fn timestamp(&self) -> DateTime<Utc>;

    /// Returns the block's byte representation.
    fn bytes(&self) -> &[u8];

    /// Returns the block's status.
    fn status(&self) -> Status;

    /// Verifies the block is valid.
    fn verify(&self) -> Result<()>;

    /// Accepts the block.
    fn accept(&mut self) -> Result<()>;

    /// Rejects the block.
    fn reject(&mut self) -> Result<()>;
}

/// Block metadata stored by Snowman.
#[derive(Debug, Clone)]
struct BlockNode {
    id: Id,
    parent: Id,
    height: u64,
    children: HashSet<Id>,
    status: Status,
}

/// Snowman linear chain consensus.
///
/// Maintains a tree of blocks and uses Snowball to converge
/// on a single preferred chain.
#[derive(Debug)]
pub struct Snowman {
    params: Parameters,
    /// Genesis block ID
    genesis: Option<Id>,
    /// Last accepted block ID
    last_accepted: Option<Id>,
    /// Block metadata indexed by ID
    blocks: HashMap<Id, BlockNode>,
    /// Current preference (tip of preferred chain)
    preference: Option<Id>,
    /// Confidence for each block
    confidence: HashMap<Id, usize>,
    /// Consecutive successful polls
    consecutive_successes: u64,
    /// Processing blocks (not yet decided)
    processing: HashSet<Id>,
    /// Whether consensus is halted
    halted: bool,
}

impl Snowman {
    /// Creates a new Snowman instance.
    pub fn new(params: Parameters) -> Self {
        Self {
            params,
            genesis: None,
            last_accepted: None,
            blocks: HashMap::new(),
            preference: None,
            confidence: HashMap::new(),
            consecutive_successes: 0,
            processing: HashSet::new(),
            halted: false,
        }
    }

    /// Sets the genesis block.
    pub fn set_genesis(&mut self, id: Id) -> Result<()> {
        if self.genesis.is_some() {
            return Err(ConsensusError::Internal("genesis already set".to_string()));
        }

        let node = BlockNode {
            id,
            parent: Id::default(),
            height: 0,
            children: HashSet::new(),
            status: Status::Accepted,
        };

        self.blocks.insert(id, node);
        self.genesis = Some(id);
        self.last_accepted = Some(id);
        self.preference = Some(id);

        Ok(())
    }

    /// Returns the genesis block ID.
    pub fn genesis(&self) -> Option<Id> {
        self.genesis
    }

    /// Returns the last accepted block ID.
    pub fn last_accepted(&self) -> Option<Id> {
        self.last_accepted
    }

    /// Returns all processing block IDs.
    pub fn processing_blocks(&self) -> &HashSet<Id> {
        &self.processing
    }

    /// Gets the height of a block.
    pub fn height(&self, id: &Id) -> Option<u64> {
        self.blocks.get(id).map(|b| b.height)
    }

    /// Gets the status of a block.
    pub fn status(&self, id: &Id) -> Status {
        self.blocks
            .get(id)
            .map(|b| b.status)
            .unwrap_or(Status::Unknown)
    }

    /// Adds a block to be processed.
    pub fn add_block(&mut self, id: Id, parent: Id, height: u64) -> Result<()> {
        if self.halted {
            return Err(ConsensusError::AlreadyFinalized);
        }

        // Check if block already exists
        if self.blocks.contains_key(&id) {
            return Err(ConsensusError::BlockExists(id.to_string()));
        }

        // Check if parent exists
        let parent_node = self
            .blocks
            .get_mut(&parent)
            .ok_or_else(|| ConsensusError::ParentNotFound(parent.to_string()))?;

        // Parent must not be rejected
        if parent_node.status == Status::Rejected {
            return Err(ConsensusError::InvalidBlock(
                "parent is rejected".to_string(),
            ));
        }

        // Add child to parent
        parent_node.children.insert(id);

        // Create new block node
        let node = BlockNode {
            id,
            parent,
            height,
            children: HashSet::new(),
            status: Status::Processing,
        };

        self.blocks.insert(id, node);
        self.processing.insert(id);
        self.confidence.insert(id, 0);

        // Update preference if this extends the preferred chain
        if self.preference == Some(parent) {
            self.preference = Some(id);
        }

        Ok(())
    }

    /// Accepts a block and all its ancestors.
    fn accept_block(&mut self, id: Id) -> Result<()> {
        let node = self
            .blocks
            .get(&id)
            .ok_or_else(|| ConsensusError::BlockNotFound(id.to_string()))?
            .clone();

        if node.status != Status::Processing {
            return Ok(());
        }

        // First accept parent if not already accepted
        if node.parent != Id::default() && self.status(&node.parent) == Status::Processing {
            self.accept_block(node.parent)?;
        }

        // Accept this block
        if let Some(block_node) = self.blocks.get_mut(&id) {
            block_node.status = Status::Accepted;
        }
        self.processing.remove(&id);
        self.last_accepted = Some(id);

        // Reject siblings (blocks at same height with different parent path)
        self.reject_conflicts(id)?;

        Ok(())
    }

    /// Rejects blocks that conflict with the accepted block.
    fn reject_conflicts(&mut self, accepted_id: Id) -> Result<()> {
        let accepted = match self.blocks.get(&accepted_id) {
            Some(b) => b.clone(),
            None => return Ok(()),
        };

        // Find all siblings (same parent, different block)
        if let Some(parent) = self.blocks.get(&accepted.parent).cloned() {
            for sibling_id in &parent.children {
                if *sibling_id != accepted_id {
                    self.reject_subtree(*sibling_id)?;
                }
            }
        }

        Ok(())
    }

    /// Rejects a block and all its descendants.
    fn reject_subtree(&mut self, id: Id) -> Result<()> {
        let node = match self.blocks.get(&id).cloned() {
            Some(n) => n,
            None => return Ok(()),
        };

        if node.status == Status::Rejected {
            return Ok(());
        }

        // Reject this block
        if let Some(block_node) = self.blocks.get_mut(&id) {
            block_node.status = Status::Rejected;
        }
        self.processing.remove(&id);

        // Reject all children
        for child_id in &node.children {
            self.reject_subtree(*child_id)?;
        }

        Ok(())
    }

    /// Gets the preferred block at a given height.
    fn preferred_at_height(&self, height: u64) -> Option<Id> {
        let mut current = self.preference?;

        loop {
            let node = self.blocks.get(&current)?;
            if node.height == height {
                return Some(current);
            }
            if node.height < height {
                return None;
            }
            current = node.parent;
        }
    }
}

impl Consensus for Snowman {
    fn initialize(&mut self, params: Parameters) -> Result<()> {
        params.validate().map_err(ConsensusError::InvalidParameters)?;
        self.params = params;
        Ok(())
    }

    fn add(&mut self, id: Id) -> Result<()> {
        // For Snowman, use add_block instead
        Err(ConsensusError::Internal(
            "use add_block for Snowman".to_string(),
        ))
    }

    fn record_poll(&mut self, votes: &Bag) -> Result<bool> {
        if self.halted || votes.is_empty() {
            return Ok(false);
        }

        let (winner, winner_votes) = match votes.mode() {
            Some(m) => m,
            None => return Ok(false),
        };

        // Winner must be a processing block
        if !self.processing.contains(&winner) {
            self.consecutive_successes = 0;
            return Ok(false);
        }

        let successful = winner_votes >= self.params.alpha;

        if successful {
            // Update confidence
            let conf = self.confidence.entry(winner).or_insert(0);
            *conf += 1;

            // Update preference to winner if it has higher confidence
            let should_update = self.preference.map_or(true, |pref| {
                let pref_conf = self.confidence.get(&pref).copied().unwrap_or(0);
                self.confidence.get(&winner).copied().unwrap_or(0) > pref_conf
            });

            if should_update || self.preference == Some(winner) {
                self.preference = Some(winner);
                self.consecutive_successes += 1;
            } else {
                self.consecutive_successes = 1;
                self.preference = Some(winner);
            }

            // Check if we should accept blocks
            let beta = if self.processing.len() == 1 {
                self.params.beta_virtuous
            } else {
                self.params.beta_rogue
            };

            if self.consecutive_successes as usize >= beta {
                // Accept the winning block and ancestors
                self.accept_block(winner)?;
                return Ok(true);
            }
        } else {
            self.consecutive_successes = 0;
        }

        Ok(successful)
    }

    fn finalized(&self) -> bool {
        self.halted || self.processing.is_empty()
    }

    fn preference(&self) -> Option<Id> {
        self.preference
    }

    fn num_successful_polls(&self) -> u64 {
        self.consecutive_successes
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_id(byte: u8) -> Id {
        Id::from_slice(&[byte; 32]).unwrap()
    }

    #[test]
    fn test_snowman_genesis() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowman = Snowman::new(params);

        let genesis = make_id(0);
        snowman.set_genesis(genesis).unwrap();

        assert_eq!(snowman.genesis(), Some(genesis));
        assert_eq!(snowman.last_accepted(), Some(genesis));
        assert_eq!(snowman.preference(), Some(genesis));
        assert_eq!(snowman.status(&genesis), Status::Accepted);
    }

    #[test]
    fn test_snowman_add_block() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowman = Snowman::new(params);

        let genesis = make_id(0);
        snowman.set_genesis(genesis).unwrap();

        let block1 = make_id(1);
        snowman.add_block(block1, genesis, 1).unwrap();

        assert_eq!(snowman.status(&block1), Status::Processing);
        assert!(snowman.processing_blocks().contains(&block1));
        // Preference should update to new tip
        assert_eq!(snowman.preference(), Some(block1));
    }

    #[test]
    fn test_snowman_linear_chain() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowman = Snowman::new(params);

        let genesis = make_id(0);
        snowman.set_genesis(genesis).unwrap();

        let block1 = make_id(1);
        snowman.add_block(block1, genesis, 1).unwrap();

        // Finalize block1 with beta_virtuous polls
        for _ in 0..3 {
            let mut bag = Bag::new();
            bag.add_count(block1, 4);
            snowman.record_poll(&bag).unwrap();
        }

        assert_eq!(snowman.status(&block1), Status::Accepted);
        assert_eq!(snowman.last_accepted(), Some(block1));
    }

    #[test]
    fn test_snowman_fork_resolution() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowman = Snowman::new(params);

        let genesis = make_id(0);
        snowman.set_genesis(genesis).unwrap();

        // Create fork: two blocks at height 1
        let block1a = make_id(1);
        let block1b = make_id(2);
        snowman.add_block(block1a, genesis, 1).unwrap();
        snowman.add_block(block1b, genesis, 1).unwrap();

        // Vote for block1a - needs beta_rogue polls
        for _ in 0..5 {
            let mut bag = Bag::new();
            bag.add_count(block1a, 4);
            bag.add_count(block1b, 1);
            snowman.record_poll(&bag).unwrap();
        }

        assert_eq!(snowman.status(&block1a), Status::Accepted);
        assert_eq!(snowman.status(&block1b), Status::Rejected);
    }

    #[test]
    fn test_snowman_reject_descendants() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowman = Snowman::new(params);

        let genesis = make_id(0);
        snowman.set_genesis(genesis).unwrap();

        // Create fork with descendants
        let block1a = make_id(1);
        let block1b = make_id(2);
        let block2b = make_id(3); // Child of block1b

        snowman.add_block(block1a, genesis, 1).unwrap();
        snowman.add_block(block1b, genesis, 1).unwrap();
        snowman.add_block(block2b, block1b, 2).unwrap();

        // Accept block1a
        for _ in 0..5 {
            let mut bag = Bag::new();
            bag.add_count(block1a, 4);
            snowman.record_poll(&bag).unwrap();
        }

        assert_eq!(snowman.status(&block1a), Status::Accepted);
        assert_eq!(snowman.status(&block1b), Status::Rejected);
        assert_eq!(snowman.status(&block2b), Status::Rejected);
    }
}
