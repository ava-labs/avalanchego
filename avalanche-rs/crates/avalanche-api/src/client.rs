//! HTTP client for Avalanche API calls.

use std::sync::atomic::{AtomicI64, Ordering};
use std::time::Duration;

use serde::{de::DeserializeOwned, Serialize};
use serde_json::Value;

use crate::jsonrpc::{Request, RequestId, Response, RpcError};

/// API client error.
#[derive(Debug, thiserror::Error)]
pub enum ClientError {
    /// HTTP error
    #[error("HTTP error: {0}")]
    Http(String),
    /// JSON serialization error
    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),
    /// RPC error from server
    #[error("RPC error: {0}")]
    Rpc(#[from] RpcError),
    /// Timeout
    #[error("request timeout")]
    Timeout,
    /// Connection error
    #[error("connection error: {0}")]
    Connection(String),
}

/// Configuration for the API client.
#[derive(Debug, Clone)]
pub struct ClientConfig {
    /// Base URL for API requests
    pub url: String,
    /// Request timeout
    pub timeout: Duration,
    /// Maximum retries
    pub max_retries: u32,
    /// Retry delay
    pub retry_delay: Duration,
}

impl Default for ClientConfig {
    fn default() -> Self {
        Self {
            url: "http://127.0.0.1:9650".to_string(),
            timeout: Duration::from_secs(30),
            max_retries: 3,
            retry_delay: Duration::from_millis(500),
        }
    }
}

impl ClientConfig {
    /// Creates a new config with the given URL.
    pub fn new(url: impl Into<String>) -> Self {
        Self {
            url: url.into(),
            ..Default::default()
        }
    }

    /// Sets the timeout.
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.timeout = timeout;
        self
    }

    /// Sets max retries.
    pub fn with_retries(mut self, max_retries: u32) -> Self {
        self.max_retries = max_retries;
        self
    }
}

/// Avalanche API client.
pub struct Client {
    config: ClientConfig,
    request_id: AtomicI64,
}

impl Client {
    /// Creates a new client with default configuration.
    pub fn new() -> Self {
        Self::with_config(ClientConfig::default())
    }

    /// Creates a new client with the given URL.
    pub fn with_url(url: impl Into<String>) -> Self {
        Self::with_config(ClientConfig::new(url))
    }

    /// Creates a new client with the given configuration.
    pub fn with_config(config: ClientConfig) -> Self {
        Self {
            config,
            request_id: AtomicI64::new(1),
        }
    }

    /// Returns the base URL.
    pub fn url(&self) -> &str {
        &self.config.url
    }

    /// Gets the next request ID.
    fn next_id(&self) -> RequestId {
        RequestId::Number(self.request_id.fetch_add(1, Ordering::SeqCst))
    }

    /// Makes a JSON-RPC call.
    pub async fn call<T: DeserializeOwned>(
        &self,
        endpoint: &str,
        method: &str,
        params: Option<Value>,
    ) -> Result<T, ClientError> {
        let request = Request::new(method, params, self.next_id());
        let response = self.send_request(endpoint, &request).await?;

        let result = response.into_result()?;
        let value: T = serde_json::from_value(result)?;
        Ok(value)
    }

    /// Makes a JSON-RPC call with no result (void return).
    pub async fn call_void(
        &self,
        endpoint: &str,
        method: &str,
        params: Option<Value>,
    ) -> Result<(), ClientError> {
        let request = Request::new(method, params, self.next_id());
        let response = self.send_request(endpoint, &request).await?;
        response.into_result()?;
        Ok(())
    }

    /// Sends a raw request and returns the response.
    async fn send_request(&self, endpoint: &str, request: &Request) -> Result<Response, ClientError> {
        let _url = format!("{}/{}", self.config.url, endpoint);

        // In a real implementation, this would use an HTTP client like reqwest
        // For now, return a mock response for testing

        // Simulate network call
        tokio::time::sleep(Duration::from_millis(1)).await;

        // Return a placeholder response
        Ok(Response::success(
            request.id.clone(),
            serde_json::json!(null),
        ))
    }

    // --- Info API ---

    /// Gets node information.
    pub async fn info_get_node_id(&self) -> Result<NodeIdResponse, ClientError> {
        self.call("ext/info", "info.getNodeID", None).await
    }

    /// Gets network ID.
    pub async fn info_get_network_id(&self) -> Result<NetworkIdResponse, ClientError> {
        self.call("ext/info", "info.getNetworkID", None).await
    }

    /// Gets network name.
    pub async fn info_get_network_name(&self) -> Result<NetworkNameResponse, ClientError> {
        self.call("ext/info", "info.getNetworkName", None).await
    }

    /// Checks if node is bootstrapped.
    pub async fn info_is_bootstrapped(&self, chain: &str) -> Result<IsBootstrappedResponse, ClientError> {
        self.call(
            "ext/info",
            "info.isBootstrapped",
            Some(serde_json::json!({ "chain": chain })),
        ).await
    }

    // --- Health API ---

    /// Gets health status.
    pub async fn health(&self) -> Result<HealthResponse, ClientError> {
        self.call("ext/health", "health.health", None).await
    }

    // --- P-Chain API ---

    /// Gets current validators.
    pub async fn platform_get_current_validators(&self, subnet_id: Option<&str>) -> Result<ValidatorsResponse, ClientError> {
        let params = subnet_id.map(|id| serde_json::json!({ "subnetID": id }));
        self.call("ext/P", "platform.getCurrentValidators", params).await
    }

    /// Gets pending validators.
    pub async fn platform_get_pending_validators(&self, subnet_id: Option<&str>) -> Result<ValidatorsResponse, ClientError> {
        let params = subnet_id.map(|id| serde_json::json!({ "subnetID": id }));
        self.call("ext/P", "platform.getPendingValidators", params).await
    }

    /// Gets UTXOs.
    pub async fn platform_get_utxos(&self, addresses: &[String]) -> Result<UtxosResponse, ClientError> {
        self.call(
            "ext/P",
            "platform.getUTXOs",
            Some(serde_json::json!({ "addresses": addresses })),
        ).await
    }

    /// Gets balance.
    pub async fn platform_get_balance(&self, address: &str) -> Result<BalanceResponse, ClientError> {
        self.call(
            "ext/P",
            "platform.getBalance",
            Some(serde_json::json!({ "address": address })),
        ).await
    }

    /// Gets staking asset ID.
    pub async fn platform_get_staking_asset_id(&self) -> Result<AssetIdResponse, ClientError> {
        self.call("ext/P", "platform.getStakingAssetID", None).await
    }

    /// Gets blockchain status.
    pub async fn platform_get_blockchain_status(&self, blockchain_id: &str) -> Result<BlockchainStatusResponse, ClientError> {
        self.call(
            "ext/P",
            "platform.getBlockchainStatus",
            Some(serde_json::json!({ "blockchainID": blockchain_id })),
        ).await
    }

    // --- X-Chain API ---

    /// Gets X-Chain asset balance.
    pub async fn avm_get_balance(&self, address: &str, asset_id: &str) -> Result<BalanceResponse, ClientError> {
        self.call(
            "ext/bc/X",
            "avm.getBalance",
            Some(serde_json::json!({
                "address": address,
                "assetID": asset_id
            })),
        ).await
    }

    /// Gets all balances for an address.
    pub async fn avm_get_all_balances(&self, address: &str) -> Result<AllBalancesResponse, ClientError> {
        self.call(
            "ext/bc/X",
            "avm.getAllBalances",
            Some(serde_json::json!({ "address": address })),
        ).await
    }

    // --- C-Chain API (EVM-compatible) ---

    /// Gets C-Chain block number.
    pub async fn eth_block_number(&self) -> Result<String, ClientError> {
        self.call("ext/bc/C/rpc", "eth_blockNumber", Some(serde_json::json!([]))).await
    }

    /// Gets C-Chain balance.
    pub async fn eth_get_balance(&self, address: &str, block: &str) -> Result<String, ClientError> {
        self.call(
            "ext/bc/C/rpc",
            "eth_getBalance",
            Some(serde_json::json!([address, block])),
        ).await
    }

    /// Gets C-Chain transaction count (nonce).
    pub async fn eth_get_transaction_count(&self, address: &str, block: &str) -> Result<String, ClientError> {
        self.call(
            "ext/bc/C/rpc",
            "eth_getTransactionCount",
            Some(serde_json::json!([address, block])),
        ).await
    }

    /// Sends a raw C-Chain transaction.
    pub async fn eth_send_raw_transaction(&self, tx_hex: &str) -> Result<String, ClientError> {
        self.call(
            "ext/bc/C/rpc",
            "eth_sendRawTransaction",
            Some(serde_json::json!([tx_hex])),
        ).await
    }

    /// Calls a C-Chain contract (read-only).
    pub async fn eth_call(&self, call_data: Value, block: &str) -> Result<String, ClientError> {
        self.call(
            "ext/bc/C/rpc",
            "eth_call",
            Some(serde_json::json!([call_data, block])),
        ).await
    }
}

impl Default for Client {
    fn default() -> Self {
        Self::new()
    }
}

// --- Response types ---

/// Node ID response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NodeIdResponse {
    pub node_id: String,
}

/// Network ID response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NetworkIdResponse {
    pub network_id: String,
}

/// Network name response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NetworkNameResponse {
    pub network_name: String,
}

/// Bootstrap status response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IsBootstrappedResponse {
    pub is_bootstrapped: bool,
}

/// Health response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct HealthResponse {
    pub healthy: bool,
}

/// Validators response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct ValidatorsResponse {
    pub validators: Vec<ValidatorInfo>,
}

/// Validator info.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ValidatorInfo {
    pub node_id: String,
    pub start_time: String,
    pub end_time: String,
    pub stake_amount: Option<String>,
    pub weight: Option<String>,
}

/// UTXOs response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct UtxosResponse {
    pub utxos: Vec<String>,
    #[serde(rename = "endIndex")]
    pub end_index: Option<UtxoIndex>,
}

/// UTXO index for pagination.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct UtxoIndex {
    pub address: String,
    pub utxo: String,
}

/// Balance response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct BalanceResponse {
    pub balance: String,
}

/// Asset ID response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssetIdResponse {
    pub asset_id: String,
}

/// Blockchain status response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct BlockchainStatusResponse {
    pub status: String,
}

/// All balances response.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
pub struct AllBalancesResponse {
    pub balances: Vec<AssetBalance>,
}

/// Asset balance.
#[derive(Debug, Clone, Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssetBalance {
    pub asset: String,
    pub balance: String,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_client_config() {
        let config = ClientConfig::new("http://localhost:9650")
            .with_timeout(Duration::from_secs(60))
            .with_retries(5);

        assert_eq!(config.url, "http://localhost:9650");
        assert_eq!(config.timeout, Duration::from_secs(60));
        assert_eq!(config.max_retries, 5);
    }

    #[test]
    fn test_client_creation() {
        let client = Client::with_url("http://localhost:9650");
        assert_eq!(client.url(), "http://localhost:9650");
    }

    #[tokio::test]
    async fn test_request_id_increment() {
        let client = Client::new();

        let id1 = client.next_id();
        let id2 = client.next_id();

        match (id1, id2) {
            (RequestId::Number(n1), RequestId::Number(n2)) => {
                assert_eq!(n2, n1 + 1);
            }
            _ => panic!("Expected numeric IDs"),
        }
    }
}
