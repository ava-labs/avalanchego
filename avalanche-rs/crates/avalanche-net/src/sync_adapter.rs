//! Adapter connecting the sync engine to the network layer.
//!
//! This module bridges the gap between the SyncNetwork trait in avalanche-snow
//! and the Network trait in avalanche-net.

use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use tracing::debug;

use avalanche_ids::{Id, NodeId};
use avalanche_proto::{
    Accepted, AcceptedFrontier, Ancestors, EngineTypeProto, Get, GetAccepted,
    GetAcceptedFrontier, GetAcceptedStateSummary, GetAncestors, GetStateSummaryFrontier, Message,
    StateSummaryFrontier,
};

use crate::{Network, NetworkError, Result, SendConfig};

/// Default deadline offset in nanoseconds (30 seconds).
const DEFAULT_DEADLINE_NS: u64 = 30_000_000_000;

/// Adapter that implements avalanche_snow::SyncNetwork using the Network trait.
pub struct SyncNetworkAdapter<N: Network> {
    /// The underlying network.
    network: Arc<N>,
}

impl<N: Network> SyncNetworkAdapter<N> {
    /// Creates a new sync network adapter.
    pub fn new(network: Arc<N>) -> Self {
        Self { network }
    }

    /// Generates a deadline from now.
    fn deadline() -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos() as u64
            + DEFAULT_DEADLINE_NS
    }
}

/// Error type for sync operations.
#[derive(Debug, thiserror::Error)]
pub enum SyncError {
    #[error("network error: {0}")]
    Network(String),
}

impl From<NetworkError> for SyncError {
    fn from(e: NetworkError) -> Self {
        SyncError::Network(e.to_string())
    }
}

/// This trait is defined here to match the SyncNetwork trait in avalanche-snow.
/// We re-implement it here to avoid circular dependencies.
#[async_trait]
pub trait SyncNetworkTrait: Send + Sync {
    /// Sends a GetStateSummaryFrontier request.
    async fn get_state_summary_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> std::result::Result<(), SyncError>;

    /// Sends a GetAcceptedStateSummary request.
    async fn get_accepted_state_summary(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        heights: Vec<u64>,
    ) -> std::result::Result<(), SyncError>;

    /// Sends a GetAcceptedFrontier request.
    async fn get_accepted_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> std::result::Result<(), SyncError>;

    /// Sends a GetAccepted request.
    async fn get_accepted(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_ids: Vec<Id>,
    ) -> std::result::Result<(), SyncError>;

    /// Sends a GetAncestors request.
    async fn get_ancestors(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    ) -> std::result::Result<(), SyncError>;

    /// Sends a Get request.
    async fn get_block(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    ) -> std::result::Result<(), SyncError>;

    /// Returns the list of connected peers.
    fn connected_peers(&self) -> Vec<NodeId>;
}

#[async_trait]
impl<N: Network> SyncNetworkTrait for SyncNetworkAdapter<N> {
    async fn get_state_summary_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending GetStateSummaryFrontier to {} (request_id={})",
            peer, request_id
        );

        let msg = Message::GetStateSummaryFrontier(GetStateSummaryFrontier {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    async fn get_accepted_state_summary(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        heights: Vec<u64>,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending GetAcceptedStateSummary to {} (request_id={}, heights={:?})",
            peer, request_id, heights
        );

        let msg = Message::GetAcceptedStateSummary(GetAcceptedStateSummary {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
            heights,
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    async fn get_accepted_frontier(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending GetAcceptedFrontier to {} (request_id={})",
            peer, request_id
        );

        let msg = Message::GetAcceptedFrontier(GetAcceptedFrontier {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    async fn get_accepted(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_ids: Vec<Id>,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending GetAccepted to {} (request_id={}, {} containers)",
            peer,
            request_id,
            container_ids.len()
        );

        let msg = Message::GetAccepted(GetAccepted {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
            container_ids: container_ids.iter().map(|id| id.as_bytes().to_vec()).collect(),
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    async fn get_ancestors(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending GetAncestors to {} (request_id={}, container={})",
            peer, request_id, container_id
        );

        let msg = Message::GetAncestors(GetAncestors {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
            container_id: container_id.as_bytes().to_vec(),
            engine_type: EngineTypeProto::Snowman as i32,
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    async fn get_block(
        &self,
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    ) -> std::result::Result<(), SyncError> {
        debug!(
            "Sending Get to {} (request_id={}, container={})",
            peer, request_id, container_id
        );

        let msg = Message::Get(Get {
            chain_id: chain_id.as_bytes().to_vec(),
            request_id,
            deadline: Self::deadline(),
            container_id: container_id.as_bytes().to_vec(),
        });

        self.network
            .send(peer, msg, SendConfig::default())
            .await
            .map_err(SyncError::from)?;

        Ok(())
    }

    fn connected_peers(&self) -> Vec<NodeId> {
        self.network.connected_peers()
    }
}

/// Message handler that routes sync-related responses to the sync engine.
pub struct SyncMessageRouter {
    /// Channel to send responses to the sync engine.
    response_tx: tokio::sync::mpsc::Sender<SyncResponseMessage>,
}

/// Sync response message.
#[derive(Debug, Clone)]
pub enum SyncResponseMessage {
    /// State summary frontier response.
    StateSummaryFrontier {
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        summary: Vec<u8>,
    },
    /// Accepted state summary response.
    AcceptedStateSummary {
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        summary_ids: Vec<Id>,
    },
    /// Accepted frontier response.
    AcceptedFrontier {
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_id: Id,
    },
    /// Accepted containers response.
    Accepted {
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        container_ids: Vec<Id>,
    },
    /// Ancestors response.
    Ancestors {
        peer: NodeId,
        chain_id: Id,
        request_id: u32,
        containers: Vec<Vec<u8>>,
    },
}

impl SyncMessageRouter {
    /// Creates a new sync message router.
    pub fn new(response_tx: tokio::sync::mpsc::Sender<SyncResponseMessage>) -> Self {
        Self { response_tx }
    }

    /// Routes an incoming message to the sync engine if it's a sync-related response.
    pub async fn route(&self, peer: NodeId, msg: Message) -> Option<()> {
        let response = match msg {
            Message::StateSummaryFrontier(m) => {
                let chain_id = Id::from_slice(&m.chain_id).ok()?;
                SyncResponseMessage::StateSummaryFrontier {
                    peer,
                    chain_id,
                    request_id: m.request_id,
                    summary: m.summary,
                }
            }

            Message::AcceptedFrontier(m) => {
                let chain_id = Id::from_slice(&m.chain_id).ok()?;
                let container_id = Id::from_slice(&m.container_id).ok()?;
                SyncResponseMessage::AcceptedFrontier {
                    peer,
                    chain_id,
                    request_id: m.request_id,
                    container_id,
                }
            }

            Message::Accepted(m) => {
                let chain_id = Id::from_slice(&m.chain_id).ok()?;
                let container_ids: Vec<Id> = m
                    .container_ids
                    .iter()
                    .filter_map(|id| Id::from_slice(id).ok())
                    .collect();
                SyncResponseMessage::Accepted {
                    peer,
                    chain_id,
                    request_id: m.request_id,
                    container_ids,
                }
            }

            Message::Ancestors(m) => {
                let chain_id = Id::from_slice(&m.chain_id).ok()?;
                SyncResponseMessage::Ancestors {
                    peer,
                    chain_id,
                    request_id: m.request_id,
                    containers: m.containers,
                }
            }

            // Not a sync message
            _ => return None,
        };

        self.response_tx.send(response).await.ok()?;
        Some(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::NoOpHandler;
    use std::collections::HashSet;

    struct MockNetwork {
        peers: Vec<NodeId>,
    }

    #[async_trait]
    impl Network for MockNetwork {
        async fn send(&self, _peer: NodeId, _msg: Message, _config: SendConfig) -> Result<bool> {
            Ok(true)
        }

        async fn send_to(
            &self,
            _peer_ids: &[NodeId],
            _msg: Message,
            _config: SendConfig,
        ) -> Result<HashSet<NodeId>> {
            Ok(HashSet::new())
        }

        async fn gossip(&self, _msg: Message, _config: SendConfig) -> Result<HashSet<NodeId>> {
            Ok(HashSet::new())
        }

        fn connected_peers(&self) -> Vec<NodeId> {
            self.peers.clone()
        }

        fn peer_info(&self, _peer_id: NodeId) -> Option<crate::PeerInfo> {
            None
        }

        fn node_id(&self) -> NodeId {
            NodeId::from_bytes([0; 20])
        }

        fn ip(&self) -> std::net::SocketAddr {
            "127.0.0.1:9651".parse().unwrap()
        }

        fn track(&self, _peer_id: NodeId, _addr: std::net::SocketAddr) {}

        async fn start(&self) -> Result<()> {
            Ok(())
        }

        async fn shutdown(&self) -> Result<()> {
            Ok(())
        }

        fn is_healthy(&self) -> bool {
            true
        }
    }

    #[tokio::test]
    async fn test_sync_adapter_get_frontier() {
        let peers = vec![NodeId::from_bytes([1; 20]), NodeId::from_bytes([2; 20])];
        let network = Arc::new(MockNetwork {
            peers: peers.clone(),
        });
        let adapter = SyncNetworkAdapter::new(network);

        let result = adapter
            .get_accepted_frontier(peers[0], Id::from_bytes([0; 32]), 1)
            .await;

        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_sync_adapter_get_ancestors() {
        let peers = vec![NodeId::from_bytes([1; 20])];
        let network = Arc::new(MockNetwork {
            peers: peers.clone(),
        });
        let adapter = SyncNetworkAdapter::new(network);

        let result = adapter
            .get_ancestors(
                peers[0],
                Id::from_bytes([0; 32]),
                1,
                Id::from_bytes([1; 32]),
            )
            .await;

        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_message_router() {
        let (tx, mut rx) = tokio::sync::mpsc::channel(10);
        let router = SyncMessageRouter::new(tx);

        let peer = NodeId::from_bytes([1; 20]);
        let msg = Message::AcceptedFrontier(AcceptedFrontier {
            chain_id: vec![0; 32],
            request_id: 42,
            container_id: vec![1; 32],
        });

        router.route(peer, msg).await;

        let response = rx.recv().await.unwrap();
        match response {
            SyncResponseMessage::AcceptedFrontier {
                request_id,
                ..
            } => {
                assert_eq!(request_id, 42);
            }
            _ => panic!("Wrong response type"),
        }
    }

    #[test]
    fn test_connected_peers() {
        let peers = vec![NodeId::from_bytes([1; 20]), NodeId::from_bytes([2; 20])];
        let network = Arc::new(MockNetwork {
            peers: peers.clone(),
        });
        let adapter = SyncNetworkAdapter::new(network);

        assert_eq!(adapter.connected_peers(), peers);
    }
}
