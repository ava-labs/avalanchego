//! Error types for consensus.

use avalanche_db::DatabaseError;
use thiserror::Error;

/// Result type for consensus operations.
pub type Result<T> = std::result::Result<T, ConsensusError>;

/// Errors that can occur during consensus operations.
#[derive(Debug, Error, Clone)]
pub enum ConsensusError {
    /// Consensus has already finalized
    #[error("consensus already finalized")]
    AlreadyFinalized,

    /// Invalid consensus parameters
    #[error("invalid parameters: {0}")]
    InvalidParameters(String),

    /// Unknown choice in poll
    #[error("unknown choice: {0}")]
    UnknownChoice(String),

    /// Block not found
    #[error("block not found: {0}")]
    BlockNotFound(String),

    /// Invalid block
    #[error("invalid block: {0}")]
    InvalidBlock(String),

    /// Parent block not found
    #[error("parent block not found: {0}")]
    ParentNotFound(String),

    /// Block already exists
    #[error("block already exists: {0}")]
    BlockExists(String),

    /// Engine not in correct state
    #[error("invalid engine state: expected {expected}, got {actual}")]
    InvalidState { expected: String, actual: String },

    /// Timeout waiting for response
    #[error("timeout: {0}")]
    Timeout(String),

    /// Not enough validators to sample
    #[error("insufficient validators: need {needed}, have {have}")]
    InsufficientValidators { needed: usize, have: usize },

    /// Database error
    #[error("database error: {0}")]
    Database(String),

    /// Internal error
    #[error("internal error: {0}")]
    Internal(String),

    /// Not enough peers to sync
    #[error("not enough peers available for sync")]
    NotEnoughPeers,

    /// Sync engine already running
    #[error("sync engine is already running")]
    AlreadyRunning,

    /// Network error
    #[error("network error: {0}")]
    Network(String),

    /// VM already initialized
    #[error("VM already initialized")]
    VMAlreadyInitialized,

    /// VM not initialized
    #[error("VM not initialized")]
    VMNotInitialized,

    /// VM error
    #[error("VM error: {0}")]
    VM(String),

    /// Not found error
    #[error("not found: {0}")]
    NotFound(String),
}

impl From<DatabaseError> for ConsensusError {
    fn from(e: DatabaseError) -> Self {
        ConsensusError::Database(e.to_string())
    }
}
