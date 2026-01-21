//! Error types for virtual machines.

use thiserror::Error;

/// Result type for VM operations.
pub type Result<T> = std::result::Result<T, VMError>;

/// Errors that can occur in virtual machine operations.
#[derive(Debug, Error, Clone)]
pub enum VMError {
    /// VM is not initialized
    #[error("VM not initialized")]
    NotInitialized,

    /// VM is already initialized
    #[error("VM already initialized")]
    AlreadyInitialized,

    /// VM is shutting down
    #[error("VM is shutting down")]
    ShuttingDown,

    /// Block not found
    #[error("block not found: {0}")]
    BlockNotFound(String),

    /// Invalid block
    #[error("invalid block: {0}")]
    InvalidBlock(String),

    /// Block parsing failed
    #[error("failed to parse block: {0}")]
    ParseError(String),

    /// Block verification failed
    #[error("block verification failed: {0}")]
    VerificationFailed(String),

    /// State error
    #[error("state error: {0}")]
    StateError(String),

    /// Database error
    #[error("database error: {0}")]
    DatabaseError(String),

    /// Codec error
    #[error("codec error: {0}")]
    CodecError(String),

    /// Network error
    #[error("network error: {0}")]
    NetworkError(String),

    /// Timeout
    #[error("timeout: {0}")]
    Timeout(String),

    /// Invalid parameter
    #[error("invalid parameter: {0}")]
    InvalidParameter(String),

    /// Not implemented
    #[error("not implemented: {0}")]
    NotImplemented(String),

    /// Internal error
    #[error("internal error: {0}")]
    Internal(String),
}

impl From<avalanche_db::DatabaseError> for VMError {
    fn from(err: avalanche_db::DatabaseError) -> Self {
        VMError::DatabaseError(err.to_string())
    }
}
