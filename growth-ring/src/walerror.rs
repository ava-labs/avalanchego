// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::sync::Arc;

use nix::errno::Errno;
use thiserror::Error;

#[derive(Clone, Debug, Error)]
pub enum WalError {
    #[error("an unclassified error has occurred: {0}")]
    Other(String),
    #[error("an OS error {0} has occurred")]
    UnixError(#[from] Errno),
    #[error("a checksum check has failed")]
    InvalidChecksum,
    #[error("an I/O error has occurred")]
    IOError(Arc<std::io::Error>),
    #[error("Wal directory already exists")]
    WalDirExists,
}

impl From<i32> for WalError {
    fn from(value: i32) -> Self {
        Self::UnixError(Errno::from_raw(value))
    }
}

impl From<std::io::Error> for WalError {
    fn from(err: std::io::Error) -> Self {
        Self::IOError(Arc::new(err))
    }
}
