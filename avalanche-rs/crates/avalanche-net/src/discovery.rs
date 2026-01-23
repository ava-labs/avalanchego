//! Peer discovery mechanisms.
//!
//! This module provides peer discovery for the Avalanche network:
//! - DNS seed resolution
//! - Static bootstrap nodes
//! - Peer list exchange (gossip)
//! - IP tracking and reputation

use std::collections::{HashMap, HashSet, VecDeque};
use std::net::{IpAddr, SocketAddr, ToSocketAddrs};
use std::sync::Arc;
use std::time::{Duration, Instant};

use avalanche_ids::NodeId;
use parking_lot::RwLock;
use tokio::net::lookup_host;
use tokio::sync::mpsc;
use tokio::time::interval;
use tracing::{debug, info, warn};

/// DNS seeds for peer discovery.
pub mod seeds {
    /// Mainnet DNS seeds.
    pub const MAINNET_DNS_SEEDS: &[&str] = &[
        "seeds.avax.network",
        "seeds.avalabs.org",
    ];

    /// Fuji testnet DNS seeds.
    pub const FUJI_DNS_SEEDS: &[&str] = &[
        "seeds-testnet.avax.network",
        "seeds-fuji.avalabs.org",
    ];

    /// Local network has no seeds.
    pub const LOCAL_DNS_SEEDS: &[&str] = &[];

    /// Default staking port.
    pub const DEFAULT_STAKING_PORT: u16 = 9651;

    /// Returns DNS seeds for a network.
    pub fn for_network(network_id: u32) -> &'static [&'static str] {
        match network_id {
            1 => MAINNET_DNS_SEEDS,
            5 => FUJI_DNS_SEEDS,
            _ => LOCAL_DNS_SEEDS,
        }
    }
}

/// Discovered peer information.
#[derive(Debug, Clone)]
pub struct DiscoveredPeer {
    /// Peer's address.
    pub addr: SocketAddr,
    /// Peer's node ID (if known).
    pub node_id: Option<NodeId>,
    /// When the peer was first discovered.
    pub discovered_at: Instant,
    /// When the peer was last seen.
    pub last_seen: Instant,
    /// Number of successful connections.
    pub successful_connections: u32,
    /// Number of failed connection attempts.
    pub failed_connections: u32,
    /// Source of discovery.
    pub source: DiscoverySource,
}

impl DiscoveredPeer {
    /// Creates a new discovered peer.
    pub fn new(addr: SocketAddr, source: DiscoverySource) -> Self {
        let now = Instant::now();
        Self {
            addr,
            node_id: None,
            discovered_at: now,
            last_seen: now,
            successful_connections: 0,
            failed_connections: 0,
            source,
        }
    }

    /// Updates the peer as recently seen.
    pub fn seen(&mut self) {
        self.last_seen = Instant::now();
    }

    /// Records a successful connection.
    pub fn connection_succeeded(&mut self, node_id: NodeId) {
        self.successful_connections += 1;
        self.node_id = Some(node_id);
        self.last_seen = Instant::now();
    }

    /// Records a failed connection attempt.
    pub fn connection_failed(&mut self) {
        self.failed_connections += 1;
    }

    /// Returns the connection success rate.
    pub fn success_rate(&self) -> f64 {
        let total = self.successful_connections + self.failed_connections;
        if total == 0 {
            0.5 // Unknown, assume neutral
        } else {
            self.successful_connections as f64 / total as f64
        }
    }

    /// Returns a priority score for connection attempts.
    pub fn priority(&self) -> i64 {
        let success = self.successful_connections as i64;
        let failed = self.failed_connections as i64;
        let age_bonus = if self.last_seen.elapsed() < Duration::from_secs(60) {
            10
        } else {
            0
        };
        let source_bonus = match self.source {
            DiscoverySource::Bootstrap => 100,
            DiscoverySource::Dns => 50,
            DiscoverySource::PeerExchange => 20,
            DiscoverySource::Manual => 200,
        };

        source_bonus + age_bonus + (success * 10) - (failed * 5)
    }
}

/// Source of peer discovery.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DiscoverySource {
    /// Static bootstrap node.
    Bootstrap,
    /// DNS seed resolution.
    Dns,
    /// Received from another peer.
    PeerExchange,
    /// Manually added.
    Manual,
}

/// Peer discovery configuration.
#[derive(Debug, Clone)]
pub struct DiscoveryConfig {
    /// Network ID.
    pub network_id: u32,
    /// DNS lookup interval.
    pub dns_lookup_interval: Duration,
    /// Peer exchange interval.
    pub peer_exchange_interval: Duration,
    /// Maximum number of tracked peers.
    pub max_tracked_peers: usize,
    /// Maximum peer age before removal.
    pub max_peer_age: Duration,
    /// Static bootstrap nodes.
    pub bootstrap_nodes: Vec<String>,
    /// Whether DNS discovery is enabled.
    pub dns_enabled: bool,
    /// Custom DNS seeds (overrides default).
    pub custom_dns_seeds: Vec<String>,
    /// Staking port.
    pub staking_port: u16,
}

impl Default for DiscoveryConfig {
    fn default() -> Self {
        Self {
            network_id: 1,
            dns_lookup_interval: Duration::from_secs(60 * 5), // 5 minutes
            peer_exchange_interval: Duration::from_secs(60),
            max_tracked_peers: 1000,
            max_peer_age: Duration::from_secs(60 * 60 * 24), // 24 hours
            bootstrap_nodes: Vec::new(),
            dns_enabled: true,
            custom_dns_seeds: Vec::new(),
            staking_port: seeds::DEFAULT_STAKING_PORT,
        }
    }
}

/// Peer discovery service.
pub struct PeerDiscovery {
    /// Configuration.
    config: DiscoveryConfig,
    /// Tracked peers by address.
    peers: RwLock<HashMap<SocketAddr, DiscoveredPeer>>,
    /// Peers by node ID.
    by_node_id: RwLock<HashMap<NodeId, SocketAddr>>,
    /// Recently connected peers (for gossip prioritization).
    recent_connections: RwLock<VecDeque<SocketAddr>>,
    /// Addresses that should not be connected to.
    banned: RwLock<HashSet<IpAddr>>,
    /// When discovery was last run.
    last_dns_lookup: RwLock<Option<Instant>>,
}

impl PeerDiscovery {
    /// Creates a new peer discovery service.
    pub fn new(config: DiscoveryConfig) -> Self {
        Self {
            config,
            peers: RwLock::new(HashMap::new()),
            by_node_id: RwLock::new(HashMap::new()),
            recent_connections: RwLock::new(VecDeque::new()),
            banned: RwLock::new(HashSet::new()),
            last_dns_lookup: RwLock::new(None),
        }
    }

    /// Initializes discovery with bootstrap nodes.
    pub async fn initialize(&self) {
        // Add bootstrap nodes
        for node in &self.config.bootstrap_nodes {
            if let Ok(addr) = node.parse::<SocketAddr>() {
                self.add_peer(addr, DiscoverySource::Bootstrap);
            } else if let Some(addr) = self.resolve_address(node).await {
                self.add_peer(addr, DiscoverySource::Bootstrap);
            }
        }

        // Perform initial DNS lookup
        if self.config.dns_enabled {
            self.dns_lookup().await;
        }
    }

    /// Performs DNS seed lookup.
    pub async fn dns_lookup(&self) {
        let dns_seeds = if !self.config.custom_dns_seeds.is_empty() {
            self.config.custom_dns_seeds.iter().map(|s| s.as_str()).collect()
        } else {
            seeds::for_network(self.config.network_id).to_vec()
        };

        let mut discovered = 0;
        for seed in dns_seeds {
            match self.resolve_dns_seed(seed).await {
                Ok(addrs) => {
                    for addr in addrs {
                        self.add_peer(addr, DiscoverySource::Dns);
                        discovered += 1;
                    }
                }
                Err(e) => {
                    warn!("Failed to resolve DNS seed {}: {}", seed, e);
                }
            }
        }

        info!("DNS lookup discovered {} peers", discovered);
        *self.last_dns_lookup.write() = Some(Instant::now());
    }

    /// Resolves a DNS seed to addresses.
    async fn resolve_dns_seed(&self, seed: &str) -> Result<Vec<SocketAddr>, std::io::Error> {
        let host = format!("{}:{}", seed, self.config.staking_port);
        let addrs: Vec<SocketAddr> = lookup_host(&host).await?.collect();
        Ok(addrs)
    }

    /// Resolves a hostname to an address.
    async fn resolve_address(&self, addr: &str) -> Option<SocketAddr> {
        // Try parsing as socket address first
        if let Ok(sock) = addr.parse::<SocketAddr>() {
            return Some(sock);
        }

        // Try with default port
        let with_port = if addr.contains(':') {
            addr.to_string()
        } else {
            format!("{}:{}", addr, self.config.staking_port)
        };

        lookup_host(&with_port)
            .await
            .ok()
            .and_then(|mut addrs| addrs.next())
    }

    /// Adds a discovered peer.
    pub fn add_peer(&self, addr: SocketAddr, source: DiscoverySource) {
        // Check if banned
        if self.banned.read().contains(&addr.ip()) {
            return;
        }

        let mut peers = self.peers.write();

        // Check capacity
        if peers.len() >= self.config.max_tracked_peers {
            // Remove oldest peer with lowest priority
            if let Some(oldest) = self.find_lowest_priority_peer(&peers) {
                peers.remove(&oldest);
            }
        }

        peers
            .entry(addr)
            .and_modify(|p| p.seen())
            .or_insert_with(|| DiscoveredPeer::new(addr, source));
    }

    /// Adds multiple peers from a peer exchange message.
    pub fn add_peers_from_exchange(&self, addrs: Vec<SocketAddr>) {
        for addr in addrs {
            self.add_peer(addr, DiscoverySource::PeerExchange);
        }
    }

    /// Records a successful connection.
    pub fn connection_succeeded(&self, addr: SocketAddr, node_id: NodeId) {
        if let Some(peer) = self.peers.write().get_mut(&addr) {
            peer.connection_succeeded(node_id);
        }

        self.by_node_id.write().insert(node_id, addr);

        let mut recent = self.recent_connections.write();
        recent.push_front(addr);
        if recent.len() > 100 {
            recent.pop_back();
        }
    }

    /// Records a failed connection attempt.
    pub fn connection_failed(&self, addr: SocketAddr) {
        if let Some(peer) = self.peers.write().get_mut(&addr) {
            peer.connection_failed();
        }
    }

    /// Bans an IP address.
    pub fn ban(&self, ip: IpAddr, _reason: &str) {
        self.banned.write().insert(ip);

        // Remove any peers with this IP
        let mut peers = self.peers.write();
        peers.retain(|addr, _| addr.ip() != ip);
    }

    /// Unbans an IP address.
    pub fn unban(&self, ip: &IpAddr) {
        self.banned.write().remove(ip);
    }

    /// Returns peers sorted by priority for connection attempts.
    pub fn peers_for_connection(&self, count: usize) -> Vec<DiscoveredPeer> {
        let peers = self.peers.read();
        let mut candidates: Vec<_> = peers.values().cloned().collect();

        // Sort by priority (descending)
        candidates.sort_by(|a, b| b.priority().cmp(&a.priority()));

        candidates.into_iter().take(count).collect()
    }

    /// Returns peers for gossip (recently seen, good reputation).
    pub fn peers_for_gossip(&self, count: usize) -> Vec<SocketAddr> {
        let peers = self.peers.read();
        let recent = self.recent_connections.read();

        // Prefer recently connected peers
        let mut result: Vec<SocketAddr> = recent
            .iter()
            .take(count)
            .filter(|addr| peers.contains_key(addr))
            .copied()
            .collect();

        // Fill remaining with high-priority peers
        if result.len() < count {
            let mut candidates: Vec<_> = peers
                .iter()
                .filter(|(addr, _)| !result.contains(addr))
                .map(|(addr, peer)| (*addr, peer.priority()))
                .collect();
            candidates.sort_by(|a, b| b.1.cmp(&a.1));

            for (addr, _) in candidates.into_iter().take(count - result.len()) {
                result.push(addr);
            }
        }

        result
    }

    /// Returns the address for a node ID.
    pub fn address_for_node(&self, node_id: &NodeId) -> Option<SocketAddr> {
        self.by_node_id.read().get(node_id).copied()
    }

    /// Returns total number of tracked peers.
    pub fn peer_count(&self) -> usize {
        self.peers.read().len()
    }

    /// Returns number of peers with known node IDs.
    pub fn known_peer_count(&self) -> usize {
        self.peers
            .read()
            .values()
            .filter(|p| p.node_id.is_some())
            .count()
    }

    /// Cleans up old peers.
    pub fn cleanup(&self) {
        let mut peers = self.peers.write();
        let max_age = self.config.max_peer_age;

        peers.retain(|_, peer| {
            peer.last_seen.elapsed() < max_age
                || peer.source == DiscoverySource::Bootstrap
                || peer.source == DiscoverySource::Manual
        });

        // Clean up node ID mapping
        let valid_addrs: HashSet<_> = peers.keys().copied().collect();
        self.by_node_id
            .write()
            .retain(|_, addr| valid_addrs.contains(addr));
    }

    /// Returns whether DNS lookup should be performed.
    pub fn should_dns_lookup(&self) -> bool {
        if !self.config.dns_enabled {
            return false;
        }

        match *self.last_dns_lookup.read() {
            Some(last) => last.elapsed() >= self.config.dns_lookup_interval,
            None => true,
        }
    }

    // Helper methods

    fn find_lowest_priority_peer(&self, peers: &HashMap<SocketAddr, DiscoveredPeer>) -> Option<SocketAddr> {
        peers
            .iter()
            .filter(|(_, p)| p.source != DiscoverySource::Bootstrap && p.source != DiscoverySource::Manual)
            .min_by_key(|(_, p)| p.priority())
            .map(|(addr, _)| *addr)
    }
}

/// Discovery service background task.
pub struct DiscoveryTask {
    /// Discovery service.
    discovery: Arc<PeerDiscovery>,
    /// Shutdown signal receiver.
    shutdown: mpsc::Receiver<()>,
}

impl DiscoveryTask {
    /// Creates a new discovery task.
    pub fn new(discovery: Arc<PeerDiscovery>) -> (Self, mpsc::Sender<()>) {
        let (tx, rx) = mpsc::channel(1);
        (Self { discovery, shutdown: rx }, tx)
    }

    /// Runs the discovery task.
    pub async fn run(mut self) {
        let mut dns_interval = interval(self.discovery.config.dns_lookup_interval);
        let mut cleanup_interval = interval(Duration::from_secs(60 * 10)); // 10 minutes

        loop {
            tokio::select! {
                _ = dns_interval.tick() => {
                    if self.discovery.should_dns_lookup() {
                        self.discovery.dns_lookup().await;
                    }
                }
                _ = cleanup_interval.tick() => {
                    self.discovery.cleanup();
                    debug!(
                        "Discovery cleanup: {} peers tracked, {} with known IDs",
                        self.discovery.peer_count(),
                        self.discovery.known_peer_count()
                    );
                }
                _ = self.shutdown.recv() => {
                    info!("Discovery task shutting down");
                    break;
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_addr(port: u16) -> SocketAddr {
        format!("192.168.1.{}:{}", port % 256, port).parse().unwrap()
    }

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    #[test]
    fn test_discovered_peer() {
        let addr = make_addr(9651);
        let mut peer = DiscoveredPeer::new(addr, DiscoverySource::Dns);

        assert_eq!(peer.success_rate(), 0.5);
        assert!(peer.priority() > 0);

        peer.connection_succeeded(make_node_id(1));
        assert_eq!(peer.success_rate(), 1.0);
        assert!(peer.node_id.is_some());

        peer.connection_failed();
        assert_eq!(peer.success_rate(), 0.5);
    }

    #[test]
    fn test_discovery_sources() {
        let bootstrap = DiscoveredPeer::new(make_addr(1), DiscoverySource::Bootstrap);
        let dns = DiscoveredPeer::new(make_addr(2), DiscoverySource::Dns);
        let exchange = DiscoveredPeer::new(make_addr(3), DiscoverySource::PeerExchange);
        let manual = DiscoveredPeer::new(make_addr(4), DiscoverySource::Manual);

        assert!(manual.priority() > bootstrap.priority());
        assert!(bootstrap.priority() > dns.priority());
        assert!(dns.priority() > exchange.priority());
    }

    #[test]
    fn test_peer_discovery_add() {
        let config = DiscoveryConfig {
            dns_enabled: false,
            max_tracked_peers: 10,
            ..Default::default()
        };
        let discovery = PeerDiscovery::new(config);

        for i in 1..=5 {
            discovery.add_peer(make_addr(i), DiscoverySource::Dns);
        }

        assert_eq!(discovery.peer_count(), 5);
    }

    #[test]
    fn test_peer_discovery_capacity() {
        let config = DiscoveryConfig {
            dns_enabled: false,
            max_tracked_peers: 3,
            ..Default::default()
        };
        let discovery = PeerDiscovery::new(config);

        for i in 1..=10 {
            discovery.add_peer(make_addr(i), DiscoverySource::Dns);
        }

        assert_eq!(discovery.peer_count(), 3);
    }

    #[test]
    fn test_connection_tracking() {
        let config = DiscoveryConfig::default();
        let discovery = PeerDiscovery::new(config);

        let addr = make_addr(9651);
        let node_id = make_node_id(1);

        discovery.add_peer(addr, DiscoverySource::Dns);
        discovery.connection_succeeded(addr, node_id);

        assert_eq!(discovery.address_for_node(&node_id), Some(addr));
        assert_eq!(discovery.known_peer_count(), 1);
    }

    #[test]
    fn test_ban() {
        let config = DiscoveryConfig::default();
        let discovery = PeerDiscovery::new(config);

        let addr: SocketAddr = "192.168.1.1:9651".parse().unwrap();
        discovery.add_peer(addr, DiscoverySource::Dns);
        assert_eq!(discovery.peer_count(), 1);

        discovery.ban(addr.ip(), "test");
        assert_eq!(discovery.peer_count(), 0);

        // Adding banned peer should fail
        discovery.add_peer(addr, DiscoverySource::Dns);
        assert_eq!(discovery.peer_count(), 0);
    }

    #[test]
    fn test_dns_seeds() {
        let mainnet = seeds::for_network(1);
        assert!(!mainnet.is_empty());

        let fuji = seeds::for_network(5);
        assert!(!fuji.is_empty());

        let local = seeds::for_network(12345);
        assert!(local.is_empty());
    }

    #[test]
    fn test_peers_for_connection() {
        let config = DiscoveryConfig::default();
        let discovery = PeerDiscovery::new(config);

        // Add peers with different sources
        discovery.add_peer(make_addr(1), DiscoverySource::Bootstrap);
        discovery.add_peer(make_addr(2), DiscoverySource::Dns);
        discovery.add_peer(make_addr(3), DiscoverySource::PeerExchange);

        let peers = discovery.peers_for_connection(10);
        assert_eq!(peers.len(), 3);

        // Bootstrap should be first
        assert_eq!(peers[0].source, DiscoverySource::Bootstrap);
    }

    #[test]
    fn test_cleanup() {
        let config = DiscoveryConfig {
            max_peer_age: Duration::from_millis(1),
            ..Default::default()
        };
        let discovery = PeerDiscovery::new(config);

        discovery.add_peer(make_addr(1), DiscoverySource::Dns);
        discovery.add_peer(make_addr(2), DiscoverySource::Bootstrap);

        std::thread::sleep(Duration::from_millis(10));
        discovery.cleanup();

        // Bootstrap nodes should survive cleanup
        assert_eq!(discovery.peer_count(), 1);
    }
}
