//! Avalanche API client and server implementation.
//!
//! This crate provides JSON-RPC API implementations for Avalanche VMs.

pub mod client;
pub mod jsonrpc;
pub mod server;

pub use client::Client;
pub use jsonrpc::{Request, Response, RpcError};
pub use server::Server;
