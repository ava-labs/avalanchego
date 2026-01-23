//! Network client for state sync and bootstrapping.
//!
//! This module provides the network integration for syncing blocks and state
//! from peers. It coordinates between the StateSync/Bootstrapper modules
//! and the Network layer.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use parking_lot::RwLock;
use tokio::sync::{mpsc, oneshot};
use tokio::time::timeout;
use tracing::{debug, info, warn};

use avalanche_ids::{Id, NodeId};

use crate::bootstrapper::{Bootstrapper, FetchedBlock};
use crate::sync::{StateChunk, StateSync, StateSummary};
use crate::{ConsensusError, Result};

/// Request ID counter for tracking responses.
static REQUEST_ID: AtomicU32 = AtomicU32::new(1);

/// Generate a new unique request ID.
fn next_request_id() -> u32 {
    REQUEST_ID.fetch_add(1, Ordering::SeqCst)
}

/// A pending request waiting for a response.
struct PendingRequest<T> {
    sender: oneshot::Sender<T>,
}

/// Configuration for the sync client.
#[derive(Debug, Clone)]
pub struct SyncClientConfig {
    /// Request timeout.
    pub request_timeout: Duration,
    /// Maximum concurrent requests.
    pub max_concurrent_requests: usize,
    /// Retry delay between attempts.
    pub retry_delay: Duration,
}

impl Default for SyncClientConfig {
    fn default() -> Self {
        Self {
            request_timeout: Duration::from_secs(10),
            max_concurrent_requests: 32,
            retry_delay: Duration::from_millis(500),
        }
    }
}

/// Trait for sending sync-related network messages.
///
/// This is abstracted to allow testing without a real network.
#[async_trait]
pub trait SyncNetwork: Send + Sync {
    /// Sends a GetStateSummaryFrontier request.
    async fn get_state_summary_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> Result<()>;

    /// Sends a GetAcceptedStateSummary request.
    async fn get_accepted_state_summary(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        heights: Vec<u64>,
    ) -> Result<()>;

    /// Sends a GetAcceptedFrontier request.
    async fn get_accepted_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> Result<()>;

    /// Sends a GetAccepted request.
    async fn get_accepted(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_ids: Vec<Id>,
    ) -> Result<()>;

    /// Sends a GetAncestors request.
    async fn get_ancestors(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    ) -> Result<()>;

    /// Returns the list of connected peers.
    fn connected_peers(&self) -> Vec<NodeId>;
}

/// Response types that can be received from peers.
#[derive(Debug, Clone)]
pub enum SyncResponse {
    /// State summary frontier response.
    StateSummaryFrontier {
        peer: NodeId,
        request_id: u32,
        summary: Vec<u8>,
    },
    /// Accepted state summary response.
    AcceptedStateSummary {
        peer: NodeId,
        request_id: u32,
        summary_ids: Vec<Id>,
    },
    /// Accepted frontier response.
    AcceptedFrontier {
        peer: NodeId,
        request_id: u32,
        container_id: Id,
    },
    /// Accepted containers response.
    Accepted {
        peer: NodeId,
        request_id: u32,
        container_ids: Vec<Id>,
    },
    /// Ancestors response.
    Ancestors {
        peer: NodeId,
        request_id: u32,
        containers: Vec<Vec<u8>>,
    },
    /// Request failed or timed out.
    Failed {
        peer: NodeId,
        request_id: u32,
    },
}

/// The sync engine coordinates state sync and bootstrapping with the network.
pub struct SyncEngine<N: SyncNetwork> {
    /// Network client.
    network: Arc<N>,
    /// Chain ID being synced.
    chain_id: Id,
    /// State synchronizer.
    state_sync: Arc<StateSync>,
    /// Block bootstrapper.
    bootstrapper: Arc<Bootstrapper>,
    /// Configuration.
    config: SyncClientConfig,
    /// Pending summary requests (request_id -> sender).
    pending_summaries: RwLock<HashMap<u32, oneshot::Sender<(NodeId, Vec<u8>)>>>,
    /// Pending block requests (request_id -> sender).
    pending_blocks: RwLock<HashMap<u32, oneshot::Sender<(NodeId, Vec<Vec<u8>>)>>>,
    /// Pending frontier requests.
    pending_frontiers: RwLock<HashMap<u32, oneshot::Sender<(NodeId, Vec<Id>)>>>,
    /// Response receiver.
    response_rx: RwLock<Option<mpsc::Receiver<SyncResponse>>>,
    /// Response sender (for tests and external use).
    response_tx: mpsc::Sender<SyncResponse>,
}

impl<N: SyncNetwork> SyncEngine<N> {
    /// Creates a new sync engine.
    pub fn new(
        network: Arc<N>,
        chain_id: Id,
        state_sync: Arc<StateSync>,
        bootstrapper: Arc<Bootstrapper>,
        config: SyncClientConfig,
    ) -> Self {
        let (response_tx, response_rx) = mpsc::channel(256);
        Self {
            network,
            chain_id,
            state_sync,
            bootstrapper,
            config,
            pending_summaries: RwLock::new(HashMap::new()),
            pending_blocks: RwLock::new(HashMap::new()),
            pending_frontiers: RwLock::new(HashMap::new()),
            response_rx: RwLock::new(Some(response_rx)),
            response_tx,
        }
    }

    /// Returns a sender for delivering responses to this engine.
    pub fn response_sender(&self) -> mpsc::Sender<SyncResponse> {
        self.response_tx.clone()
    }

    /// Starts state sync from peers.
    pub async fn start_state_sync(&self) -> Result<()> {
        let peers = self.network.connected_peers();
        if peers.is_empty() {
            return Err(ConsensusError::NotEnoughPeers);
        }

        info!("Starting state sync with {} peers", peers.len());
        self.state_sync.start(peers.clone());

        // Request summaries from peers
        self.request_state_summaries().await?;

        Ok(())
    }

    /// Starts block bootstrapping from peers.
    pub async fn start_bootstrap(&self, last_accepted: Id) -> Result<()> {
        let peers = self.network.connected_peers();
        if peers.is_empty() {
            return Err(ConsensusError::NotEnoughPeers);
        }

        info!("Starting bootstrap with {} peers", peers.len());
        self.bootstrapper.start(last_accepted, peers.clone());

        // Request frontier from peers
        self.request_frontier().await?;

        Ok(())
    }

    /// Requests state summaries from peers.
    async fn request_state_summaries(&self) -> Result<()> {
        let peers = self.state_sync.peers_to_query_summaries();

        for peer in peers {
            let request_id = next_request_id();
            debug!("Requesting state summary frontier from {}", peer);

            if let Err(e) = self
                .network
                .get_state_summary_frontier(peer, self.chain_id, request_id)
                .await
            {
                warn!("Failed to request summary from {}: {}", peer, e);
            }
        }

        Ok(())
    }

    /// Requests accepted frontier from peers.
    async fn request_frontier(&self) -> Result<()> {
        let peers = self.bootstrapper.peers_to_query_frontier();

        for peer in peers {
            let request_id = next_request_id();
            debug!("Requesting accepted frontier from {}", peer);

            if let Err(e) = self
                .network
                .get_accepted_frontier(peer, self.chain_id, request_id)
                .await
            {
                warn!("Failed to request frontier from {}: {}", peer, e);
            }
        }

        Ok(())
    }

    /// Requests blocks that need to be fetched.
    pub async fn request_blocks(&self) -> Result<()> {
        let blocks_to_fetch = self.bootstrapper.blocks_to_fetch();

        for block_id in blocks_to_fetch {
            if let Some(peer) = self.bootstrapper.select_peer() {
                let request_id = next_request_id();
                debug!("Requesting ancestors for {} from {}", block_id, peer);

                self.bootstrapper.on_block_requested(block_id, peer);

                if let Err(e) = self
                    .network
                    .get_ancestors(peer, self.chain_id, request_id, block_id)
                    .await
                {
                    warn!("Failed to request ancestors from {}: {}", peer, e);
                    self.bootstrapper.on_block_failed(block_id);
                }
            }
        }

        Ok(())
    }

    /// Requests state chunks that need to be downloaded.
    pub async fn request_chunks(&self) -> Result<()> {
        let chunks_to_download = self.state_sync.chunks_to_download();

        for chunk_idx in chunks_to_download {
            if let Some(peer) = self.state_sync.select_peer_for_chunk() {
                debug!("Requesting chunk {} from {}", chunk_idx, peer);
                self.state_sync.on_chunk_requested(chunk_idx, peer);

                // State chunks are requested via app-level protocol
                // In a real implementation, this would send a GetStateChunk message
                // For now, we just track that the request was made
            }
        }

        Ok(())
    }

    /// Handles a response from a peer.
    pub fn handle_response(&self, response: SyncResponse) {
        match response {
            SyncResponse::StateSummaryFrontier {
                peer,
                request_id: _,
                summary,
            } => {
                // Parse summary and notify state sync
                if let Some(parsed) = self.parse_state_summary(&summary) {
                    self.state_sync.on_summary_received(peer, parsed);
                }
            }

            SyncResponse::AcceptedFrontier {
                peer,
                request_id: _,
                container_id,
            } => {
                self.bootstrapper
                    .on_frontier_received(peer, vec![container_id]);
            }

            SyncResponse::Accepted {
                peer,
                request_id: _,
                container_ids,
            } => {
                self.bootstrapper.on_accepted_received(peer, container_ids);
            }

            SyncResponse::Ancestors {
                peer,
                request_id: _,
                containers,
            } => {
                // Parse and deliver blocks
                for (i, container) in containers.iter().enumerate() {
                    if let Some(block) = self.parse_block(container) {
                        self.bootstrapper.on_block_received(block);
                    } else {
                        warn!("Failed to parse block {} from {}", i, peer);
                    }
                }
            }

            SyncResponse::Failed { peer, request_id: _ } => {
                debug!("Request to {} failed", peer);
                // The bootstrapper/state_sync handle retries via timeout mechanism
            }

            _ => {}
        }
    }

    /// Parses raw bytes into a StateSummary.
    fn parse_state_summary(&self, data: &[u8]) -> Option<StateSummary> {
        // Simple parsing - in a real implementation this would deserialize properly
        if data.len() < 96 {
            return None;
        }

        // Extract fields from the summary bytes
        let id = Id::from_slice(&data[0..32]).ok()?;
        let height = u64::from_be_bytes(data[32..40].try_into().ok()?);
        let block_id = Id::from_slice(&data[40..72]).ok()?;
        let state_root = Id::from_slice(&data[72..104]).ok()?;
        let chunk_count = u64::from_be_bytes(data[104..112].try_into().ok()?);

        Some(StateSummary {
            id,
            height,
            block_id,
            state_root,
            chunk_count,
            bytes: data.to_vec(),
        })
    }

    /// Parses raw bytes into a FetchedBlock.
    fn parse_block(&self, data: &[u8]) -> Option<FetchedBlock> {
        // Simple parsing - in a real implementation this would deserialize properly
        if data.len() < 72 {
            return None;
        }

        let id = Id::from_slice(&data[0..32]).ok()?;
        let parent_id = Id::from_slice(&data[32..64]).ok()?;
        let height = u64::from_be_bytes(data[64..72].try_into().ok()?);

        Some(FetchedBlock::new(id, parent_id, height, data.to_vec()))
    }

    /// Runs the sync engine main loop.
    pub async fn run(&self) -> Result<()> {
        // Take ownership of the response receiver
        let mut response_rx = self
            .response_rx
            .write()
            .take()
            .ok_or(ConsensusError::AlreadyRunning)?;

        let mut check_interval = tokio::time::interval(Duration::from_millis(100));

        loop {
            tokio::select! {
                Some(response) = response_rx.recv() => {
                    self.handle_response(response);
                }

                _ = check_interval.tick() => {
                    // Check for timed out requests
                    let timed_out_blocks = self.bootstrapper.check_timeouts();
                    for block_id in timed_out_blocks {
                        debug!("Block request {} timed out", block_id);
                    }

                    let timed_out_chunks = self.state_sync.check_timeouts();
                    for chunk_idx in timed_out_chunks {
                        debug!("Chunk request {} timed out", chunk_idx);
                    }

                    // Request more blocks/chunks if needed
                    if self.bootstrapper.is_bootstrapping() {
                        let _ = self.request_blocks().await;
                    }

                    if self.state_sync.is_syncing() {
                        let _ = self.request_chunks().await;
                    }

                    // Check if complete
                    if self.bootstrapper.is_complete() {
                        self.bootstrapper.complete();
                        info!("Bootstrap complete!");
                    }

                    if self.state_sync.phase() == crate::sync::SyncPhase::VerifyingState {
                        if self.state_sync.verify_state() {
                            info!("State sync verification complete!");
                        }
                    }

                    if self.state_sync.phase() == crate::sync::SyncPhase::Complete
                        && !self.bootstrapper.is_bootstrapping()
                    {
                        info!("Sync complete!");
                        break;
                    }
                }
            }
        }

        Ok(())
    }

    /// Returns the current sync progress (0.0 - 100.0).
    pub fn progress(&self) -> f64 {
        if self.state_sync.is_syncing() {
            self.state_sync.progress() * 0.5 // State sync is first half
        } else if self.bootstrapper.is_bootstrapping() {
            50.0 + self.bootstrapper.progress() * 0.5 // Bootstrap is second half
        } else {
            100.0
        }
    }

    /// Returns true if sync is in progress.
    pub fn is_syncing(&self) -> bool {
        self.state_sync.is_syncing() || self.bootstrapper.is_bootstrapping()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bootstrapper::BootstrapConfig;
    use crate::sync::StateSyncConfig;
    use std::sync::Mutex;

    struct MockNetwork {
        peers: Vec<NodeId>,
        requests: Mutex<Vec<String>>,
    }

    impl MockNetwork {
        fn new(peer_count: usize) -> Self {
            let peers = (0..peer_count)
                .map(|i| NodeId::from_bytes([i as u8; 20]))
                .collect();
            Self {
                peers,
                requests: Mutex::new(Vec::new()),
            }
        }
    }

    #[async_trait]
    impl SyncNetwork for MockNetwork {
        async fn get_state_summary_frontier(
            &self,
            peer: NodeId,
            _chain_id: Id,
            request_id: u32,
        ) -> Result<()> {
            self.requests.lock().unwrap().push(format!(
                "GetStateSummaryFrontier({}, {})",
                peer, request_id
            ));
            Ok(())
        }

        async fn get_accepted_state_summary(
            &self,
            peer: NodeId,
            _chain_id: Id,
            request_id: u32,
            heights: Vec<u64>,
        ) -> Result<()> {
            self.requests.lock().unwrap().push(format!(
                "GetAcceptedStateSummary({}, {}, {:?})",
                peer, request_id, heights
            ));
            Ok(())
        }

        async fn get_accepted_frontier(
            &self,
            peer: NodeId,
            _chain_id: Id,
            request_id: u32,
        ) -> Result<()> {
            self.requests.lock().unwrap().push(format!(
                "GetAcceptedFrontier({}, {})",
                peer, request_id
            ));
            Ok(())
        }

        async fn get_accepted(
            &self,
            peer: NodeId,
            _chain_id: Id,
            request_id: u32,
            container_ids: Vec<Id>,
        ) -> Result<()> {
            self.requests.lock().unwrap().push(format!(
                "GetAccepted({}, {}, {} ids)",
                peer,
                request_id,
                container_ids.len()
            ));
            Ok(())
        }

        async fn get_ancestors(
            &self,
            peer: NodeId,
            _chain_id: Id,
            request_id: u32,
            container_id: Id,
        ) -> Result<()> {
            self.requests.lock().unwrap().push(format!(
                "GetAncestors({}, {}, {})",
                peer, request_id, container_id
            ));
            Ok(())
        }

        fn connected_peers(&self) -> Vec<NodeId> {
            self.peers.clone()
        }
    }

    #[tokio::test]
    async fn test_start_state_sync() {
        let network = Arc::new(MockNetwork::new(5));
        let chain_id = Id::from_bytes([1; 32]);
        let state_sync = Arc::new(StateSync::new(StateSyncConfig {
            summary_sample_size: 3,
            ..Default::default()
        }));
        let bootstrapper = Arc::new(Bootstrapper::with_defaults());

        let engine = SyncEngine::new(
            network.clone(),
            chain_id,
            state_sync,
            bootstrapper,
            SyncClientConfig::default(),
        );

        engine.start_state_sync().await.unwrap();

        let requests = network.requests.lock().unwrap();
        assert!(!requests.is_empty());
        assert!(requests.iter().any(|r| r.starts_with("GetStateSummaryFrontier")));
    }

    #[tokio::test]
    async fn test_start_bootstrap() {
        let network = Arc::new(MockNetwork::new(5));
        let chain_id = Id::from_bytes([1; 32]);
        let state_sync = Arc::new(StateSync::with_defaults());
        let bootstrapper = Arc::new(Bootstrapper::new(BootstrapConfig {
            frontier_sample_size: 3,
            ..Default::default()
        }));

        let engine = SyncEngine::new(
            network.clone(),
            chain_id,
            state_sync,
            bootstrapper,
            SyncClientConfig::default(),
        );

        let last_accepted = Id::from_bytes([0; 32]);
        engine.start_bootstrap(last_accepted).await.unwrap();

        let requests = network.requests.lock().unwrap();
        assert!(!requests.is_empty());
        assert!(requests.iter().any(|r| r.starts_with("GetAcceptedFrontier")));
    }

    #[tokio::test]
    async fn test_handle_frontier_response() {
        let network = Arc::new(MockNetwork::new(5));
        let chain_id = Id::from_bytes([1; 32]);
        let state_sync = Arc::new(StateSync::with_defaults());
        let bootstrapper = Arc::new(Bootstrapper::new(BootstrapConfig {
            frontier_alpha: 1,
            ..Default::default()
        }));

        let engine = SyncEngine::new(
            network.clone(),
            chain_id,
            state_sync,
            bootstrapper.clone(),
            SyncClientConfig::default(),
        );

        let last_accepted = Id::from_bytes([0; 32]);
        engine.start_bootstrap(last_accepted).await.unwrap();

        // Simulate frontier response
        let peer = network.peers[0];
        let frontier_id = Id::from_bytes([10; 32]);
        engine.handle_response(SyncResponse::AcceptedFrontier {
            peer,
            request_id: 1,
            container_id: frontier_id,
        });

        // Should have accepted the frontier
        assert!(!bootstrapper.target_frontier().is_empty());
    }

    #[tokio::test]
    async fn test_progress() {
        let network = Arc::new(MockNetwork::new(5));
        let chain_id = Id::from_bytes([1; 32]);
        let state_sync = Arc::new(StateSync::with_defaults());
        let bootstrapper = Arc::new(Bootstrapper::with_defaults());

        let engine = SyncEngine::new(
            network,
            chain_id,
            state_sync,
            bootstrapper,
            SyncClientConfig::default(),
        );

        // Initially not syncing, so 100%
        assert_eq!(engine.progress(), 100.0);
        assert!(!engine.is_syncing());
    }

    #[test]
    fn test_request_id_generation() {
        let id1 = next_request_id();
        let id2 = next_request_id();
        assert_ne!(id1, id2);
        assert!(id2 > id1);
    }
}
