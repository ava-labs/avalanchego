//! P2P protocol message types.
//!
//! These types are compatible with the protobuf wire format used by the Go implementation.

use bytes::{Buf, BufMut, Bytes, BytesMut};
use prost::Message as ProstMessage;
use thiserror::Error;

// Note: avalanche_ids types will be used in future integration

/// Errors that can occur when encoding/decoding messages.
#[derive(Debug, Error)]
pub enum MessageError {
    #[error("insufficient data: need {needed} bytes, have {available}")]
    InsufficientData { needed: usize, available: usize },

    #[error("invalid message type: {0}")]
    InvalidMessageType(u32),

    #[error("protobuf decode error: {0}")]
    ProtobufDecode(#[from] prost::DecodeError),

    #[error("protobuf encode error: {0}")]
    ProtobufEncode(#[from] prost::EncodeError),

    #[error("compression error: {0}")]
    Compression(String),

    #[error("decompression error: {0}")]
    Decompression(String),

    #[error("invalid field: {0}")]
    InvalidField(String),
}

/// The main P2P message envelope.
#[derive(Clone, Debug, PartialEq)]
pub enum Message {
    /// Zstd-compressed message bytes
    CompressedZstd(Bytes),

    // Network messages
    Ping(Ping),
    Pong(Pong),
    Handshake(Box<Handshake>),
    GetPeerList(GetPeerList),
    PeerList(PeerList),

    // State sync messages
    GetStateSummaryFrontier(GetStateSummaryFrontier),
    StateSummaryFrontier(StateSummaryFrontier),
    GetAcceptedStateSummary(GetAcceptedStateSummary),
    AcceptedStateSummary(AcceptedStateSummary),

    // Bootstrapping messages
    GetAcceptedFrontier(GetAcceptedFrontier),
    AcceptedFrontier(AcceptedFrontier),
    GetAccepted(GetAccepted),
    Accepted(Accepted),
    GetAncestors(GetAncestors),
    Ancestors(Ancestors),

    // Consensus messages
    Get(Get),
    Put(Put),
    PushQuery(PushQuery),
    PullQuery(PullQuery),
    Chits(Chits),

    // App messages
    AppRequest(AppRequest),
    AppResponse(AppResponse),
    AppGossip(AppGossip),
    AppError(AppError),
}

impl Message {
    /// Encodes this message to bytes using protobuf encoding.
    pub fn encode(&self) -> Result<Bytes, MessageError> {
        let mut buf = BytesMut::with_capacity(256);
        self.encode_to_buf(&mut buf)?;
        Ok(buf.freeze())
    }

    /// Encodes this message to the given buffer.
    pub fn encode_to_buf(&self, buf: &mut BytesMut) -> Result<(), MessageError> {
        // We use a simple encoding format compatible with protobuf oneof
        match self {
            Self::CompressedZstd(data) => {
                encode_field(buf, 2, data);
            }
            Self::Ping(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 11, &encoded);
            }
            Self::Pong(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 12, &encoded);
            }
            Self::Handshake(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 13, &encoded);
            }
            Self::GetPeerList(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 35, &encoded);
            }
            Self::PeerList(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 14, &encoded);
            }
            Self::GetStateSummaryFrontier(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 15, &encoded);
            }
            Self::StateSummaryFrontier(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 16, &encoded);
            }
            Self::GetAcceptedStateSummary(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 17, &encoded);
            }
            Self::AcceptedStateSummary(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 18, &encoded);
            }
            Self::GetAcceptedFrontier(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 19, &encoded);
            }
            Self::AcceptedFrontier(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 20, &encoded);
            }
            Self::GetAccepted(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 21, &encoded);
            }
            Self::Accepted(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 22, &encoded);
            }
            Self::GetAncestors(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 23, &encoded);
            }
            Self::Ancestors(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 24, &encoded);
            }
            Self::Get(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 25, &encoded);
            }
            Self::Put(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 26, &encoded);
            }
            Self::PushQuery(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 27, &encoded);
            }
            Self::PullQuery(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 28, &encoded);
            }
            Self::Chits(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 29, &encoded);
            }
            Self::AppRequest(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 30, &encoded);
            }
            Self::AppResponse(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 31, &encoded);
            }
            Self::AppGossip(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 32, &encoded);
            }
            Self::AppError(msg) => {
                let encoded = msg.encode_to_vec();
                encode_field(buf, 34, &encoded);
            }
        }
        Ok(())
    }

    /// Decodes a message from bytes.
    pub fn decode(mut buf: &[u8]) -> Result<Self, MessageError> {
        while buf.has_remaining() {
            let (field_number, wire_type) = decode_key(&mut buf)?;

            // Wire type 2 = length-delimited
            if wire_type != 2 {
                skip_field(&mut buf, wire_type)?;
                continue;
            }

            let len = decode_varint(&mut buf)? as usize;
            if buf.remaining() < len {
                return Err(MessageError::InsufficientData {
                    needed: len,
                    available: buf.remaining(),
                });
            }

            let data = &buf[..len];
            buf.advance(len);

            return match field_number {
                2 => Ok(Self::CompressedZstd(Bytes::copy_from_slice(data))),
                11 => Ok(Self::Ping(Ping::decode(data)?)),
                12 => Ok(Self::Pong(Pong::decode(data)?)),
                13 => Ok(Self::Handshake(Box::new(Handshake::decode(data)?))),
                14 => Ok(Self::PeerList(PeerList::decode(data)?)),
                35 => Ok(Self::GetPeerList(GetPeerList::decode(data)?)),
                15 => Ok(Self::GetStateSummaryFrontier(GetStateSummaryFrontier::decode(data)?)),
                16 => Ok(Self::StateSummaryFrontier(StateSummaryFrontier::decode(data)?)),
                17 => Ok(Self::GetAcceptedStateSummary(GetAcceptedStateSummary::decode(data)?)),
                18 => Ok(Self::AcceptedStateSummary(AcceptedStateSummary::decode(data)?)),
                19 => Ok(Self::GetAcceptedFrontier(GetAcceptedFrontier::decode(data)?)),
                20 => Ok(Self::AcceptedFrontier(AcceptedFrontier::decode(data)?)),
                21 => Ok(Self::GetAccepted(GetAccepted::decode(data)?)),
                22 => Ok(Self::Accepted(Accepted::decode(data)?)),
                23 => Ok(Self::GetAncestors(GetAncestors::decode(data)?)),
                24 => Ok(Self::Ancestors(Ancestors::decode(data)?)),
                25 => Ok(Self::Get(Get::decode(data)?)),
                26 => Ok(Self::Put(Put::decode(data)?)),
                27 => Ok(Self::PushQuery(PushQuery::decode(data)?)),
                28 => Ok(Self::PullQuery(PullQuery::decode(data)?)),
                29 => Ok(Self::Chits(Chits::decode(data)?)),
                30 => Ok(Self::AppRequest(AppRequest::decode(data)?)),
                31 => Ok(Self::AppResponse(AppResponse::decode(data)?)),
                32 => Ok(Self::AppGossip(AppGossip::decode(data)?)),
                34 => Ok(Self::AppError(AppError::decode(data)?)),
                _ => Err(MessageError::InvalidMessageType(field_number)),
            };
        }

        Err(MessageError::InsufficientData {
            needed: 1,
            available: 0,
        })
    }

    /// Compresses this message using zstd if compression is beneficial.
    pub fn compress(&self) -> Result<Self, MessageError> {
        // Don't double-compress
        if matches!(self, Self::CompressedZstd(_)) {
            return Ok(self.clone());
        }

        // Don't compress small messages
        let uncompressed = self.encode()?;
        if uncompressed.len() < 128 {
            return Ok(self.clone());
        }

        let compressed = zstd::encode_all(uncompressed.as_ref(), 3)
            .map_err(|e| MessageError::Compression(e.to_string()))?;

        // Only use compression if it actually saves space
        if compressed.len() < uncompressed.len() {
            Ok(Self::CompressedZstd(Bytes::from(compressed)))
        } else {
            Ok(self.clone())
        }
    }

    /// Decompresses this message if it's compressed.
    pub fn decompress(&self) -> Result<Self, MessageError> {
        match self {
            Self::CompressedZstd(data) => {
                let decompressed = zstd::decode_all(data.as_ref())
                    .map_err(|e| MessageError::Decompression(e.to_string()))?;
                Self::decode(&decompressed)
            }
            _ => Ok(self.clone()),
        }
    }
}

// ============================================================================
// Network Messages
// ============================================================================

/// Ping reports a peer's perceived uptime percentage.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Ping {
    /// Uptime percentage on the primary network [0, 100]
    #[prost(uint32, tag = "1")]
    pub uptime: u32,
}

/// Pong is sent in response to a Ping.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Pong {}

/// Client version metadata.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Client {
    /// Client name (e.g., "avalanchego")
    #[prost(string, tag = "1")]
    pub name: String,
    /// Major version
    #[prost(uint32, tag = "2")]
    pub major: u32,
    /// Minor version
    #[prost(uint32, tag = "3")]
    pub minor: u32,
    /// Patch version
    #[prost(uint32, tag = "4")]
    pub patch: u32,
}

impl Client {
    /// Creates a new client version.
    #[must_use]
    pub fn new(name: impl Into<String>, major: u32, minor: u32, patch: u32) -> Self {
        Self {
            name: name.into(),
            major,
            minor,
            patch,
        }
    }

    /// Returns the version string (e.g., "1.2.3").
    #[must_use]
    pub fn version_string(&self) -> String {
        format!("{}.{}.{}", self.major, self.minor, self.patch)
    }
}

/// Bloom filter with random salt.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct BloomFilter {
    /// Filter bytes
    #[prost(bytes = "vec", tag = "1")]
    pub filter: Vec<u8>,
    /// Random salt (32 bytes)
    #[prost(bytes = "vec", tag = "2")]
    pub salt: Vec<u8>,
}

/// Handshake is the first message sent when establishing a connection.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Handshake {
    /// Network ID (e.g., 1 for mainnet, 5 for fuji)
    #[prost(uint32, tag = "1")]
    pub network_id: u32,
    /// Unix timestamp when this message was created
    #[prost(uint64, tag = "2")]
    pub my_time: u64,
    /// IP address (4 or 16 bytes)
    #[prost(bytes = "vec", tag = "3")]
    pub ip_addr: Vec<u8>,
    /// IP port
    #[prost(uint32, tag = "4")]
    pub ip_port: u32,
    /// Timestamp of the IP signing
    #[prost(uint64, tag = "6")]
    pub ip_signing_time: u64,
    /// TLS signature of the IP
    #[prost(bytes = "vec", tag = "7")]
    pub ip_node_id_sig: Vec<u8>,
    /// Tracked subnet IDs (32 bytes each)
    #[prost(bytes = "vec", repeated, tag = "8")]
    pub tracked_subnets: Vec<Vec<u8>>,
    /// Client version
    #[prost(message, optional, tag = "9")]
    pub client: Option<Client>,
    /// Supported ACPs
    #[prost(uint32, repeated, tag = "10")]
    pub supported_acps: Vec<u32>,
    /// Objected ACPs
    #[prost(uint32, repeated, tag = "11")]
    pub objected_acps: Vec<u32>,
    /// Known peers bloom filter
    #[prost(message, optional, tag = "12")]
    pub known_peers: Option<BloomFilter>,
    /// BLS signature of the IP
    #[prost(bytes = "vec", tag = "13")]
    pub ip_bls_sig: Vec<u8>,
    /// Whether tracking all subnets
    #[prost(bool, tag = "14")]
    pub all_subnets: bool,
}

/// Claimed IP and port with certificate.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct ClaimedIpPort {
    /// X509 certificate (DER encoded)
    #[prost(bytes = "vec", tag = "1")]
    pub x509_certificate: Vec<u8>,
    /// IP address (4 or 16 bytes)
    #[prost(bytes = "vec", tag = "2")]
    pub ip_addr: Vec<u8>,
    /// IP port
    #[prost(uint32, tag = "3")]
    pub ip_port: u32,
    /// Timestamp
    #[prost(uint64, tag = "4")]
    pub timestamp: u64,
    /// TLS signature
    #[prost(bytes = "vec", tag = "5")]
    pub signature: Vec<u8>,
    /// Transaction ID that added this validator
    #[prost(bytes = "vec", tag = "6")]
    pub tx_id: Vec<u8>,
}

/// Request for peer list.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetPeerList {
    /// Known peers bloom filter
    #[prost(message, optional, tag = "1")]
    pub known_peers: Option<BloomFilter>,
    /// Whether requesting all subnets
    #[prost(bool, tag = "2")]
    pub all_subnets: bool,
}

/// Response with peer list.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct PeerList {
    /// Claimed IP ports
    #[prost(message, repeated, tag = "1")]
    pub claimed_ip_ports: Vec<ClaimedIpPort>,
}

// ============================================================================
// State Sync Messages
// ============================================================================

/// Request for state summary frontier.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetStateSummaryFrontier {
    /// Chain ID (32 bytes)
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
}

/// State summary frontier response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct StateSummaryFrontier {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Summary bytes
    #[prost(bytes = "vec", tag = "3")]
    pub summary: Vec<u8>,
}

/// Request for accepted state summaries.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetAcceptedStateSummary {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Heights being requested
    #[prost(uint64, repeated, tag = "4")]
    pub heights: Vec<u64>,
}

/// Accepted state summary response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AcceptedStateSummary {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Summary IDs (32 bytes each)
    #[prost(bytes = "vec", repeated, tag = "3")]
    pub summary_ids: Vec<Vec<u8>>,
}

// ============================================================================
// Bootstrapping Messages
// ============================================================================

/// Request for accepted frontier.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetAcceptedFrontier {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
}

/// Accepted frontier response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AcceptedFrontier {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Container ID (32 bytes)
    #[prost(bytes = "vec", tag = "3")]
    pub container_id: Vec<u8>,
}

/// Request for accepted containers.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetAccepted {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Container IDs (32 bytes each)
    #[prost(bytes = "vec", repeated, tag = "4")]
    pub container_ids: Vec<Vec<u8>>,
}

/// Accepted containers response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Accepted {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Container IDs (32 bytes each)
    #[prost(bytes = "vec", repeated, tag = "3")]
    pub container_ids: Vec<Vec<u8>>,
}

/// Engine type for consensus.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(i32)]
pub enum EngineType {
    #[default]
    Unspecified = 0,
    Avalanche = 1,
    Snowman = 2,
}

/// Request for ancestors.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct GetAncestors {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Container ID to get ancestors for
    #[prost(bytes = "vec", tag = "4")]
    pub container_id: Vec<u8>,
    /// Engine type
    #[prost(enumeration = "EngineTypeProto", tag = "5")]
    pub engine_type: i32,
}

/// Ancestors response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Ancestors {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Ancestor containers
    #[prost(bytes = "vec", repeated, tag = "3")]
    pub containers: Vec<Vec<u8>>,
}

// Protobuf enumeration for engine type
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, prost::Enumeration)]
#[repr(i32)]
pub enum EngineTypeProto {
    Unspecified = 0,
    Avalanche = 1,
    Snowman = 2,
}

// ============================================================================
// Consensus Messages
// ============================================================================

/// Request for a container.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Get {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Container ID
    #[prost(bytes = "vec", tag = "4")]
    pub container_id: Vec<u8>,
}

/// Container response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Put {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Container bytes
    #[prost(bytes = "vec", tag = "3")]
    pub container: Vec<u8>,
}

/// Push query with container.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct PushQuery {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Container being gossiped
    #[prost(bytes = "vec", tag = "4")]
    pub container: Vec<u8>,
    /// Requested height
    #[prost(uint64, tag = "6")]
    pub requested_height: u64,
}

/// Pull query for preferences.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct PullQuery {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Container ID
    #[prost(bytes = "vec", tag = "4")]
    pub container_id: Vec<u8>,
    /// Requested height
    #[prost(uint64, tag = "6")]
    pub requested_height: u64,
}

/// Voting preferences response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct Chits {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Preferred block ID
    #[prost(bytes = "vec", tag = "3")]
    pub preferred_id: Vec<u8>,
    /// Last accepted block ID
    #[prost(bytes = "vec", tag = "4")]
    pub accepted_id: Vec<u8>,
    /// Preferred ID at requested height
    #[prost(bytes = "vec", tag = "5")]
    pub preferred_id_at_height: Vec<u8>,
    /// Last accepted height
    #[prost(uint64, tag = "6")]
    pub accepted_height: u64,
}

// ============================================================================
// App Messages
// ============================================================================

/// Application-defined request.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AppRequest {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Deadline in nanoseconds
    #[prost(uint64, tag = "3")]
    pub deadline: u64,
    /// Application bytes
    #[prost(bytes = "vec", tag = "4")]
    pub app_bytes: Vec<u8>,
}

/// Application-defined response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AppResponse {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Application bytes
    #[prost(bytes = "vec", tag = "3")]
    pub app_bytes: Vec<u8>,
}

/// Application gossip message.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AppGossip {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Application bytes
    #[prost(bytes = "vec", tag = "2")]
    pub app_bytes: Vec<u8>,
}

/// Application error response.
#[derive(Clone, PartialEq, ProstMessage)]
pub struct AppError {
    /// Chain ID
    #[prost(bytes = "vec", tag = "1")]
    pub chain_id: Vec<u8>,
    /// Request ID
    #[prost(uint32, tag = "2")]
    pub request_id: u32,
    /// Error code
    #[prost(sint32, tag = "3")]
    pub error_code: i32,
    /// Error message
    #[prost(string, tag = "4")]
    pub error_message: String,
}

// ============================================================================
// Protobuf Encoding Helpers
// ============================================================================

fn encode_field(buf: &mut BytesMut, field_number: u32, data: &[u8]) {
    let key = (field_number << 3) | 2; // wire type 2 = length-delimited
    encode_varint(buf, key as u64);
    encode_varint(buf, data.len() as u64);
    buf.put_slice(data);
}

fn encode_varint(buf: &mut BytesMut, mut value: u64) {
    loop {
        if value < 0x80 {
            buf.put_u8(value as u8);
            return;
        }
        buf.put_u8(((value & 0x7F) | 0x80) as u8);
        value >>= 7;
    }
}

fn decode_varint(buf: &mut &[u8]) -> Result<u64, MessageError> {
    let mut result = 0u64;
    let mut shift = 0;

    loop {
        if !buf.has_remaining() {
            return Err(MessageError::InsufficientData {
                needed: 1,
                available: 0,
            });
        }
        let byte = buf.get_u8();
        result |= ((byte & 0x7F) as u64) << shift;
        if byte < 0x80 {
            return Ok(result);
        }
        shift += 7;
        if shift >= 64 {
            return Err(MessageError::InvalidField("varint too long".into()));
        }
    }
}

fn decode_key(buf: &mut &[u8]) -> Result<(u32, u8), MessageError> {
    let key = decode_varint(buf)?;
    let field_number = (key >> 3) as u32;
    let wire_type = (key & 0x7) as u8;
    Ok((field_number, wire_type))
}

fn skip_field(buf: &mut &[u8], wire_type: u8) -> Result<(), MessageError> {
    match wire_type {
        0 => {
            decode_varint(buf)?;
        }
        1 => {
            if buf.remaining() < 8 {
                return Err(MessageError::InsufficientData {
                    needed: 8,
                    available: buf.remaining(),
                });
            }
            buf.advance(8);
        }
        2 => {
            let len = decode_varint(buf)? as usize;
            if buf.remaining() < len {
                return Err(MessageError::InsufficientData {
                    needed: len,
                    available: buf.remaining(),
                });
            }
            buf.advance(len);
        }
        5 => {
            if buf.remaining() < 4 {
                return Err(MessageError::InsufficientData {
                    needed: 4,
                    available: buf.remaining(),
                });
            }
            buf.advance(4);
        }
        _ => {
            return Err(MessageError::InvalidField(format!(
                "unknown wire type: {}",
                wire_type
            )));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ping_roundtrip() {
        let ping = Ping { uptime: 95 };
        let msg = Message::Ping(ping.clone());
        let encoded = msg.encode().unwrap();
        let decoded = Message::decode(&encoded).unwrap();
        assert_eq!(decoded, Message::Ping(ping));
    }

    #[test]
    fn test_pong_roundtrip() {
        let msg = Message::Pong(Pong {});
        let encoded = msg.encode().unwrap();
        let decoded = Message::decode(&encoded).unwrap();
        assert_eq!(decoded, msg);
    }

    #[test]
    fn test_handshake_roundtrip() {
        let handshake = Handshake {
            network_id: 1,
            my_time: 1234567890,
            ip_addr: vec![127, 0, 0, 1],
            ip_port: 9651,
            ip_signing_time: 1234567890,
            client: Some(Client::new("avalanche-rs", 0, 1, 0)),
            ..Default::default()
        };

        let msg = Message::Handshake(Box::new(handshake.clone()));
        let encoded = msg.encode().unwrap();
        let decoded = Message::decode(&encoded).unwrap();

        if let Message::Handshake(decoded_handshake) = decoded {
            assert_eq!(*decoded_handshake, handshake);
        } else {
            panic!("Expected Handshake message");
        }
    }

    #[test]
    fn test_get_peer_list_roundtrip() {
        let msg = Message::GetPeerList(GetPeerList {
            known_peers: Some(BloomFilter {
                filter: vec![0xff; 32],
                salt: vec![0x42; 32],
            }),
            all_subnets: true,
        });

        let encoded = msg.encode().unwrap();
        let decoded = Message::decode(&encoded).unwrap();
        assert_eq!(decoded, msg);
    }

    #[test]
    fn test_client_version_string() {
        let client = Client::new("avalanchego", 1, 11, 3);
        assert_eq!(client.version_string(), "1.11.3");
    }

    #[test]
    fn test_compression() {
        // Create a message with enough data to benefit from compression
        let msg = Message::Ancestors(Ancestors {
            chain_id: vec![0x42; 32],
            request_id: 123,
            containers: vec![vec![0xAB; 1000]; 10],
        });

        let compressed = msg.compress().unwrap();

        // Verify we can decompress and get the same message back
        let decompressed = compressed.decompress().unwrap();
        assert_eq!(decompressed, msg);
    }

    #[test]
    fn test_app_request_roundtrip() {
        let msg = Message::AppRequest(AppRequest {
            chain_id: vec![0x11; 32],
            request_id: 42,
            deadline: 1_000_000_000,
            app_bytes: b"hello world".to_vec(),
        });

        let encoded = msg.encode().unwrap();
        let decoded = Message::decode(&encoded).unwrap();
        assert_eq!(decoded, msg);
    }
}
