// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! A `LinearStore` provides a view of a set of bytes at
//! a given time. A `LinearStore` has three different types,
//! which refer to another base type, as follows:
//! ```mermaid
//! stateDiagram-v2
//!     R1(Committed) --> R2(Committed)
//!     R2(Committed) --> R3(FileBacked)
//!     P1(Proposed) --> R3(FileBacked)
//!     P2(Proposed) --> P1(Proposed)
//! ```
//!
//! Each type is described in more detail below.

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 4 occurrences after enabling the lint."
)]

use std::fmt::Debug;
use std::io::{Cursor, Read};
use std::ops::Deref;
use std::path::PathBuf;

use crate::{CacheReadStrategy, LinearAddress, MaybePersistedNode, SharedNode};
pub(super) mod filebacked;
pub mod memory;

/// An error that occurs when reading or writing to a [`ReadableStorage`] or [`WritableStorage`]
///
/// This error is used to wrap errors that occur when reading or writing to a file.
/// It contains the filename, offset, and context of the error.
#[derive(Debug)]
pub struct FileIoError {
    inner: std::io::Error,
    filename: Option<PathBuf>,
    offset: u64,
    context: Option<String>,
}

impl FileIoError {
    /// Create a new [`FileIoError`] from a generic error
    ///
    /// Only use this constructor if you do not have any file or line information.
    ///
    /// # Arguments
    ///
    /// * `error` - The error to wrap
    pub fn from_generic_no_file<T: std::error::Error>(error: T, context: &str) -> Self {
        Self {
            inner: std::io::Error::other(error.to_string()),
            filename: None,
            offset: 0,
            context: Some(context.into()),
        }
    }

    /// Create a new [`FileIoError`]
    ///
    /// # Arguments
    ///
    /// * `inner` - The inner error
    /// * `filename` - The filename of the file that caused the error
    /// * `offset` - The offset of the file that caused the error
    /// * `context` - The context of this error
    #[must_use]
    pub const fn new(
        inner: std::io::Error,
        filename: Option<PathBuf>,
        offset: u64,
        context: Option<String>,
    ) -> Self {
        Self {
            inner,
            filename,
            offset,
            context,
        }
    }
}

impl std::error::Error for FileIoError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        Some(&self.inner)
    }
}

impl std::fmt::Display for FileIoError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{inner} at offset {offset} of file '{filename}' {context}",
            inner = self.inner,
            offset = self.offset,
            filename = self
                .filename
                .as_ref()
                .unwrap_or(&PathBuf::from("[unknown]"))
                .display(),
            context = self.context.as_ref().unwrap_or(&String::new())
        )
    }
}

impl Deref for FileIoError {
    type Target = std::io::Error;

    fn deref(&self) -> &Self::Target {
        &self.inner
    }
}
/// Trait for readable storage.
pub trait ReadableStorage: Debug + Sync + Send {
    /// Stream data from the specified address.
    ///
    /// # Arguments
    ///
    /// * `addr` - The address from which to stream the data.
    ///
    /// # Returns
    ///
    /// A `Result` containing a boxed `Read` trait object, or an `Error` if the operation fails.
    fn stream_from(&self, addr: u64) -> Result<impl OffsetReader, FileIoError>;

    /// Return the size of the underlying storage, in bytes
    fn size(&self) -> Result<u64, FileIoError>;

    /// Read a node from the cache (if any)
    fn read_cached_node(&self, _addr: LinearAddress, _mode: &'static str) -> Option<SharedNode> {
        None
    }

    /// Fetch the next pointer from the freelist cache
    fn free_list_cache(&self, _addr: LinearAddress) -> Option<Option<LinearAddress>> {
        None
    }

    /// Return the cache read strategy for this readable storage
    fn cache_read_strategy(&self) -> &CacheReadStrategy {
        &CacheReadStrategy::WritesOnly
    }

    /// Cache a node for future reads
    fn cache_node(&self, _addr: LinearAddress, _node: SharedNode) {}

    /// Return the filename of the underlying storage
    fn filename(&self) -> Option<PathBuf> {
        None
    }

    /// Convert an `io::Error` into a `FileIoError`
    fn file_io_error(
        &self,
        error: std::io::Error,
        offset: u64,
        context: Option<String>,
    ) -> FileIoError {
        FileIoError {
            inner: error,
            filename: self.filename(),
            offset,
            context,
        }
    }
}

/// Trait for writable storage.
pub trait WritableStorage: ReadableStorage {
    /// Writes the given object at the specified offset.
    ///
    /// # Arguments
    ///
    /// * `offset` - The offset at which to write the object.
    /// * `object` - The object to write.
    ///
    /// # Returns
    ///
    /// The number of bytes written, or an error if the write operation fails.
    fn write(&self, offset: u64, object: &[u8]) -> Result<usize, FileIoError>;

    /// Write all nodes to the cache (if any)
    fn write_cached_nodes(
        &self,
        _nodes: impl IntoIterator<Item = (LinearAddress, SharedNode)>,
    ) -> Result<(), FileIoError> {
        Ok(())
    }

    /// Invalidate all nodes that are part of a specific revision, as these will never be referenced again
    fn invalidate_cached_nodes<'a>(
        &self,
        _addresses: impl Iterator<Item = &'a MaybePersistedNode>,
    ) {
    }

    /// Add a new entry to the freelist cache
    fn add_to_free_list_cache(&self, _addr: LinearAddress, _next: Option<LinearAddress>) {}
}

pub trait OffsetReader: Read {
    fn offset(&self) -> u64;
}

impl<T> OffsetReader for Cursor<T>
where
    Cursor<T>: Read,
{
    fn offset(&self) -> u64 {
        self.position()
    }
}
