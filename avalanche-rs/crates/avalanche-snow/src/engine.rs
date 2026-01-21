//! Consensus engine state machine.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;
use std::time::{Duration, Instant};

use avalanche_ids::{Id, NodeId};
use parking_lot::RwLock;

use crate::consensus::Bag;
use crate::{ConsensusError, Parameters, Result, ValidatorSet};

/// State of the consensus engine.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EngineState {
    /// Initial state, not yet started
    Initializing,
    /// Bootstrapping from network
    Bootstrapping,
    /// Normal consensus operation
    Consensus,
    /// Engine is halted
    Halted,
}

impl std::fmt::Display for EngineState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            EngineState::Initializing => write!(f, "Initializing"),
            EngineState::Bootstrapping => write!(f, "Bootstrapping"),
            EngineState::Consensus => write!(f, "Consensus"),
            EngineState::Halted => write!(f, "Halted"),
        }
    }
}

/// A pending request awaiting response.
#[derive(Debug)]
struct PendingRequest {
    /// Nodes we're waiting for
    pending_nodes: HashSet<NodeId>,
    /// Start time for timeout
    start_time: Instant,
    /// Timeout duration
    timeout: Duration,
    /// Request type
    request_type: RequestType,
}

/// Types of requests.
#[derive(Debug, Clone, Copy)]
pub enum RequestType {
    /// Pull query (request block + vote)
    PullQuery,
    /// Push query (send block, request vote)
    PushQuery,
    /// Get block
    GetBlock,
    /// Get ancestors
    GetAncestors,
}

/// Tracks outstanding requests.
#[derive(Debug, Default)]
struct RequestTracker {
    /// Pending requests by request ID
    requests: HashMap<u32, PendingRequest>,
    /// Next request ID
    next_id: u32,
}

impl RequestTracker {
    fn new() -> Self {
        Self::default()
    }

    fn add_request(
        &mut self,
        nodes: HashSet<NodeId>,
        timeout: Duration,
        request_type: RequestType,
    ) -> u32 {
        let id = self.next_id;
        self.next_id = self.next_id.wrapping_add(1);

        self.requests.insert(
            id,
            PendingRequest {
                pending_nodes: nodes,
                start_time: Instant::now(),
                timeout,
                request_type,
            },
        );

        id
    }

    fn record_response(&mut self, request_id: u32, node_id: &NodeId) -> bool {
        if let Some(request) = self.requests.get_mut(&request_id) {
            request.pending_nodes.remove(node_id);
            request.pending_nodes.is_empty()
        } else {
            false
        }
    }

    fn remove_request(&mut self, request_id: u32) -> Option<PendingRequest> {
        self.requests.remove(&request_id)
    }

    fn timed_out_requests(&self) -> Vec<u32> {
        self.requests
            .iter()
            .filter(|(_, req)| req.start_time.elapsed() > req.timeout)
            .map(|(id, _)| *id)
            .collect()
    }

    fn pending_count(&self) -> usize {
        self.requests.len()
    }
}

/// The consensus engine manages consensus lifecycle.
#[derive(Debug)]
pub struct Engine {
    /// Current state
    state: RwLock<EngineState>,
    /// Consensus parameters
    params: Parameters,
    /// Validator set for sampling
    validators: Arc<ValidatorSet>,
    /// Request tracking
    requests: RwLock<RequestTracker>,
    /// Current poll accumulator
    current_poll: RwLock<Option<(u32, Bag)>>,
    /// Blocks we've seen but haven't processed
    pending_blocks: RwLock<HashSet<Id>>,
    /// Blocks that failed verification
    failed_blocks: RwLock<HashSet<Id>>,
    /// Number of polls completed
    polls_completed: RwLock<u64>,
}

impl Engine {
    /// Creates a new engine.
    pub fn new(params: Parameters, validators: Arc<ValidatorSet>) -> Self {
        Self {
            state: RwLock::new(EngineState::Initializing),
            params,
            validators,
            requests: RwLock::new(RequestTracker::new()),
            current_poll: RwLock::new(None),
            pending_blocks: RwLock::new(HashSet::new()),
            failed_blocks: RwLock::new(HashSet::new()),
            polls_completed: RwLock::new(0),
        }
    }

    /// Returns the current state.
    pub fn state(&self) -> EngineState {
        *self.state.read()
    }

    /// Transitions to a new state.
    pub fn transition(&self, new_state: EngineState) -> Result<()> {
        let mut state = self.state.write();
        let current = *state;

        // Validate transition
        let valid = match (current, new_state) {
            (EngineState::Initializing, EngineState::Bootstrapping) => true,
            (EngineState::Initializing, EngineState::Consensus) => true,
            (EngineState::Bootstrapping, EngineState::Consensus) => true,
            (EngineState::Consensus, EngineState::Halted) => true,
            (EngineState::Bootstrapping, EngineState::Halted) => true,
            _ => false,
        };

        if !valid {
            return Err(ConsensusError::InvalidState {
                expected: format!("valid transition from {}", current),
                actual: format!("{} -> {}", current, new_state),
            });
        }

        *state = new_state;
        Ok(())
    }

    /// Starts a new poll.
    pub fn start_poll(&self, block_id: Id) -> Result<(u32, Vec<NodeId>)> {
        self.require_state(EngineState::Consensus)?;

        // Sample validators
        let sampled = self.validators.sample(self.params.k)?;

        // Create request
        let nodes: HashSet<NodeId> = sampled.iter().cloned().collect();
        let request_id = self.requests.write().add_request(
            nodes,
            self.params.max_item_processing_time,
            RequestType::PushQuery,
        );

        // Initialize poll accumulator
        *self.current_poll.write() = Some((request_id, Bag::new()));

        Ok((request_id, sampled))
    }

    /// Records a vote from a validator.
    pub fn record_vote(&self, request_id: u32, node_id: &NodeId, vote: Id) -> Result<bool> {
        // Record response
        let complete = self.requests.write().record_response(request_id, node_id);

        // Add vote to current poll
        if let Some((poll_id, ref mut bag)) = *self.current_poll.write() {
            if poll_id == request_id {
                bag.add(vote);
            }
        }

        Ok(complete)
    }

    /// Gets the accumulated votes for the current poll.
    pub fn get_poll_results(&self, request_id: u32) -> Option<Bag> {
        let poll = self.current_poll.read();
        if let Some((poll_id, ref bag)) = *poll {
            if poll_id == request_id {
                return Some(bag.clone());
            }
        }
        None
    }

    /// Completes the current poll.
    pub fn complete_poll(&self, request_id: u32) -> Option<Bag> {
        let mut poll = self.current_poll.write();
        if let Some((poll_id, bag)) = poll.take() {
            if poll_id == request_id {
                self.requests.write().remove_request(request_id);
                *self.polls_completed.write() += 1;
                return Some(bag);
            }
            // Put it back if IDs don't match
            *poll = Some((poll_id, bag));
        }
        None
    }

    /// Marks a block as pending.
    pub fn add_pending_block(&self, id: Id) {
        self.pending_blocks.write().insert(id);
    }

    /// Removes a block from pending.
    pub fn remove_pending_block(&self, id: &Id) {
        self.pending_blocks.write().remove(id);
    }

    /// Returns pending blocks.
    pub fn pending_blocks(&self) -> Vec<Id> {
        self.pending_blocks.read().iter().copied().collect()
    }

    /// Marks a block as failed.
    pub fn mark_failed(&self, id: Id) {
        self.failed_blocks.write().insert(id);
        self.pending_blocks.write().remove(&id);
    }

    /// Returns true if the block has failed.
    pub fn is_failed(&self, id: &Id) -> bool {
        self.failed_blocks.read().contains(id)
    }

    /// Returns the number of pending requests.
    pub fn pending_requests(&self) -> usize {
        self.requests.read().pending_count()
    }

    /// Returns the number of polls completed.
    pub fn polls_completed(&self) -> u64 {
        *self.polls_completed.read()
    }

    /// Processes timed out requests.
    pub fn process_timeouts(&self) -> Vec<u32> {
        let timed_out = self.requests.read().timed_out_requests();
        let mut requests = self.requests.write();

        for id in &timed_out {
            requests.remove_request(*id);
        }

        timed_out
    }

    fn require_state(&self, required: EngineState) -> Result<()> {
        let current = *self.state.read();
        if current != required {
            Err(ConsensusError::InvalidState {
                expected: required.to_string(),
                actual: current.to_string(),
            })
        } else {
            Ok(())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    fn make_id(byte: u8) -> Id {
        Id::from_slice(&[byte; 32]).unwrap()
    }

    fn create_validator_set(count: usize) -> Arc<ValidatorSet> {
        let subnet = Id::from_slice(&[0; 32]).unwrap();
        let set = ValidatorSet::new(subnet);

        for i in 0..count {
            let v = crate::Validator::new(make_node_id(i as u8), 100);
            set.add(v).unwrap();
        }

        Arc::new(set)
    }

    #[test]
    fn test_engine_state_transitions() {
        let validators = create_validator_set(10);
        let engine = Engine::new(Parameters::default(), validators);

        assert_eq!(engine.state(), EngineState::Initializing);

        engine.transition(EngineState::Bootstrapping).unwrap();
        assert_eq!(engine.state(), EngineState::Bootstrapping);

        engine.transition(EngineState::Consensus).unwrap();
        assert_eq!(engine.state(), EngineState::Consensus);

        engine.transition(EngineState::Halted).unwrap();
        assert_eq!(engine.state(), EngineState::Halted);
    }

    #[test]
    fn test_engine_invalid_transition() {
        let validators = create_validator_set(10);
        let engine = Engine::new(Parameters::default(), validators);

        // Can't go directly to Halted from Initializing
        assert!(engine.transition(EngineState::Halted).is_err());
    }

    #[test]
    fn test_engine_poll() {
        let validators = create_validator_set(30);
        let engine = Engine::new(Parameters::default(), validators);

        // Must be in Consensus state
        engine.transition(EngineState::Consensus).unwrap();

        let block_id = make_id(1);
        let (request_id, sampled) = engine.start_poll(block_id).unwrap();

        assert_eq!(sampled.len(), 20); // k = 20

        // Record votes
        for (i, node_id) in sampled.iter().enumerate() {
            let complete = engine.record_vote(request_id, node_id, block_id).unwrap();
            if i < sampled.len() - 1 {
                assert!(!complete);
            } else {
                assert!(complete);
            }
        }

        // Complete poll
        let results = engine.complete_poll(request_id).unwrap();
        assert_eq!(results.len(), 20);
        assert_eq!(results.count(&block_id), 20);
    }

    #[test]
    fn test_engine_pending_blocks() {
        let validators = create_validator_set(10);
        let engine = Engine::new(Parameters::default(), validators);

        let id1 = make_id(1);
        let id2 = make_id(2);

        engine.add_pending_block(id1);
        engine.add_pending_block(id2);

        assert_eq!(engine.pending_blocks().len(), 2);

        engine.remove_pending_block(&id1);
        assert_eq!(engine.pending_blocks().len(), 1);

        engine.mark_failed(id2);
        assert!(engine.is_failed(&id2));
        assert_eq!(engine.pending_blocks().len(), 0);
    }
}
