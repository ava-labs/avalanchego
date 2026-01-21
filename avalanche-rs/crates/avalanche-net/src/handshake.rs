//! P2P handshake protocol implementation.
//!
//! The handshake protocol is the first step after TLS connection establishment:
//!
//! 1. Both peers send Handshake messages simultaneously
//! 2. Both peers receive and validate the other's Handshake
//! 3. Both peers respond with PeerList
//! 4. Connection is established

use std::net::{IpAddr, SocketAddr};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::time;
use tracing::debug;

use avalanche_ids::NodeId;
use avalanche_proto::{
    BloomFilter, Client, ClaimedIpPort, Handshake, Message, PeerList,
};

use crate::{NetworkConfig, NetworkError, Result};

/// Handshake timeout.
const HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(15);

/// Maximum clock skew allowed (5 minutes).
const MAX_CLOCK_SKEW: Duration = Duration::from_secs(300);

/// Minimum compatible client version.
pub const MIN_COMPATIBLE_VERSION: (u32, u32, u32) = (1, 11, 0);

/// Current client version.
pub const CLIENT_VERSION: (u32, u32, u32) = (0, 1, 0);
pub const CLIENT_NAME: &str = "avalanche-rs";

/// Result of a successful handshake.
#[derive(Debug, Clone)]
pub struct HandshakeResult {
    /// Peer's node ID (extracted from TLS certificate).
    pub node_id: NodeId,
    /// Peer's claimed IP.
    pub ip: SocketAddr,
    /// Peer's client info.
    pub client: Option<Client>,
    /// Peer's network ID.
    pub network_id: u32,
    /// Peer's tracked subnets.
    pub tracked_subnets: Vec<[u8; 32]>,
    /// Peers discovered during handshake.
    pub discovered_peers: Vec<ClaimedIpPort>,
}

/// Performs the handshake protocol.
pub struct HandshakeProtocol {
    /// Our node ID.
    pub node_id: NodeId,
    /// Our network ID.
    pub network_id: u32,
    /// Our IP address.
    pub ip: SocketAddr,
    /// Tracked subnets (32-byte IDs).
    pub tracked_subnets: Vec<[u8; 32]>,
    /// TLS signature of our IP.
    pub ip_signature: Vec<u8>,
    /// BLS signature of our IP (optional).
    pub ip_bls_signature: Vec<u8>,
    /// Signing time.
    pub signing_time: u64,
    /// Known peers bloom filter.
    pub known_peers_filter: Option<BloomFilter>,
}

impl HandshakeProtocol {
    /// Creates a new handshake protocol with the given configuration.
    pub fn new(node_id: NodeId, network_id: u32, ip: SocketAddr) -> Self {
        let signing_time = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        Self {
            node_id,
            network_id,
            ip,
            tracked_subnets: vec![],
            ip_signature: vec![],
            ip_bls_signature: vec![],
            signing_time,
            known_peers_filter: None,
        }
    }

    /// Creates a handshake protocol from network config.
    pub fn from_config(node_id: NodeId, config: &NetworkConfig) -> Self {
        let ip = SocketAddr::new(config.listen_host, config.staking_port);
        Self::new(node_id, config.network_id, ip)
    }

    /// Sets the TLS signature for our IP.
    pub fn with_ip_signature(mut self, signature: Vec<u8>) -> Self {
        self.ip_signature = signature;
        self
    }

    /// Sets the BLS signature for our IP.
    pub fn with_bls_signature(mut self, signature: Vec<u8>) -> Self {
        self.ip_bls_signature = signature;
        self
    }

    /// Sets the tracked subnets.
    pub fn with_subnets(mut self, subnets: Vec<[u8; 32]>) -> Self {
        self.tracked_subnets = subnets;
        self
    }

    /// Sets the known peers bloom filter.
    pub fn with_known_peers(mut self, filter: BloomFilter) -> Self {
        self.known_peers_filter = Some(filter);
        self
    }

    /// Creates our Handshake message.
    pub fn create_handshake(&self) -> Handshake {
        let my_time = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        let ip_addr = match self.ip.ip() {
            IpAddr::V4(v4) => v4.octets().to_vec(),
            IpAddr::V6(v6) => v6.octets().to_vec(),
        };

        Handshake {
            network_id: self.network_id,
            my_time,
            ip_addr,
            ip_port: self.ip.port() as u32,
            ip_signing_time: self.signing_time,
            ip_node_id_sig: self.ip_signature.clone(),
            tracked_subnets: self.tracked_subnets.iter().map(|s| s.to_vec()).collect(),
            client: Some(Client::new(
                CLIENT_NAME,
                CLIENT_VERSION.0,
                CLIENT_VERSION.1,
                CLIENT_VERSION.2,
            )),
            supported_acps: vec![],
            objected_acps: vec![],
            known_peers: self.known_peers_filter.clone(),
            ip_bls_sig: self.ip_bls_signature.clone(),
            all_subnets: false,
        }
    }

    /// Creates a PeerList response.
    pub fn create_peer_list(&self, peers: Vec<ClaimedIpPort>) -> PeerList {
        PeerList {
            claimed_ip_ports: peers,
        }
    }

    /// Validates a received handshake.
    pub fn validate_handshake(&self, handshake: &Handshake) -> Result<()> {
        // Check network ID
        if handshake.network_id != self.network_id {
            return Err(NetworkError::HandshakeFailed(format!(
                "network ID mismatch: expected {}, got {}",
                self.network_id, handshake.network_id
            )));
        }

        // Check clock skew
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        let skew = if handshake.my_time > now {
            handshake.my_time - now
        } else {
            now - handshake.my_time
        };

        if skew > MAX_CLOCK_SKEW.as_secs() {
            return Err(NetworkError::HandshakeFailed(format!(
                "clock skew too large: {} seconds",
                skew
            )));
        }

        // Check client version compatibility
        if let Some(client) = &handshake.client {
            if !is_compatible_version(client.major, client.minor, client.patch) {
                return Err(NetworkError::HandshakeFailed(format!(
                    "incompatible client version: {}.{}.{}",
                    client.major, client.minor, client.patch
                )));
            }
        }

        // Validate IP address
        if handshake.ip_addr.is_empty() {
            return Err(NetworkError::HandshakeFailed(
                "missing IP address".to_string(),
            ));
        }

        if handshake.ip_port == 0 || handshake.ip_port > 65535 {
            return Err(NetworkError::HandshakeFailed(format!(
                "invalid port: {}",
                handshake.ip_port
            )));
        }

        Ok(())
    }

    /// Extracts peer IP from handshake.
    pub fn extract_peer_ip(handshake: &Handshake) -> Option<SocketAddr> {
        let ip = match handshake.ip_addr.len() {
            4 => {
                let octets: [u8; 4] = handshake.ip_addr[..4].try_into().ok()?;
                IpAddr::from(octets)
            }
            16 => {
                let octets: [u8; 16] = handshake.ip_addr[..16].try_into().ok()?;
                IpAddr::from(octets)
            }
            _ => return None,
        };

        Some(SocketAddr::new(ip, handshake.ip_port as u16))
    }

    /// Performs the handshake over a connection.
    ///
    /// This sends our Handshake, receives the peer's Handshake,
    /// validates it, and exchanges PeerLists.
    pub async fn perform<S>(
        &self,
        stream: &mut S,
        peer_node_id: NodeId,
        known_peers: Vec<ClaimedIpPort>,
    ) -> Result<HandshakeResult>
    where
        S: AsyncReadExt + AsyncWriteExt + Unpin,
    {
        // Create and send our handshake
        let our_handshake = self.create_handshake();
        let handshake_msg = Message::Handshake(Box::new(our_handshake));
        let handshake_bytes = handshake_msg
            .encode()
            .map_err(|e| NetworkError::HandshakeFailed(e.to_string()))?;

        debug!("Sending handshake to {}", peer_node_id);

        // Write length-delimited message
        let len = handshake_bytes.len() as u32;
        stream.write_all(&len.to_be_bytes()).await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to send handshake length: {}", e))
        })?;
        stream.write_all(&handshake_bytes).await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to send handshake: {}", e))
        })?;
        stream.flush().await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to flush handshake: {}", e))
        })?;

        // Receive peer's handshake with timeout
        let peer_handshake = time::timeout(HANDSHAKE_TIMEOUT, async {
            // Read length
            let mut len_buf = [0u8; 4];
            stream.read_exact(&mut len_buf).await.map_err(|e| {
                NetworkError::HandshakeFailed(format!("failed to read handshake length: {}", e))
            })?;
            let len = u32::from_be_bytes(len_buf) as usize;

            if len > 1024 * 1024 {
                return Err(NetworkError::HandshakeFailed(
                    "handshake message too large".to_string(),
                ));
            }

            // Read message
            let mut msg_buf = vec![0u8; len];
            stream.read_exact(&mut msg_buf).await.map_err(|e| {
                NetworkError::HandshakeFailed(format!("failed to read handshake: {}", e))
            })?;

            // Decode message
            let msg = Message::decode(&msg_buf)
                .map_err(|e| NetworkError::HandshakeFailed(format!("failed to decode: {}", e)))?;

            match msg {
                Message::Handshake(h) => Ok(*h),
                _ => Err(NetworkError::HandshakeFailed(
                    "expected Handshake message".to_string(),
                )),
            }
        })
        .await
        .map_err(|_| NetworkError::HandshakeFailed("handshake timeout".to_string()))??;

        debug!("Received handshake from {}", peer_node_id);

        // Validate peer's handshake
        self.validate_handshake(&peer_handshake)?;

        // Extract peer info
        let peer_ip = Self::extract_peer_ip(&peer_handshake).ok_or_else(|| {
            NetworkError::HandshakeFailed("invalid peer IP in handshake".to_string())
        })?;

        // Send our PeerList
        let peer_list = self.create_peer_list(known_peers);
        let peer_list_msg = Message::PeerList(peer_list);
        let peer_list_bytes = peer_list_msg
            .encode()
            .map_err(|e| NetworkError::HandshakeFailed(e.to_string()))?;

        debug!("Sending peer list to {}", peer_node_id);

        let len = peer_list_bytes.len() as u32;
        stream.write_all(&len.to_be_bytes()).await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to send peer list length: {}", e))
        })?;
        stream.write_all(&peer_list_bytes).await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to send peer list: {}", e))
        })?;
        stream.flush().await.map_err(|e| {
            NetworkError::HandshakeFailed(format!("failed to flush peer list: {}", e))
        })?;

        // Receive peer's PeerList
        let peer_list = time::timeout(HANDSHAKE_TIMEOUT, async {
            let mut len_buf = [0u8; 4];
            stream.read_exact(&mut len_buf).await.map_err(|e| {
                NetworkError::HandshakeFailed(format!("failed to read peer list length: {}", e))
            })?;
            let len = u32::from_be_bytes(len_buf) as usize;

            if len > 10 * 1024 * 1024 {
                return Err(NetworkError::HandshakeFailed(
                    "peer list message too large".to_string(),
                ));
            }

            let mut msg_buf = vec![0u8; len];
            stream.read_exact(&mut msg_buf).await.map_err(|e| {
                NetworkError::HandshakeFailed(format!("failed to read peer list: {}", e))
            })?;

            let msg = Message::decode(&msg_buf)
                .map_err(|e| NetworkError::HandshakeFailed(format!("failed to decode: {}", e)))?;

            match msg {
                Message::PeerList(pl) => Ok(pl),
                _ => Err(NetworkError::HandshakeFailed(
                    "expected PeerList message".to_string(),
                )),
            }
        })
        .await
        .map_err(|_| NetworkError::HandshakeFailed("peer list timeout".to_string()))??;

        debug!(
            "Received peer list from {} with {} peers",
            peer_node_id,
            peer_list.claimed_ip_ports.len()
        );

        // Extract tracked subnets
        let tracked_subnets: Vec<[u8; 32]> = peer_handshake
            .tracked_subnets
            .iter()
            .filter_map(|s| {
                if s.len() == 32 {
                    let mut arr = [0u8; 32];
                    arr.copy_from_slice(s);
                    Some(arr)
                } else {
                    None
                }
            })
            .collect();

        Ok(HandshakeResult {
            node_id: peer_node_id,
            ip: peer_ip,
            client: peer_handshake.client,
            network_id: peer_handshake.network_id,
            tracked_subnets,
            discovered_peers: peer_list.claimed_ip_ports,
        })
    }
}

/// Checks if a client version is compatible.
fn is_compatible_version(major: u32, minor: u32, patch: u32) -> bool {
    // We accept any version for now - in production you'd check minimum version
    // (major, minor, patch) >= MIN_COMPATIBLE_VERSION
    let version = (major, minor, patch);
    version >= MIN_COMPATIBLE_VERSION || major == 0 // Allow dev versions
}

/// Creates a ClaimedIpPort from peer info.
pub fn create_claimed_ip_port(
    cert_der: &[u8],
    ip: SocketAddr,
    timestamp: u64,
    signature: &[u8],
    tx_id: &[u8],
) -> ClaimedIpPort {
    let ip_addr = match ip.ip() {
        IpAddr::V4(v4) => v4.octets().to_vec(),
        IpAddr::V6(v6) => v6.octets().to_vec(),
    };

    ClaimedIpPort {
        x509_certificate: cert_der.to_vec(),
        ip_addr,
        ip_port: ip.port() as u32,
        timestamp,
        signature: signature.to_vec(),
        tx_id: tx_id.to_vec(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_create_handshake() {
        let node_id = NodeId::from_bytes([1u8; 20]);
        let ip = "127.0.0.1:9651".parse().unwrap();
        let protocol = HandshakeProtocol::new(node_id, 1, ip);

        let handshake = protocol.create_handshake();

        assert_eq!(handshake.network_id, 1);
        assert_eq!(handshake.ip_addr, vec![127, 0, 0, 1]);
        assert_eq!(handshake.ip_port, 9651);
        assert!(handshake.client.is_some());

        let client = handshake.client.unwrap();
        assert_eq!(client.name, CLIENT_NAME);
    }

    #[test]
    fn test_validate_handshake_network_mismatch() {
        let node_id = NodeId::from_bytes([1u8; 20]);
        let ip = "127.0.0.1:9651".parse().unwrap();
        let protocol = HandshakeProtocol::new(node_id, 1, ip);

        let mut handshake = protocol.create_handshake();
        handshake.network_id = 999; // Different network

        let result = protocol.validate_handshake(&handshake);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("network ID"));
    }

    #[test]
    fn test_validate_handshake_invalid_port() {
        let node_id = NodeId::from_bytes([1u8; 20]);
        let ip = "127.0.0.1:9651".parse().unwrap();
        let protocol = HandshakeProtocol::new(node_id, 1, ip);

        let mut handshake = protocol.create_handshake();
        handshake.ip_port = 0; // Invalid port

        let result = protocol.validate_handshake(&handshake);
        assert!(result.is_err());
    }

    #[test]
    fn test_extract_peer_ip_v4() {
        let handshake = Handshake {
            ip_addr: vec![192, 168, 1, 1],
            ip_port: 9651,
            ..Default::default()
        };

        let ip = HandshakeProtocol::extract_peer_ip(&handshake).unwrap();
        assert_eq!(ip.to_string(), "192.168.1.1:9651");
    }

    #[test]
    fn test_extract_peer_ip_v6() {
        let handshake = Handshake {
            ip_addr: vec![0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1],
            ip_port: 9651,
            ..Default::default()
        };

        let ip = HandshakeProtocol::extract_peer_ip(&handshake).unwrap();
        assert_eq!(ip.to_string(), "[::1]:9651");
    }

    #[test]
    fn test_version_compatibility() {
        // Dev versions allowed
        assert!(is_compatible_version(0, 1, 0));

        // Minimum version
        assert!(is_compatible_version(1, 11, 0));

        // Above minimum
        assert!(is_compatible_version(1, 12, 0));
        assert!(is_compatible_version(2, 0, 0));

        // Below minimum
        assert!(!is_compatible_version(1, 10, 0));
        assert!(!is_compatible_version(1, 0, 0));
    }

    #[test]
    fn test_create_peer_list() {
        let node_id = NodeId::from_bytes([1u8; 20]);
        let ip = "127.0.0.1:9651".parse().unwrap();
        let protocol = HandshakeProtocol::new(node_id, 1, ip);

        let peers = vec![create_claimed_ip_port(
            &[1, 2, 3],
            "192.168.1.1:9651".parse().unwrap(),
            12345,
            &[4, 5, 6],
            &[7, 8, 9],
        )];

        let peer_list = protocol.create_peer_list(peers);
        assert_eq!(peer_list.claimed_ip_ports.len(), 1);
    }
}
