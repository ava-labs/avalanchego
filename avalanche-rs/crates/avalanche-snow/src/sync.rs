//! State synchronization implementation.
//!
//! State sync allows a node to quickly catch up with the network by downloading
//! a recent state summary and then syncing the remaining blocks. This is much
//! faster than replaying all blocks from genesis.
//!
//! The protocol:
//! 1. Request state summary frontier from peers
//! 2. Vote on accepted state summary
//! 3. Download state chunks in parallel
//! 4. Verify state against summary
//! 5. Resume normal block sync

use std::collections::{HashMap, HashSet};
use std::time::{Duration, Instant};

use parking_lot::RwLock;

use avalanche_ids::{Id, NodeId};

/// State sync configuration.
#[derive(Debug, Clone)]
pub struct StateSyncConfig {
    /// Number of peers to request summaries from.
    pub summary_sample_size: usize,
    /// Minimum peers that must agree on a summary.
    pub summary_alpha: usize,
    /// Chunk size for state downloads.
    pub chunk_size: usize,
    /// Maximum concurrent chunk downloads.
    pub max_concurrent_chunks: usize,
    /// Request timeout.
    pub request_timeout: Duration,
    /// Maximum retries per request.
    pub max_retries: usize,
}

impl Default for StateSyncConfig {
    fn default() -> Self {
        Self {
            summary_sample_size: 20,
            summary_alpha: 14,
            chunk_size: 64 * 1024, // 64 KB
            max_concurrent_chunks: 16,
            request_timeout: Duration::from_secs(10),
            max_retries: 3,
        }
    }
}

/// A state summary from a peer.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct StateSummary {
    /// Summary ID (hash of the summary).
    pub id: Id,
    /// Height this summary represents.
    pub height: u64,
    /// Block ID at this height.
    pub block_id: Id,
    /// Root hash of the state trie.
    pub state_root: Id,
    /// Total number of chunks.
    pub chunk_count: u64,
    /// Summary bytes (serialized).
    pub bytes: Vec<u8>,
}

/// A chunk of state data.
#[derive(Debug, Clone)]
pub struct StateChunk {
    /// Chunk index.
    pub index: u64,
    /// Chunk data.
    pub data: Vec<u8>,
    /// Proof for this chunk.
    pub proof: Vec<u8>,
}

/// State sync phase.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyncPhase {
    /// Not syncing.
    Idle,
    /// Fetching state summaries from peers.
    FetchingSummaries,
    /// Voting on summaries.
    VotingSummaries,
    /// Downloading state chunks.
    DownloadingState,
    /// Verifying downloaded state.
    VerifyingState,
    /// Syncing remaining blocks.
    SyncingBlocks,
    /// Sync complete.
    Complete,
    /// Sync failed.
    Failed,
}

/// Chunk download status.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ChunkStatus {
    /// Not started.
    Pending,
    /// Download in progress.
    Downloading,
    /// Downloaded and verified.
    Complete,
    /// Download failed.
    Failed,
}

/// Tracks a pending chunk request.
struct ChunkRequest {
    index: u64,
    peer: NodeId,
    sent_at: Instant,
    retries: usize,
}

/// The state synchronizer.
pub struct StateSync {
    /// Configuration.
    config: StateSyncConfig,
    /// Current sync phase.
    phase: RwLock<SyncPhase>,
    /// Collected summaries from peers.
    summaries: RwLock<HashMap<Id, (StateSummary, HashSet<NodeId>)>>,
    /// Accepted summary for syncing.
    accepted_summary: RwLock<Option<StateSummary>>,
    /// Chunk statuses.
    chunk_status: RwLock<HashMap<u64, ChunkStatus>>,
    /// Downloaded chunks.
    chunks: RwLock<HashMap<u64, StateChunk>>,
    /// Pending chunk requests.
    pending_requests: RwLock<HashMap<u64, ChunkRequest>>,
    /// Available peers for requests.
    available_peers: RwLock<Vec<NodeId>>,
    /// Start time for metrics.
    start_time: RwLock<Option<Instant>>,
}

impl StateSync {
    /// Creates a new state synchronizer.
    pub fn new(config: StateSyncConfig) -> Self {
        Self {
            config,
            phase: RwLock::new(SyncPhase::Idle),
            summaries: RwLock::new(HashMap::new()),
            accepted_summary: RwLock::new(None),
            chunk_status: RwLock::new(HashMap::new()),
            chunks: RwLock::new(HashMap::new()),
            pending_requests: RwLock::new(HashMap::new()),
            available_peers: RwLock::new(Vec::new()),
            start_time: RwLock::new(None),
        }
    }

    /// Creates a new state synchronizer with default config.
    pub fn with_defaults() -> Self {
        Self::new(StateSyncConfig::default())
    }

    /// Returns the current sync phase.
    pub fn phase(&self) -> SyncPhase {
        *self.phase.read()
    }

    /// Returns true if sync is in progress.
    pub fn is_syncing(&self) -> bool {
        let phase = *self.phase.read();
        !matches!(phase, SyncPhase::Idle | SyncPhase::Complete | SyncPhase::Failed)
    }

    /// Starts state sync.
    pub fn start(&self, peers: Vec<NodeId>) {
        *self.phase.write() = SyncPhase::FetchingSummaries;
        *self.start_time.write() = Some(Instant::now());
        *self.available_peers.write() = peers;
        self.summaries.write().clear();
    }

    /// Returns peers to request summaries from.
    pub fn peers_to_query_summaries(&self) -> Vec<NodeId> {
        let peers = self.available_peers.read();
        peers
            .iter()
            .take(self.config.summary_sample_size)
            .copied()
            .collect()
    }

    /// Handles a received state summary.
    pub fn on_summary_received(&self, peer: NodeId, summary: StateSummary) {
        if *self.phase.read() != SyncPhase::FetchingSummaries {
            return;
        }

        let mut summaries = self.summaries.write();
        let entry = summaries
            .entry(summary.id)
            .or_insert_with(|| (summary.clone(), HashSet::new()));
        entry.1.insert(peer);

        // Check if we have enough votes for any summary
        let accepted = summaries
            .iter()
            .find(|(_, (_, voters))| voters.len() >= self.config.summary_alpha)
            .map(|(_, (summary, _))| summary.clone());

        drop(summaries);

        if let Some(summary) = accepted {
            self.accept_summary(summary);
        }
    }

    /// Accepts a summary and moves to downloading.
    fn accept_summary(&self, summary: StateSummary) {
        // Initialize chunk tracking
        let mut chunk_status = self.chunk_status.write();
        chunk_status.clear();
        for i in 0..summary.chunk_count {
            chunk_status.insert(i, ChunkStatus::Pending);
        }

        *self.accepted_summary.write() = Some(summary);
        *self.phase.write() = SyncPhase::DownloadingState;
    }

    /// Returns the accepted summary.
    pub fn accepted_summary(&self) -> Option<StateSummary> {
        self.accepted_summary.read().clone()
    }

    /// Returns chunks that need to be downloaded.
    pub fn chunks_to_download(&self) -> Vec<u64> {
        let chunk_status = self.chunk_status.read();
        let pending = self.pending_requests.read();

        let active_downloads = pending.len();
        let max_new = self.config.max_concurrent_chunks.saturating_sub(active_downloads);

        chunk_status
            .iter()
            .filter(|(idx, status)| {
                **status == ChunkStatus::Pending && !pending.contains_key(idx)
            })
            .map(|(idx, _)| *idx)
            .take(max_new)
            .collect()
    }

    /// Selects a peer for a chunk request.
    pub fn select_peer_for_chunk(&self) -> Option<NodeId> {
        let peers = self.available_peers.read();
        if peers.is_empty() {
            return None;
        }
        // Simple round-robin selection
        let idx = rand_usize() % peers.len();
        Some(peers[idx])
    }

    /// Records that a chunk request was sent.
    pub fn on_chunk_requested(&self, index: u64, peer: NodeId) {
        let mut pending = self.pending_requests.write();
        pending.insert(
            index,
            ChunkRequest {
                index,
                peer,
                sent_at: Instant::now(),
                retries: 0,
            },
        );

        let mut status = self.chunk_status.write();
        status.insert(index, ChunkStatus::Downloading);
    }

    /// Handles a received chunk.
    pub fn on_chunk_received(&self, index: u64, chunk: StateChunk) -> bool {
        if *self.phase.read() != SyncPhase::DownloadingState {
            return false;
        }

        // Verify the chunk
        if !self.verify_chunk(&chunk) {
            let mut status = self.chunk_status.write();
            status.insert(index, ChunkStatus::Failed);
            return false;
        }

        // Store the chunk
        self.chunks.write().insert(index, chunk);
        self.pending_requests.write().remove(&index);

        let mut status = self.chunk_status.write();
        status.insert(index, ChunkStatus::Complete);

        // Check if all chunks are downloaded
        let all_complete = status.values().all(|s| *s == ChunkStatus::Complete);
        drop(status);

        if all_complete {
            *self.phase.write() = SyncPhase::VerifyingState;
        }

        true
    }

    /// Handles a failed chunk request.
    pub fn on_chunk_failed(&self, index: u64) {
        let mut pending = self.pending_requests.write();
        if let Some(mut req) = pending.remove(&index) {
            req.retries += 1;
            if req.retries < self.config.max_retries {
                // Will be retried
                let mut status = self.chunk_status.write();
                status.insert(index, ChunkStatus::Pending);
            } else {
                // Max retries reached
                let mut status = self.chunk_status.write();
                status.insert(index, ChunkStatus::Failed);
            }
        }
    }

    /// Checks for timed out requests.
    pub fn check_timeouts(&self) -> Vec<u64> {
        let mut pending = self.pending_requests.write();
        let timeout = self.config.request_timeout;

        let timed_out: Vec<u64> = pending
            .iter()
            .filter(|(_, req)| req.sent_at.elapsed() > timeout)
            .map(|(idx, _)| *idx)
            .collect();

        for idx in &timed_out {
            if let Some(mut req) = pending.remove(idx) {
                req.retries += 1;
                if req.retries < self.config.max_retries {
                    let mut status = self.chunk_status.write();
                    status.insert(*idx, ChunkStatus::Pending);
                } else {
                    let mut status = self.chunk_status.write();
                    status.insert(*idx, ChunkStatus::Failed);
                }
            }
        }

        timed_out
    }

    /// Verifies a chunk against the summary.
    fn verify_chunk(&self, chunk: &StateChunk) -> bool {
        // In a real implementation, this would verify the chunk's Merkle proof
        // against the state root in the accepted summary.
        // For now, just check it has data.
        !chunk.data.is_empty()
    }

    /// Verifies the complete state.
    pub fn verify_state(&self) -> bool {
        if *self.phase.read() != SyncPhase::VerifyingState {
            return false;
        }

        let summary = match self.accepted_summary.read().clone() {
            Some(s) => s,
            None => return false,
        };

        let chunks = self.chunks.read();

        // Check we have all chunks
        if chunks.len() != summary.chunk_count as usize {
            *self.phase.write() = SyncPhase::Failed;
            return false;
        }

        // In a real implementation, reconstruct the state trie and verify
        // the root hash matches the summary's state_root.
        // For now, just verify we have all chunks.
        for i in 0..summary.chunk_count {
            if !chunks.contains_key(&i) {
                *self.phase.write() = SyncPhase::Failed;
                return false;
            }
        }

        *self.phase.write() = SyncPhase::SyncingBlocks;
        true
    }

    /// Returns the downloaded chunks for state application.
    pub fn take_chunks(&self) -> HashMap<u64, StateChunk> {
        std::mem::take(&mut *self.chunks.write())
    }

    /// Marks block sync as complete.
    pub fn complete(&self) {
        *self.phase.write() = SyncPhase::Complete;
    }

    /// Marks sync as failed.
    pub fn fail(&self) {
        *self.phase.write() = SyncPhase::Failed;
    }

    /// Resets the synchronizer.
    pub fn reset(&self) {
        *self.phase.write() = SyncPhase::Idle;
        self.summaries.write().clear();
        *self.accepted_summary.write() = None;
        self.chunk_status.write().clear();
        self.chunks.write().clear();
        self.pending_requests.write().clear();
        *self.start_time.write() = None;
    }

    /// Returns sync progress as percentage.
    pub fn progress(&self) -> f64 {
        let status = self.chunk_status.read();
        if status.is_empty() {
            return 0.0;
        }

        let complete = status
            .values()
            .filter(|s| **s == ChunkStatus::Complete)
            .count();

        (complete as f64 / status.len() as f64) * 100.0
    }

    /// Returns sync duration.
    pub fn duration(&self) -> Option<Duration> {
        self.start_time.read().map(|t| t.elapsed())
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

impl Default for StateSync {
    fn default() -> Self {
        Self::with_defaults()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_summary(height: u64, chunks: u64) -> StateSummary {
        StateSummary {
            id: Id::from_bytes([height as u8; 32]),
            height,
            block_id: Id::from_bytes([1; 32]),
            state_root: Id::from_bytes([2; 32]),
            chunk_count: chunks,
            bytes: vec![0; 100],
        }
    }

    fn make_peers(count: usize) -> Vec<NodeId> {
        (0..count)
            .map(|i| NodeId::from_bytes([i as u8; 20]))
            .collect()
    }

    #[test]
    fn test_start_sync() {
        let sync = StateSync::with_defaults();
        let peers = make_peers(20);

        sync.start(peers);
        assert_eq!(sync.phase(), SyncPhase::FetchingSummaries);
        assert!(sync.is_syncing());
    }

    #[test]
    fn test_summary_voting() {
        let config = StateSyncConfig {
            summary_alpha: 3, // Require 3 votes
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 10);

        sync.start(peers.clone());

        // First two votes shouldn't be enough
        sync.on_summary_received(peers[0], summary.clone());
        sync.on_summary_received(peers[1], summary.clone());
        assert_eq!(sync.phase(), SyncPhase::FetchingSummaries);

        // Third vote should accept
        sync.on_summary_received(peers[2], summary.clone());
        assert_eq!(sync.phase(), SyncPhase::DownloadingState);
        assert!(sync.accepted_summary().is_some());
    }

    #[test]
    fn test_chunk_download() {
        let config = StateSyncConfig {
            summary_alpha: 1,
            max_concurrent_chunks: 4,
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 10);

        sync.start(peers.clone());
        sync.on_summary_received(peers[0], summary);

        // Should have chunks to download
        let to_download = sync.chunks_to_download();
        assert!(!to_download.is_empty());
        assert!(to_download.len() <= 4); // max_concurrent_chunks
    }

    #[test]
    fn test_chunk_received() {
        let config = StateSyncConfig {
            summary_alpha: 1,
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 2); // Only 2 chunks

        sync.start(peers.clone());
        sync.on_summary_received(peers[0], summary);

        // Receive first chunk
        let chunk0 = StateChunk {
            index: 0,
            data: vec![1, 2, 3],
            proof: vec![],
        };
        sync.on_chunk_requested(0, peers[0]);
        sync.on_chunk_received(0, chunk0);
        assert_eq!(sync.phase(), SyncPhase::DownloadingState);

        // Receive second chunk
        let chunk1 = StateChunk {
            index: 1,
            data: vec![4, 5, 6],
            proof: vec![],
        };
        sync.on_chunk_requested(1, peers[1]);
        sync.on_chunk_received(1, chunk1);

        // Should move to verifying
        assert_eq!(sync.phase(), SyncPhase::VerifyingState);
    }

    #[test]
    fn test_verify_state() {
        let config = StateSyncConfig {
            summary_alpha: 1,
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 2);

        sync.start(peers.clone());
        sync.on_summary_received(peers[0], summary);

        // Download all chunks
        for i in 0..2 {
            sync.on_chunk_requested(i, peers[0]);
            sync.on_chunk_received(
                i,
                StateChunk {
                    index: i,
                    data: vec![i as u8],
                    proof: vec![],
                },
            );
        }

        // Verify
        assert!(sync.verify_state());
        assert_eq!(sync.phase(), SyncPhase::SyncingBlocks);
    }

    #[test]
    fn test_progress() {
        let config = StateSyncConfig {
            summary_alpha: 1,
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 4);

        sync.start(peers.clone());
        sync.on_summary_received(peers[0], summary);

        assert_eq!(sync.progress(), 0.0);

        // Complete 2 of 4 chunks
        for i in 0..2 {
            sync.on_chunk_requested(i, peers[0]);
            sync.on_chunk_received(
                i,
                StateChunk {
                    index: i,
                    data: vec![i as u8],
                    proof: vec![],
                },
            );
        }

        assert_eq!(sync.progress(), 50.0);
    }

    #[test]
    fn test_reset() {
        let sync = StateSync::with_defaults();
        let peers = make_peers(5);

        sync.start(peers);
        assert!(sync.is_syncing());

        sync.reset();
        assert_eq!(sync.phase(), SyncPhase::Idle);
        assert!(!sync.is_syncing());
    }

    #[test]
    fn test_chunk_timeout() {
        let config = StateSyncConfig {
            summary_alpha: 1,
            request_timeout: Duration::from_millis(1), // Very short for testing
            max_retries: 2,
            ..Default::default()
        };
        let sync = StateSync::new(config);
        let peers = make_peers(5);
        let summary = make_summary(100, 2);

        sync.start(peers.clone());
        sync.on_summary_received(peers[0], summary);

        sync.on_chunk_requested(0, peers[0]);

        // Wait for timeout
        std::thread::sleep(Duration::from_millis(10));

        let timed_out = sync.check_timeouts();
        assert!(!timed_out.is_empty());
    }
}
