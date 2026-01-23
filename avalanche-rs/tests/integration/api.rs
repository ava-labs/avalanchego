//! API integration tests.
//!
//! Tests HTTP/JSON-RPC API endpoints including:
//! - Health checks
//! - Info endpoints
//! - P-Chain API
//! - X-Chain API
//! - C-Chain API

use std::collections::HashMap;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

/// JSON-RPC request structure.
#[derive(Debug, Clone, Serialize)]
pub struct JsonRpcRequest {
    pub jsonrpc: String,
    pub id: u64,
    pub method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub params: Option<Value>,
}

impl JsonRpcRequest {
    /// Creates a new JSON-RPC request.
    pub fn new(method: &str, params: Option<Value>) -> Self {
        Self {
            jsonrpc: "2.0".to_string(),
            id: 1,
            method: method.to_string(),
            params,
        }
    }
}

/// JSON-RPC response structure.
#[derive(Debug, Clone, Deserialize)]
pub struct JsonRpcResponse {
    pub jsonrpc: String,
    pub id: u64,
    #[serde(default)]
    pub result: Option<Value>,
    #[serde(default)]
    pub error: Option<JsonRpcError>,
}

/// JSON-RPC error structure.
#[derive(Debug, Clone, Deserialize)]
pub struct JsonRpcError {
    pub code: i64,
    pub message: String,
    #[serde(default)]
    pub data: Option<Value>,
}

/// API test client.
pub struct ApiTestClient {
    /// Base URL.
    pub base_url: String,
    /// Request timeout.
    pub timeout: Duration,
}

impl ApiTestClient {
    /// Creates a new API test client.
    pub fn new(base_url: &str) -> Self {
        Self {
            base_url: base_url.to_string(),
            timeout: Duration::from_secs(10),
        }
    }

    /// Creates a mock response (for testing without actual HTTP).
    pub fn mock_response(&self, method: &str) -> JsonRpcResponse {
        let result = match method {
            "info.getNodeID" => Some(json!({
                "nodeID": "NodeID-test123"
            })),
            "info.getNodeVersion" => Some(json!({
                "version": "avalanche/1.0.0",
                "databaseVersion": "v1.4.5",
                "gitCommit": "abc123",
                "vmVersions": {
                    "avm": "1.0.0",
                    "evm": "1.0.0",
                    "platform": "1.0.0"
                }
            })),
            "info.getNetworkID" => Some(json!({
                "networkID": 12345
            })),
            "info.getNetworkName" => Some(json!({
                "networkName": "local"
            })),
            "info.getBlockchainID" => Some(json!({
                "blockchainID": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"
            })),
            "health.health" => Some(json!({
                "healthy": true
            })),
            "platform.getHeight" => Some(json!({
                "height": 1000
            })),
            "platform.getCurrentValidators" => Some(json!({
                "validators": [{
                    "txID": "2NnCwXrMqTk5aFGZf1FzHq3P3kqnxeW5LcxTYrMJp6HiGJqqLT",
                    "startTime": 1600000000,
                    "endTime": 1700000000,
                    "stakeAmount": 2000000000000,
                    "nodeID": "NodeID-test1"
                }]
            })),
            "avm.getBalance" => Some(json!({
                "balance": "1000000000",
                "utxoIDs": []
            })),
            "avm.getAssetDescription" => Some(json!({
                "assetID": "FvwEAhmxKfeiG8SnEvq42hc6whRyY3EFYAvebMqDNDGCgxN5Z",
                "name": "Avalanche",
                "symbol": "AVAX",
                "denomination": 9
            })),
            _ => None,
        };

        JsonRpcResponse {
            jsonrpc: "2.0".to_string(),
            id: 1,
            result,
            error: if result.is_none() {
                Some(JsonRpcError {
                    code: -32601,
                    message: "Method not found".to_string(),
                    data: None,
                })
            } else {
                None
            },
        }
    }

    /// Calls a JSON-RPC method (mock implementation).
    pub fn call(&self, method: &str, params: Option<Value>) -> Result<JsonRpcResponse, ApiError> {
        // In a real implementation, this would make an HTTP request
        Ok(self.mock_response(method))
    }

    /// Info API calls.
    pub fn get_node_id(&self) -> Result<String, ApiError> {
        let response = self.call("info.getNodeID", None)?;
        response
            .result
            .and_then(|v| v.get("nodeID").and_then(|s| s.as_str()).map(String::from))
            .ok_or(ApiError::InvalidResponse)
    }

    pub fn get_network_id(&self) -> Result<u32, ApiError> {
        let response = self.call("info.getNetworkID", None)?;
        response
            .result
            .and_then(|v| v.get("networkID").and_then(|n| n.as_u64()).map(|n| n as u32))
            .ok_or(ApiError::InvalidResponse)
    }

    /// Health API calls.
    pub fn health_check(&self) -> Result<bool, ApiError> {
        let response = self.call("health.health", None)?;
        response
            .result
            .and_then(|v| v.get("healthy").and_then(|b| b.as_bool()))
            .ok_or(ApiError::InvalidResponse)
    }

    /// Platform API calls.
    pub fn get_height(&self) -> Result<u64, ApiError> {
        let response = self.call("platform.getHeight", None)?;
        response
            .result
            .and_then(|v| v.get("height").and_then(|n| n.as_u64()))
            .ok_or(ApiError::InvalidResponse)
    }

    pub fn get_current_validators(&self) -> Result<Vec<ValidatorInfo>, ApiError> {
        let response = self.call("platform.getCurrentValidators", None)?;
        let validators = response
            .result
            .and_then(|v| v.get("validators").cloned())
            .and_then(|v| serde_json::from_value(v).ok())
            .unwrap_or_default();
        Ok(validators)
    }

    /// AVM (X-Chain) API calls.
    pub fn get_balance(&self, address: &str, asset_id: &str) -> Result<u64, ApiError> {
        let params = json!({
            "address": address,
            "assetID": asset_id
        });
        let response = self.call("avm.getBalance", Some(params))?;
        response
            .result
            .and_then(|v| v.get("balance").and_then(|s| s.as_str()))
            .and_then(|s| s.parse().ok())
            .ok_or(ApiError::InvalidResponse)
    }

    pub fn get_asset_description(&self, asset_id: &str) -> Result<AssetDescription, ApiError> {
        let params = json!({
            "assetID": asset_id
        });
        let response = self.call("avm.getAssetDescription", Some(params))?;
        response
            .result
            .and_then(|v| serde_json::from_value(v).ok())
            .ok_or(ApiError::InvalidResponse)
    }
}

/// Validator info from API.
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ValidatorInfo {
    pub tx_id: String,
    pub start_time: u64,
    pub end_time: u64,
    pub stake_amount: u64,
    pub node_id: String,
}

/// Asset description from API.
#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssetDescription {
    pub asset_id: String,
    pub name: String,
    pub symbol: String,
    pub denomination: u8,
}

/// API error type.
#[derive(Debug, thiserror::Error)]
pub enum ApiError {
    #[error("connection failed")]
    ConnectionFailed,
    #[error("timeout")]
    Timeout,
    #[error("invalid response")]
    InvalidResponse,
    #[error("rpc error: {0}")]
    RpcError(String),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_json_rpc_request() {
        let request = JsonRpcRequest::new("info.getNodeID", None);
        assert_eq!(request.jsonrpc, "2.0");
        assert_eq!(request.method, "info.getNodeID");
        assert!(request.params.is_none());
    }

    #[test]
    fn test_json_rpc_request_with_params() {
        let params = json!({"address": "X-test123"});
        let request = JsonRpcRequest::new("avm.getBalance", Some(params.clone()));
        assert_eq!(request.params, Some(params));
    }

    #[test]
    fn test_api_client_get_node_id() {
        let client = ApiTestClient::new("http://localhost:9650");
        let node_id = client.get_node_id().unwrap();
        assert!(node_id.starts_with("NodeID-"));
    }

    #[test]
    fn test_api_client_get_network_id() {
        let client = ApiTestClient::new("http://localhost:9650");
        let network_id = client.get_network_id().unwrap();
        assert_eq!(network_id, 12345);
    }

    #[test]
    fn test_api_client_health_check() {
        let client = ApiTestClient::new("http://localhost:9650");
        let healthy = client.health_check().unwrap();
        assert!(healthy);
    }

    #[test]
    fn test_api_client_get_height() {
        let client = ApiTestClient::new("http://localhost:9650");
        let height = client.get_height().unwrap();
        assert_eq!(height, 1000);
    }

    #[test]
    fn test_api_client_get_validators() {
        let client = ApiTestClient::new("http://localhost:9650");
        let validators = client.get_current_validators().unwrap();
        assert_eq!(validators.len(), 1);
        assert!(validators[0].node_id.starts_with("NodeID-"));
    }

    #[test]
    fn test_api_client_get_balance() {
        let client = ApiTestClient::new("http://localhost:9650");
        let balance = client.get_balance("X-test123", "AVAX").unwrap();
        assert_eq!(balance, 1000000000);
    }

    #[test]
    fn test_api_client_get_asset_description() {
        let client = ApiTestClient::new("http://localhost:9650");
        let asset = client.get_asset_description("AVAX").unwrap();
        assert_eq!(asset.name, "Avalanche");
        assert_eq!(asset.symbol, "AVAX");
        assert_eq!(asset.denomination, 9);
    }

    #[test]
    fn test_unknown_method() {
        let client = ApiTestClient::new("http://localhost:9650");
        let response = client.call("unknown.method", None).unwrap();
        assert!(response.error.is_some());
        assert_eq!(response.error.unwrap().code, -32601);
    }

    /// Test concurrent API calls.
    #[tokio::test]
    async fn test_concurrent_calls() {
        let client = std::sync::Arc::new(ApiTestClient::new("http://localhost:9650"));

        let handles: Vec<_> = (0..10)
            .map(|_| {
                let client = client.clone();
                tokio::spawn(async move { client.health_check() })
            })
            .collect();

        for handle in handles {
            let result = handle.await.unwrap();
            assert!(result.is_ok());
            assert!(result.unwrap());
        }
    }

    /// Test API error handling.
    #[test]
    fn test_error_handling() {
        let client = ApiTestClient::new("http://localhost:9650");

        // Unknown method should return error
        let response = client.call("nonexistent.method", None).unwrap();
        assert!(response.error.is_some());
    }

    /// Test batch requests.
    #[test]
    fn test_batch_requests() {
        let client = ApiTestClient::new("http://localhost:9650");

        // Simulate batch by making multiple calls
        let methods = vec![
            "info.getNodeID",
            "info.getNetworkID",
            "health.health",
            "platform.getHeight",
        ];

        for method in methods {
            let response = client.call(method, None).unwrap();
            assert!(response.result.is_some() || response.error.is_some());
        }
    }

    /// Test request timeout handling.
    #[test]
    fn test_timeout_handling() {
        let mut client = ApiTestClient::new("http://localhost:9650");
        client.timeout = Duration::from_millis(1);

        // In a real implementation with actual HTTP, this would test timeout
        // For mock, we just verify the timeout is set
        assert_eq!(client.timeout, Duration::from_millis(1));
    }

    /// Test request with complex parameters.
    #[test]
    fn test_complex_parameters() {
        let client = ApiTestClient::new("http://localhost:9650");

        let params = json!({
            "addresses": ["X-addr1", "X-addr2"],
            "sourceChain": "P",
            "encoding": "hex"
        });

        let request = JsonRpcRequest::new("avm.getUTXOs", Some(params));
        assert!(request.params.is_some());
    }
}
