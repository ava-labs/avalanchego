//! Peer information and state tracking.

use std::net::SocketAddr;
use std::time::{Duration, Instant};

use avalanche_ids::{Id, NodeId};

use crate::version::Version;

/// State of a peer connection.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PeerState {
    /// Connection is being established.
    Connecting,
    /// TLS handshake in progress.
    Handshaking,
    /// P2P handshake in progress.
    P2PHandshaking,
    /// Fully connected and ready.
    Connected,
    /// Disconnecting.
    Disconnecting,
    /// Disconnected.
    Disconnected,
}

impl PeerState {
    /// Returns true if the peer is ready for message exchange.
    #[must_use]
    pub const fn is_ready(self) -> bool {
        matches!(self, Self::Connected)
    }

    /// Returns true if the peer is in a connecting state.
    #[must_use]
    pub const fn is_connecting(self) -> bool {
        matches!(
            self,
            Self::Connecting | Self::Handshaking | Self::P2PHandshaking
        )
    }
}

/// Information about a connected peer.
#[derive(Debug, Clone)]
pub struct PeerInfo {
    /// Peer's node ID.
    pub node_id: NodeId,
    /// Peer's reported IP address.
    pub ip: SocketAddr,
    /// Peer's client version.
    pub version: Version,
    /// Peer's client name (e.g., "avalanchego").
    pub client_name: String,
    /// Subnets the peer is tracking.
    pub tracked_subnets: Vec<Id>,
    /// Whether the peer is tracking all subnets.
    pub all_subnets: bool,
    /// Supported ACPs.
    pub supported_acps: Vec<u32>,
    /// Objected ACPs.
    pub objected_acps: Vec<u32>,
    /// Observed uptime (0-100).
    pub observed_uptime: u32,
    /// Time of last message received.
    pub last_received: Instant,
    /// Time of last message sent.
    pub last_sent: Instant,
    /// Whether this is an inbound connection.
    pub inbound: bool,
    /// Current connection state.
    pub state: PeerState,
}

impl PeerInfo {
    /// Creates a new peer info with default values.
    #[must_use]
    pub fn new(node_id: NodeId, ip: SocketAddr, inbound: bool) -> Self {
        let now = Instant::now();
        Self {
            node_id,
            ip,
            version: Version::default(),
            client_name: String::new(),
            tracked_subnets: Vec::new(),
            all_subnets: false,
            supported_acps: Vec::new(),
            objected_acps: Vec::new(),
            observed_uptime: 0,
            last_received: now,
            last_sent: now,
            inbound,
            state: PeerState::Connecting,
        }
    }

    /// Returns the time since the last message was received.
    #[must_use]
    pub fn time_since_received(&self) -> Duration {
        self.last_received.elapsed()
    }

    /// Returns the time since the last message was sent.
    #[must_use]
    pub fn time_since_sent(&self) -> Duration {
        self.last_sent.elapsed()
    }

    /// Updates the last received timestamp.
    pub fn mark_received(&mut self) {
        self.last_received = Instant::now();
    }

    /// Updates the last sent timestamp.
    pub fn mark_sent(&mut self) {
        self.last_sent = Instant::now();
    }
}

/// An unsigned IP address claim.
#[derive(Debug, Clone)]
pub struct UnsignedIp {
    /// IP address and port.
    pub addr: SocketAddr,
    /// Unix timestamp when this claim was created.
    pub timestamp: u64,
}

impl UnsignedIp {
    /// Creates a new unsigned IP.
    #[must_use]
    pub fn new(addr: SocketAddr, timestamp: u64) -> Self {
        Self { addr, timestamp }
    }

    /// Returns the IP bytes (4 for IPv4, 16 for IPv6).
    #[must_use]
    pub fn ip_bytes(&self) -> Vec<u8> {
        match self.addr.ip() {
            std::net::IpAddr::V4(ip) => ip.octets().to_vec(),
            std::net::IpAddr::V6(ip) => ip.octets().to_vec(),
        }
    }

    /// Returns the port.
    #[must_use]
    pub fn port(&self) -> u16 {
        self.addr.port()
    }
}

/// A signed IP address claim.
#[derive(Debug, Clone)]
pub struct SignedIp {
    /// The unsigned IP.
    pub unsigned: UnsignedIp,
    /// TLS signature of the IP claim.
    pub tls_signature: Vec<u8>,
    /// Optional BLS signature for validators.
    pub bls_signature: Option<Vec<u8>>,
}

impl SignedIp {
    /// Creates a new signed IP (without signatures for now).
    #[must_use]
    pub fn new(addr: SocketAddr, timestamp: u64) -> Self {
        Self {
            unsigned: UnsignedIp::new(addr, timestamp),
            tls_signature: Vec::new(),
            bls_signature: None,
        }
    }

    /// Returns the socket address.
    #[must_use]
    pub fn addr(&self) -> SocketAddr {
        self.unsigned.addr
    }

    /// Returns the timestamp.
    #[must_use]
    pub fn timestamp(&self) -> u64 {
        self.unsigned.timestamp
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_peer_state_is_ready() {
        assert!(!PeerState::Connecting.is_ready());
        assert!(!PeerState::Handshaking.is_ready());
        assert!(PeerState::Connected.is_ready());
        assert!(!PeerState::Disconnected.is_ready());
    }

    #[test]
    fn test_peer_state_is_connecting() {
        assert!(PeerState::Connecting.is_connecting());
        assert!(PeerState::Handshaking.is_connecting());
        assert!(PeerState::P2PHandshaking.is_connecting());
        assert!(!PeerState::Connected.is_connecting());
        assert!(!PeerState::Disconnected.is_connecting());
    }

    #[test]
    fn test_peer_info_new() {
        let addr: SocketAddr = "127.0.0.1:9651".parse().unwrap();
        let info = PeerInfo::new(NodeId::EMPTY, addr, true);

        assert_eq!(info.node_id, NodeId::EMPTY);
        assert_eq!(info.ip, addr);
        assert!(info.inbound);
        assert_eq!(info.state, PeerState::Connecting);
    }

    #[test]
    fn test_peer_info_timestamps() {
        let addr: SocketAddr = "127.0.0.1:9651".parse().unwrap();
        let mut info = PeerInfo::new(NodeId::EMPTY, addr, false);

        std::thread::sleep(std::time::Duration::from_millis(10));

        info.mark_received();
        assert!(info.time_since_received() < Duration::from_millis(5));
        assert!(info.time_since_sent() >= Duration::from_millis(10));
    }

    #[test]
    fn test_unsigned_ip() {
        let addr: SocketAddr = "192.168.1.1:9651".parse().unwrap();
        let ip = UnsignedIp::new(addr, 1234567890);

        assert_eq!(ip.addr, addr);
        assert_eq!(ip.timestamp, 1234567890);
        assert_eq!(ip.ip_bytes(), vec![192, 168, 1, 1]);
        assert_eq!(ip.port(), 9651);
    }

    #[test]
    fn test_unsigned_ip_v6() {
        let addr: SocketAddr = "[::1]:9651".parse().unwrap();
        let ip = UnsignedIp::new(addr, 1234567890);

        assert_eq!(ip.ip_bytes().len(), 16);
        assert_eq!(ip.port(), 9651);
    }

    #[test]
    fn test_signed_ip() {
        let addr: SocketAddr = "10.0.0.1:9651".parse().unwrap();
        let signed = SignedIp::new(addr, 1234567890);

        assert_eq!(signed.addr(), addr);
        assert_eq!(signed.timestamp(), 1234567890);
        assert!(signed.tls_signature.is_empty());
        assert!(signed.bls_signature.is_none());
    }
}
