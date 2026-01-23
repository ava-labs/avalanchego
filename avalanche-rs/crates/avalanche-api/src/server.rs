//! JSON-RPC server implementation.

use std::collections::HashMap;
use std::future::Future;
use std::pin::Pin;
use std::sync::Arc;

use parking_lot::RwLock;
use serde_json::Value;

use crate::jsonrpc::{BatchRequest, BatchResponse, Request, Response, RpcError};

/// Type alias for async handler function result.
pub type HandlerResult = Pin<Box<dyn Future<Output = Result<Value, RpcError>> + Send>>;

/// Type alias for handler function.
pub type HandlerFn = Arc<dyn Fn(Option<Value>) -> HandlerResult + Send + Sync>;

/// JSON-RPC method handler.
pub struct Handler {
    /// Handler function
    func: HandlerFn,
    /// Method description
    description: String,
}

impl Handler {
    /// Creates a new handler.
    pub fn new<F, Fut>(description: impl Into<String>, func: F) -> Self
    where
        F: Fn(Option<Value>) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<Value, RpcError>> + Send + 'static,
    {
        Self {
            func: Arc::new(move |params| Box::pin(func(params))),
            description: description.into(),
        }
    }
}

/// JSON-RPC endpoint.
pub struct Endpoint {
    /// Endpoint path
    path: String,
    /// Registered methods
    methods: RwLock<HashMap<String, Handler>>,
}

impl Endpoint {
    /// Creates a new endpoint.
    pub fn new(path: impl Into<String>) -> Self {
        Self {
            path: path.into(),
            methods: RwLock::new(HashMap::new()),
        }
    }

    /// Returns the endpoint path.
    pub fn path(&self) -> &str {
        &self.path
    }

    /// Registers a method handler.
    pub fn register<F, Fut>(&self, method: impl Into<String>, description: impl Into<String>, func: F)
    where
        F: Fn(Option<Value>) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = Result<Value, RpcError>> + Send + 'static,
    {
        let handler = Handler::new(description, func);
        self.methods.write().insert(method.into(), handler);
    }

    /// Handles a single request.
    pub async fn handle_request(&self, request: Request) -> Response {
        // Clone the handler function to avoid holding the lock across await
        let handler_fn = {
            let methods = self.methods.read();
            methods.get(&request.method).map(|h| h.func.clone())
        };

        match handler_fn {
            Some(func) => match func(request.params).await {
                Ok(result) => Response::success(request.id, result),
                Err(error) => Response::error(request.id, error),
            },
            None => Response::error(request.id, RpcError::method_not_found()),
        }
    }

    /// Handles a batch request.
    pub async fn handle_batch(&self, batch: BatchRequest) -> BatchResponse {
        match batch {
            BatchRequest::Single(req) => {
                BatchResponse::Single(self.handle_request(req).await)
            }
            BatchRequest::Batch(requests) => {
                let mut responses = Vec::with_capacity(requests.len());
                for req in requests {
                    responses.push(self.handle_request(req).await);
                }
                BatchResponse::Batch(responses)
            }
        }
    }

    /// Returns list of registered methods.
    pub fn list_methods(&self) -> Vec<String> {
        self.methods.read().keys().cloned().collect()
    }
}

/// JSON-RPC server.
pub struct Server {
    /// Registered endpoints
    endpoints: RwLock<HashMap<String, Arc<Endpoint>>>,
    /// Server address
    address: String,
}

impl Server {
    /// Creates a new server.
    pub fn new(address: impl Into<String>) -> Self {
        Self {
            endpoints: RwLock::new(HashMap::new()),
            address: address.into(),
        }
    }

    /// Returns the server address.
    pub fn address(&self) -> &str {
        &self.address
    }

    /// Registers an endpoint.
    pub fn register_endpoint(&self, endpoint: Endpoint) {
        let path = endpoint.path.clone();
        self.endpoints.write().insert(path, Arc::new(endpoint));
    }

    /// Gets an endpoint by path.
    pub fn get_endpoint(&self, path: &str) -> Option<Arc<Endpoint>> {
        self.endpoints.read().get(path).cloned()
    }

    /// Routes a request to the appropriate endpoint.
    pub async fn route(&self, path: &str, request: Request) -> Response {
        match self.get_endpoint(path) {
            Some(endpoint) => endpoint.handle_request(request).await,
            None => Response::error(
                request.id,
                RpcError::new(-32000, format!("endpoint not found: {}", path)),
            ),
        }
    }

    /// Routes a batch request to the appropriate endpoint.
    pub async fn route_batch(&self, path: &str, batch: BatchRequest) -> BatchResponse {
        match self.get_endpoint(path) {
            Some(endpoint) => endpoint.handle_batch(batch).await,
            None => {
                let error = RpcError::new(-32000, format!("endpoint not found: {}", path));
                match batch {
                    BatchRequest::Single(req) => {
                        BatchResponse::Single(Response::error(req.id, error))
                    }
                    BatchRequest::Batch(requests) => {
                        let responses = requests
                            .into_iter()
                            .map(|req| Response::error(req.id, error.clone()))
                            .collect();
                        BatchResponse::Batch(responses)
                    }
                }
            }
        }
    }

    /// Lists all registered endpoints.
    pub fn list_endpoints(&self) -> Vec<String> {
        self.endpoints.read().keys().cloned().collect()
    }
}

impl Default for Server {
    fn default() -> Self {
        Self::new("127.0.0.1:9650")
    }
}

/// Creates a standard Info API endpoint.
pub fn create_info_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/info");

    endpoint.register("info.getNodeID", "Get the node's ID", |_| async {
        Ok(serde_json::json!({
            "nodeID": "NodeID-example123"
        }))
    });

    endpoint.register("info.getNetworkID", "Get the network ID", |_| async {
        Ok(serde_json::json!({
            "networkID": "1"
        }))
    });

    endpoint.register("info.getNetworkName", "Get the network name", |_| async {
        Ok(serde_json::json!({
            "networkName": "mainnet"
        }))
    });

    endpoint.register("info.isBootstrapped", "Check if chain is bootstrapped", |params| async move {
        let chain = params
            .and_then(|p| p.get("chain").and_then(|c| c.as_str()).map(String::from))
            .unwrap_or_else(|| "P".to_string());

        Ok(serde_json::json!({
            "isBootstrapped": true,
            "chain": chain
        }))
    });

    endpoint
}

/// Creates a standard Health API endpoint.
pub fn create_health_endpoint() -> Endpoint {
    let endpoint = Endpoint::new("ext/health");

    endpoint.register("health.health", "Get health status", |_| async {
        Ok(serde_json::json!({
            "healthy": true
        }))
    });

    endpoint
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::jsonrpc::RequestId;

    #[tokio::test]
    async fn test_endpoint_handler() {
        let endpoint = Endpoint::new("test");

        endpoint.register("test.echo", "Echo params back", |params| async move {
            Ok(params.unwrap_or(Value::Null))
        });

        let request = Request::new(
            "test.echo",
            Some(serde_json::json!({"message": "hello"})),
            1,
        );

        let response = endpoint.handle_request(request).await;
        assert!(response.is_success());

        let result = response.result.unwrap();
        assert_eq!(result.get("message").unwrap(), "hello");
    }

    #[tokio::test]
    async fn test_method_not_found() {
        let endpoint = Endpoint::new("test");

        let request = Request::new("unknown.method", None, 1);
        let response = endpoint.handle_request(request).await;

        assert!(response.is_error());
        assert_eq!(response.error.unwrap().code, -32601);
    }

    #[tokio::test]
    async fn test_server_routing() {
        let server = Server::new("127.0.0.1:9650");

        let endpoint = Endpoint::new("ext/test");
        endpoint.register("test.ping", "Ping test", |_| async {
            Ok(serde_json::json!("pong"))
        });

        server.register_endpoint(endpoint);

        let request = Request::new("test.ping", None, 1);
        let response = server.route("ext/test", request).await;

        assert!(response.is_success());
        assert_eq!(response.result.unwrap(), "pong");
    }

    #[tokio::test]
    async fn test_endpoint_not_found() {
        let server = Server::new("127.0.0.1:9650");

        let request = Request::new("test.method", None, 1);
        let response = server.route("unknown/path", request).await;

        assert!(response.is_error());
        assert!(response.error.unwrap().message.contains("endpoint not found"));
    }

    #[tokio::test]
    async fn test_batch_request() {
        let endpoint = Endpoint::new("test");
        endpoint.register("add", "Add numbers", |params| async move {
            let p = params.ok_or_else(|| RpcError::invalid_params("missing params"))?;
            let a = p.get("a").and_then(|v| v.as_i64()).unwrap_or(0);
            let b = p.get("b").and_then(|v| v.as_i64()).unwrap_or(0);
            Ok(serde_json::json!(a + b))
        });

        let batch = BatchRequest::Batch(vec![
            Request::new("add", Some(serde_json::json!({"a": 1, "b": 2})), 1),
            Request::new("add", Some(serde_json::json!({"a": 3, "b": 4})), 2),
        ]);

        let response = endpoint.handle_batch(batch).await;

        match response {
            BatchResponse::Batch(responses) => {
                assert_eq!(responses.len(), 2);
                assert_eq!(responses[0].result.as_ref().unwrap(), &serde_json::json!(3));
                assert_eq!(responses[1].result.as_ref().unwrap(), &serde_json::json!(7));
            }
            _ => panic!("Expected batch response"),
        }
    }

    #[test]
    fn test_info_endpoint() {
        let endpoint = create_info_endpoint();
        let methods = endpoint.list_methods();

        assert!(methods.contains(&"info.getNodeID".to_string()));
        assert!(methods.contains(&"info.getNetworkID".to_string()));
        assert!(methods.contains(&"info.isBootstrapped".to_string()));
    }
}
