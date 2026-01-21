//! Main network implementation.

use std::collections::HashSet;
use std::net::SocketAddr;
use std::sync::Arc;

use async_trait::async_trait;
use parking_lot::RwLock;
use tokio::net::TcpListener;
use tokio::sync::broadcast;
use tracing::{debug, error, info, warn};

use avalanche_ids::NodeId;
use avalanche_proto::Message;

use crate::peer::PeerInfo;
use crate::peer_manager::{PeerEvent, PeerManager};
use crate::tls::TlsConfig;
use crate::{MessageHandler, Network, NetworkConfig, NetworkError, Result, SendConfig};

/// Implementation of the Network trait.
pub struct NetworkImpl {
    /// Peer manager.
    peer_manager: Arc<PeerManager>,
    /// Message handler.
    handler: Arc<dyn MessageHandler>,
    /// TLS configuration.
    tls_config: Arc<TlsConfig>,
    /// Network configuration.
    config: NetworkConfig,
    /// Listen address.
    listen_addr: SocketAddr,
    /// Whether the network is running.
    running: RwLock<bool>,
    /// Shutdown signal.
    shutdown_tx: broadcast::Sender<()>,
}

impl NetworkImpl {
    /// Creates a new network instance.
    pub fn new(
        config: NetworkConfig,
        tls_config: TlsConfig,
        handler: Arc<dyn MessageHandler>,
    ) -> Result<Self> {
        let listen_addr = SocketAddr::new(config.listen_host, config.staking_port);
        let tls_config = Arc::new(tls_config);
        let (peer_manager, _) = PeerManager::new(
            tls_config.clone(),
            config.clone(),
            config.max_inbound + config.max_outbound,
        );

        let (shutdown_tx, _) = broadcast::channel(1);

        Ok(Self {
            peer_manager: Arc::new(peer_manager),
            handler,
            tls_config,
            config,
            listen_addr,
            running: RwLock::new(false),
            shutdown_tx,
        })
    }

    /// Starts accepting incoming connections.
    async fn accept_loop(self: Arc<Self>, listener: TcpListener) {
        let mut shutdown_rx = self.shutdown_tx.subscribe();

        loop {
            tokio::select! {
                result = listener.accept() => {
                    match result {
                        Ok((stream, addr)) => {
                            debug!("Accepted connection from {}", addr);
                            let this = self.clone();
                            tokio::spawn(async move {
                                match this.peer_manager.handle_inbound(stream, addr).await {
                                    Ok(node_id) => {
                                        info!("Inbound connection established: {}", node_id);
                                    }
                                    Err(e) => {
                                        warn!("Failed to handle inbound connection from {}: {}", addr, e);
                                    }
                                }
                            });
                        }
                        Err(e) => {
                            error!("Accept error: {}", e);
                        }
                    }
                }
                _ = shutdown_rx.recv() => {
                    info!("Accept loop shutting down");
                    break;
                }
            }
        }
    }

    /// Processes peer events.
    async fn event_loop(self: Arc<Self>, mut event_rx: broadcast::Receiver<PeerEvent>) {
        let mut shutdown_rx = self.shutdown_tx.subscribe();

        loop {
            tokio::select! {
                result = event_rx.recv() => {
                    match result {
                        Ok(PeerEvent::Connected(node_id, info)) => {
                            self.handler.on_connected(node_id, info).await;
                        }
                        Ok(PeerEvent::Disconnected(node_id)) => {
                            self.handler.on_disconnected(node_id).await;
                        }
                        Ok(PeerEvent::Message(node_id, data)) => {
                            // Decode and handle message
                            match Message::decode(&data) {
                                Ok(msg) => {
                                    match self.handler.handle_message(node_id, msg).await {
                                        Ok(Some(response)) => {
                                            // Send response
                                            if let Err(e) = self.send(node_id, response, SendConfig::default()).await {
                                                warn!("Failed to send response to {}: {}", node_id, e);
                                            }
                                        }
                                        Ok(None) => {}
                                        Err(e) => {
                                            warn!("Error handling message from {}: {}", node_id, e);
                                        }
                                    }
                                }
                                Err(e) => {
                                    warn!("Failed to decode message from {}: {}", node_id, e);
                                }
                            }
                        }
                        Err(broadcast::error::RecvError::Lagged(n)) => {
                            warn!("Event loop lagged by {} messages", n);
                        }
                        Err(broadcast::error::RecvError::Closed) => {
                            debug!("Event channel closed");
                            break;
                        }
                    }
                }
                _ = shutdown_rx.recv() => {
                    info!("Event loop shutting down");
                    break;
                }
            }
        }
    }

    /// Connects to bootstrap nodes.
    async fn connect_bootstrap(&self) {
        for addr_str in &self.config.bootstrap_nodes {
            match addr_str.parse::<SocketAddr>() {
                Ok(addr) => {
                    // For bootstrap, we don't know the NodeId yet
                    // In a real implementation, we'd discover it during handshake
                    debug!("Connecting to bootstrap node: {}", addr);
                }
                Err(e) => {
                    warn!("Invalid bootstrap address '{}': {}", addr_str, e);
                }
            }
        }
    }
}

#[async_trait]
impl Network for NetworkImpl {
    async fn send(&self, peer_id: NodeId, msg: Message, _config: SendConfig) -> Result<bool> {
        let data = msg.encode().map_err(NetworkError::from)?;
        self.peer_manager.send(peer_id, data).await?;
        Ok(true)
    }

    async fn send_to(
        &self,
        peer_ids: &[NodeId],
        msg: Message,
        config: SendConfig,
    ) -> Result<HashSet<NodeId>> {
        let mut sent = HashSet::new();

        for &peer_id in peer_ids {
            match self.send(peer_id, msg.clone(), config.clone()).await {
                Ok(true) => {
                    sent.insert(peer_id);
                }
                Ok(false) => {}
                Err(e) => {
                    debug!("Failed to send to {}: {}", peer_id, e);
                }
            }
        }

        Ok(sent)
    }

    async fn gossip(&self, msg: Message, config: SendConfig) -> Result<HashSet<NodeId>> {
        let peers = self.peer_manager.connected_peers();

        // Select random subset for gossip
        let gossip_size = std::cmp::min(self.config.gossip_size, peers.len());
        let selected: Vec<_> = peers.into_iter().take(gossip_size).collect();

        self.send_to(&selected, msg, config).await
    }

    fn connected_peers(&self) -> Vec<NodeId> {
        self.peer_manager.connected_peers()
    }

    fn peer_info(&self, peer_id: NodeId) -> Option<PeerInfo> {
        self.peer_manager.peer_info(&peer_id)
    }

    fn node_id(&self) -> NodeId {
        self.tls_config.node_id
    }

    fn ip(&self) -> SocketAddr {
        self.listen_addr
    }

    fn track(&self, peer_id: NodeId, addr: SocketAddr) {
        self.peer_manager.track(peer_id, addr);
    }

    async fn start(&self) -> Result<()> {
        info!("Starting network on {}", self.listen_addr);

        // Bind listener
        let listener = TcpListener::bind(self.listen_addr)
            .await
            .map_err(|e| NetworkError::ConnectionFailed(e.to_string()))?;

        info!("Listening on {}", self.listen_addr);

        *self.running.write() = true;

        // Start peer manager
        self.peer_manager.start().await?;

        // Connect to bootstrap nodes
        self.connect_bootstrap().await;

        // Start accept loop (needs Arc<Self>)
        // In practice, this would be called from the node that holds Arc<NetworkImpl>

        Ok(())
    }

    async fn shutdown(&self) -> Result<()> {
        info!("Shutting down network");
        *self.running.write() = false;
        let _ = self.shutdown_tx.send(());
        self.peer_manager.shutdown().await;
        Ok(())
    }

    fn is_healthy(&self) -> bool {
        *self.running.read() && self.peer_manager.peer_count() > 0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_network_config() {
        let config = NetworkConfig::default();
        assert!(config.max_inbound > 0);
        assert!(config.max_outbound > 0);
    }
}
