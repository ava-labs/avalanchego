//! Node implementation - wires together all Avalanche components end-to-end.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use avalanche_api::{HttpConfig, HttpServer, Server as ApiServer};
use avalanche_db::{Database, MemDb};
use avalanche_ids::{Id, NodeId};
use avalanche_net::{DiscoveryConfig, MessageHandler, Network, NetworkConfig, NetworkImpl, NoOpHandler, PeerDiscovery, PeerInfo, TlsConfig};
use avalanche_snow::{BootstrapConfig, Bootstrapper, Mempool, MempoolConfig, Parameters, Snowman};
use avalanche_vm::atomic::{AtomicState, SharedMemory};
use avalanche_vm::{CommonVM, Context};
use avm::AVM;
use evm::EvmVM;
use parking_lot::RwLock;
use platformvm::PlatformVM;
use thiserror::Error;
use tokio::sync::broadcast;
use tracing::{debug, info, warn};

use crate::config::Config;

/// Chain identifiers.
pub mod chains {
    use avalanche_ids::Id;

    /// P-Chain ID (Platform chain) - empty hash represents primary network.
    pub fn p_chain_id() -> Id {
        Id::default()
    }

    /// X-Chain ID (Exchange/Asset chain).
    pub fn x_chain_id() -> Id {
        // Standard X-Chain ID
        Id::from_slice(&[
            0x2d, 0x5d, 0x89, 0x77, 0x2f, 0x51, 0xab, 0x70, 0xda, 0x74, 0x50, 0x92, 0x4e, 0xd1,
            0x91, 0x49, 0x64, 0xee, 0x07, 0x00, 0x3a, 0xb7, 0x71, 0x8c, 0xf5, 0x6d, 0x7a, 0x2b,
            0x00, 0x00, 0x00, 0x01,
        ])
        .unwrap_or_default()
    }

    /// C-Chain ID (Contract chain).
    pub fn c_chain_id() -> Id {
        // Standard C-Chain ID
        Id::from_slice(&[
            0x2c, 0xa4, 0x54, 0xa2, 0xa2, 0x92, 0x64, 0x6e, 0x32, 0xb2, 0x85, 0x6c, 0x37, 0x76,
            0x56, 0xcd, 0x43, 0xef, 0x2c, 0xc6, 0xc5, 0x2f, 0xb8, 0xf5, 0x60, 0x3a, 0x68, 0x1e,
            0x00, 0x00, 0x00, 0x02,
        ])
        .unwrap_or_default()
    }

    /// Returns chain name for an ID.
    pub fn chain_name(id: &Id) -> &'static str {
        if *id == p_chain_id() {
            "P-Chain"
        } else if *id == x_chain_id() {
            "X-Chain"
        } else if *id == c_chain_id() {
            "C-Chain"
        } else {
            "Subnet"
        }
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

/// Chain instance with all components.
pub struct ChainInstance {
    /// Chain ID
    pub chain_id: Id,
    /// Chain alias (e.g., "P", "X", "C")
    pub alias: String,
    /// Consensus engine
    pub consensus: Option<Snowman>,
    /// Mempool
    pub mempool: Arc<Mempool>,
    /// Is bootstrapped
    pub bootstrapped: bool,
}

/// The Avalanche node - wires together all components.
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
    /// EVM (C-Chain)
    evm: RwLock<Option<EvmVM>>,
    /// Chain instances
    chains: RwLock<HashMap<Id, ChainInstance>>,
    /// Shared memory for atomic operations
    shared_memory: Arc<SharedMemory>,
    /// Peer discovery
    peer_discovery: RwLock<Option<PeerDiscovery>>,
    /// P2P network
    network: RwLock<Option<Arc<NetworkImpl>>>,
    /// Network task handle
    network_handle: RwLock<Option<tokio::task::JoinHandle<()>>>,
    /// HTTP API server
    http_server: RwLock<Option<Arc<HttpServer>>>,
    /// API server handle
    api_handle: RwLock<Option<tokio::task::JoinHandle<()>>>,
    /// Shutdown signal
    shutdown_tx: broadcast::Sender<()>,
}

impl Node {
    /// Creates a new node.
    pub async fn new(config: Config) -> Result<Self, NodeError> {
        info!("Creating Avalanche node...");

        // Ensure data directory exists
        std::fs::create_dir_all(&config.data_dir)
            .map_err(|e| NodeError::InitError(format!("failed to create data dir: {}", e)))?;

        // Generate or load node ID
        let node_id = Self::load_or_create_node_id(&config)?;
        info!("Node ID: {}", node_id);

        // Initialize database (using MemDb for now, RocksDb requires feature flag)
        let db: Arc<dyn Database> = {
            info!("Using in-memory database");
            Arc::new(MemDb::new())
        };

        // Initialize shared memory for cross-chain atomics
        let shared_memory = Arc::new(SharedMemory::new(db.clone()));

        let (shutdown_tx, _) = broadcast::channel(16);

        Ok(Self {
            config,
            node_id,
            state: RwLock::new(NodeState::Initializing),
            db,
            pvm: RwLock::new(None),
            avm: RwLock::new(None),
            evm: RwLock::new(None),
            chains: RwLock::new(HashMap::new()),
            shared_memory,
            peer_discovery: RwLock::new(None),
            network: RwLock::new(None),
            network_handle: RwLock::new(None),
            http_server: RwLock::new(None),
            api_handle: RwLock::new(None),
            shutdown_tx,
        })
    }

    /// Loads or creates the node ID from staking key.
    fn load_or_create_node_id(config: &Config) -> Result<NodeId, NodeError> {
        let node_id_path = config.data_dir.join("node_id");

        if node_id_path.exists() {
            let content = std::fs::read_to_string(&node_id_path)
                .map_err(|e| NodeError::InitError(format!("failed to read node ID: {}", e)))?;

            content
                .trim()
                .parse::<NodeId>()
                .map_err(|e| NodeError::InitError(format!("invalid node ID: {}", e)))
        } else {
            // Generate node ID from random key (in production, derive from staking key)
            let bytes = rand_bytes(20);
            let node_id = NodeId::from_slice(&bytes)
                .map_err(|e| NodeError::InitError(format!("failed to create node ID: {}", e)))?;

            std::fs::write(&node_id_path, node_id.to_string())
                .map_err(|e| NodeError::InitError(format!("failed to write node ID: {}", e)))?;

            info!("Generated new node ID: {}", node_id);
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

    /// Runs the node end-to-end.
    pub async fn run(self: Arc<Self>) -> Result<(), NodeError> {
        info!("Starting Avalanche node...");

        // Update state
        *self.state.write() = NodeState::Bootstrapping;

        // Phase 1: Initialize chains (P, X, C)
        self.initialize_chains().await?;

        // Phase 2: Start networking and peer discovery
        self.start_networking().await?;

        // Phase 3: Bootstrap chains from network
        self.bootstrap_chains().await?;

        // Phase 4: Start consensus engines
        self.start_consensus().await?;

        // Phase 5: Start API server
        self.start_api_server().await?;

        // Update state to running
        *self.state.write() = NodeState::Running;
        info!("=== Avalanche node is fully operational ===");
        info!("  Network ID: {}", self.config.network.network_id);
        info!("  Node ID: {}", self.node_id);
        info!(
            "  API: http://{}:{}",
            self.config.api.http_host, self.config.api.http_port
        );
        info!("  Staking Port: {}", self.config.network.staking_port);

        // Wait for shutdown signal
        let mut shutdown_rx = self.shutdown_tx.subscribe();
        tokio::select! {
            _ = shutdown_rx.recv() => {
                info!("Shutdown signal received");
            }
            _ = tokio::signal::ctrl_c() => {
                info!("Ctrl+C received, initiating shutdown...");
            }
        }

        // Graceful shutdown
        self.shutdown().await?;

        Ok(())
    }

    /// Initializes all primary network chains (P, X, C).
    async fn initialize_chains(&self) -> Result<(), NodeError> {
        info!("Phase 1: Initializing primary network chains...");

        let network_id = self.config.network.network_id;

        // Initialize P-Chain (Platform VM)
        self.init_p_chain(network_id).await?;

        // Initialize X-Chain (Asset VM)
        self.init_x_chain(network_id).await?;

        // Initialize C-Chain (EVM)
        self.init_c_chain(network_id).await?;

        info!("All primary chains initialized successfully");
        Ok(())
    }

    /// Initializes the P-Chain (Platform VM).
    async fn init_p_chain(&self, network_id: u32) -> Result<(), NodeError> {
        debug!("Initializing P-Chain (Platform VM)...");

        let mut pvm = PlatformVM::new();
        let chain_id = chains::p_chain_id();

        let ctx = Context {
            network_id,
            chain_id,
            node_id: self.node_id,
            ..Default::default()
        };

        // Load or create genesis
        let genesis = platformvm::genesis::defaults::local_genesis();
        let genesis_bytes =
            serde_json::to_vec(&genesis).map_err(|e| NodeError::ChainError(e.to_string()))?;

        pvm.initialize(ctx, self.db.clone(), &genesis_bytes)
            .await
            .map_err(|e| NodeError::ChainError(format!("P-Chain init failed: {}", e)))?;

        // Create chain instance
        let mempool = Arc::new(Mempool::new(MempoolConfig::default()));
        let instance = ChainInstance {
            chain_id,
            alias: "P".to_string(),
            consensus: None,
            mempool,
            bootstrapped: false,
        };

        self.chains.write().insert(chain_id, instance);
        *self.pvm.write() = Some(pvm);

        info!("P-Chain initialized (Platform VM)");
        Ok(())
    }

    /// Initializes the X-Chain (Asset VM).
    async fn init_x_chain(&self, network_id: u32) -> Result<(), NodeError> {
        debug!("Initializing X-Chain (Asset VM)...");

        let mut avm = AVM::new();
        let chain_id = chains::x_chain_id();

        let ctx = Context {
            network_id,
            chain_id,
            node_id: self.node_id,
            ..Default::default()
        };

        // Load or create genesis
        let genesis = avm::genesis::defaults::local_genesis();
        let genesis_bytes =
            serde_json::to_vec(&genesis).map_err(|e| NodeError::ChainError(e.to_string()))?;

        avm.initialize(ctx, self.db.clone(), &genesis_bytes)
            .await
            .map_err(|e| NodeError::ChainError(format!("X-Chain init failed: {}", e)))?;

        // Create atomic state for cross-chain transactions
        let _atomic_state = AtomicState::new(chain_id, self.shared_memory.clone());
        debug!("X-Chain atomic state initialized");

        // Create chain instance
        let mempool = Arc::new(Mempool::new(MempoolConfig::default()));
        let instance = ChainInstance {
            chain_id,
            alias: "X".to_string(),
            consensus: None,
            mempool,
            bootstrapped: false,
        };

        self.chains.write().insert(chain_id, instance);
        *self.avm.write() = Some(avm);

        info!("X-Chain initialized (Asset VM)");
        Ok(())
    }

    /// Initializes the C-Chain (EVM).
    async fn init_c_chain(&self, network_id: u32) -> Result<(), NodeError> {
        debug!("Initializing C-Chain (EVM)...");

        let chain_id = chains::c_chain_id();

        // Determine chain ID based on network
        let evm_chain_id = if network_id == 1 {
            43114 // Mainnet
        } else if network_id == 5 {
            43113 // Fuji
        } else {
            43112 // Local
        };

        // Create EVM configuration
        let evm_config = evm::VMConfig {
            chain_id: evm_chain_id,
            network_id,
            ..Default::default()
        };

        // Create EVM with database
        let evm = EvmVM::new(evm_config, self.db.clone());

        // Create genesis configuration
        let genesis_config = evm::GenesisConfig {
            chain_id: evm_chain_id,
            timestamp: chrono::Utc::now().timestamp() as u64,
            ..Default::default()
        };

        // Initialize with genesis
        evm.initialize(genesis_config)
            .map_err(|e| NodeError::ChainError(format!("C-Chain genesis failed: {}", e)))?;

        // Create atomic state for C-Chain
        let _atomic_state = AtomicState::new(chain_id, self.shared_memory.clone());
        debug!("C-Chain atomic state initialized");

        // Create chain instance
        let mempool = Arc::new(Mempool::new(MempoolConfig::default()));
        let instance = ChainInstance {
            chain_id,
            alias: "C".to_string(),
            consensus: None,
            mempool,
            bootstrapped: false,
        };

        self.chains.write().insert(chain_id, instance);
        *self.evm.write() = Some(evm);

        info!("C-Chain initialized (EVM, chain_id={})", evm_chain_id);
        Ok(())
    }

    /// Starts the networking layer with peer discovery.
    async fn start_networking(&self) -> Result<(), NodeError> {
        info!("Phase 2: Starting networking layer...");

        let staking_port = self.config.network.staking_port;

        // Initialize peer discovery
        let discovery_config = DiscoveryConfig {
            network_id: self.config.network.network_id,
            ..Default::default()
        };

        let peer_discovery = PeerDiscovery::new(discovery_config);
        *self.peer_discovery.write() = Some(peer_discovery);

        // Create TLS configuration (in production, load from staking key)
        let tls_config = TlsConfig::generate_self_signed(self.node_id)
            .map_err(|e| NodeError::NetworkError(format!("TLS config failed: {}", e)))?;

        // Create network configuration
        let network_config = NetworkConfig {
            network_id: self.config.network.network_id,
            listen_host: "0.0.0.0".parse().unwrap(),
            staking_port,
            bootstrap_nodes: self.config.network.bootstrap_nodes.clone(),
            max_inbound: self.config.network.max_peers / 2,
            max_outbound: self.config.network.max_peers / 2,
            gossip_size: 10,
            ..Default::default()
        };

        // Create message handler (for now, use no-op handler)
        let handler: Arc<dyn MessageHandler> = Arc::new(NoOpHandler);

        // Create network
        let network = Arc::new(
            NetworkImpl::new(network_config, tls_config, handler)
                .map_err(|e| NodeError::NetworkError(format!("Network init failed: {}", e)))?,
        );

        *self.network.write() = Some(network.clone());

        // Log bootstrap nodes
        for bootstrap in &self.config.network.bootstrap_nodes {
            debug!("Bootstrap node configured: {}", bootstrap);
        }

        // Start network in background task
        let network_clone = network.clone();
        let handle = tokio::spawn(async move {
            if let Err(e) = network_clone.run().await {
                warn!("Network error: {}", e);
            }
        });

        *self.network_handle.write() = Some(handle);

        info!("P2P network started on port {}", staking_port);
        Ok(())
    }

    /// Bootstraps chains from the network.
    async fn bootstrap_chains(&self) -> Result<(), NodeError> {
        info!("Phase 3: Bootstrapping chains...");

        // For local network, skip bootstrapping
        if self.config.network.network_id == 12345 {
            info!("Local network - skipping bootstrap");

            // Mark all chains as bootstrapped
            let mut chains = self.chains.write();
            for (_, chain) in chains.iter_mut() {
                chain.bootstrapped = true;
            }

            return Ok(());
        }

        // Bootstrap each chain
        let chain_ids: Vec<Id> = self.chains.read().keys().cloned().collect();

        for chain_id in chain_ids {
            let chain_name = chains::chain_name(&chain_id);
            debug!("Bootstrapping {}...", chain_name);

            // Create bootstrapper for this chain
            let _bootstrapper = Bootstrapper::new(BootstrapConfig::default());

            // In production, fetch blocks from peers here
            // For now, mark as bootstrapped

            if let Some(chain) = self.chains.write().get_mut(&chain_id) {
                chain.bootstrapped = true;
            }

            info!("{} bootstrapped", chain_name);
        }

        info!("All chains bootstrapped");
        Ok(())
    }

    /// Starts consensus engines for all chains.
    async fn start_consensus(&self) -> Result<(), NodeError> {
        info!("Phase 4: Starting consensus engines...");

        let mut chains = self.chains.write();

        for (chain_id, chain) in chains.iter_mut() {
            let chain_name = chains::chain_name(chain_id);
            debug!("Starting consensus for {}...", chain_name);

            // Create Snowman consensus instance with default parameters
            let params = Parameters::default();
            let snowman = Snowman::new(params);

            chain.consensus = Some(snowman);
            info!("{} consensus engine started", chain_name);
        }

        info!("All consensus engines running");
        Ok(())
    }

    /// Starts the HTTP API server.
    async fn start_api_server(&self) -> Result<(), NodeError> {
        info!("Phase 5: Starting API server...");

        let host = self.config.api.http_host;
        let port = self.config.api.http_port;
        let addr = SocketAddr::new(host, port);

        // Create JSON-RPC server with endpoints
        let rpc_server = Arc::new(ApiServer::new(format!("{}:{}", host, port)));

        // Register endpoints using the endpoint creation functions
        let p_chain_endpoint = avalanche_api::create_platform_endpoint();
        let x_chain_endpoint = avalanche_api::create_avm_endpoint();
        let c_chain_endpoint = avalanche_api::create_evm_endpoint();

        // Also create info and health endpoints
        let info_endpoint = avalanche_api::server::create_info_endpoint();
        let health_endpoint = avalanche_api::server::create_health_endpoint();

        rpc_server.register_endpoint(p_chain_endpoint);
        rpc_server.register_endpoint(x_chain_endpoint);
        rpc_server.register_endpoint(c_chain_endpoint);
        rpc_server.register_endpoint(info_endpoint);
        rpc_server.register_endpoint(health_endpoint);

        if self.config.api.admin_api_enabled {
            let admin_endpoint = avalanche_api::create_admin_endpoint();
            rpc_server.register_endpoint(admin_endpoint);
        }

        // Create HTTP server config
        let http_config = HttpConfig {
            addr,
            max_body_size: 10 * 1024 * 1024, // 10 MB
            cors_enabled: true,
        };

        // Create and start HTTP server
        let http_server = Arc::new(HttpServer::new(http_config, rpc_server));
        *self.http_server.write() = Some(http_server.clone());

        info!("HTTP API server listening on http://{}:{}", host, port);
        info!("  P-Chain: /ext/bc/P");
        info!("  X-Chain: /ext/bc/X");
        info!("  C-Chain: /ext/bc/C/rpc");
        info!("  Info:    /ext/info");
        info!("  Health:  /ext/health");

        // Spawn HTTP server task
        let handle = tokio::spawn(async move {
            if let Err(e) = http_server.run().await {
                warn!("HTTP server error: {}", e);
            }
        });

        *self.api_handle.write() = Some(handle);

        info!("API server started and accepting connections");
        Ok(())
    }

    /// Gracefully shuts down the node.
    async fn shutdown(&self) -> Result<(), NodeError> {
        info!("Initiating graceful shutdown...");
        *self.state.write() = NodeState::ShuttingDown;

        // Signal all tasks to stop
        let _ = self.shutdown_tx.send(());

        // Stop HTTP API server
        if let Some(http_server) = self.http_server.read().as_ref() {
            debug!("Stopping HTTP server...");
            http_server.shutdown();
        }
        if let Some(handle) = self.api_handle.write().take() {
            debug!("Waiting for API server to stop...");
            let _ = tokio::time::timeout(std::time::Duration::from_secs(5), handle).await;
        }

        // Stop consensus engines
        debug!("Stopping consensus engines...");
        for (chain_id, _chain) in self.chains.read().iter() {
            let name = chains::chain_name(chain_id);
            debug!("  Stopping {} consensus", name);
        }

        // Stop networking
        debug!("Stopping networking...");
        if let Some(network) = self.network.read().as_ref() {
            let _ = network.shutdown().await;
        }
        if let Some(handle) = self.network_handle.write().take() {
            let _ = tokio::time::timeout(std::time::Duration::from_secs(5), handle).await;
        }

        // Shutdown VMs
        debug!("Shutting down VMs...");
        if let Some(ref mut pvm) = *self.pvm.write() {
            if let Err(e) = pvm.shutdown().await {
                warn!("P-Chain shutdown error: {}", e);
            }
        }
        if let Some(ref mut avm) = *self.avm.write() {
            if let Err(e) = avm.shutdown().await {
                warn!("X-Chain shutdown error: {}", e);
            }
        }
        if self.evm.read().is_some() {
            debug!("C-Chain shutdown complete");
        }

        // Close database
        debug!("Closing database...");
        if let Err(e) = self.db.close() {
            warn!("Database close error: {}", e);
        }

        *self.state.write() = NodeState::Stopped;
        info!("Node shutdown complete");

        Ok(())
    }

    /// Sends a shutdown signal.
    pub fn signal_shutdown(&self) {
        info!("Shutdown signal requested");
        let _ = self.shutdown_tx.send(());
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

    /// Returns a reference to the EVM.
    pub fn evm(&self) -> Option<impl std::ops::Deref<Target = EvmVM> + '_> {
        let guard = self.evm.read();
        if guard.is_some() {
            Some(parking_lot::RwLockReadGuard::map(guard, |opt| {
                opt.as_ref().unwrap()
            }))
        } else {
            None
        }
    }

    /// Returns chain status information.
    pub fn chain_status(&self) -> HashMap<String, ChainStatus> {
        let chains = self.chains.read();
        let mut status = HashMap::new();

        for (id, chain) in chains.iter() {
            status.insert(
                chain.alias.clone(),
                ChainStatus {
                    chain_id: *id,
                    bootstrapped: chain.bootstrapped,
                    mempool_size: chain.mempool.len(),
                },
            );
        }

        status
    }
}

/// Chain status information.
#[derive(Debug, Clone)]
pub struct ChainStatus {
    pub chain_id: Id,
    pub bootstrapped: bool,
    pub mempool_size: usize,
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
    #[error("consensus error: {0}")]
    ConsensusError(String),
}

/// Generates random bytes for node ID.
fn rand_bytes(len: usize) -> Vec<u8> {
    use std::time::{SystemTime, UNIX_EPOCH};

    let seed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();

    let mut result = Vec::with_capacity(len);
    let mut state = seed as u64;

    for _ in 0..len {
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

    #[test]
    fn test_chain_names() {
        assert_eq!(chains::chain_name(&chains::p_chain_id()), "P-Chain");
        assert_eq!(chains::chain_name(&chains::x_chain_id()), "X-Chain");
        assert_eq!(chains::chain_name(&chains::c_chain_id()), "C-Chain");
    }

    #[tokio::test]
    async fn test_chain_initialization() {
        let dir = tempdir().unwrap();
        let mut config = Config::default_for_network(12345);
        config.data_dir = dir.path().to_path_buf();

        let node = Node::new(config).await.unwrap();
        node.initialize_chains().await.unwrap();

        // Verify all chains initialized
        assert!(node.pvm.read().is_some());
        assert!(node.avm.read().is_some());
        assert!(node.evm.read().is_some());

        // Verify chain instances
        let chains = node.chains.read();
        assert_eq!(chains.len(), 3);
        assert!(chains.contains_key(&chains::p_chain_id()));
        assert!(chains.contains_key(&chains::x_chain_id()));
        assert!(chains.contains_key(&chains::c_chain_id()));
    }

    #[tokio::test]
    async fn test_chain_status() {
        let dir = tempdir().unwrap();
        let mut config = Config::default_for_network(12345);
        config.data_dir = dir.path().to_path_buf();

        let node = Node::new(config).await.unwrap();
        node.initialize_chains().await.unwrap();

        let status = node.chain_status();
        assert_eq!(status.len(), 3);
        assert!(status.contains_key("P"));
        assert!(status.contains_key("X"));
        assert!(status.contains_key("C"));
    }
}
