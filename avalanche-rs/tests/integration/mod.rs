//! Integration tests for avalanche-rs.
//!
//! These tests verify end-to-end functionality including:
//! - Node startup and shutdown
//! - P2P networking
//! - Consensus operation
//! - API endpoints
//! - State synchronization

pub mod network;
pub mod consensus;
pub mod api;
pub mod vm;
