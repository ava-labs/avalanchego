// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Header Module
//!
//! This module defines the nodestore header structure and validation logic for ensuring
//! database compatibility across different versions and configurations.
//!
//! ## Header Structure
//!
//! The `NodeStoreHeader` is stored at the beginning of every nodestore file and contains:
//!
//! - **Version String** - Human-readable firewood version (e.g., "firewood 0.1.0")
//! - **Endianness Test** - Detects byte order mismatches between platforms
//! - **Root Address** - Points to the merkle trie root node (if any)
//! - **Storage Size** - Total allocated storage space
//! - **Free Lists** - Array of free space linked list heads for each area size
//!
//! ## Storage Layout
//!
//! The header occupies the first 2048 bytes of storage:
//! - Fixed size for alignment with disk block boundaries
//! - Zero-padded to full size for consistent layout
//! - Uses C-compatible representation for cross-language access
//!

use bytemuck_derive::{Pod, Zeroable};
use std::io::{Error, ErrorKind, Read};

use super::alloc::FreeLists;
use super::primitives::{LinearAddress, area_size_hash};
use crate::logger::{debug, trace};
use crate::{NodeHashAlgorithm, TrieHash};

/// A tuple indicating the address and hash of a node (the root node).
pub type RootNodeInfo = (LinearAddress, TrieHash);

/// Can be used by filesystem tooling such as "file" to identify the version of
/// firewood used to create this `NodeStore` file.
///
/// From firewood v0.0.4 to sometime between v0.0.18, the 16 bytes "firewood 0.0.x"
/// followed by enought NUL bytes to pad to 16 bytes. Between v0.0.18 and v0.1.0,
/// the string was change to the literal "firewood-v1" plus padding. The version
/// number in the string will stay fixed at `v1` and will be updated independently
/// from the package version in the event of a major data format change.
///
/// The firewood version information stored is now stored in a different
/// location in the header. The fixed string prevents issues where the version
/// string length may overflow the allocated space truncating the string and
/// losing relevant information.
///
/// Validation will continue to accept the old format for compatibility with
/// existing databases. This change is purely cosmetic and does not affect any
/// other functionality or data structures.
///
/// When a major data format is changed, this string should be updated to
/// indicate the new major version.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Zeroable, Pod)]
#[repr(transparent)]
pub struct Version {
    bytes: [u8; 16],
}

impl Version {
    /// Construct a [`Version`] instance for the current build of firewood.
    pub const fn new() -> Self {
        // NB: const block prevents indexing into the array at runtime
        const { Self::VALID_V1_VERSIONS[0] }
    }

    /// Validates that the version identifier is valid and compatible with the current
    /// build of firewood.
    ///
    /// # Errors
    ///
    /// Returns an error if the version is not recognized as one of the known
    /// valid versions.
    pub fn validate(self) -> Result<(), Error> {
        if Self::VALID_V1_VERSIONS.contains(&self) {
            debug!("Database version {:?} is valid.", self.as_str());
            Ok(())
        } else {
            Err(Error::new(
                ErrorKind::InvalidData,
                format!(
                    "Database cannot be opened due to incompatible version: {:?}",
                    self.as_str()
                ),
            ))
        }
    }

    /// Returns the version string as a `&str`, trimming any trailing null bytes.
    pub fn as_str(&self) -> &str {
        std::str::from_utf8(&self.bytes)
            .unwrap_or("<invalid utf8>")
            .trim_matches('\0')
    }

    /// Returns a u128 representation of the version bytes.
    ///
    /// This is useful for comparisons and hashing as we can use integer
    /// operations which are more efficient than byte-wise operations.
    pub const fn as_u128(self) -> u128 {
        u128::from_ne_bytes(self.bytes)
    }

    const fn is_firewood_v1(self) -> bool {
        self.as_u128() == const { Self::VALID_V1_VERSIONS[0].as_u128() }
    }

    const fn from_static(bytes: &'static [u8; 16]) -> Self {
        Self { bytes: *bytes }
    }

    /// After making the version a static string, there is no need to use semver
    /// to parse the valid version strings since there are a finite set of valid
    /// strings.
    const VALID_V1_VERSIONS: [Version; 16] = [
        Version::from_static(b"firewood-v1\0\0\0\0\0"),
        Version::from_static(b"firewood 0.0.18\0"),
        Version::from_static(b"firewood 0.0.17\0"),
        Version::from_static(b"firewood 0.0.16\0"),
        Version::from_static(b"firewood 0.0.15\0"),
        Version::from_static(b"firewood 0.0.14\0"),
        Version::from_static(b"firewood 0.0.13\0"),
        Version::from_static(b"firewood 0.0.12\0"),
        Version::from_static(b"firewood 0.0.11\0"),
        Version::from_static(b"firewood 0.0.10\0"),
        Version::from_static(b"firewood 0.0.9\0\0"),
        Version::from_static(b"firewood 0.0.8\0\0"),
        Version::from_static(b"firewood 0.0.7\0\0"),
        Version::from_static(b"firewood 0.0.6\0\0"),
        Version::from_static(b"firewood 0.0.5\0\0"),
        Version::from_static(b"firewood 0.0.4\0\0"),
    ];
}

impl Default for Version {
    fn default() -> Self {
        Self::new()
    }
}

/// The cargo version used to create this database. The field is padded to 32
/// bytes with null bytes.
///
/// This is the value of the `CARGO_PKG_VERSION` environment variable set by
/// cargo at build time.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Zeroable, Pod)]
#[repr(transparent)]
pub struct CargoVersion {
    bytes: [u8; 32],
}

// assert at compile time that the version fits in the allocated space
const _: () = assert!(CargoVersion::CARGO_PKG_VERSION_LEN <= 32);

impl CargoVersion {
    const CARGO_PKG_VERSION: &'static str = env!("CARGO_PKG_VERSION");
    const CARGO_PKG_VERSION_LEN: usize = Self::CARGO_PKG_VERSION.len();

    // craft in constant context to avoid runtime `memcpy` call for `new()`
    const INSTANCE: Self = {
        let mut bytes = [0u8; 32];
        const_copy(Self::CARGO_PKG_VERSION.as_bytes(), &mut bytes);
        Self { bytes }
    };

    #[inline]
    pub fn len(&self) -> usize {
        self.bytes
            .iter()
            .position(|&b| b == 0)
            .unwrap_or(self.bytes.len())
    }

    pub const fn is_empty(&self) -> bool {
        self.bytes[0] == 0
    }

    pub fn as_str(&self) -> std::borrow::Cow<'_, str> {
        // will not allocate for properly sourced values
        #[expect(clippy::indexing_slicing, reason = "len() ensures we stay in bounds")]
        String::from_utf8_lossy(&self.bytes[..self.len()])
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Zeroable, Pod)]
#[repr(transparent)]
pub struct GitDescribe {
    bytes: [u8; 64],
}

// assert at compile time that the output fits in the allocated space
const _: () = assert!(GitDescribe::GIT_DESCRIBE_LEN <= 64);

impl GitDescribe {
    const GIT_DESCRIBE: &'static str = git_version::git_version!(fallback = "");
    const GIT_DESCRIBE_LEN: usize = Self::GIT_DESCRIBE.len();

    // craft in constant context to avoid runtime `memcpy` call for `new()`
    const INSTANCE: Self = {
        let mut bytes = [0u8; 64];
        const_copy(Self::GIT_DESCRIBE.as_bytes(), &mut bytes);
        Self { bytes }
    };

    #[inline]
    pub fn len(&self) -> usize {
        self.bytes
            .iter()
            .position(|&b| b == 0)
            .unwrap_or(self.bytes.len())
    }

    pub const fn is_empty(&self) -> bool {
        self.bytes[0] == 0
    }

    pub fn as_str(&self) -> std::borrow::Cow<'_, str> {
        // will not allocate for properly sourced values
        #[expect(clippy::indexing_slicing, reason = "len() ensures we stay in bounds")]
        String::from_utf8_lossy(&self.bytes[..self.len()])
    }
}

/// Persisted metadata for a `NodeStore`.
/// The [`NodeStoreHeader`] is at the start of the `ReadableStorage`.
///
/// `root_hash`, `cargo_version` and `git_describe` were added between v0.0.18
/// and v0.1.0. If `version` is not equal to `firewood-v1`, this field may contain
/// "uninitialized" data and must be ignored. Uninitialized data in this context
/// means whatever the filesystem returns when reading from the sparse region of
/// the file where there previously was no data written. This does not mean
/// uninitialized memory with respect to memory safety, but does mean that the
/// data may be garbage.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Zeroable, Pod)]
#[repr(C)]
pub struct NodeStoreHeader {
    /// Identifies the version of the data format written to this `NodeStore`.
    version: Version,
    /// always "1"; verifies endianness
    endian_test: u64,
    /// Total allocated storage size (high water mark). New nodes are allocated
    /// at this offset when the free list is empty, then this value is incremented.
    size: u64,
    /// Heads of the free lists for each area size class.
    ///
    /// Element `i` points to the first free area of size `AREA_SIZES[i]`. Each free area
    /// contains a pointer to the next free area of the same size, forming a linked list.
    ///
    /// When allocating a new node, the allocator first checks the appropriate free list.
    /// If empty, it falls back to bumping `size`. Free lists are populated at commit time
    /// during revision reaping, when deleted nodes from evicted revisions are reclaimed.
    free_lists: FreeLists,
    /// Disk address of the merkle trie root node, or `None` for an empty trie.
    ///
    /// This is updated at commit time to point to the new root after changes are persisted.
    /// The address is a direct file offset, not a hash-based reference.
    root_address: Option<LinearAddress>,
    /// The hash of the area sizes used in this database to prevent someone from changing the
    /// area sizes and trying to read old databases with the wrong area sizes.
    area_size_hash: [u8; 32],
    /// Whether ethhash was enabled when this database was created.
    node_hash_algorithm: u64,
    /// The merkle root hash of the entire database when it was last committed.
    ///
    /// The value is meaningful only if `root_address` is `Some`.
    root_hash: [u8; 32],
    /// The cargo version used to create this database.
    ///
    /// This is the value of the `CARGO_PKG_VERSION` environment variable set by
    /// cargo at build time. It is guaranteed to always be set if `version` is
    /// equal to `firewood-v1`.
    ///
    /// The field is padded to 32 bytes with null bytes.
    cargo_version: CargoVersion,
    /// The git hash of the local repository used to create this database. This
    /// may include a "-modified" suffix if there were uncommitted changes at
    /// the time of building.
    ///
    /// If available, this is the output of `git describe --always --dirty=-modified`
    /// at the time of build. Otherwise this will be an empty string.
    ///
    /// Note that this only applies when building from within the `firewood`
    /// repository. Users importing `firewood-storage` from `crates.io` will
    /// always get an empty string here.
    ///
    /// The field is padded to 64 bytes with null bytes.
    ///
    /// Example: `v0.0.18-31-ga6909f32f-modified`
    ///
    /// This field was added between v0.0.18 and v0.1.0. If `version` is not
    /// equal to `firewood-v1`, this field may contain uninitialized data and
    /// must be ignored.
    git_describe: GitDescribe,
}

// Compile-time assertion that SIZE is large enough for the header. Does not work
// within the impl block.
const _: () = assert!(size_of::<NodeStoreHeader>() <= NodeStoreHeader::SIZE as usize);

impl NodeStoreHeader {
    /// The first SIZE bytes of the `ReadableStorage` are reserved for the
    /// [`NodeStoreHeader`].
    /// We also want it aligned to a disk block
    pub const SIZE: u64 = 2048;

    /// Reads the [`NodeStoreHeader`] from the start of the given storage.
    ///
    /// # Errors
    ///
    /// Returns an error if unable to read the header from storage or if there's
    /// a node hash algorithm mismatch.
    pub fn read_from_storage<S: crate::linear::ReadableStorage>(
        storage: &S,
    ) -> Result<Self, crate::FileIoError> {
        // TODO(#1088): remove this after implementing runtime selection of hash algorithms
        storage.node_hash_algorithm().validate_init().map_err(|e| {
            storage.file_io_error(e, 0, Some("NodeHashAlgorithm::validate_init".to_string()))
        })?;

        let mut this = bytemuck::zeroed::<Self>();
        let header_bytes = bytemuck::bytes_of_mut(&mut this);
        storage
            .stream_from(0)?
            .read_exact(header_bytes)
            .map_err(|e| {
                storage.file_io_error(e, 0, Some("NodeStoreHeader::read_from_storage".to_string()))
            })?;
        this.validate(storage.node_hash_algorithm()).map_err(|e| {
            storage.file_io_error(e, 0, Some("NodeStoreHeader::validate".to_string()))
        })?;

        Ok(this)
    }

    /// Creates a new header with default values and no root address.
    #[must_use]
    pub fn new(node_hash_algorithm: NodeHashAlgorithm) -> Self {
        Self {
            // The store just contains the header at this point
            size: Self::SIZE,
            endian_test: 1,
            root_address: None,
            version: Version::new(),
            free_lists: Default::default(),
            area_size_hash: area_size_hash().into(),
            node_hash_algorithm: node_hash_algorithm as u64,
            root_hash: TrieHash::empty().into(),
            cargo_version: CargoVersion::INSTANCE,
            git_describe: GitDescribe::INSTANCE,
        }
    }

    /// Validates the header fields to ensure correctness and compatibility with
    /// the current build of firewood.
    ///
    /// Checks performed:
    ///
    /// - Magic number / version string is valid. The first 16 bytes must match
    ///   an expected magic number.
    /// - Endianness test matches expected value (1). If not, reading from the
    ///   database will produce incorrect results from mis-interpreted byte order.
    /// - Area size hash matches the expected hash for the current build. This
    ///   prevents corrupting the allocation structures by changing area sizes.
    /// - Node hash algorithm flag matches the expected algorithm for this
    ///   storage.
    ///
    /// # Errors
    ///
    /// Returns an error if validation fails.
    pub fn validate(&self, expected_node_hash_algorithm: NodeHashAlgorithm) -> Result<(), Error> {
        trace!("Checking version...");
        self.version.validate()?;

        trace!("Checking endianness...");
        self.validate_endian_test()?;

        trace!("Checking area size hash...");
        self.validate_area_size_hash()?;

        trace!("Checking if node hash algorithm flag matches storage...");
        self.validate_node_hash_algorithm(expected_node_hash_algorithm)?;

        if self.version == Version::VALID_V1_VERSIONS[0] {
            debug!(
                "Database was created with firewood version {}",
                self.firewood_version_str()
                    .as_deref()
                    .unwrap_or("<unknown>"),
            );
        }

        Ok(())
    }

    /// Get the size of the nodestore
    #[must_use]
    pub const fn size(&self) -> u64 {
        self.size
    }

    /// Set the size of the nodestore
    pub const fn set_size(&mut self, size: u64) {
        self.size = size;
    }

    /// Get the free lists
    #[must_use]
    pub const fn free_lists(&self) -> &FreeLists {
        &self.free_lists
    }

    /// Get mutable access to the free lists
    pub const fn free_lists_mut(&mut self) -> &mut FreeLists {
        &mut self.free_lists
    }

    /// Get the root address
    #[must_use]
    pub const fn root_address(&self) -> Option<LinearAddress> {
        self.root_address
    }

    /// Get the root hash.
    ///
    /// This is None if the database was created before v0.1.0.
    #[must_use]
    pub fn root_hash(&self) -> Option<TrieHash> {
        if self.version.is_firewood_v1() && self.root_address.is_some() {
            Some(TrieHash::from(self.root_hash))
        } else {
            None
        }
    }

    /// Update the root location, both address and hash.
    ///
    /// Note that this does not overwrite the version field, so it is possible
    /// the root hash will be ignored when set.
    pub fn set_root_location(&mut self, root_location: Option<RootNodeInfo>) {
        let (root_address, root_hash) = root_location.unzip();
        self.root_address = root_address;
        self.root_hash = root_hash.unwrap_or_else(TrieHash::empty).into();
    }

    /// Get the offset of the `free_lists` field for use with `offset_of`!
    #[must_use]
    pub const fn free_lists_offset() -> u64 {
        std::mem::offset_of!(NodeStoreHeader, free_lists) as u64
    }

    /// Get the cargo version of `firewood-storage` used to create this database,
    /// if available.
    #[must_use]
    pub const fn cargo_version(&self) -> Option<&CargoVersion> {
        if self.version.is_firewood_v1() {
            Some(&self.cargo_version)
        } else {
            None
        }
    }

    /// Get the git describe string of `firewood` used to create this database,
    #[must_use]
    pub const fn git_describe(&self) -> Option<&GitDescribe> {
        if self.version.is_firewood_v1() {
            Some(&self.git_describe)
        } else {
            None
        }
    }

    /// Returns a version string identifying the version of `firewood-storage`
    /// used to create this database, if available.
    ///
    /// This field was added between v0.0.18 and v0.1.0 and may be absent in
    /// older databases.
    ///
    /// The returned string is either the `git describe` output (from the time
    /// of build) if available, otherwise the cargo package version of the
    /// `firewood-storage` crate.
    pub fn firewood_version_str(&self) -> Option<std::borrow::Cow<'_, str>> {
        self.git_describe()
            .map(GitDescribe::as_str)
            .or_else(|| self.cargo_version().map(CargoVersion::as_str))
    }

    fn validate_endian_test(&self) -> Result<(), Error> {
        if self.endian_test == 1 {
            Ok(())
        } else {
            Err(Error::new(
                ErrorKind::InvalidData,
                "Database cannot be opened due to difference in endianness",
            ))
        }
    }

    fn validate_area_size_hash(&self) -> Result<(), Error> {
        if self.area_size_hash == area_size_hash().as_slice() {
            Ok(())
        } else {
            Err(Error::new(
                ErrorKind::InvalidData,
                "Database cannot be opened due to difference in area size hash",
            ))
        }
    }

    fn validate_node_hash_algorithm(&self, expected: NodeHashAlgorithm) -> Result<(), Error> {
        expected.validate_init()?;
        NodeHashAlgorithm::try_from(self.node_hash_algorithm)
            .map_err(|err| {
                std::io::Error::new(
                    std::io::ErrorKind::InvalidData,
                    format!("hash mode flag in database header is invalid: {err}"),
                )
            })?
            .validate_open(expected)
    }
}

// memcpy but in const context. This bypasses the current limitation that
// `slice::copy_from_slice` cannot be used in const fns (or const{} blocks).
const fn const_copy(src: &[u8], dst: &mut [u8]) {
    #![expect(
        clippy::indexing_slicing,
        clippy::arithmetic_side_effects,
        reason = "const and sufficient bounds checks"
    )]

    let mut i = 0;
    while i < src.len() && i < dst.len() {
        dst[i] = src[i];
        i += 1;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use test_case::test_case;

    #[test]
    fn test_version_new_is_valid() {
        Version::new()
            .validate()
            .expect("Version::new() should always be valid");
    }

    #[test_case(*b"invalid\0\0\0\0\0\0\0\0\0")]
    #[test_case(*b"avalanche 0.1.0\0")]
    #[test_case(*b"firewood 0.0.1\0\0")]
    fn test_invalid_version_strings(bytes: [u8; 16]) {
        assert!(Version { bytes }.validate().is_err());
    }

    #[test]
    fn test_header_new() {
        let header = NodeStoreHeader::new(NodeHashAlgorithm::compile_option());

        // Check the header is correctly initialized.
        assert_eq!(header.version, Version::new());
        let empty_free_list: FreeLists = Default::default();
        assert_eq!(*header.free_lists(), empty_free_list);
    }
}
