//! HTTP server implementation using hyper.
//!
//! This module provides a real HTTP server that serves JSON-RPC requests.

use std::convert::Infallible;
use std::net::SocketAddr;
use std::sync::Arc;

use bytes::Bytes;
use http_body_util::{BodyExt, Full};
use hyper::body::Incoming;
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Method, Request as HttpRequest, Response as HttpResponse, StatusCode};
use hyper_util::rt::TokioIo;
use tokio::net::TcpListener;
use tokio::sync::broadcast;
use tracing::{debug, error, info, warn};

use crate::jsonrpc::{BatchRequest, BatchResponse, Request, Response, RpcError};
use crate::server::Server;

/// HTTP server configuration.
#[derive(Debug, Clone)]
pub struct HttpConfig {
    /// Address to bind to.
    pub addr: SocketAddr,
    /// Maximum request body size in bytes.
    pub max_body_size: usize,
    /// Enable CORS.
    pub cors_enabled: bool,
}

impl Default for HttpConfig {
    fn default() -> Self {
        Self {
            addr: "127.0.0.1:9650".parse().unwrap(),
            max_body_size: 10 * 1024 * 1024, // 10 MB
            cors_enabled: true,
        }
    }
}

/// HTTP server wrapping the JSON-RPC server.
pub struct HttpServer {
    /// Configuration.
    config: HttpConfig,
    /// JSON-RPC server.
    rpc_server: Arc<Server>,
    /// Shutdown signal sender.
    shutdown_tx: broadcast::Sender<()>,
}

impl HttpServer {
    /// Creates a new HTTP server.
    pub fn new(config: HttpConfig, rpc_server: Arc<Server>) -> Self {
        let (shutdown_tx, _) = broadcast::channel(1);
        Self {
            config,
            rpc_server,
            shutdown_tx,
        }
    }

    /// Returns a shutdown receiver.
    pub fn shutdown_receiver(&self) -> broadcast::Receiver<()> {
        self.shutdown_tx.subscribe()
    }

    /// Signals the server to shut down.
    pub fn shutdown(&self) {
        let _ = self.shutdown_tx.send(());
    }

    /// Runs the HTTP server.
    pub async fn run(self: Arc<Self>) -> Result<(), HttpServerError> {
        let listener = TcpListener::bind(self.config.addr)
            .await
            .map_err(|e| HttpServerError::Bind(e.to_string()))?;

        info!("HTTP server listening on {}", self.config.addr);

        let mut shutdown_rx = self.shutdown_tx.subscribe();

        loop {
            tokio::select! {
                result = listener.accept() => {
                    match result {
                        Ok((stream, addr)) => {
                            debug!("Accepted connection from {}", addr);
                            let server = self.clone();
                            tokio::spawn(async move {
                                if let Err(e) = server.handle_connection(stream).await {
                                    warn!("Connection error from {}: {}", addr, e);
                                }
                            });
                        }
                        Err(e) => {
                            error!("Accept error: {}", e);
                        }
                    }
                }
                _ = shutdown_rx.recv() => {
                    info!("HTTP server shutting down");
                    break;
                }
            }
        }

        Ok(())
    }

    /// Handles a single TCP connection.
    async fn handle_connection(
        self: Arc<Self>,
        stream: tokio::net::TcpStream,
    ) -> Result<(), HttpServerError> {
        let io = TokioIo::new(stream);

        let service = service_fn(move |req| {
            let server = self.clone();
            async move { server.handle_request(req).await }
        });

        http1::Builder::new()
            .serve_connection(io, service)
            .await
            .map_err(|e| HttpServerError::Connection(e.to_string()))?;

        Ok(())
    }

    /// Handles an HTTP request.
    async fn handle_request(
        self: Arc<Self>,
        req: HttpRequest<Incoming>,
    ) -> Result<HttpResponse<Full<Bytes>>, Infallible> {
        let method = req.method().clone();
        let path = req.uri().path().to_string();

        debug!("{} {}", method, path);

        // Handle CORS preflight
        if method == Method::OPTIONS {
            return Ok(self.cors_response(StatusCode::NO_CONTENT));
        }

        // Only accept POST for JSON-RPC
        if method != Method::POST {
            return Ok(self.error_response(
                StatusCode::METHOD_NOT_ALLOWED,
                "Only POST method is supported",
            ));
        }

        // Check content type
        let content_type = req
            .headers()
            .get("content-type")
            .and_then(|v| v.to_str().ok())
            .unwrap_or("");

        if !content_type.contains("application/json") {
            return Ok(self.error_response(
                StatusCode::UNSUPPORTED_MEDIA_TYPE,
                "Content-Type must be application/json",
            ));
        }

        // Read body
        let body = match self.read_body(req).await {
            Ok(body) => body,
            Err(e) => {
                return Ok(self.error_response(StatusCode::BAD_REQUEST, &e.to_string()));
            }
        };

        // Parse JSON-RPC request
        let response_body = match self.process_rpc_request(&path, &body).await {
            Ok(resp) => resp,
            Err(e) => {
                let error_response = Response::error(
                    crate::jsonrpc::RequestId::Null,
                    RpcError::new(-32700, format!("Parse error: {}", e)),
                );
                serde_json::to_vec(&error_response).unwrap_or_default()
            }
        };

        Ok(self.json_response(StatusCode::OK, response_body))
    }

    /// Reads the request body.
    async fn read_body(&self, req: HttpRequest<Incoming>) -> Result<Vec<u8>, HttpServerError> {
        let body = req.collect().await.map_err(|e| HttpServerError::Body(e.to_string()))?;
        let bytes = body.to_bytes();

        if bytes.len() > self.config.max_body_size {
            return Err(HttpServerError::Body("Request body too large".to_string()));
        }

        Ok(bytes.to_vec())
    }

    /// Processes a JSON-RPC request.
    async fn process_rpc_request(
        &self,
        path: &str,
        body: &[u8],
    ) -> Result<Vec<u8>, HttpServerError> {
        // Try to parse as batch or single request
        let batch_request: BatchRequest =
            serde_json::from_slice(body).map_err(|e| HttpServerError::Parse(e.to_string()))?;

        // Route to the appropriate endpoint
        // Remove leading slash and convert to endpoint path
        let endpoint_path = path.trim_start_matches('/');

        let response = self.rpc_server.route_batch(endpoint_path, batch_request).await;

        let response_body = match response {
            BatchResponse::Single(resp) => {
                serde_json::to_vec(&resp).map_err(|e| HttpServerError::Parse(e.to_string()))?
            }
            BatchResponse::Batch(resps) => {
                serde_json::to_vec(&resps).map_err(|e| HttpServerError::Parse(e.to_string()))?
            }
        };

        Ok(response_body)
    }

    /// Creates a JSON response.
    fn json_response(&self, status: StatusCode, body: Vec<u8>) -> HttpResponse<Full<Bytes>> {
        let mut response = HttpResponse::builder()
            .status(status)
            .header("Content-Type", "application/json")
            .header("Content-Length", body.len());

        if self.config.cors_enabled {
            response = response
                .header("Access-Control-Allow-Origin", "*")
                .header("Access-Control-Allow-Methods", "POST, OPTIONS")
                .header("Access-Control-Allow-Headers", "Content-Type");
        }

        response.body(Full::new(Bytes::from(body))).unwrap()
    }

    /// Creates an error response.
    fn error_response(&self, status: StatusCode, message: &str) -> HttpResponse<Full<Bytes>> {
        let body = serde_json::json!({
            "error": message
        });
        self.json_response(status, serde_json::to_vec(&body).unwrap())
    }

    /// Creates a CORS preflight response.
    fn cors_response(&self, status: StatusCode) -> HttpResponse<Full<Bytes>> {
        HttpResponse::builder()
            .status(status)
            .header("Access-Control-Allow-Origin", "*")
            .header("Access-Control-Allow-Methods", "POST, OPTIONS")
            .header("Access-Control-Allow-Headers", "Content-Type")
            .header("Access-Control-Max-Age", "86400")
            .body(Full::new(Bytes::new()))
            .unwrap()
    }
}

/// HTTP server errors.
#[derive(Debug, thiserror::Error)]
pub enum HttpServerError {
    #[error("bind error: {0}")]
    Bind(String),
    #[error("connection error: {0}")]
    Connection(String),
    #[error("body error: {0}")]
    Body(String),
    #[error("parse error: {0}")]
    Parse(String),
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::server::Endpoint;
    use std::time::Duration;

    #[tokio::test]
    async fn test_http_server_creation() {
        let rpc_server = Arc::new(Server::new("127.0.0.1:9650"));

        let endpoint = Endpoint::new("ext/test");
        endpoint.register("test.ping", "Ping", |_| async { Ok(serde_json::json!("pong")) });
        rpc_server.register_endpoint(endpoint);

        let config = HttpConfig {
            addr: "127.0.0.1:0".parse().unwrap(), // Use port 0 for testing
            ..Default::default()
        };

        let http_server = Arc::new(HttpServer::new(config, rpc_server));

        // Just verify it was created successfully
        assert!(http_server.config.cors_enabled);
    }

    #[tokio::test]
    async fn test_http_server_startup_shutdown() {
        let rpc_server = Arc::new(Server::new("127.0.0.1:9651"));
        let config = HttpConfig {
            addr: "127.0.0.1:19650".parse().unwrap(),
            ..Default::default()
        };

        let http_server = Arc::new(HttpServer::new(config, rpc_server));
        let server_clone = http_server.clone();

        // Start server in background
        let handle = tokio::spawn(async move {
            let _ = server_clone.run().await;
        });

        // Give it time to start
        tokio::time::sleep(Duration::from_millis(100)).await;

        // Signal shutdown
        http_server.shutdown();

        // Wait for server to stop
        tokio::time::timeout(Duration::from_secs(2), handle)
            .await
            .expect("Server should shut down")
            .expect("Server task should complete");
    }
}
