//! Avalanche API client and server implementation.
//!
//! This crate provides JSON-RPC API implementations for Avalanche VMs.

pub mod client;
pub mod endpoints;
pub mod http;
pub mod jsonrpc;
pub mod server;

pub use client::Client;
pub use endpoints::{
    create_admin_endpoint, create_avm_endpoint, create_evm_endpoint, create_platform_endpoint,
    create_standard_server,
};
pub use http::{HttpConfig, HttpServer, HttpServerError};
pub use jsonrpc::{Request, Response, RpcError};
pub use server::Server;
