//! Chain bootstrapper implementation.
//!
//! The bootstrapper handles syncing blocks from genesis or from a state sync point.
//! It requests blocks from peers, verifies them, and applies them to the chain.
//!
//! Bootstrapping phases:
//! 1. Get accepted frontier from peers
//! 2. Find common ancestor if any
//! 3. Request missing blocks
//! 4. Verify and apply blocks
//! 5. Transition to normal consensus

use std::collections::{HashMap, HashSet, VecDeque};
use std::time::{Duration, Instant};

use parking_lot::RwLock;

use avalanche_ids::{Id, NodeId};

/// Bootstrapper configuration.
#[derive(Debug, Clone)]
pub struct BootstrapConfig {
    /// Number of peers to sample for frontier.
    pub frontier_sample_size: usize,
    /// Number of peers that must agree on frontier.
    pub frontier_alpha: usize,
    /// Maximum ancestors to request at once.
    pub max_ancestors_request: usize,
    /// Maximum blocks to request in parallel.
    pub max_outstanding_requests: usize,
    /// Request timeout.
    pub request_timeout: Duration,
    /// Maximum retries per request.
    pub max_retries: usize,
    /// Batch size for block processing.
    pub process_batch_size: usize,
}

impl Default for BootstrapConfig {
    fn default() -> Self {
        Self {
            frontier_sample_size: 20,
            frontier_alpha: 14,
            max_ancestors_request: 2048,
            max_outstanding_requests: 32,
            request_timeout: Duration::from_secs(30),
            max_retries: 3,
            process_batch_size: 256,
        }
    }
}

/// Bootstrap phase.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BootstrapPhase {
    /// Not bootstrapping.
    Idle,
    /// Getting accepted frontier.
    FetchingFrontier,
    /// Getting accepted blocks.
    FetchingAccepted,
    /// Fetching ancestor blocks.
    FetchingAncestors,
    /// Processing fetched blocks.
    Processing,
    /// Bootstrap complete.
    Complete,
    /// Bootstrap failed.
    Failed,
}

/// Block fetch status.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FetchStatus {
    /// Not yet fetched.
    Pending,
    /// Fetch in progress.
    Fetching,
    /// Fetched and ready.
    Ready,
    /// Already processed.
    Processed,
    /// Fetch failed.
    Failed,
}

/// A pending block request.
#[derive(Debug, Clone)]
struct BlockRequest {
    block_id: Id,
    peer: NodeId,
    sent_at: Instant,
    retries: usize,
}

/// A fetched block.
#[derive(Debug, Clone)]
pub struct FetchedBlock {
    /// Block ID.
    pub id: Id,
    /// Parent block ID.
    pub parent_id: Id,
    /// Block height.
    pub height: u64,
    /// Block bytes.
    pub bytes: Vec<u8>,
}

impl FetchedBlock {
    /// Creates a new fetched block.
    pub fn new(id: Id, parent_id: Id, height: u64, bytes: Vec<u8>) -> Self {
        Self {
            id,
            parent_id,
            height,
            bytes,
        }
    }
}

/// The chain bootstrapper.
pub struct Bootstrapper {
    /// Configuration.
    config: BootstrapConfig,
    /// Current phase.
    phase: RwLock<BootstrapPhase>,
    /// Our last accepted block.
    last_accepted: RwLock<Option<Id>>,
    /// Target frontier.
    target_frontier: RwLock<HashSet<Id>>,
    /// Frontier votes from peers.
    frontier_votes: RwLock<HashMap<Id, HashSet<NodeId>>>,
    /// Blocks to fetch.
    blocks_to_fetch: RwLock<VecDeque<Id>>,
    /// Block fetch status.
    fetch_status: RwLock<HashMap<Id, FetchStatus>>,
    /// Pending requests.
    pending_requests: RwLock<HashMap<Id, BlockRequest>>,
    /// Fetched blocks ready for processing.
    fetched_blocks: RwLock<HashMap<Id, FetchedBlock>>,
    /// Processing queue (ordered by height).
    process_queue: RwLock<Vec<Id>>,
    /// Available peers.
    available_peers: RwLock<Vec<NodeId>>,
    /// Blocks processed count.
    processed_count: RwLock<u64>,
    /// Start time.
    start_time: RwLock<Option<Instant>>,
}

impl Bootstrapper {
    /// Creates a new bootstrapper.
    pub fn new(config: BootstrapConfig) -> Self {
        Self {
            config,
            phase: RwLock::new(BootstrapPhase::Idle),
            last_accepted: RwLock::new(None),
            target_frontier: RwLock::new(HashSet::new()),
            frontier_votes: RwLock::new(HashMap::new()),
            blocks_to_fetch: RwLock::new(VecDeque::new()),
            fetch_status: RwLock::new(HashMap::new()),
            pending_requests: RwLock::new(HashMap::new()),
            fetched_blocks: RwLock::new(HashMap::new()),
            process_queue: RwLock::new(Vec::new()),
            available_peers: RwLock::new(Vec::new()),
            processed_count: RwLock::new(0),
            start_time: RwLock::new(None),
        }
    }

    /// Creates a bootstrapper with default config.
    pub fn with_defaults() -> Self {
        Self::new(BootstrapConfig::default())
    }

    /// Returns the current phase.
    pub fn phase(&self) -> BootstrapPhase {
        *self.phase.read()
    }

    /// Returns true if bootstrapping is in progress.
    pub fn is_bootstrapping(&self) -> bool {
        let phase = *self.phase.read();
        !matches!(phase, BootstrapPhase::Idle | BootstrapPhase::Complete | BootstrapPhase::Failed)
    }

    /// Starts bootstrapping.
    pub fn start(&self, last_accepted: Id, peers: Vec<NodeId>) {
        *self.phase.write() = BootstrapPhase::FetchingFrontier;
        *self.last_accepted.write() = Some(last_accepted);
        *self.available_peers.write() = peers;
        *self.start_time.write() = Some(Instant::now());
        self.frontier_votes.write().clear();
        self.target_frontier.write().clear();
    }

    /// Returns peers to query for frontier.
    pub fn peers_to_query_frontier(&self) -> Vec<NodeId> {
        let peers = self.available_peers.read();
        peers
            .iter()
            .take(self.config.frontier_sample_size)
            .copied()
            .collect()
    }

    /// Handles received frontier from a peer.
    pub fn on_frontier_received(&self, peer: NodeId, frontier: Vec<Id>) {
        if *self.phase.read() != BootstrapPhase::FetchingFrontier {
            return;
        }

        let mut votes = self.frontier_votes.write();
        for block_id in frontier {
            votes
                .entry(block_id)
                .or_insert_with(HashSet::new)
                .insert(peer);
        }

        // Check if any block has enough votes
        let mut accepted = Vec::new();
        for (block_id, voters) in votes.iter() {
            if voters.len() >= self.config.frontier_alpha {
                accepted.push(*block_id);
            }
        }

        if !accepted.is_empty() {
            drop(votes);
            self.accept_frontier(accepted);
        }
    }

    /// Accepts a frontier and moves to fetching accepted blocks.
    fn accept_frontier(&self, frontier: Vec<Id>) {
        let mut target = self.target_frontier.write();
        target.clear();
        for id in frontier {
            target.insert(id);
        }
        *self.phase.write() = BootstrapPhase::FetchingAccepted;
    }

    /// Returns the accepted frontier.
    pub fn target_frontier(&self) -> HashSet<Id> {
        self.target_frontier.read().clone()
    }

    /// Returns blocks to query accepted status for.
    pub fn blocks_to_query_accepted(&self) -> Vec<Id> {
        self.target_frontier.read().iter().copied().collect()
    }

    /// Handles accepted response from a peer.
    pub fn on_accepted_received(&self, _peer: NodeId, accepted: Vec<Id>) {
        if *self.phase.read() != BootstrapPhase::FetchingAccepted {
            return;
        }

        // Add accepted blocks to fetch queue
        let mut to_fetch = self.blocks_to_fetch.write();
        let mut status = self.fetch_status.write();

        for block_id in accepted {
            if !status.contains_key(&block_id) {
                to_fetch.push_back(block_id);
                status.insert(block_id, FetchStatus::Pending);
            }
        }

        drop(to_fetch);
        drop(status);

        *self.phase.write() = BootstrapPhase::FetchingAncestors;
    }

    /// Returns the next block IDs to fetch.
    pub fn blocks_to_fetch(&self) -> Vec<Id> {
        let to_fetch = self.blocks_to_fetch.read();
        let pending = self.pending_requests.read();

        let active = pending.len();
        let max_new = self.config.max_outstanding_requests.saturating_sub(active);

        to_fetch.iter().take(max_new).copied().collect()
    }

    /// Selects a peer for a block request.
    pub fn select_peer(&self) -> Option<NodeId> {
        let peers = self.available_peers.read();
        if peers.is_empty() {
            return None;
        }
        let idx = rand_usize() % peers.len();
        Some(peers[idx])
    }

    /// Records a block request.
    pub fn on_block_requested(&self, block_id: Id, peer: NodeId) {
        self.pending_requests.write().insert(
            block_id,
            BlockRequest {
                block_id,
                peer,
                sent_at: Instant::now(),
                retries: 0,
            },
        );
        self.fetch_status.write().insert(block_id, FetchStatus::Fetching);

        // Remove from to_fetch queue
        self.blocks_to_fetch.write().retain(|id| *id != block_id);
    }

    /// Handles a received block.
    pub fn on_block_received(&self, block: FetchedBlock) {
        let phase = *self.phase.read();
        if !matches!(phase, BootstrapPhase::FetchingAncestors | BootstrapPhase::Processing) {
            return;
        }

        let block_id = block.id;

        // Remove from pending
        self.pending_requests.write().remove(&block_id);

        // Update status
        self.fetch_status.write().insert(block_id, FetchStatus::Ready);

        // Check if we need to fetch the parent
        let parent_id = block.parent_id;
        let last_accepted = self.last_accepted.read().unwrap_or_default();

        if parent_id != last_accepted {
            let mut status = self.fetch_status.write();
            if !status.contains_key(&parent_id) {
                // Need to fetch parent
                status.insert(parent_id, FetchStatus::Pending);
                self.blocks_to_fetch.write().push_front(parent_id);
            }
        }

        // Store the block
        self.fetched_blocks.write().insert(block_id, block);

        // Check if we should start processing
        self.maybe_start_processing();
    }

    /// Handles a failed block request.
    pub fn on_block_failed(&self, block_id: Id) {
        let mut pending = self.pending_requests.write();
        if let Some(mut req) = pending.remove(&block_id) {
            req.retries += 1;
            if req.retries < self.config.max_retries {
                // Retry
                self.fetch_status.write().insert(block_id, FetchStatus::Pending);
                self.blocks_to_fetch.write().push_back(block_id);
            } else {
                self.fetch_status.write().insert(block_id, FetchStatus::Failed);
            }
        }
    }

    /// Checks for timed out requests.
    pub fn check_timeouts(&self) -> Vec<Id> {
        let mut pending = self.pending_requests.write();
        let timeout = self.config.request_timeout;

        let timed_out: Vec<Id> = pending
            .iter()
            .filter(|(_, req)| req.sent_at.elapsed() > timeout)
            .map(|(id, _)| *id)
            .collect();

        for id in &timed_out {
            if let Some(mut req) = pending.remove(id) {
                req.retries += 1;
                if req.retries < self.config.max_retries {
                    self.fetch_status.write().insert(*id, FetchStatus::Pending);
                    self.blocks_to_fetch.write().push_back(*id);
                } else {
                    self.fetch_status.write().insert(*id, FetchStatus::Failed);
                }
            }
        }

        timed_out
    }

    /// Checks if we should start processing blocks.
    fn maybe_start_processing(&self) {
        let pending = self.pending_requests.read();
        let to_fetch = self.blocks_to_fetch.read();

        // Start processing if no more fetching needed
        if pending.is_empty() && to_fetch.is_empty() {
            drop(pending);
            drop(to_fetch);
            *self.phase.write() = BootstrapPhase::Processing;
            self.build_process_queue();
        }
    }

    /// Builds the processing queue ordered by height.
    fn build_process_queue(&self) {
        let fetched = self.fetched_blocks.read();
        let mut blocks: Vec<_> = fetched.values().collect();

        // Sort by height (lowest first)
        blocks.sort_by_key(|b| b.height);

        let queue: Vec<Id> = blocks.iter().map(|b| b.id).collect();
        *self.process_queue.write() = queue;
    }

    /// Returns the next batch of blocks to process.
    pub fn blocks_to_process(&self) -> Vec<FetchedBlock> {
        if *self.phase.read() != BootstrapPhase::Processing {
            return Vec::new();
        }

        let mut queue = self.process_queue.write();
        let fetched = self.fetched_blocks.read();

        let count = self.config.process_batch_size.min(queue.len());
        let batch: Vec<FetchedBlock> = queue
            .drain(..count)
            .filter_map(|id| fetched.get(&id).cloned())
            .collect();

        batch
    }

    /// Marks blocks as processed.
    pub fn on_blocks_processed(&self, block_ids: &[Id]) {
        let mut status = self.fetch_status.write();
        let mut count = self.processed_count.write();

        for id in block_ids {
            status.insert(*id, FetchStatus::Processed);
            *count += 1;
        }
    }

    /// Returns true if bootstrap is complete.
    pub fn is_complete(&self) -> bool {
        if *self.phase.read() != BootstrapPhase::Processing {
            return false;
        }

        let queue = self.process_queue.read();
        let pending = self.pending_requests.read();
        let to_fetch = self.blocks_to_fetch.read();

        queue.is_empty() && pending.is_empty() && to_fetch.is_empty()
    }

    /// Completes the bootstrap.
    pub fn complete(&self) {
        *self.phase.write() = BootstrapPhase::Complete;
    }

    /// Fails the bootstrap.
    pub fn fail(&self) {
        *self.phase.write() = BootstrapPhase::Failed;
    }

    /// Resets the bootstrapper.
    pub fn reset(&self) {
        *self.phase.write() = BootstrapPhase::Idle;
        self.frontier_votes.write().clear();
        self.target_frontier.write().clear();
        self.blocks_to_fetch.write().clear();
        self.fetch_status.write().clear();
        self.pending_requests.write().clear();
        self.fetched_blocks.write().clear();
        self.process_queue.write().clear();
        *self.processed_count.write() = 0;
        *self.start_time.write() = None;
    }

    /// Returns the number of blocks processed.
    pub fn processed_count(&self) -> u64 {
        *self.processed_count.read()
    }

    /// Returns the bootstrap duration.
    pub fn duration(&self) -> Option<Duration> {
        self.start_time.read().map(|t| t.elapsed())
    }

    /// Returns progress percentage.
    pub fn progress(&self) -> f64 {
        let status = self.fetch_status.read();
        if status.is_empty() {
            return 0.0;
        }

        let processed = status
            .values()
            .filter(|s| **s == FetchStatus::Processed)
            .count();

        (processed as f64 / status.len() as f64) * 100.0
    }
}

/// Simple PRNG for peer selection.
fn rand_usize() -> usize {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos() as usize
}

impl Default for Bootstrapper {
    fn default() -> Self {
        Self::with_defaults()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_peers(count: usize) -> Vec<NodeId> {
        (0..count)
            .map(|i| NodeId::from_bytes([i as u8; 20]))
            .collect()
    }

    fn make_block(id_byte: u8, parent_byte: u8, height: u64) -> FetchedBlock {
        FetchedBlock::new(
            Id::from_bytes([id_byte; 32]),
            Id::from_bytes([parent_byte; 32]),
            height,
            vec![0; 100],
        )
    }

    #[test]
    fn test_start_bootstrap() {
        let boot = Bootstrapper::with_defaults();
        let peers = make_peers(20);
        let last = Id::from_bytes([0; 32]);

        boot.start(last, peers);
        assert_eq!(boot.phase(), BootstrapPhase::FetchingFrontier);
        assert!(boot.is_bootstrapping());
    }

    #[test]
    fn test_frontier_voting() {
        let config = BootstrapConfig {
            frontier_alpha: 3,
            ..Default::default()
        };
        let boot = Bootstrapper::new(config);
        let peers = make_peers(5);
        let frontier = vec![Id::from_bytes([1; 32])];

        boot.start(Id::from_bytes([0; 32]), peers.clone());

        // Two votes not enough
        boot.on_frontier_received(peers[0], frontier.clone());
        boot.on_frontier_received(peers[1], frontier.clone());
        assert_eq!(boot.phase(), BootstrapPhase::FetchingFrontier);

        // Third vote accepts
        boot.on_frontier_received(peers[2], frontier);
        assert_eq!(boot.phase(), BootstrapPhase::FetchingAccepted);
    }

    #[test]
    fn test_block_fetching() {
        let config = BootstrapConfig {
            frontier_alpha: 1,
            max_outstanding_requests: 4,
            ..Default::default()
        };
        let boot = Bootstrapper::new(config);
        let peers = make_peers(5);
        let frontier = vec![Id::from_bytes([10; 32])];

        boot.start(Id::from_bytes([0; 32]), peers.clone());
        boot.on_frontier_received(peers[0], frontier.clone());
        boot.on_accepted_received(peers[0], frontier);

        // Should have blocks to fetch
        let to_fetch = boot.blocks_to_fetch();
        assert!(!to_fetch.is_empty());
    }

    #[test]
    fn test_block_received() {
        let config = BootstrapConfig {
            frontier_alpha: 1,
            ..Default::default()
        };
        let boot = Bootstrapper::new(config);
        let peers = make_peers(5);
        let last_accepted = Id::from_bytes([0; 32]);
        let frontier_id = Id::from_bytes([1; 32]);

        boot.start(last_accepted, peers.clone());
        boot.on_frontier_received(peers[0], vec![frontier_id]);
        boot.on_accepted_received(peers[0], vec![frontier_id]);

        // Request and receive block
        boot.on_block_requested(frontier_id, peers[0]);

        let block = FetchedBlock::new(
            frontier_id,
            last_accepted, // Parent is last accepted
            1,
            vec![0; 100],
        );
        boot.on_block_received(block);

        // Should move to processing since parent is last accepted
        assert_eq!(boot.phase(), BootstrapPhase::Processing);
    }

    #[test]
    fn test_ancestor_fetching() {
        let config = BootstrapConfig {
            frontier_alpha: 1,
            ..Default::default()
        };
        let boot = Bootstrapper::new(config);
        let peers = make_peers(5);
        let last_accepted = Id::from_bytes([0; 32]);
        let frontier_id = Id::from_bytes([3; 32]);
        let parent_id = Id::from_bytes([2; 32]);

        boot.start(last_accepted, peers.clone());
        boot.on_frontier_received(peers[0], vec![frontier_id]);
        boot.on_accepted_received(peers[0], vec![frontier_id]);

        boot.on_block_requested(frontier_id, peers[0]);

        // Block with unknown parent
        let block = FetchedBlock::new(frontier_id, parent_id, 3, vec![0; 100]);
        boot.on_block_received(block);

        // Should need to fetch parent
        let to_fetch = boot.blocks_to_fetch();
        assert!(to_fetch.contains(&parent_id));
    }

    #[test]
    fn test_process_queue_ordering() {
        let config = BootstrapConfig {
            frontier_alpha: 1,
            ..Default::default()
        };
        let boot = Bootstrapper::new(config);
        let peers = make_peers(5);
        let last_accepted = Id::from_bytes([0; 32]);

        boot.start(last_accepted, peers.clone());
        boot.on_frontier_received(peers[0], vec![Id::from_bytes([3; 32])]);
        boot.on_accepted_received(
            peers[0],
            vec![
                Id::from_bytes([1; 32]),
                Id::from_bytes([2; 32]),
                Id::from_bytes([3; 32]),
            ],
        );

        // Add blocks in random height order
        let blocks = vec![
            make_block(3, 2, 3),
            make_block(1, 0, 1),
            make_block(2, 1, 2),
        ];

        for block in blocks {
            boot.on_block_requested(block.id, peers[0]);
            boot.on_block_received(block);
        }

        // Process queue should be height-ordered
        let to_process = boot.blocks_to_process();
        assert_eq!(to_process[0].height, 1);
        assert_eq!(to_process[1].height, 2);
        assert_eq!(to_process[2].height, 3);
    }

    #[test]
    fn test_reset() {
        let boot = Bootstrapper::with_defaults();
        let peers = make_peers(5);

        boot.start(Id::from_bytes([0; 32]), peers);
        assert!(boot.is_bootstrapping());

        boot.reset();
        assert_eq!(boot.phase(), BootstrapPhase::Idle);
        assert!(!boot.is_bootstrapping());
    }
}
