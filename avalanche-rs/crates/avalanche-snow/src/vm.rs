//! VM traits for consensus integration.
//!
//! These traits define the interface between consensus and virtual machines.
//! They are re-exported by the avalanche-vm crate for use by VM implementations.

use std::sync::Arc;

use async_trait::async_trait;

use avalanche_db::Database;
use avalanche_ids::{Id, NodeId};

use crate::error::{ConsensusError, Result};

/// Block status in consensus.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BlockStatus {
    /// Block is being processed.
    Processing,
    /// Block has been accepted.
    Accepted,
    /// Block has been rejected.
    Rejected,
    /// Block status is unknown.
    Unknown,
}

/// VM execution context.
#[derive(Debug, Clone)]
pub struct Context {
    /// Network ID.
    pub network_id: u32,
    /// Subnet ID.
    pub subnet_id: Id,
    /// Chain ID.
    pub chain_id: Id,
    /// Node ID.
    pub node_id: NodeId,
    /// Whether this is a primary network validator.
    pub primary_validator: bool,
}

impl Default for Context {
    fn default() -> Self {
        Self {
            network_id: 1,
            subnet_id: Id::default(),
            chain_id: Id::default(),
            node_id: NodeId::default(),
            primary_validator: false,
        }
    }
}

/// VM health status.
#[derive(Debug, Clone)]
pub struct HealthStatus {
    /// Whether the VM is healthy.
    pub healthy: bool,
    /// Optional reason if unhealthy.
    pub reason: Option<String>,
}

impl HealthStatus {
    /// Creates a healthy status.
    pub fn healthy() -> Self {
        Self {
            healthy: true,
            reason: None,
        }
    }

    /// Creates an unhealthy status.
    pub fn unhealthy(reason: &str) -> Self {
        Self {
            healthy: false,
            reason: Some(reason.to_string()),
        }
    }
}

/// VM version information.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Version {
    /// Major version.
    pub major: u32,
    /// Minor version.
    pub minor: u32,
    /// Patch version.
    pub patch: u32,
}

impl Version {
    /// Creates a new version.
    pub fn new(major: u32, minor: u32, patch: u32) -> Self {
        Self { major, minor, patch }
    }
}

impl std::fmt::Display for Version {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}.{}", self.major, self.minor, self.patch)
    }
}

/// VM error types.
#[derive(Debug, thiserror::Error)]
pub enum VMError {
    #[error("VM already initialized")]
    AlreadyInitialized,
    #[error("VM not initialized")]
    NotInitialized,
    #[error("block not found")]
    BlockNotFound,
    #[error("invalid block: {0}")]
    InvalidBlock(String),
    #[error("database error: {0}")]
    Database(String),
    #[error("consensus error: {0}")]
    Consensus(String),
    #[error("internal error: {0}")]
    Internal(String),
}

impl From<ConsensusError> for VMError {
    fn from(e: ConsensusError) -> Self {
        VMError::Consensus(e.to_string())
    }
}

/// Options for building a block.
#[derive(Debug, Clone, Default)]
pub struct BuildBlockOptions {
    /// Parent block ID (if different from preferred).
    pub parent_id: Option<Id>,
    /// Maximum block size.
    pub max_size: Option<u64>,
    /// Timestamp to use.
    pub timestamp: Option<u64>,
}

/// Block trait for Snowman consensus.
pub trait Block: Send + Sync {
    /// Returns the block's ID.
    fn id(&self) -> Id;

    /// Returns the parent block's ID.
    fn parent(&self) -> Id;

    /// Returns the block's height.
    fn height(&self) -> u64;

    /// Returns the block's timestamp.
    fn timestamp(&self) -> u64;

    /// Returns the block's bytes.
    fn bytes(&self) -> &[u8];

    /// Verifies the block.
    fn verify(&self) -> Result<()>;
}

/// Application handler trait.
pub trait AppHandler: Send + Sync {
    // App handlers are optional for basic VMs
}

/// Common VM trait shared by all virtual machines.
#[async_trait]
pub trait CommonVM: Send + Sync {
    /// Initializes the VM.
    async fn initialize(
        &mut self,
        ctx: Context,
        db: Arc<dyn Database>,
        genesis_bytes: &[u8],
    ) -> Result<()>;

    /// Shuts down the VM.
    async fn shutdown(&mut self) -> Result<()>;

    /// Returns the VM version.
    fn version(&self) -> Version;

    /// Creates application handlers.
    fn create_handlers(&self) -> Vec<Box<dyn AppHandler>>;

    /// Performs a health check.
    async fn health_check(&self) -> Result<HealthStatus>;

    /// Called when a peer connects.
    async fn connected(&self, node_id: &NodeId, version: &str) -> Result<()>;

    /// Called when a peer disconnects.
    async fn disconnected(&self, node_id: &NodeId) -> Result<()>;
}

/// Chain VM trait for linear chain VMs (Snowman).
#[async_trait]
pub trait ChainVM: CommonVM {
    /// Builds a new block.
    async fn build_block(&self, options: BuildBlockOptions) -> Result<Box<dyn Block>>;

    /// Parses a block from bytes.
    async fn parse_block(&self, bytes: &[u8]) -> Result<Box<dyn Block>>;

    /// Gets a block by ID.
    async fn get_block(&self, id: Id) -> Result<Option<Box<dyn Block>>>;

    /// Sets the preferred block.
    async fn set_preference(&mut self, id: Id) -> Result<()>;

    /// Returns the last accepted block ID.
    fn last_accepted(&self) -> Id;

    /// Returns the preferred block ID.
    fn preferred(&self) -> Id;

    /// Called when a block is accepted.
    async fn block_accepted(&mut self, block: &dyn Block) -> Result<()>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_version_display() {
        let v = Version::new(1, 2, 3);
        assert_eq!(v.to_string(), "1.2.3");
    }

    #[test]
    fn test_health_status() {
        let healthy = HealthStatus::healthy();
        assert!(healthy.healthy);
        assert!(healthy.reason.is_none());

        let unhealthy = HealthStatus::unhealthy("test reason");
        assert!(!unhealthy.healthy);
        assert_eq!(unhealthy.reason, Some("test reason".to_string()));
    }
}
