//! Avalanche Warp Messaging (AWM) implementation.
//!
//! Warp messaging enables cross-subnet communication using BLS signatures.
//! Validators sign messages and these signatures are aggregated to prove
//! that a sufficient stake weight has attested to the message.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use avalanche_ids::{Id, NodeId};
use parking_lot::RwLock;
use sha2::{Digest, Sha256};
use thiserror::Error;

/// Warp message ID (hash of the message).
pub type MessageId = [u8; 32];

/// A Warp message that can be sent between subnets.
#[derive(Debug, Clone)]
pub struct WarpMessage {
    /// Unique message ID (hash).
    pub id: MessageId,
    /// Source subnet ID.
    pub source_subnet_id: Id,
    /// Source blockchain ID.
    pub source_chain_id: Id,
    /// Destination subnet ID (empty for any subnet).
    pub destination_subnet_id: Option<Id>,
    /// Message payload.
    pub payload: Vec<u8>,
}

impl WarpMessage {
    /// Creates a new Warp message.
    pub fn new(
        source_subnet_id: Id,
        source_chain_id: Id,
        payload: Vec<u8>,
    ) -> Self {
        let id = Self::compute_id(&source_subnet_id, &source_chain_id, &payload);
        Self {
            id,
            source_subnet_id,
            source_chain_id,
            destination_subnet_id: None,
            payload,
        }
    }

    /// Creates a message with a specific destination.
    pub fn with_destination(
        source_subnet_id: Id,
        source_chain_id: Id,
        destination_subnet_id: Id,
        payload: Vec<u8>,
    ) -> Self {
        let mut msg = Self::new(source_subnet_id, source_chain_id, payload);
        msg.destination_subnet_id = Some(destination_subnet_id);
        msg
    }

    /// Computes the message ID.
    fn compute_id(subnet_id: &Id, chain_id: &Id, payload: &[u8]) -> MessageId {
        let mut hasher = Sha256::new();
        hasher.update(subnet_id.as_ref());
        hasher.update(chain_id.as_ref());
        hasher.update(payload);
        let result = hasher.finalize();
        let mut id = [0u8; 32];
        id.copy_from_slice(&result);
        id
    }

    /// Returns the bytes to be signed.
    pub fn unsigned_bytes(&self) -> Vec<u8> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(self.source_subnet_id.as_ref());
        bytes.extend_from_slice(self.source_chain_id.as_ref());
        if let Some(dest) = &self.destination_subnet_id {
            bytes.extend_from_slice(dest.as_ref());
        }
        bytes.extend_from_slice(&self.payload);
        bytes
    }

    /// Serializes the message.
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(&self.id);
        bytes.extend_from_slice(self.source_subnet_id.as_ref());
        bytes.extend_from_slice(self.source_chain_id.as_ref());
        let has_dest = self.destination_subnet_id.is_some() as u8;
        bytes.push(has_dest);
        if let Some(dest) = &self.destination_subnet_id {
            bytes.extend_from_slice(dest.as_ref());
        }
        bytes.extend_from_slice(&(self.payload.len() as u32).to_be_bytes());
        bytes.extend_from_slice(&self.payload);
        bytes
    }

    /// Deserializes a message from bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self, WarpError> {
        if bytes.len() < 32 + 32 + 32 + 1 + 4 {
            return Err(WarpError::InvalidMessage("message too short".to_string()));
        }

        let mut offset = 0;

        let mut id = [0u8; 32];
        id.copy_from_slice(&bytes[offset..offset + 32]);
        offset += 32;

        let source_subnet_id = Id::from_slice(&bytes[offset..offset + 32])
            .map_err(|_| WarpError::InvalidMessage("invalid source subnet ID".to_string()))?;
        offset += 32;

        let source_chain_id = Id::from_slice(&bytes[offset..offset + 32])
            .map_err(|_| WarpError::InvalidMessage("invalid source chain ID".to_string()))?;
        offset += 32;

        let has_dest = bytes[offset] != 0;
        offset += 1;

        let destination_subnet_id = if has_dest {
            if bytes.len() < offset + 32 {
                return Err(WarpError::InvalidMessage("missing destination".to_string()));
            }
            let dest = Id::from_slice(&bytes[offset..offset + 32])
                .map_err(|_| WarpError::InvalidMessage("invalid destination ID".to_string()))?;
            offset += 32;
            Some(dest)
        } else {
            None
        };

        if bytes.len() < offset + 4 {
            return Err(WarpError::InvalidMessage("missing payload length".to_string()));
        }
        let payload_len = u32::from_be_bytes(bytes[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4;

        if bytes.len() < offset + payload_len {
            return Err(WarpError::InvalidMessage("payload truncated".to_string()));
        }
        let payload = bytes[offset..offset + payload_len].to_vec();

        Ok(Self {
            id,
            source_subnet_id,
            source_chain_id,
            destination_subnet_id,
            payload,
        })
    }
}

/// A BLS signature on a Warp message.
#[derive(Debug, Clone)]
pub struct WarpSignature {
    /// Signer's node ID.
    pub signer: NodeId,
    /// BLS signature bytes (96 bytes).
    pub signature: Vec<u8>,
}

/// Aggregated signature on a Warp message.
#[derive(Debug, Clone)]
pub struct AggregatedSignature {
    /// Aggregated BLS signature.
    pub signature: Vec<u8>,
    /// Bitmap of signers (by validator index).
    pub signer_bitmap: Vec<u8>,
    /// Total stake weight that signed.
    pub total_weight: u64,
    /// Number of signers.
    pub signer_count: u32,
}

impl AggregatedSignature {
    /// Creates an empty aggregated signature.
    pub fn empty() -> Self {
        Self {
            signature: Vec::new(),
            signer_bitmap: Vec::new(),
            total_weight: 0,
            signer_count: 0,
        }
    }

    /// Returns whether the signature has sufficient weight.
    pub fn has_quorum(&self, total_stake: u64, threshold_percentage: u8) -> bool {
        if total_stake == 0 {
            return false;
        }
        let required = (total_stake as u128 * threshold_percentage as u128 / 100) as u64;
        self.total_weight >= required
    }
}

/// A signed Warp message with aggregated signatures.
#[derive(Debug, Clone)]
pub struct SignedWarpMessage {
    /// The underlying message.
    pub message: WarpMessage,
    /// Aggregated signature.
    pub signature: AggregatedSignature,
}

impl SignedWarpMessage {
    /// Creates a new signed message.
    pub fn new(message: WarpMessage, signature: AggregatedSignature) -> Self {
        Self { message, signature }
    }

    /// Serializes to bytes.
    pub fn to_bytes(&self) -> Vec<u8> {
        let msg_bytes = self.message.to_bytes();
        let mut bytes = Vec::new();
        bytes.extend_from_slice(&(msg_bytes.len() as u32).to_be_bytes());
        bytes.extend_from_slice(&msg_bytes);
        bytes.extend_from_slice(&(self.signature.signature.len() as u32).to_be_bytes());
        bytes.extend_from_slice(&self.signature.signature);
        bytes.extend_from_slice(&(self.signature.signer_bitmap.len() as u32).to_be_bytes());
        bytes.extend_from_slice(&self.signature.signer_bitmap);
        bytes.extend_from_slice(&self.signature.total_weight.to_be_bytes());
        bytes.extend_from_slice(&self.signature.signer_count.to_be_bytes());
        bytes
    }
}

/// Warp message request for signature collection.
#[derive(Debug, Clone)]
pub struct SignatureRequest {
    /// Message to sign.
    pub message_id: MessageId,
    /// Requesting node.
    pub requester: NodeId,
    /// When the request was made.
    pub timestamp: Instant,
}

/// Pending signature collection for a message.
#[derive(Debug)]
pub struct PendingSignature {
    /// The message.
    pub message: WarpMessage,
    /// Collected signatures by node ID.
    pub signatures: HashMap<NodeId, WarpSignature>,
    /// When collection started.
    pub started: Instant,
    /// Request timeout.
    pub timeout: Duration,
}

impl PendingSignature {
    /// Creates a new pending signature collection.
    pub fn new(message: WarpMessage, timeout: Duration) -> Self {
        Self {
            message,
            signatures: HashMap::new(),
            started: Instant::now(),
            timeout,
        }
    }

    /// Adds a signature.
    pub fn add_signature(&mut self, sig: WarpSignature) {
        self.signatures.insert(sig.signer, sig);
    }

    /// Returns whether collection has timed out.
    pub fn is_expired(&self) -> bool {
        self.started.elapsed() >= self.timeout
    }

    /// Returns collected signature count.
    pub fn signature_count(&self) -> usize {
        self.signatures.len()
    }
}

/// Warp message manager.
pub struct WarpManager {
    /// This node's ID.
    node_id: NodeId,
    /// Subnet ID this manager is for.
    subnet_id: Id,
    /// Pending signature collections.
    pending: RwLock<HashMap<MessageId, PendingSignature>>,
    /// Received messages (cache).
    received_messages: RwLock<HashMap<MessageId, SignedWarpMessage>>,
    /// Configuration.
    config: WarpConfig,
}

/// Warp configuration.
#[derive(Debug, Clone)]
pub struct WarpConfig {
    /// Signature collection timeout.
    pub signature_timeout: Duration,
    /// Maximum messages to cache.
    pub max_cached_messages: usize,
    /// Quorum threshold percentage (67 = 67%).
    pub quorum_threshold: u8,
}

impl Default for WarpConfig {
    fn default() -> Self {
        Self {
            signature_timeout: Duration::from_secs(30),
            max_cached_messages: 1000,
            quorum_threshold: 67,
        }
    }
}

impl WarpManager {
    /// Creates a new Warp manager.
    pub fn new(node_id: NodeId, subnet_id: Id, config: WarpConfig) -> Self {
        Self {
            node_id,
            subnet_id,
            pending: RwLock::new(HashMap::new()),
            received_messages: RwLock::new(HashMap::new()),
            config,
        }
    }

    /// Initiates signature collection for a message.
    pub fn request_signatures(&self, message: WarpMessage) -> MessageId {
        let id = message.id;
        let pending = PendingSignature::new(message, self.config.signature_timeout);
        self.pending.write().insert(id, pending);
        id
    }

    /// Adds a signature response.
    pub fn add_signature(&self, message_id: &MessageId, signature: WarpSignature) -> Result<(), WarpError> {
        let mut pending = self.pending.write();
        let collection = pending.get_mut(message_id)
            .ok_or(WarpError::MessageNotFound(*message_id))?;

        if collection.is_expired() {
            return Err(WarpError::SignatureTimeout);
        }

        collection.add_signature(signature);
        Ok(())
    }

    /// Checks if a message has enough signatures.
    pub fn check_quorum(&self, message_id: &MessageId, validator_weights: &HashMap<NodeId, u64>) -> Option<AggregatedSignature> {
        let pending = self.pending.read();
        let collection = pending.get(message_id)?;

        let total_stake: u64 = validator_weights.values().sum();
        let signed_stake: u64 = collection
            .signatures
            .keys()
            .filter_map(|node| validator_weights.get(node))
            .sum();

        let threshold = self.config.quorum_threshold;
        let required = (total_stake as u128 * threshold as u128 / 100) as u64;

        if signed_stake >= required {
            // Create aggregated signature
            // In production, this would actually aggregate BLS signatures
            let mut signer_bitmap = Vec::new();
            for (i, (node_id, _)) in validator_weights.iter().enumerate() {
                let byte_idx = i / 8;
                let bit_idx = i % 8;
                while signer_bitmap.len() <= byte_idx {
                    signer_bitmap.push(0);
                }
                if collection.signatures.contains_key(node_id) {
                    signer_bitmap[byte_idx] |= 1 << bit_idx;
                }
            }

            // Concatenate signatures (placeholder for BLS aggregation)
            let signature: Vec<u8> = collection
                .signatures
                .values()
                .flat_map(|s| s.signature.clone())
                .collect();

            Some(AggregatedSignature {
                signature,
                signer_bitmap,
                total_weight: signed_stake,
                signer_count: collection.signatures.len() as u32,
            })
        } else {
            None
        }
    }

    /// Completes signature collection and returns signed message.
    pub fn complete(&self, message_id: &MessageId, validator_weights: &HashMap<NodeId, u64>) -> Result<SignedWarpMessage, WarpError> {
        let sig = self.check_quorum(message_id, validator_weights)
            .ok_or(WarpError::InsufficientSignatures)?;

        let mut pending = self.pending.write();
        let collection = pending.remove(message_id)
            .ok_or(WarpError::MessageNotFound(*message_id))?;

        let signed = SignedWarpMessage::new(collection.message, sig);

        // Cache the signed message
        let mut cache = self.received_messages.write();
        if cache.len() >= self.config.max_cached_messages {
            // Remove oldest entry
            if let Some(oldest) = cache.keys().next().cloned() {
                cache.remove(&oldest);
            }
        }
        cache.insert(*message_id, signed.clone());

        Ok(signed)
    }

    /// Stores a received signed message.
    pub fn store_received(&self, message: SignedWarpMessage) {
        let mut cache = self.received_messages.write();
        if cache.len() >= self.config.max_cached_messages {
            if let Some(oldest) = cache.keys().next().cloned() {
                cache.remove(&oldest);
            }
        }
        cache.insert(message.message.id, message);
    }

    /// Gets a cached message.
    pub fn get_message(&self, id: &MessageId) -> Option<SignedWarpMessage> {
        self.received_messages.read().get(id).cloned()
    }

    /// Verifies a signed message has sufficient weight.
    pub fn verify_message(&self, message: &SignedWarpMessage, total_stake: u64) -> bool {
        message.signature.has_quorum(total_stake, self.config.quorum_threshold)
    }

    /// Cleans up expired pending signatures.
    pub fn cleanup(&self) {
        let mut pending = self.pending.write();
        pending.retain(|_, p| !p.is_expired());
    }

    /// Returns the number of pending signature collections.
    pub fn pending_count(&self) -> usize {
        self.pending.read().len()
    }

    /// Returns the number of cached messages.
    pub fn cached_count(&self) -> usize {
        self.received_messages.read().len()
    }
}

/// Warp errors.
#[derive(Debug, Error)]
pub enum WarpError {
    #[error("invalid message: {0}")]
    InvalidMessage(String),

    #[error("message not found: {0:?}")]
    MessageNotFound(MessageId),

    #[error("signature collection timeout")]
    SignatureTimeout,

    #[error("insufficient signatures for quorum")]
    InsufficientSignatures,

    #[error("invalid signature: {0}")]
    InvalidSignature(String),

    #[error("verification failed: {0}")]
    VerificationFailed(String),
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_id(byte: u8) -> Id {
        Id::from_slice(&[byte; 32]).unwrap()
    }

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    fn make_signature(node: u8) -> WarpSignature {
        WarpSignature {
            signer: make_node_id(node),
            signature: vec![node; 96],
        }
    }

    #[test]
    fn test_warp_message() {
        let msg = WarpMessage::new(
            make_id(1),
            make_id(2),
            b"hello world".to_vec(),
        );

        assert_eq!(msg.source_subnet_id, make_id(1));
        assert_eq!(msg.source_chain_id, make_id(2));
        assert!(msg.destination_subnet_id.is_none());

        // Serialize and deserialize
        let bytes = msg.to_bytes();
        let parsed = WarpMessage::from_bytes(&bytes).unwrap();

        assert_eq!(parsed.id, msg.id);
        assert_eq!(parsed.payload, msg.payload);
    }

    #[test]
    fn test_warp_message_with_destination() {
        let msg = WarpMessage::with_destination(
            make_id(1),
            make_id(2),
            make_id(3),
            b"cross-subnet".to_vec(),
        );

        assert!(msg.destination_subnet_id.is_some());
        assert_eq!(msg.destination_subnet_id.unwrap(), make_id(3));

        let bytes = msg.to_bytes();
        let parsed = WarpMessage::from_bytes(&bytes).unwrap();
        assert_eq!(parsed.destination_subnet_id, msg.destination_subnet_id);
    }

    #[test]
    fn test_aggregated_signature_quorum() {
        let sig = AggregatedSignature {
            signature: vec![1, 2, 3],
            signer_bitmap: vec![0xFF],
            total_weight: 70,
            signer_count: 4,
        };

        // 70% with 67% threshold should pass
        assert!(sig.has_quorum(100, 67));

        // 60% with 67% threshold should fail
        let sig2 = AggregatedSignature {
            total_weight: 60,
            ..sig.clone()
        };
        assert!(!sig2.has_quorum(100, 67));
    }

    #[test]
    fn test_pending_signature() {
        let msg = WarpMessage::new(make_id(1), make_id(2), vec![]);
        let mut pending = PendingSignature::new(msg, Duration::from_secs(30));

        assert_eq!(pending.signature_count(), 0);
        assert!(!pending.is_expired());

        pending.add_signature(make_signature(1));
        pending.add_signature(make_signature(2));

        assert_eq!(pending.signature_count(), 2);
    }

    #[test]
    fn test_warp_manager() {
        let manager = WarpManager::new(
            make_node_id(0),
            make_id(1),
            WarpConfig::default(),
        );

        let msg = WarpMessage::new(make_id(1), make_id(2), b"test".to_vec());
        let msg_id = manager.request_signatures(msg);

        assert_eq!(manager.pending_count(), 1);

        // Add signatures
        manager.add_signature(&msg_id, make_signature(1)).unwrap();
        manager.add_signature(&msg_id, make_signature(2)).unwrap();
        manager.add_signature(&msg_id, make_signature(3)).unwrap();

        // Check quorum with weights
        let mut weights = HashMap::new();
        weights.insert(make_node_id(1), 30);
        weights.insert(make_node_id(2), 30);
        weights.insert(make_node_id(3), 20);
        weights.insert(make_node_id(4), 20);

        let agg = manager.check_quorum(&msg_id, &weights);
        assert!(agg.is_some());

        let agg = agg.unwrap();
        assert_eq!(agg.total_weight, 80); // nodes 1, 2, 3 = 30+30+20
    }

    #[test]
    fn test_complete_signature_collection() {
        let manager = WarpManager::new(
            make_node_id(0),
            make_id(1),
            WarpConfig::default(),
        );

        let msg = WarpMessage::new(make_id(1), make_id(2), b"test".to_vec());
        let msg_id = manager.request_signatures(msg);

        manager.add_signature(&msg_id, make_signature(1)).unwrap();
        manager.add_signature(&msg_id, make_signature(2)).unwrap();

        let mut weights = HashMap::new();
        weights.insert(make_node_id(1), 50);
        weights.insert(make_node_id(2), 50);

        let signed = manager.complete(&msg_id, &weights).unwrap();
        assert!(signed.signature.has_quorum(100, 67));

        // Should be cached
        assert!(manager.get_message(&msg_id).is_some());
        assert_eq!(manager.pending_count(), 0);
    }

    #[test]
    fn test_insufficient_signatures() {
        let manager = WarpManager::new(
            make_node_id(0),
            make_id(1),
            WarpConfig::default(),
        );

        let msg = WarpMessage::new(make_id(1), make_id(2), b"test".to_vec());
        let msg_id = manager.request_signatures(msg);

        manager.add_signature(&msg_id, make_signature(1)).unwrap();

        let mut weights = HashMap::new();
        weights.insert(make_node_id(1), 30);
        weights.insert(make_node_id(2), 70);

        // Only 30% signed, need 67%
        let result = manager.complete(&msg_id, &weights);
        assert!(matches!(result, Err(WarpError::InsufficientSignatures)));
    }

    #[test]
    fn test_message_not_found() {
        let manager = WarpManager::new(
            make_node_id(0),
            make_id(1),
            WarpConfig::default(),
        );

        let fake_id = [0u8; 32];
        let result = manager.add_signature(&fake_id, make_signature(1));
        assert!(matches!(result, Err(WarpError::MessageNotFound(_))));
    }

    #[test]
    fn test_cleanup() {
        let config = WarpConfig {
            signature_timeout: Duration::from_millis(1),
            ..Default::default()
        };
        let manager = WarpManager::new(make_node_id(0), make_id(1), config);

        let msg = WarpMessage::new(make_id(1), make_id(2), vec![]);
        manager.request_signatures(msg);
        assert_eq!(manager.pending_count(), 1);

        std::thread::sleep(Duration::from_millis(10));
        manager.cleanup();
        assert_eq!(manager.pending_count(), 0);
    }
}
