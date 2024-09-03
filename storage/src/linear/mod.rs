// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// TODO: remove this once we have code that uses it
#![allow(dead_code)]

//! A LinearStore provides a view of a set of bytes at
//! a given time. A LinearStore has three different types,
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

use std::fmt::Debug;
use std::io::{Error, Read};
use std::num::NonZero;
use std::sync::Arc;

use crate::{LinearAddress, Node};
pub(super) mod filebacked;
pub mod memory;

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

    fn stream_from(&self, addr: u64) -> Result<Box<dyn Read>, Error>;

    /// Return the size of the underlying storage, in bytes
    fn size(&self) -> Result<u64, Error>;

    /// Read a node from the cache (if any)
    fn read_cached_node(&self, _addr: LinearAddress) -> Option<Arc<Node>> {
        None
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
    fn write(&self, offset: u64, object: &[u8]) -> Result<usize, Error>;

    /// Write all nodes to the cache (if any)
    fn write_cached_nodes<'a>(
        &self,
        _nodes: impl Iterator<Item = (&'a NonZero<u64>, &'a Arc<Node>)>,
    ) -> Result<(), Error> {
        Ok(())
    }

    /// Invalidate all nodes that are part of a specific revision, as these will never be referenced again
    fn invalidate_cached_nodes<'a>(&self, _addresses: impl Iterator<Item = &'a LinearAddress>) {}
}
