//! JSON-RPC 2.0 types and utilities.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// JSON-RPC version string.
pub const JSONRPC_VERSION: &str = "2.0";

/// JSON-RPC request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Request {
    /// JSON-RPC version (must be "2.0")
    pub jsonrpc: String,
    /// Method name
    pub method: String,
    /// Parameters (positional or named)
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub params: Option<Value>,
    /// Request ID (for matching responses)
    pub id: RequestId,
}

impl Request {
    /// Creates a new JSON-RPC request.
    pub fn new(method: impl Into<String>, params: Option<Value>, id: impl Into<RequestId>) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            method: method.into(),
            params,
            id: id.into(),
        }
    }

    /// Creates a request with no parameters.
    pub fn simple(method: impl Into<String>, id: impl Into<RequestId>) -> Self {
        Self::new(method, None, id)
    }
}

/// JSON-RPC request ID.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Hash)]
#[serde(untagged)]
pub enum RequestId {
    /// Numeric ID
    Number(i64),
    /// String ID
    String(String),
    /// Null ID (notification, no response expected)
    Null,
}

impl From<i64> for RequestId {
    fn from(n: i64) -> Self {
        RequestId::Number(n)
    }
}

impl From<String> for RequestId {
    fn from(s: String) -> Self {
        RequestId::String(s)
    }
}

impl From<&str> for RequestId {
    fn from(s: &str) -> Self {
        RequestId::String(s.to_string())
    }
}

/// JSON-RPC response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Response {
    /// JSON-RPC version
    pub jsonrpc: String,
    /// Result (mutually exclusive with error)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    /// Error (mutually exclusive with result)
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<RpcError>,
    /// Request ID
    pub id: RequestId,
}

impl Response {
    /// Creates a successful response.
    pub fn success(id: RequestId, result: Value) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            result: Some(result),
            error: None,
            id,
        }
    }

    /// Creates an error response.
    pub fn error(id: RequestId, error: RpcError) -> Self {
        Self {
            jsonrpc: JSONRPC_VERSION.to_string(),
            result: None,
            error: Some(error),
            id,
        }
    }

    /// Returns true if this is an error response.
    pub fn is_error(&self) -> bool {
        self.error.is_some()
    }

    /// Returns true if this is a success response.
    pub fn is_success(&self) -> bool {
        self.result.is_some()
    }

    /// Gets the result, if successful.
    pub fn into_result(self) -> Result<Value, RpcError> {
        if let Some(error) = self.error {
            Err(error)
        } else {
            self.result.ok_or_else(|| RpcError::internal("missing result"))
        }
    }
}

/// JSON-RPC error.
#[derive(Debug, Clone, Serialize, Deserialize, thiserror::Error)]
#[error("RPC error {code}: {message}")]
pub struct RpcError {
    /// Error code
    pub code: i32,
    /// Error message
    pub message: String,
    /// Additional error data
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
}

impl RpcError {
    /// Creates a new RPC error.
    pub fn new(code: i32, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
            data: None,
        }
    }

    /// Creates an RPC error with data.
    pub fn with_data(code: i32, message: impl Into<String>, data: Value) -> Self {
        Self {
            code,
            message: message.into(),
            data: Some(data),
        }
    }

    /// Standard error: Parse error.
    pub fn parse_error() -> Self {
        Self::new(-32700, "Parse error")
    }

    /// Standard error: Invalid request.
    pub fn invalid_request() -> Self {
        Self::new(-32600, "Invalid request")
    }

    /// Standard error: Method not found.
    pub fn method_not_found() -> Self {
        Self::new(-32601, "Method not found")
    }

    /// Standard error: Invalid params.
    pub fn invalid_params(msg: impl Into<String>) -> Self {
        Self::new(-32602, format!("Invalid params: {}", msg.into()))
    }

    /// Standard error: Internal error.
    pub fn internal(msg: impl Into<String>) -> Self {
        Self::new(-32603, format!("Internal error: {}", msg.into()))
    }

    /// Application-specific error.
    pub fn application(code: i32, message: impl Into<String>) -> Self {
        assert!(code >= -32000 && code <= -32099, "Application error codes must be in -32000 to -32099");
        Self::new(code, message)
    }
}

/// Batch of JSON-RPC requests.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum BatchRequest {
    /// Single request
    Single(Request),
    /// Batch of requests
    Batch(Vec<Request>),
}

/// Batch of JSON-RPC responses.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum BatchResponse {
    /// Single response
    Single(Response),
    /// Batch of responses
    Batch(Vec<Response>),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_request_serialization() {
        let req = Request::new(
            "eth_blockNumber",
            Some(serde_json::json!([])),
            1,
        );

        let json = serde_json::to_string(&req).unwrap();
        assert!(json.contains("eth_blockNumber"));
        assert!(json.contains("\"id\":1"));

        let parsed: Request = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.method, "eth_blockNumber");
    }

    #[test]
    fn test_response_success() {
        let resp = Response::success(
            RequestId::Number(1),
            serde_json::json!("0x123"),
        );

        assert!(resp.is_success());
        assert!(!resp.is_error());

        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("\"result\":\"0x123\""));
    }

    #[test]
    fn test_response_error() {
        let resp = Response::error(
            RequestId::Number(1),
            RpcError::method_not_found(),
        );

        assert!(resp.is_error());
        assert!(!resp.is_success());
    }

    #[test]
    fn test_error_codes() {
        assert_eq!(RpcError::parse_error().code, -32700);
        assert_eq!(RpcError::invalid_request().code, -32600);
        assert_eq!(RpcError::method_not_found().code, -32601);
    }
}
