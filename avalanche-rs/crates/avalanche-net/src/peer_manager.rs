//! Peer connection management.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use bytes::Bytes;
use dashmap::DashMap;
use tokio::net::TcpStream;
use tokio::sync::{broadcast, RwLock};
use tokio::time;
use tracing::{debug, info, warn};

use avalanche_ids::NodeId;

use crate::connection::Connection;
use crate::peer::{PeerInfo, PeerState};
use crate::tls::TlsConfig;
use crate::{NetworkConfig, NetworkError, Result};

/// Events from the peer manager.
#[derive(Debug, Clone)]
pub enum PeerEvent {
    /// A peer connected.
    Connected(NodeId, PeerInfo),
    /// A peer disconnected.
    Disconnected(NodeId),
    /// A message was received from a peer.
    Message(NodeId, Bytes),
}

/// A managed peer connection.
struct ManagedPeer {
    /// Peer information.
    info: PeerInfo,
    /// Sender for outbound messages (cloneable).
    outbound_tx: Option<tokio::sync::mpsc::Sender<Bytes>>,
}

/// Manages connections to multiple peers.
pub struct PeerManager {
    /// TLS configuration.
    tls_config: Arc<TlsConfig>,
    /// Network configuration.
    config: NetworkConfig,
    /// Active connections (peer info only, connection handles stored separately).
    peers: DashMap<NodeId, Arc<RwLock<ManagedPeer>>>,
    /// Peers we want to connect to.
    tracked_peers: DashMap<NodeId, SocketAddr>,
    /// Event sender.
    event_tx: broadcast::Sender<PeerEvent>,
    /// Our node ID.
    node_id: NodeId,
    /// Maximum number of peers.
    max_peers: usize,
    /// Shutdown signal.
    shutdown_tx: broadcast::Sender<()>,
}

impl PeerManager {
    /// Creates a new peer manager.
    pub fn new(
        tls_config: Arc<TlsConfig>,
        config: NetworkConfig,
        max_peers: usize,
    ) -> (Self, broadcast::Receiver<PeerEvent>) {
        let (event_tx, event_rx) = broadcast::channel(1024);
        let (shutdown_tx, _) = broadcast::channel(1);
        let node_id = tls_config.node_id;

        (
            Self {
                tls_config,
                config,
                peers: DashMap::new(),
                tracked_peers: DashMap::new(),
                event_tx,
                node_id,
                max_peers,
                shutdown_tx,
            },
            event_rx,
        )
    }

    /// Returns our node ID.
    pub fn node_id(&self) -> NodeId {
        self.node_id
    }

    /// Tracks a peer for connection.
    pub fn track(&self, node_id: NodeId, addr: SocketAddr) {
        if node_id == self.node_id {
            debug!("Ignoring attempt to track self");
            return;
        }

        if !self.peers.contains_key(&node_id) {
            self.tracked_peers.insert(node_id, addr);
            info!("Tracking peer {} at {}", node_id, addr);
        }
    }

    /// Untracks a peer.
    pub fn untrack(&self, node_id: &NodeId) {
        self.tracked_peers.remove(node_id);
    }

    /// Returns all connected peers.
    pub fn connected_peers(&self) -> Vec<NodeId> {
        self.peers.iter().map(|e| *e.key()).collect()
    }

    /// Returns peer info if available.
    pub fn peer_info(&self, node_id: &NodeId) -> Option<PeerInfo> {
        // Note: We can't easily get the info without async, so return None for now
        // In a real impl, we'd store info separately
        None
    }

    /// Returns the number of connected peers.
    pub fn peer_count(&self) -> usize {
        self.peers.len()
    }

    /// Handles an incoming connection.
    pub async fn handle_inbound(&self, stream: TcpStream, addr: SocketAddr) -> Result<NodeId> {
        debug!("Handling inbound connection from {}", addr);

        // Check peer limit
        if self.peer_count() >= self.max_peers {
            warn!("Rejecting connection from {}: at max peers", addr);
            return Err(NetworkError::ConnectionFailed("at max peers".to_string()));
        }

        // TLS handshake
        let tls_stream = self
            .tls_config
            .acceptor
            .accept(stream)
            .await
            .map_err(|e| NetworkError::Tls(e.to_string()))?;

        // Extract peer's NodeId from their certificate
        let peer_certs = tls_stream.get_ref().1.peer_certificates();
        let peer_node_id = if let Some(certs) = peer_certs {
            if let Some(cert) = certs.first() {
                crate::tls::node_id_from_cert(cert)?
            } else {
                return Err(NetworkError::HandshakeFailed(
                    "no peer certificate".to_string(),
                ));
            }
        } else {
            return Err(NetworkError::HandshakeFailed(
                "no peer certificates".to_string(),
            ));
        };

        // Don't connect to ourselves
        if peer_node_id == self.node_id {
            return Err(NetworkError::ConnectionFailed(
                "cannot connect to self".to_string(),
            ));
        }

        // Check if already connected
        if self.peers.contains_key(&peer_node_id) {
            return Err(NetworkError::ConnectionFailed(
                "already connected".to_string(),
            ));
        }

        // Create connection
        let connection = Connection::from_server_tls(tls_stream, peer_node_id, addr);
        self.register_connection(peer_node_id, addr, connection, true)
            .await?;

        Ok(peer_node_id)
    }

    /// Connects to a peer.
    pub async fn connect(&self, node_id: NodeId, addr: SocketAddr) -> Result<()> {
        if node_id == self.node_id {
            return Err(NetworkError::ConnectionFailed(
                "cannot connect to self".to_string(),
            ));
        }

        // Check if already connected
        if self.peers.contains_key(&node_id) {
            return Ok(());
        }

        // Check peer limit
        if self.peer_count() >= self.max_peers {
            return Err(NetworkError::ConnectionFailed("at max peers".to_string()));
        }

        debug!("Connecting to {} at {}", node_id, addr);

        // TCP connect with timeout
        let stream = time::timeout(Duration::from_secs(10), TcpStream::connect(addr))
            .await
            .map_err(|_| NetworkError::Timeout)?
            .map_err(|e| NetworkError::ConnectionFailed(e.to_string()))?;

        // TLS handshake
        let server_name = rustls::pki_types::ServerName::try_from("avalanche")
            .map_err(|_| NetworkError::Tls("invalid server name".to_string()))?;

        let tls_stream = self
            .tls_config
            .connector
            .connect(server_name, stream)
            .await
            .map_err(|e| NetworkError::Tls(e.to_string()))?;

        // Create connection
        let connection = Connection::from_client_tls(tls_stream, node_id, addr);
        self.register_connection(node_id, addr, connection, false)
            .await?;

        Ok(())
    }

    /// Registers a new connection.
    async fn register_connection(
        &self,
        node_id: NodeId,
        addr: SocketAddr,
        connection: Connection,
        inbound: bool,
    ) -> Result<()> {
        let mut info = PeerInfo::new(node_id, addr, inbound);
        info.state = PeerState::Connected;

        // Get the outbound sender from the connection
        let outbound_tx = Some(connection.outbound_tx.clone());

        let managed = ManagedPeer {
            info: info.clone(),
            outbound_tx,
        };

        self.peers.insert(node_id, Arc::new(RwLock::new(managed)));

        // Spawn task to handle the connection
        let event_tx = self.event_tx.clone();
        let peers = self.peers.clone();
        tokio::spawn(async move {
            // Connection will be dropped when this task ends
            drop(connection);
        });

        // Emit event
        let _ = self.event_tx.send(PeerEvent::Connected(node_id, info));

        info!(
            "Peer {} connected ({})",
            node_id,
            if inbound { "inbound" } else { "outbound" }
        );

        Ok(())
    }

    /// Sends a message to a peer.
    pub async fn send(&self, node_id: NodeId, data: Bytes) -> Result<()> {
        let peer = self
            .peers
            .get(&node_id)
            .ok_or(NetworkError::PeerNotFound(node_id))?;

        // Get sender without holding lock across await
        let tx = {
            let guard = peer.read().await;
            guard.outbound_tx.clone()
        };

        if let Some(tx) = tx {
            tx.send(data)
                .await
                .map_err(|_| NetworkError::SendFailed("channel closed".to_string()))
        } else {
            Err(NetworkError::ConnectionFailed("not connected".to_string()))
        }
    }

    /// Disconnects a peer.
    pub async fn disconnect(&self, node_id: &NodeId) {
        if let Some((_, peer)) = self.peers.remove(node_id) {
            let mut guard = peer.write().await;
            guard.outbound_tx = None;
            guard.info.state = PeerState::Disconnected;
        }

        let _ = self.event_tx.send(PeerEvent::Disconnected(*node_id));
        info!("Peer {} disconnected", node_id);
    }

    /// Starts the peer manager background tasks.
    pub async fn start(&self) -> Result<()> {
        info!("Starting peer manager");
        Ok(())
    }

    /// Shuts down the peer manager.
    pub async fn shutdown(&self) {
        info!("Shutting down peer manager");
        let _ = self.shutdown_tx.send(());

        // Disconnect all peers
        let peers: Vec<_> = self.peers.iter().map(|e| *e.key()).collect();
        for node_id in peers {
            self.disconnect(&node_id).await;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_peer_event_debug() {
        let event = PeerEvent::Disconnected(NodeId::EMPTY);
        let _ = format!("{:?}", event);
    }
}
