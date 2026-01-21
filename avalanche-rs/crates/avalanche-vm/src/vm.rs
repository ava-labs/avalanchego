//! Virtual machine traits.

use std::sync::Arc;

use async_trait::async_trait;

use avalanche_db::Database;
use avalanche_ids::{Id, NodeId};

use crate::block::{Block, BuildBlockOptions};
use crate::context::Context;
use crate::Result;

/// Health status of a VM.
#[derive(Debug, Clone)]
pub struct HealthStatus {
    /// Whether the VM is healthy
    pub healthy: bool,
    /// Optional details
    pub details: Option<String>,
}

impl HealthStatus {
    /// Creates a healthy status.
    pub fn healthy() -> Self {
        Self {
            healthy: true,
            details: None,
        }
    }

    /// Creates an unhealthy status with details.
    pub fn unhealthy(details: impl Into<String>) -> Self {
        Self {
            healthy: false,
            details: Some(details.into()),
        }
    }
}

/// Version information for a VM.
#[derive(Debug, Clone)]
pub struct Version {
    /// Major version
    pub major: u32,
    /// Minor version
    pub minor: u32,
    /// Patch version
    pub patch: u32,
    /// Optional pre-release tag
    pub pre: Option<String>,
}

impl Version {
    /// Creates a new version.
    pub fn new(major: u32, minor: u32, patch: u32) -> Self {
        Self {
            major,
            minor,
            patch,
            pre: None,
        }
    }
}

impl std::fmt::Display for Version {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}.{}", self.major, self.minor, self.patch)?;
        if let Some(ref pre) = self.pre {
            write!(f, "-{}", pre)?;
        }
        Ok(())
    }
}

/// Common VM functionality shared by all VM types.
#[async_trait]
pub trait CommonVM: Send + Sync {
    /// Initializes the VM with context and database.
    async fn initialize(
        &mut self,
        ctx: Context,
        db: Arc<dyn Database>,
        genesis_bytes: &[u8],
    ) -> Result<()>;

    /// Shuts down the VM gracefully.
    async fn shutdown(&mut self) -> Result<()>;

    /// Returns the VM's version.
    fn version(&self) -> Version;

    /// Creates a new handler for app requests.
    fn create_handlers(&self) -> Vec<Box<dyn AppHandler>>;

    /// Returns the VM's health status.
    async fn health_check(&self) -> Result<HealthStatus>;

    /// Called when connected to a peer.
    async fn connected(&self, node_id: &NodeId, version: &str) -> Result<()>;

    /// Called when disconnected from a peer.
    async fn disconnected(&self, node_id: &NodeId) -> Result<()>;
}

/// A linear chain VM (Snowman-based consensus).
#[async_trait]
pub trait ChainVM: CommonVM {
    /// Builds a new block on top of the preferred block.
    async fn build_block(&self, options: BuildBlockOptions) -> Result<Box<dyn Block>>;

    /// Parses a block from its byte representation.
    async fn parse_block(&self, bytes: &[u8]) -> Result<Box<dyn Block>>;

    /// Gets a block by its ID.
    async fn get_block(&self, id: Id) -> Result<Option<Box<dyn Block>>>;

    /// Sets the preferred block for building.
    async fn set_preference(&mut self, id: Id) -> Result<()>;

    /// Returns the ID of the last accepted block.
    fn last_accepted(&self) -> Id;

    /// Returns the ID of the preferred block.
    fn preferred(&self) -> Id;

    /// Returns true if the block should be issued to consensus.
    async fn should_verify_block(&self, block: &dyn Block) -> bool {
        // Default: verify all blocks
        true
    }

    /// Called when a block has been accepted by consensus.
    async fn block_accepted(&mut self, _block: &dyn Block) -> Result<()> {
        Ok(())
    }

    /// Called when a block has been rejected by consensus.
    async fn block_rejected(&mut self, _block: &dyn Block) -> Result<()> {
        Ok(())
    }
}

/// Handler for app-specific requests.
#[async_trait]
pub trait AppHandler: Send + Sync {
    /// Handles an app request from a peer.
    async fn app_request(
        &self,
        node_id: NodeId,
        request_id: u32,
        deadline: std::time::Instant,
        request: &[u8],
    ) -> Result<Vec<u8>>;

    /// Handles an app request that failed.
    async fn app_request_failed(
        &self,
        node_id: NodeId,
        request_id: u32,
    ) -> Result<()>;

    /// Handles a gossip message.
    async fn app_gossip(
        &self,
        node_id: NodeId,
        msg: &[u8],
    ) -> Result<()>;
}

/// Handler for cross-chain app communication.
#[async_trait]
pub trait CrossChainAppHandler: Send + Sync {
    /// Handles a cross-chain app request.
    async fn cross_chain_app_request(
        &self,
        chain_id: Id,
        request_id: u32,
        deadline: std::time::Instant,
        request: &[u8],
    ) -> Result<Vec<u8>>;

    /// Handles a cross-chain app request failure.
    async fn cross_chain_app_request_failed(
        &self,
        chain_id: Id,
        request_id: u32,
    ) -> Result<()>;
}

/// Connector for cross-chain communication.
#[async_trait]
pub trait Connector: Send + Sync {
    /// Sends a cross-chain app request.
    async fn send_cross_chain_request(
        &self,
        chain_id: Id,
        request: &[u8],
    ) -> Result<u32>;

    /// Sends a cross-chain app response.
    async fn send_cross_chain_response(
        &self,
        chain_id: Id,
        request_id: u32,
        response: &[u8],
    ) -> Result<()>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_version_display() {
        let v = Version::new(1, 2, 3);
        assert_eq!(v.to_string(), "1.2.3");

        let v_pre = Version {
            major: 1,
            minor: 0,
            patch: 0,
            pre: Some("beta".to_string()),
        };
        assert_eq!(v_pre.to_string(), "1.0.0-beta");
    }

    #[test]
    fn test_health_status() {
        let healthy = HealthStatus::healthy();
        assert!(healthy.healthy);
        assert!(healthy.details.is_none());

        let unhealthy = HealthStatus::unhealthy("connection failed");
        assert!(!unhealthy.healthy);
        assert_eq!(unhealthy.details, Some("connection failed".to_string()));
    }
}
