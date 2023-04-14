use nix::errno::Errno;
use thiserror::Error;

#[derive(Clone, Debug, Error)]
pub enum WALError {
    #[error("an unclassified error has occurred")]
    Other,
    #[error("an OS error {0} has occurred")]
    UnixError(#[from] Errno),
    #[error("a checksum check has failed")]
    InvalidChecksum,
}

impl From<i32> for WALError {
    fn from(value: i32) -> Self {
        Self::UnixError(Errno::from_i32(value))
    }
}
