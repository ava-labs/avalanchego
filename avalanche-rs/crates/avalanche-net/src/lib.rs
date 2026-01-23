//! Avalanche P2P networking implementation.
//!
//! This crate provides the networking layer for Avalanche nodes, including:
//! - Peer-to-peer connections with TLS
//! - Message framing and encoding
//! - Peer discovery and management
//! - Network health monitoring

mod codec;
mod config;
mod connection;
pub mod discovery;
mod handshake;
mod network;
mod peer;
mod peer_manager;
mod tls;
mod version;

pub use codec::{MessageCodec, MessageFrame};
pub use config::{NetworkConfig, PeerConfig, TimeoutConfig};
pub use connection::Connection;
pub use discovery::{
    DiscoveredPeer, DiscoveryConfig, DiscoverySource, DiscoveryTask, PeerDiscovery,
};
pub use handshake::{
    create_claimed_ip_port, HandshakeProtocol, HandshakeResult, CLIENT_NAME, CLIENT_VERSION,
    MIN_COMPATIBLE_VERSION,
};
pub use network::NetworkImpl;
pub use peer::{PeerInfo, PeerState, SignedIp, UnsignedIp};
pub use peer_manager::{PeerEvent, PeerManager};
pub use tls::{node_id_from_cert, TlsConfig};
pub use version::{Version, AVALANCHE_RS_CLIENT_NAME};

use std::collections::HashSet;
use std::net::SocketAddr;

use async_trait::async_trait;
use avalanche_ids::NodeId;
use avalanche_proto::Message;
use thiserror::Error;

/// Errors that can occur in the network layer.
#[derive(Debug, Error)]
pub enum NetworkError {
    #[error("connection failed: {0}")]
    ConnectionFailed(String),

    #[error("peer not found: {0}")]
    PeerNotFound(NodeId),

    #[error("handshake failed: {0}")]
    HandshakeFailed(String),

    #[error("send failed: {0}")]
    SendFailed(String),

    #[error("network closed")]
    Closed,

    #[error("timeout")]
    Timeout,

    #[error("invalid message: {0}")]
    InvalidMessage(String),

    #[error("TLS error: {0}")]
    Tls(String),

    #[error("I/O error: {0}")]
    Io(#[from] std::io::Error),

    #[error("protocol error: {0}")]
    Protocol(#[from] avalanche_proto::MessageError),
}

/// Result type for network operations.
pub type Result<T> = std::result::Result<T, NetworkError>;

/// Configuration for sending a message.
#[derive(Debug, Clone, Default)]
pub struct SendConfig {
    /// Whether this message should bypass throttling.
    pub bypass_throttling: bool,
}

/// The main network interface.
///
/// This trait defines the core operations for P2P networking.
#[async_trait]
pub trait Network: Send + Sync {
    /// Sends a message to a specific peer.
    ///
    /// Returns `true` if the message was sent successfully.
    async fn send(&self, peer_id: NodeId, msg: Message, config: SendConfig) -> Result<bool>;

    /// Sends a message to multiple peers.
    ///
    /// Returns the set of peers that received the message.
    async fn send_to(
        &self,
        peer_ids: &[NodeId],
        msg: Message,
        config: SendConfig,
    ) -> Result<HashSet<NodeId>>;

    /// Gossips a message to a random subset of connected peers.
    ///
    /// Returns the set of peers that received the message.
    async fn gossip(&self, msg: Message, config: SendConfig) -> Result<HashSet<NodeId>>;

    /// Returns the IDs of all connected peers.
    fn connected_peers(&self) -> Vec<NodeId>;

    /// Returns information about a specific peer.
    fn peer_info(&self, peer_id: NodeId) -> Option<PeerInfo>;

    /// Returns this node's ID.
    fn node_id(&self) -> NodeId;

    /// Returns this node's IP address.
    fn ip(&self) -> SocketAddr;

    /// Manually tracks a peer for connection.
    fn track(&self, peer_id: NodeId, addr: SocketAddr);

    /// Starts the network, accepting connections.
    async fn start(&self) -> Result<()>;

    /// Initiates graceful shutdown.
    async fn shutdown(&self) -> Result<()>;

    /// Returns true if the network is healthy.
    fn is_healthy(&self) -> bool;
}

/// Handler for inbound messages.
///
/// Implement this trait to process messages received from peers.
#[async_trait]
pub trait MessageHandler: Send + Sync {
    /// Handles an inbound message from a peer.
    async fn handle_message(&self, peer_id: NodeId, msg: Message) -> Result<Option<Message>>;

    /// Called when a peer connects and completes handshake.
    async fn on_connected(&self, peer_id: NodeId, info: PeerInfo);

    /// Called when a peer disconnects.
    async fn on_disconnected(&self, peer_id: NodeId);
}

/// A simple no-op message handler for testing.
pub struct NoOpHandler;

#[async_trait]
impl MessageHandler for NoOpHandler {
    async fn handle_message(&self, _peer_id: NodeId, _msg: Message) -> Result<Option<Message>> {
        Ok(None)
    }

    async fn on_connected(&self, _peer_id: NodeId, _info: PeerInfo) {}

    async fn on_disconnected(&self, _peer_id: NodeId) {}
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_send_config_default() {
        let config = SendConfig::default();
        assert!(!config.bypass_throttling);
    }
}
