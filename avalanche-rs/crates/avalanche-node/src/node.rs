//! Node implementation.

use std::sync::Arc;

use avalanche_db::{Database, MemDb};
use avalanche_ids::{Id, NodeId};
use avalanche_vm::{CommonVM, Context};
use avm::AVM;
use parking_lot::RwLock;
use platformvm::PlatformVM;
use thiserror::Error;
use tokio::sync::broadcast;
use tracing::{debug, info, warn};

use crate::config::Config;

/// Chain identifiers.
pub mod chains {
    use avalanche_ids::Id;

    /// P-Chain ID (Platform chain).
    pub fn p_chain_id() -> Id {
        Id::default()
    }

    /// X-Chain ID (Exchange/Asset chain).
    pub fn x_chain_id() -> Id {
        // A non-zero ID for X-Chain
        Id::from_slice(&[1; 32]).unwrap_or_default()
    }

    /// C-Chain ID (Contract chain).
    pub fn c_chain_id() -> Id {
        // A different non-zero ID for C-Chain
        Id::from_slice(&[2; 32]).unwrap_or_default()
    }
}

/// Node state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NodeState {
    /// Node is initializing
    Initializing,
    /// Node is bootstrapping
    Bootstrapping,
    /// Node is running normally
    Running,
    /// Node is shutting down
    ShuttingDown,
    /// Node has stopped
    Stopped,
}

/// The Avalanche node.
pub struct Node {
    /// Node configuration
    config: Config,
    /// Node ID
    node_id: NodeId,
    /// Current state
    state: RwLock<NodeState>,
    /// Database
    db: Arc<dyn Database>,
    /// Platform VM (P-Chain)
    pvm: RwLock<Option<PlatformVM>>,
    /// Asset VM (X-Chain)
    avm: RwLock<Option<AVM>>,
    /// Shutdown signal
    shutdown_tx: broadcast::Sender<()>,
}

impl Node {
    /// Creates a new node.
    pub async fn new(config: Config) -> Result<Self, NodeError> {
        info!("Creating node...");

        // Ensure data directory exists
        std::fs::create_dir_all(&config.data_dir)
            .map_err(|e| NodeError::InitError(format!("failed to create data dir: {}", e)))?;

        // Generate or load node ID
        let node_id = Self::load_or_create_node_id(&config)?;
        info!("Node ID: {}", node_id);

        // Initialize database
        let db: Arc<dyn avalanche_db::Database> = if config.database.db_type == "memdb" {
            info!("Using in-memory database");
            Arc::new(MemDb::new())
        } else {
            // For now, use MemDb as fallback
            // TODO: Implement LevelDB
            info!("Using in-memory database (LevelDB not yet implemented)");
            Arc::new(MemDb::new())
        };

        let (shutdown_tx, _) = broadcast::channel(1);

        Ok(Self {
            config,
            node_id,
            state: RwLock::new(NodeState::Initializing),
            db,
            pvm: RwLock::new(None),
            avm: RwLock::new(None),
            shutdown_tx,
        })
    }

    /// Loads or creates the node ID.
    fn load_or_create_node_id(config: &Config) -> Result<NodeId, NodeError> {
        let node_id_path = config.data_dir.join("node_id");

        if node_id_path.exists() {
            let content = std::fs::read_to_string(&node_id_path)
                .map_err(|e| NodeError::InitError(format!("failed to read node ID: {}", e)))?;

            content.trim().parse::<NodeId>()
                .map_err(|e| NodeError::InitError(format!("invalid node ID: {}", e)))
        } else {
            // Generate a random node ID
            let mut bytes = [0u8; 20];
            bytes.copy_from_slice(&rand_bytes(20));
            let node_id = NodeId::from_slice(&bytes)
                .map_err(|e| NodeError::InitError(format!("failed to create node ID: {}", e)))?;

            std::fs::write(&node_id_path, node_id.to_string())
                .map_err(|e| NodeError::InitError(format!("failed to write node ID: {}", e)))?;

            Ok(node_id)
        }
    }

    /// Returns the node ID.
    pub fn node_id(&self) -> NodeId {
        self.node_id
    }

    /// Returns the current state.
    pub fn state(&self) -> NodeState {
        *self.state.read()
    }

    /// Returns the configuration.
    pub fn config(&self) -> &Config {
        &self.config
    }

    /// Returns a shutdown receiver.
    pub fn shutdown_receiver(&self) -> broadcast::Receiver<()> {
        self.shutdown_tx.subscribe()
    }

    /// Runs the node.
    pub async fn run(self) -> Result<(), NodeError> {
        info!("Starting node services...");

        // Update state
        *self.state.write() = NodeState::Bootstrapping;

        // Initialize chains
        self.initialize_chains().await?;

        // Start networking
        self.start_networking().await?;

        // Start API server
        self.start_api_server().await?;

        // Update state
        *self.state.write() = NodeState::Running;
        info!("Node is running");

        // Wait for shutdown signal
        let mut shutdown_rx = self.shutdown_tx.subscribe();
        tokio::select! {
            _ = shutdown_rx.recv() => {
                info!("Shutdown signal received");
            }
            _ = tokio::signal::ctrl_c() => {
                info!("Ctrl+C received, shutting down...");
            }
        }

        // Shutdown
        self.shutdown().await?;

        Ok(())
    }

    /// Initializes the primary network chains.
    async fn initialize_chains(&self) -> Result<(), NodeError> {
        info!("Initializing chains...");

        // Create VM contexts
        let network_id = self.config.network.network_id;

        // Initialize P-Chain (Platform VM)
        debug!("Initializing P-Chain...");
        let mut pvm = PlatformVM::new();
        let pchain_ctx = Context {
            network_id,
            chain_id: chains::p_chain_id(),
            node_id: self.node_id,
            ..Default::default()
        };

        // Create P-Chain database prefix
        let pchain_db = self.db.new_batch();
        drop(pchain_db); // We'll use the main db for now

        // Default genesis for P-Chain (local testnet)
        let pchain_genesis = platformvm::genesis::defaults::local_genesis();
        let pchain_genesis_bytes =
            serde_json::to_vec(&pchain_genesis).map_err(|e| NodeError::ChainError(e.to_string()))?;

        pvm.initialize(pchain_ctx, self.db.clone(), &pchain_genesis_bytes)
            .await
            .map_err(|e| NodeError::ChainError(format!("P-Chain init failed: {}", e)))?;

        *self.pvm.write() = Some(pvm);
        info!("P-Chain initialized");

        // Initialize X-Chain (Asset VM)
        debug!("Initializing X-Chain...");
        let mut avm = AVM::new();
        let xchain_ctx = Context {
            network_id,
            chain_id: chains::x_chain_id(),
            node_id: self.node_id,
            ..Default::default()
        };

        // Default genesis for X-Chain (local testnet)
        let xchain_genesis = avm::genesis::defaults::local_genesis();
        let xchain_genesis_bytes =
            serde_json::to_vec(&xchain_genesis).map_err(|e| NodeError::ChainError(e.to_string()))?;

        avm.initialize(xchain_ctx, self.db.clone(), &xchain_genesis_bytes)
            .await
            .map_err(|e| NodeError::ChainError(format!("X-Chain init failed: {}", e)))?;

        *self.avm.write() = Some(avm);
        info!("X-Chain initialized");

        // Initialize C-Chain (EVM) - placeholder for now
        debug!("C-Chain initialization skipped (EVM not yet implemented)");

        info!("Primary chains initialized");
        Ok(())
    }

    /// Returns a reference to the Platform VM.
    pub fn platform_vm(&self) -> Option<impl std::ops::Deref<Target = PlatformVM> + '_> {
        let guard = self.pvm.read();
        if guard.is_some() {
            Some(parking_lot::RwLockReadGuard::map(guard, |opt| {
                opt.as_ref().unwrap()
            }))
        } else {
            None
        }
    }

    /// Returns a reference to the Asset VM.
    pub fn asset_vm(&self) -> Option<impl std::ops::Deref<Target = AVM> + '_> {
        let guard = self.avm.read();
        if guard.is_some() {
            Some(parking_lot::RwLockReadGuard::map(guard, |opt| {
                opt.as_ref().unwrap()
            }))
        } else {
            None
        }
    }

    /// Starts the networking layer.
    async fn start_networking(&self) -> Result<(), NodeError> {
        info!("Starting networking...");

        let staking_port = self.config.network.staking_port;
        info!("Staking port: {}", staking_port);

        // Connect to bootstrap nodes
        for bootstrap in &self.config.network.bootstrap_nodes {
            debug!("Bootstrap node: {}", bootstrap);
        }

        // TODO: Implement actual networking
        info!("Networking started");
        Ok(())
    }

    /// Starts the API server.
    async fn start_api_server(&self) -> Result<(), NodeError> {
        info!("Starting API server...");

        let host = self.config.api.http_host;
        let port = self.config.api.http_port;
        info!("API server listening on {}:{}", host, port);

        // TODO: Implement actual HTTP server
        info!("API server started");
        Ok(())
    }

    /// Shuts down the node.
    async fn shutdown(&self) -> Result<(), NodeError> {
        info!("Shutting down node...");
        *self.state.write() = NodeState::ShuttingDown;

        // Stop API server
        debug!("Stopping API server...");

        // Stop networking
        debug!("Stopping networking...");

        // Close database
        debug!("Closing database...");
        if let Err(e) = self.db.close() {
            warn!("Error closing database: {}", e);
        }

        *self.state.write() = NodeState::Stopped;
        info!("Node stopped");

        Ok(())
    }

    /// Sends a shutdown signal.
    pub fn signal_shutdown(&self) {
        let _ = self.shutdown_tx.send(());
    }
}

/// Node errors.
#[derive(Debug, Error)]
pub enum NodeError {
    #[error("initialization error: {0}")]
    InitError(String),
    #[error("network error: {0}")]
    NetworkError(String),
    #[error("chain error: {0}")]
    ChainError(String),
    #[error("database error: {0}")]
    DatabaseError(String),
    #[error("API error: {0}")]
    ApiError(String),
}

/// Generates random bytes (simple PRNG for node ID generation).
fn rand_bytes(len: usize) -> Vec<u8> {
    use std::time::{SystemTime, UNIX_EPOCH};

    let seed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();

    let mut result = Vec::with_capacity(len);
    let mut state = seed as u64;

    for _ in 0..len {
        // Simple xorshift
        state ^= state << 13;
        state ^= state >> 7;
        state ^= state << 17;
        result.push(state as u8);
    }

    result
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[tokio::test]
    async fn test_node_creation() {
        let dir = tempdir().unwrap();
        let mut config = Config::default_for_network(12345);
        config.data_dir = dir.path().to_path_buf();

        let node = Node::new(config).await.unwrap();
        assert_eq!(node.state(), NodeState::Initializing);
    }

    #[tokio::test]
    async fn test_node_id_persistence() {
        let dir = tempdir().unwrap();
        let mut config = Config::default_for_network(12345);
        config.data_dir = dir.path().to_path_buf();

        let node1 = Node::new(config.clone()).await.unwrap();
        let id1 = node1.node_id();

        let node2 = Node::new(config).await.unwrap();
        let id2 = node2.node_id();

        assert_eq!(id1, id2);
    }

    #[test]
    fn test_chain_ids() {
        let p = chains::p_chain_id();
        let x = chains::x_chain_id();
        let c = chains::c_chain_id();

        assert_ne!(p, x);
        assert_ne!(x, c);
        assert_ne!(p, c);
    }
}
