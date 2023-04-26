use std::sync::Arc;

use firewood_libaio::AIOError;
use nix::errno::Errno;
use thiserror::Error;

#[derive(Clone, Debug, Error)]
pub enum WALError {
    #[error("an unclassified error has occurred: {0}")]
    Other(String),
    #[error("an OS error {0} has occurred")]
    UnixError(#[from] Errno),
    #[error("a checksum check has failed")]
    InvalidChecksum,
    #[error("an I/O error has occurred")]
    IOError(Arc<std::io::Error>),
    #[error("lib AIO error has occurred")]
    AIOError(AIOError),
    #[error("WAL directory already exists")]
    WALDirExists,
}

impl From<i32> for WALError {
    fn from(value: i32) -> Self {
        Self::UnixError(Errno::from_i32(value))
    }
}

impl From<std::io::Error> for WALError {
    fn from(err: std::io::Error) -> Self {
        Self::IOError(Arc::new(err))
    }
}

impl From<AIOError> for WALError {
    fn from(err: AIOError) -> Self {
        Self::AIOError(err)
    }
}
