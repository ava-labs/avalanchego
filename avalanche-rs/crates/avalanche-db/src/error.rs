//! Database error types.

use thiserror::Error;

/// Errors that can occur during database operations.
#[derive(Debug, Error, Clone)]
pub enum DatabaseError {
    /// The database has been closed.
    #[error("database closed")]
    Closed,

    /// The requested key was not found.
    #[error("not found")]
    NotFound,

    /// An I/O error occurred.
    #[error("I/O error: {0}")]
    Io(String),

    /// A serialization error occurred.
    #[error("serialization error: {0}")]
    Serialization(String),

    /// The batch has already been written.
    #[error("batch already written")]
    BatchAlreadyWritten,

    /// Invalid operation.
    #[error("invalid operation: {0}")]
    InvalidOperation(String),

    /// Corruption detected.
    #[error("corruption: {0}")]
    Corruption(String),
}

impl From<std::io::Error> for DatabaseError {
    fn from(err: std::io::Error) -> Self {
        Self::Io(err.to_string())
    }
}

/// Result type for database operations.
pub type Result<T> = std::result::Result<T, DatabaseError>;
