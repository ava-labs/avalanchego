//! Execution context for virtual machines.

use std::sync::Arc;

use avalanche_ids::{Id, NodeId};

/// Network ID constants.
pub mod network_id {
    /// Mainnet network ID
    pub const MAINNET: u32 = 1;
    /// Fuji testnet network ID
    pub const FUJI: u32 = 5;
    /// Local network ID
    pub const LOCAL: u32 = 12345;
}

/// Context provides information about the execution environment to VMs.
#[derive(Debug, Clone)]
pub struct Context {
    /// Network ID (mainnet, fuji, local, etc.)
    pub network_id: u32,
    /// Subnet ID this VM belongs to
    pub subnet_id: Id,
    /// Chain ID for this VM instance
    pub chain_id: Id,
    /// Node ID running this VM
    pub node_id: NodeId,
    /// X-Chain ID (for cross-chain)
    pub x_chain_id: Id,
    /// C-Chain ID (for cross-chain)
    pub c_chain_id: Id,
    /// AVAX asset ID
    pub avax_asset_id: Id,
    /// Chain alias (human readable name)
    pub chain_alias: String,
}

impl Context {
    /// Creates a new context builder.
    pub fn builder() -> ContextBuilder {
        ContextBuilder::new()
    }

    /// Returns true if this is mainnet.
    pub fn is_mainnet(&self) -> bool {
        self.network_id == network_id::MAINNET
    }

    /// Returns true if this is fuji testnet.
    pub fn is_fuji(&self) -> bool {
        self.network_id == network_id::FUJI
    }

    /// Returns true if this is a local network.
    pub fn is_local(&self) -> bool {
        self.network_id == network_id::LOCAL
    }
}

impl Default for Context {
    fn default() -> Self {
        Self {
            network_id: network_id::LOCAL,
            subnet_id: Id::default(),
            chain_id: Id::default(),
            node_id: NodeId::default(),
            x_chain_id: Id::default(),
            c_chain_id: Id::default(),
            avax_asset_id: Id::default(),
            chain_alias: String::new(),
        }
    }
}

/// Builder for Context.
#[derive(Debug, Default)]
pub struct ContextBuilder {
    ctx: Context,
}

impl ContextBuilder {
    /// Creates a new builder.
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets the network ID.
    pub fn network_id(mut self, id: u32) -> Self {
        self.ctx.network_id = id;
        self
    }

    /// Sets the subnet ID.
    pub fn subnet_id(mut self, id: Id) -> Self {
        self.ctx.subnet_id = id;
        self
    }

    /// Sets the chain ID.
    pub fn chain_id(mut self, id: Id) -> Self {
        self.ctx.chain_id = id;
        self
    }

    /// Sets the node ID.
    pub fn node_id(mut self, id: NodeId) -> Self {
        self.ctx.node_id = id;
        self
    }

    /// Sets the X-Chain ID.
    pub fn x_chain_id(mut self, id: Id) -> Self {
        self.ctx.x_chain_id = id;
        self
    }

    /// Sets the C-Chain ID.
    pub fn c_chain_id(mut self, id: Id) -> Self {
        self.ctx.c_chain_id = id;
        self
    }

    /// Sets the AVAX asset ID.
    pub fn avax_asset_id(mut self, id: Id) -> Self {
        self.ctx.avax_asset_id = id;
        self
    }

    /// Sets the chain alias.
    pub fn chain_alias(mut self, alias: impl Into<String>) -> Self {
        self.ctx.chain_alias = alias.into();
        self
    }

    /// Builds the context.
    pub fn build(self) -> Context {
        self.ctx
    }
}

/// Shared context that can be passed to handlers.
pub type SharedContext = Arc<Context>;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_context_builder() {
        let chain_id = Id::from_slice(&[1; 32]).unwrap();
        let subnet_id = Id::from_slice(&[2; 32]).unwrap();

        let ctx = Context::builder()
            .network_id(network_id::MAINNET)
            .chain_id(chain_id)
            .subnet_id(subnet_id)
            .chain_alias("test-chain")
            .build();

        assert!(ctx.is_mainnet());
        assert_eq!(ctx.chain_id, chain_id);
        assert_eq!(ctx.subnet_id, subnet_id);
        assert_eq!(ctx.chain_alias, "test-chain");
    }

    #[test]
    fn test_network_detection() {
        let mainnet = Context::builder().network_id(network_id::MAINNET).build();
        let fuji = Context::builder().network_id(network_id::FUJI).build();
        let local = Context::builder().network_id(network_id::LOCAL).build();

        assert!(mainnet.is_mainnet());
        assert!(!mainnet.is_fuji());

        assert!(fuji.is_fuji());
        assert!(!fuji.is_mainnet());

        assert!(local.is_local());
    }
}
