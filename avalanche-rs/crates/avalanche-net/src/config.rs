//! Network configuration types.

use std::net::SocketAddr;
use std::time::Duration;

use avalanche_ids::NodeId;

/// Network configuration.
#[derive(Debug, Clone)]
pub struct NetworkConfig {
    /// This node's ID.
    pub node_id: NodeId,
    /// This node's listening address.
    pub listen_addr: SocketAddr,
    /// Host to listen on.
    pub listen_host: std::net::IpAddr,
    /// Staking port.
    pub staking_port: u16,
    /// Network ID (1 for mainnet, 5 for fuji).
    pub network_id: u32,
    /// Maximum number of inbound connections.
    pub max_inbound: usize,
    /// Maximum number of outbound connections.
    pub max_outbound: usize,
    /// Timeout configuration.
    pub timeouts: TimeoutConfig,
    /// Peer configuration.
    pub peer: PeerConfig,
    /// Health check configuration.
    pub health: HealthConfig,
    /// Whether to allow connections to private IPs.
    pub allow_private_ips: bool,
    /// Whether to require connecting peers to be validators.
    pub require_validator_to_connect: bool,
    /// Bootstrap node addresses.
    pub bootstrap_nodes: Vec<String>,
    /// Number of peers to gossip to.
    pub gossip_size: usize,
}

impl Default for NetworkConfig {
    fn default() -> Self {
        Self {
            node_id: NodeId::EMPTY,
            listen_addr: "0.0.0.0:9651".parse().unwrap(),
            listen_host: std::net::IpAddr::V4(std::net::Ipv4Addr::new(0, 0, 0, 0)),
            staking_port: 9651,
            network_id: 1, // Mainnet
            max_inbound: 80,
            max_outbound: 20,
            timeouts: TimeoutConfig::default(),
            peer: PeerConfig::default(),
            health: HealthConfig::default(),
            allow_private_ips: false,
            require_validator_to_connect: false,
            bootstrap_nodes: Vec::new(),
            gossip_size: 10,
        }
    }
}

/// Timeout configuration.
#[derive(Debug, Clone)]
pub struct TimeoutConfig {
    /// Timeout for TCP connection establishment.
    pub connect: Duration,
    /// Timeout for TLS handshake.
    pub tls_handshake: Duration,
    /// Timeout for reading the handshake message.
    pub read_handshake: Duration,
    /// Timeout for ping/pong.
    pub ping_pong: Duration,
    /// Initial reconnection delay.
    pub initial_reconnect_delay: Duration,
    /// Maximum reconnection delay.
    pub max_reconnect_delay: Duration,
}

impl Default for TimeoutConfig {
    fn default() -> Self {
        Self {
            connect: Duration::from_secs(10),
            tls_handshake: Duration::from_secs(10),
            read_handshake: Duration::from_secs(15),
            ping_pong: Duration::from_secs(60),
            initial_reconnect_delay: Duration::from_millis(50),
            max_reconnect_delay: Duration::from_secs(3),
        }
    }
}

/// Peer-specific configuration.
#[derive(Debug, Clone)]
pub struct PeerConfig {
    /// Read buffer size in bytes.
    pub read_buffer_size: usize,
    /// Write buffer size in bytes.
    pub write_buffer_size: usize,
    /// Ping frequency.
    pub ping_frequency: Duration,
    /// Maximum clock difference allowed between peers.
    pub max_clock_difference: Duration,
    /// Maximum message size in bytes.
    pub max_message_size: usize,
}

impl Default for PeerConfig {
    fn default() -> Self {
        Self {
            read_buffer_size: 8 * 1024,
            write_buffer_size: 8 * 1024,
            ping_frequency: Duration::from_secs(30),
            max_clock_difference: Duration::from_secs(5),
            max_message_size: 2 * 1024 * 1024, // 2 MB
        }
    }
}

/// Health check configuration.
#[derive(Debug, Clone)]
pub struct HealthConfig {
    /// Whether health checks are enabled.
    pub enabled: bool,
    /// Minimum number of connected peers to be considered healthy.
    pub min_connected_peers: usize,
    /// Maximum time since last message received.
    pub max_time_since_msg_received: Duration,
    /// Maximum time since last message sent.
    pub max_time_since_msg_sent: Duration,
    /// Maximum send failure rate (0.0 - 1.0).
    pub max_send_fail_rate: f64,
}

impl Default for HealthConfig {
    fn default() -> Self {
        Self {
            enabled: true,
            min_connected_peers: 1,
            max_time_since_msg_received: Duration::from_secs(60),
            max_time_since_msg_sent: Duration::from_secs(60),
            max_send_fail_rate: 0.2,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_network_config_default() {
        let config = NetworkConfig::default();
        assert_eq!(config.network_id, 1);
        assert_eq!(config.max_inbound, 80);
        assert_eq!(config.max_outbound, 20);
    }

    #[test]
    fn test_timeout_config_default() {
        let config = TimeoutConfig::default();
        assert_eq!(config.connect, Duration::from_secs(10));
        assert_eq!(config.ping_pong, Duration::from_secs(60));
    }

    #[test]
    fn test_peer_config_default() {
        let config = PeerConfig::default();
        assert_eq!(config.max_clock_difference, Duration::from_secs(5));
        assert_eq!(config.max_message_size, 2 * 1024 * 1024);
    }

    #[test]
    fn test_health_config_default() {
        let config = HealthConfig::default();
        assert!(config.enabled);
        assert_eq!(config.min_connected_peers, 1);
    }
}
