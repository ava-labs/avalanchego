// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Allocation Module
//!
//! This module handles memory allocation and space management for nodes in the nodestore's
//! linear storage, implementing a malloc-like free space management system.
//!
//! ### Area Sizes
//! Storage is divided into 23 predefined area sizes from 16 bytes to 16MB:
//! - Small sizes (16, 32, 64, 96, 128, 256, 512, 768, 1024 bytes) for common nodes
//! - Power-of-two larger sizes (2KB, 4KB, 8KB, ..., 16MB) for larger data
//!
//! ### Storage Format
//! Each stored area follows this layout:
//! ```text
//! [AreaIndex:1][AreaType:1][NodeData:n]
//! ```
//! - **`AreaIndex`** - Index into `AREA_SIZES` array (1 byte)
//! - **`AreaType`** - 0xFF for free areas, otherwise node type data (1 byte)
//! - **`NodeData`** - Serialized node content

use crate::linear::FileIoError;
use crate::logger::trace;
use crate::node::branch::{ReadSerializable, Serializable};
use crate::nodestore::NodeStoreHeader;
use integer_encoding::VarIntReader;

use sha2::{Digest, Sha256};
use std::fmt;
use std::io::{Error, ErrorKind, Read};
use std::iter::FusedIterator;
use std::num::NonZeroU64;

use crate::node::ExtendableBytes;
use crate::{
    FreeListParent, MaybePersistedNode, ReadableStorage, TrieHash, WritableStorage,
    firewood_counter,
};

/// Returns the maximum size needed to encode a `VarInt`.
const fn var_int_max_size<VI>() -> usize {
    const { (size_of::<VI>() * 8 + 7) / 7 }
}

/// [`NodeStore`] divides the linear store into blocks of different sizes.
/// [`AREA_SIZES`] is every valid block size.
pub const AREA_SIZES: [u64; 23] = [
    16, // Min block size
    32,
    64,
    96,
    128,
    256,
    512,
    768,
    1024,
    1024 << 1,
    1024 << 2,
    1024 << 3,
    1024 << 4,
    1024 << 5,
    1024 << 6,
    1024 << 7,
    1024 << 8,
    1024 << 9,
    1024 << 10,
    1024 << 11,
    1024 << 12,
    1024 << 13,
    1024 << 14,
];

pub fn area_size_hash() -> TrieHash {
    let mut hasher = Sha256::new();
    for size in AREA_SIZES {
        hasher.update(size.to_ne_bytes());
    }
    hasher.finalize().into()
}

/// Get the size of an area index (used by the checker)
///
/// # Panics
///
/// Panics if `index` is out of bounds for the `AREA_SIZES` array.
#[must_use]
pub const fn size_from_area_index(index: AreaIndex) -> u64 {
    #[expect(clippy::indexing_slicing)]
    AREA_SIZES[index as usize]
}

// TODO: automate this, must stay in sync with above
pub const fn index_name(index: usize) -> &'static str {
    match index {
        0 => "16",
        1 => "32",
        2 => "64",
        3 => "96",
        4 => "128",
        5 => "256",
        6 => "512",
        7 => "768",
        8 => "1024",
        9 => "2048",
        10 => "4096",
        11 => "8192",
        12 => "16384",
        13 => "32768",
        14 => "65536",
        15 => "131072",
        16 => "262144",
        17 => "524288",
        18 => "1048576",
        19 => "2097152",
        20 => "4194304",
        21 => "8388608",
        22 => "16777216",
        _ => "unknown",
    }
}

/// The type of an index into the [`AREA_SIZES`] array
/// This is not usize because we can store this as a single byte
pub type AreaIndex = u8;

const NUM_AREA_SIZES: usize = AREA_SIZES.len();
const MIN_AREA_SIZE: u64 = AREA_SIZES[0];
const MAX_AREA_SIZE: u64 = AREA_SIZES[NUM_AREA_SIZES - 1];

#[inline]
pub fn new_area_index(n: usize) -> AreaIndex {
    n.try_into().expect("Area index out of bounds")
}

/// Returns the index in `BLOCK_SIZES` of the smallest block size >= `n`.
pub fn area_size_to_index(n: u64) -> Result<AreaIndex, Error> {
    if n > MAX_AREA_SIZE {
        return Err(Error::new(
            ErrorKind::InvalidData,
            format!("Node size {n} is too large"),
        ));
    }

    if n <= MIN_AREA_SIZE {
        return Ok(0);
    }

    AREA_SIZES
        .iter()
        .position(|&size| size >= n)
        .map(new_area_index)
        .ok_or_else(|| {
            Error::new(
                ErrorKind::InvalidData,
                format!("Node size {n} is too large"),
            )
        })
}

/// A linear address in the nodestore storage.
///
/// This represents a non-zero address in the linear storage space.
#[derive(Copy, Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(transparent)]
pub struct LinearAddress(NonZeroU64);

#[expect(unsafe_code)]
unsafe impl bytemuck::ZeroableInOption for LinearAddress {}
#[expect(unsafe_code)]
unsafe impl bytemuck::PodInOption for LinearAddress {}

impl LinearAddress {
    /// Create a new `LinearAddress`, returns None if value is zero.
    #[inline]
    #[must_use]
    pub const fn new(addr: u64) -> Option<Self> {
        match NonZeroU64::new(addr) {
            Some(addr) => Some(LinearAddress(addr)),
            None => None,
        }
    }

    /// Get the underlying address as u64.
    #[inline]
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0.get()
    }

    /// Check if the address is 8-byte aligned.
    #[inline]
    #[must_use]
    pub const fn is_aligned(self) -> bool {
        self.0.get() % (Self::MIN_AREA_SIZE) == 0
    }

    /// The maximum area size available for allocation.
    pub const MAX_AREA_SIZE: u64 = *AREA_SIZES.last().unwrap();

    /// The minimum area size available for allocation.
    pub const MIN_AREA_SIZE: u64 = *AREA_SIZES.first().unwrap();

    /// Returns the number of different area sizes available.
    #[inline]
    #[must_use]
    pub const fn num_area_sizes() -> usize {
        const { AREA_SIZES.len() }
    }

    /// Returns the inner `NonZeroU64`
    #[inline]
    #[must_use]
    pub const fn into_nonzero(self) -> NonZeroU64 {
        self.0
    }

    /// Advances a `LinearAddress` by `n` bytes.
    ///
    /// Returns `None` if the result overflows a u64
    /// Some(LinearAddress) otherwise
    ///
    #[inline]
    #[must_use]
    pub const fn advance(self, n: u64) -> Option<Self> {
        match self.0.checked_add(n) {
            // overflowed
            None => None,

            // It is impossible to add a non-zero positive number to a u64 and get 0 without
            // overflowing, so we don't check for that here, and panic instead.
            Some(sum) => Some(LinearAddress(sum)),
        }
    }

    /// Returns the number of bytes between `other` and `self` if `other` is less than or equal to `self`.
    /// Otherwise, returns `None`.
    #[inline]
    #[must_use]
    pub const fn distance_from(self, other: Self) -> Option<u64> {
        self.0.get().checked_sub(other.0.get())
    }
}

impl std::ops::Deref for LinearAddress {
    type Target = NonZeroU64;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl fmt::Display for LinearAddress {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        std::fmt::Display::fmt(&self.get(), f)
    }
}

impl fmt::LowerHex for LinearAddress {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        std::fmt::LowerHex::fmt(&self.get(), f)
    }
}
impl From<LinearAddress> for u64 {
    fn from(addr: LinearAddress) -> Self {
        addr.get()
    }
}

impl From<NonZeroU64> for LinearAddress {
    fn from(addr: NonZeroU64) -> Self {
        LinearAddress(addr)
    }
}

/// Every item stored in the [`NodeStore`]'s `ReadableStorage`  after the
/// `NodeStoreHeader` is a [`StoredArea`].
///
/// As an overview of what this looks like stored, we get something like this:
///  - Byte 0: The index of the area size
///  - Byte 1: 0x255 if free, otherwise the low-order bit indicates Branch or Leaf
///  - Bytes 2..n: The actual data
#[derive(PartialEq, Eq, Clone, Debug)]
pub struct StoredArea<T> {
    /// Index in [`AREA_SIZES`] of this area's size
    area_size_index: AreaIndex,
    area: T,
}

/// `FreeLists` is an array of `Option<LinearAddress>` for each area size.
pub type FreeLists = [Option<LinearAddress>; AREA_SIZES.len()];

/// A [`FreeArea`] is stored at the start of the area that contained a node that
/// has been freed.
#[derive(Debug, PartialEq, Eq, Clone, Copy)]
pub struct FreeArea {
    next_free_block: Option<LinearAddress>,
}

impl Serializable for FreeArea {
    fn write_to<W: crate::node::ExtendableBytes>(&self, vec: &mut W) {
        vec.push(0xff); // 0xff indicates a free area
        vec.extend_var_int(self.next_free_block.map_or(0, LinearAddress::get));
    }

    /// Parse a [`FreeArea`].
    ///
    /// The old serde generate code that unintentionally encoded [`FreeArea`]s
    /// incorrectly. Integers were encoded as variable length integers, but
    /// expanded to fixed-length below:
    ///
    /// ```text
    /// [
    ///     0x01, // LE u32 begin -- field index of the old `StoredArea` struct (#1)
    ///     0x00,
    ///     0x00,
    ///     0x00, // LE u32 end
    ///     0x01, // `Option` discriminant, 1 Indicates `Some(_)` from `Option<LinearAddress>`
    ///           // because serde does not handle the niche optimization of
    ///           // `Option<NonZero<_>>`
    ///     0x2a, // LinearAddress(LE u64) start
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00, // LE u64 end
    /// ]
    /// ```
    ///
    /// Our manual encoding format is (with variable int, but expanded below):
    ///
    /// ```text
    /// [
    ///     0xff, // FreeArea marker
    ///     0x2a, // LinearAddress(LE u64) start
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00,
    ///     0x00, // LE u64 end
    /// ]
    /// ```
    fn from_reader<R: Read>(mut reader: R) -> std::io::Result<Self> {
        match reader.read_byte()? {
            0x01 => {
                // might be old format, look for option discriminant
                match reader.read_byte()? {
                    0x00 => {
                        // serde encoded `Option::None` as 0 with no following data
                        Ok(Self {
                            next_free_block: None,
                        })
                    }
                    0x01 => {
                        // encoded `Some(_)` as 1 with the data following
                        let addr = LinearAddress::new(read_bincode_varint_u64_le(&mut reader)?)
                            .ok_or_else(|| {
                                Error::new(
                                    ErrorKind::InvalidData,
                                    "Option::<LinearAddress> was Some(0) which is invalid",
                                )
                            })?;
                        Ok(Self {
                            next_free_block: Some(addr),
                        })
                    }
                    option_discriminant => Err(Error::new(
                        ErrorKind::InvalidData,
                        format!("Invalid Option discriminant: {option_discriminant}"),
                    )),
                }
            }
            0xFF => {
                // new format: read the address directly (zero is allowed here to indicate None)
                Ok(Self {
                    next_free_block: LinearAddress::new(reader.read_varint()?),
                })
            }
            first_byte => Err(Error::new(
                ErrorKind::InvalidData,
                format!(
                    "Invalid FreeArea marker, expected 0xFF (or 0x01 for old format), found {first_byte:#04x}"
                ),
            )),
        }
    }
}

impl FreeArea {
    /// Create a new `FreeArea`
    pub const fn new(next_free_block: Option<LinearAddress>) -> Self {
        Self { next_free_block }
    }

    /// Get the next free block address
    pub const fn next_free_block(self) -> Option<LinearAddress> {
        self.next_free_block
    }

    pub fn from_storage<S: ReadableStorage>(
        storage: &S,
        address: LinearAddress,
    ) -> Result<(Self, AreaIndex), FileIoError> {
        let free_area_addr = address.get();
        let stored_area_stream = storage.stream_from(free_area_addr)?;
        Self::from_storage_reader(stored_area_stream).map_err(|e| {
            storage.file_io_error(
                e,
                free_area_addr,
                Some("FreeArea::from_storage".to_string()),
            )
        })
    }

    pub fn as_bytes<T: ExtendableBytes>(self, area_index: AreaIndex, encoded: &mut T) {
        const RESERVE_SIZE: usize = size_of::<u8>() + var_int_max_size::<u64>();

        encoded.reserve(RESERVE_SIZE);
        encoded.push(area_index);
        self.write_to(encoded);
    }

    fn from_storage_reader(mut reader: impl Read) -> std::io::Result<(Self, AreaIndex)> {
        let area_index = reader.read_byte()?;
        let free_area = reader.next_value()?;
        Ok((free_area, area_index))
    }
}

// Re-export the NodeStore types we need
use super::NodeStore;

/// Writable allocator for allocating and deleting nodes
#[derive(Debug)]
pub struct NodeAllocator<'a, S> {
    storage: &'a S,
    header: &'a mut NodeStoreHeader,
}

impl<'a, S: ReadableStorage> NodeAllocator<'a, S> {
    pub const fn new(storage: &'a S, header: &'a mut NodeStoreHeader) -> Self {
        Self { storage, header }
    }

    /// Returns (index, `area_size`) for the stored area at `addr`.
    /// `index` is the index of `area_size` in the array of valid block sizes.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the area cannot be read.
    pub fn area_index_and_size(
        &self,
        addr: LinearAddress,
    ) -> Result<(AreaIndex, u64), FileIoError> {
        let mut area_stream = self.storage.stream_from(addr.get())?;

        let index: AreaIndex = area_stream.read_byte().map_err(|e| {
            self.storage.file_io_error(
                Error::new(ErrorKind::InvalidData, e),
                addr.get(),
                Some("deserialize".to_string()),
            )
        })?;

        let size = *AREA_SIZES
            .get(index as usize)
            .ok_or(self.storage.file_io_error(
                Error::other(format!("Invalid area size index {index}")),
                addr.get(),
                None,
            ))?;

        Ok((index, size))
    }

    /// Attempts to allocate `n` bytes from the free lists.
    /// If successful returns the address of the newly allocated area
    /// and the index of the free list that was used.
    /// If there are no free areas big enough for `n` bytes, returns None.
    /// TODO Consider splitting the area if we return a larger area than requested.
    #[expect(clippy::indexing_slicing)]
    fn allocate_from_freed(
        &mut self,
        n: u64,
    ) -> Result<Option<(LinearAddress, AreaIndex)>, FileIoError> {
        // Find the smallest free list that can fit this size.
        let index_wanted = area_size_to_index(n).map_err(|e| {
            self.storage
                .file_io_error(e, 0, Some("allocate_from_freed".to_string()))
        })?;

        if let Some((index, free_stored_area_addr)) = self
            .header
            .free_lists_mut()
            .iter_mut()
            .enumerate()
            .skip(index_wanted as usize)
            .find(|item| item.1.is_some())
        {
            let address = free_stored_area_addr
                .take()
                .expect("impossible due to find earlier");
            // Get the first free block of sufficient size.
            if let Some(free_head) = self.storage.free_list_cache(address) {
                trace!("free_head@{address}(cached): {free_head:?} size:{index}");
                *free_stored_area_addr = free_head;
            } else {
                let (free_head, read_index) = FreeArea::from_storage(self.storage, address)?;
                debug_assert_eq!(read_index as usize, index);

                // Update the free list to point to the next free block.
                *free_stored_area_addr = free_head.next_free_block;
            }

            firewood_counter!(
                "firewood.space.reused",
                "Bytes reused from free list by index",
                "index" => index_name(index)
            )
            .increment(AREA_SIZES[index]);
            firewood_counter!(
                "firewood.space.wasted",
                "Bytes wasted from free list by index",
                "index" => index_name(index)
            )
            .increment(AREA_SIZES[index].saturating_sub(n));

            // Return the address of the newly allocated block.
            trace!("Allocating from free list: addr: {address:?}, size: {index}");
            return Ok(Some((address, index as AreaIndex)));
        }

        trace!("No free blocks of sufficient size {index_wanted} found");
        firewood_counter!(
            "firewood.space.from_end",
            "Space allocated from end of nodestore",
            "index" => index_name(index_wanted as usize)
        )
        .increment(AREA_SIZES[index_wanted as usize]);
        Ok(None)
    }

    #[expect(clippy::indexing_slicing)]
    fn allocate_from_end(&mut self, n: u64) -> Result<(LinearAddress, AreaIndex), FileIoError> {
        let index = area_size_to_index(n).map_err(|e| {
            self.storage
                .file_io_error(e, 0, Some("allocate_from_end".to_string()))
        })?;
        let area_size = AREA_SIZES[index as usize];
        let addr = LinearAddress::new(self.header.size()).expect("node store size can't be 0");
        self.header
            .set_size(self.header.size().saturating_add(area_size));
        debug_assert!(addr.is_aligned());
        trace!("Allocating from end: addr: {addr:?}, size: {index}");
        Ok((addr, index))
    }

    /// Returns an address that can be used to store the given `node` and updates
    /// `self.header` to reflect the allocation. Doesn't actually write the node to storage.
    /// Also returns the index of the free list the node was allocated from.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be allocated.
    pub fn allocate_node(
        &mut self,
        node: &[u8],
    ) -> Result<(LinearAddress, AreaIndex), FileIoError> {
        let stored_area_size = node.len() as u64;

        // Attempt to allocate from a free list.
        // If we can't allocate from a free list, allocate past the existing
        // of the ReadableStorage.
        let (addr, index) = match self.allocate_from_freed(stored_area_size)? {
            Some((addr, index)) => (addr, index),
            None => self.allocate_from_end(stored_area_size)?,
        };

        Ok((addr, index))
    }
}

impl<S: WritableStorage> NodeAllocator<'_, S> {
    /// Deletes the `Node` and updates the header of the allocator.
    /// Nodes that are not persisted are just dropped.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the area cannot be read or written.
    #[expect(clippy::indexing_slicing)]
    pub fn delete_node(&mut self, node: MaybePersistedNode) -> Result<(), FileIoError> {
        let Some(addr) = node.as_linear_address() else {
            return Ok(());
        };
        debug_assert!(addr.is_aligned());

        let (area_size_index, _) = self.area_index_and_size(addr)?;
        trace!("Deleting node at {addr:?} of size {area_size_index}");
        firewood_counter!(
            "firewood.delete_node",
            "Nodes deleted",
            "index" => index_name(area_size_index as usize)
        )
        .increment(1);
        firewood_counter!(
            "firewood.space.freed",
            "Bytes freed in nodestore",
            "index" => index_name(area_size_index as usize)
        )
        .increment(AREA_SIZES[area_size_index as usize]);

        // The area that contained the node is now free.
        let mut stored_area_bytes = Vec::new();
        FreeArea::new(self.header.free_lists()[area_size_index as usize])
            .as_bytes(area_size_index, &mut stored_area_bytes);

        self.storage.write(addr.into(), &stored_area_bytes)?;

        self.storage
            .add_to_free_list_cache(addr, self.header.free_lists()[area_size_index as usize]);

        // The newly freed block is now the head of the free list.
        self.header.free_lists_mut()[area_size_index as usize] = Some(addr);

        Ok(())
    }
}

/// Iterator over free lists in the nodestore
struct FreeListIterator<'a, S: ReadableStorage> {
    storage: &'a S,
    next_addr: Option<LinearAddress>,
    parent: FreeListParent,
}

impl<'a, S: ReadableStorage> FreeListIterator<'a, S> {
    const fn new(
        storage: &'a S,
        next_addr: Option<LinearAddress>,
        src_ptr: FreeListParent,
    ) -> Self {
        Self {
            storage,
            next_addr,
            parent: src_ptr,
        }
    }

    #[expect(
        clippy::type_complexity,
        reason = "TODO: we need some additional newtypes here"
    )]
    fn next_with_parent(
        &mut self,
    ) -> Option<Result<((LinearAddress, AreaIndex), FreeListParent), FileIoError>> {
        let parent = self.parent;
        let next_addr = self.next()?;
        Some(next_addr.map(|free_area| (free_area, parent)))
    }
}

impl<S: ReadableStorage> Iterator for FreeListIterator<'_, S> {
    type Item = Result<(LinearAddress, AreaIndex), FileIoError>;

    fn next(&mut self) -> Option<Self::Item> {
        let next_addr = self.next_addr?;

        // read the free area, propagate any IO error if it occurs
        let (free_area, stored_area_index) = match FreeArea::from_storage(self.storage, next_addr) {
            Ok(free_area) => free_area,
            Err(e) => {
                // if the read fails, we cannot proceed with the current freelist
                self.next_addr = None;
                return Some(Err(e));
            }
        };

        // update the next address to the next free block
        self.parent = FreeListParent::PrevFreeArea(next_addr);
        self.next_addr = free_area.next_free_block();
        Some(Ok((next_addr, stored_area_index)))
    }
}

impl<S: ReadableStorage> FusedIterator for FreeListIterator<'_, S> {}

#[derive(Debug, PartialEq, Eq)]
pub(crate) struct FreeAreaWithMetadata {
    pub addr: LinearAddress,
    pub area_index: AreaIndex,
    pub free_list_id: AreaIndex,
    pub parent: FreeListParent,
}

pub(crate) struct FreeListsIterator<'a, S: ReadableStorage> {
    storage: &'a S,
    free_lists_iter: std::iter::Skip<
        std::iter::Enumerate<std::slice::Iter<'a, std::option::Option<LinearAddress>>>,
    >,
    current_free_list_id: AreaIndex,
    free_list_iter: FreeListIterator<'a, S>,
}

impl<'a, S: ReadableStorage> FreeListsIterator<'a, S> {
    pub(crate) fn new(
        storage: &'a S,
        free_lists: &'a FreeLists,
        start_area_index: AreaIndex,
    ) -> Self {
        let mut free_lists_iter = free_lists
            .iter()
            .enumerate()
            .skip(start_area_index as usize);
        let (current_free_list_id, free_list_head) = match free_lists_iter.next() {
            Some((id, head)) => (id as AreaIndex, *head),
            None => (NUM_AREA_SIZES as AreaIndex, None),
        };
        let start_iterator = FreeListIterator::new(
            storage,
            free_list_head,
            FreeListParent::FreeListHead(current_free_list_id),
        );
        Self {
            storage,
            free_lists_iter,
            current_free_list_id,
            free_list_iter: start_iterator,
        }
    }

    pub(crate) fn next_with_metadata(
        &mut self,
    ) -> Option<Result<FreeAreaWithMetadata, FileIoError>> {
        self.next_inner(FreeListIterator::next_with_parent)
            .map(|next_with_parent| {
                next_with_parent.map(|((addr, area_index), parent)| FreeAreaWithMetadata {
                    addr,
                    area_index,
                    free_list_id: self.current_free_list_id,
                    parent,
                })
            })
    }

    fn next_inner<T, F: FnMut(&mut FreeListIterator<'a, S>) -> Option<T>>(
        &mut self,
        mut next_fn: F,
    ) -> Option<T> {
        loop {
            if let Some(next) = next_fn(&mut self.free_list_iter) {
                // the current free list is not exhausted, return the next free area
                return Some(next);
            }

            match self.free_lists_iter.next() {
                Some((current_free_list_id, next_free_list_head)) => {
                    self.current_free_list_id = current_free_list_id as AreaIndex;
                    self.free_list_iter = FreeListIterator::new(
                        self.storage,
                        *next_free_list_head,
                        FreeListParent::FreeListHead(self.current_free_list_id),
                    );
                }
                None => {
                    // no more free lists to iterate over
                    return None;
                }
            }
        }
    }

    // TODO: this function will be used by the checker to ignore free areas pointed by an invalid parent free area
    #[allow(dead_code)]
    fn move_to_next_free_list(&mut self) {
        let (current_free_list_id, next_free_list_head) = match self.free_lists_iter.next() {
            Some((id, head)) => (id as AreaIndex, *head),
            None => (NUM_AREA_SIZES as AreaIndex, None), // skip unvisited free areas in the current iterator
        };
        self.current_free_list_id = current_free_list_id;
        self.free_list_iter = FreeListIterator::new(
            self.storage,
            next_free_list_head,
            FreeListParent::FreeListHead(self.current_free_list_id),
        );
    }
}

impl<S: ReadableStorage> Iterator for FreeListsIterator<'_, S> {
    type Item = Result<(LinearAddress, AreaIndex), FileIoError>;

    fn next(&mut self) -> Option<Self::Item> {
        self.next_inner(FreeListIterator::next)
    }
}

/// Extension methods for `NodeStore` to provide free list iteration capabilities
impl<T, S: ReadableStorage> NodeStore<T, S> {
    // Returns an iterator over the free lists of size no smaller than the size corresponding to `start_area_index`.
    // The iterator returns a tuple of the address and the area index of the free area.
    // Since this is a low-level iterator, we avoid safe conversion to AreaIndex for performance
    pub(crate) fn free_list_iter(&self, start_area_index: AreaIndex) -> FreeListsIterator<'_, S> {
        FreeListsIterator::new(self.storage.as_ref(), self.freelists(), start_area_index)
    }
}

fn read_bincode_varint_u64_le(reader: &mut impl Read) -> std::io::Result<u64> {
    // See https://github.com/ava-labs/firewood/issues/1146 for full details.
    // emulate this behavior: https://github.com/bincode-org/bincode/blob/c44b5e364e7084cdbabf9f94b63a3c7f32b8fb68/src/config/int.rs#L241-L258

    const SINGLE_BYTE_MAX: u8 = 250;
    const U16_BYTE: u8 = 251;
    const U32_BYTE: u8 = 252;
    const U64_BYTE: u8 = 253;

    match reader.read_byte()? {
        byte @ 0..=SINGLE_BYTE_MAX => Ok(u64::from(byte)),
        U16_BYTE => {
            let mut buf = [0u8; 2];
            reader.read_exact(&mut buf)?;
            Ok(u64::from(u16::from_le_bytes(buf)))
        }
        U32_BYTE => {
            let mut buf = [0u8; 4];
            reader.read_exact(&mut buf)?;
            Ok(u64::from(u32::from_le_bytes(buf)))
        }
        U64_BYTE => {
            let mut buf = [0u8; 8];
            reader.read_exact(&mut buf)?;
            Ok(u64::from_le_bytes(buf))
        }
        byte => Err(Error::new(
            ErrorKind::InvalidData,
            format!(
                "Invalid bincode varint byte, expected 0-250, 251, 252, or 253, found {byte:#04x}"
            ),
        )),
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
pub mod test_utils {
    use super::*;

    use crate::node::Node;
    use crate::nodestore::{Committed, NodeStore, NodeStoreHeader};

    // Helper function to wrap the node in a StoredArea and write it to the given offset. Returns the size of the area on success.
    pub fn test_write_new_node<S: WritableStorage>(
        nodestore: &NodeStore<Committed, S>,
        node: &Node,
        offset: u64,
    ) -> (u64, u64) {
        #![expect(clippy::indexing_slicing)]
        let mut encoded_node = Vec::new();
        node.as_bytes(0, &mut encoded_node);
        let encoded_node_len = encoded_node.len() as u64;
        let area_size_index = area_size_to_index(encoded_node_len).unwrap();
        let mut stored_area_bytes = Vec::new();
        node.as_bytes(area_size_index, &mut stored_area_bytes);
        let bytes_written = (stored_area_bytes.len() as u64)
            .checked_sub(1)
            .expect("serialized node should be at least 1 byte"); // -1 for area size index
        nodestore
            .storage
            .write(offset, stored_area_bytes.as_slice())
            .unwrap();
        (bytes_written, AREA_SIZES[area_size_index as usize])
    }

    // Helper function to write a free area to the given offset.
    pub fn test_write_free_area<S: WritableStorage>(
        nodestore: &NodeStore<Committed, S>,
        next_free_block: Option<LinearAddress>,
        area_size_index: AreaIndex,
        offset: u64,
    ) {
        let mut stored_area_bytes = Vec::new();
        FreeArea::new(next_free_block).as_bytes(area_size_index, &mut stored_area_bytes);
        nodestore.storage.write(offset, &stored_area_bytes).unwrap();
    }

    // Helper function to write the NodeStoreHeader
    pub fn test_write_header<S: WritableStorage>(
        nodestore: &mut NodeStore<Committed, S>,
        size: u64,
        root_addr: Option<LinearAddress>,
        free_lists: FreeLists,
    ) {
        let mut header = NodeStoreHeader::new();
        header.set_size(size);
        header.set_root_address(root_addr);
        *header.free_lists_mut() = free_lists;
        let header_bytes = bytemuck::bytes_of(&header);
        nodestore.header = header;
        nodestore.storage.write(0, header_bytes).unwrap();
    }

    // Helper function to write a random stored area to the given offset.
    pub(crate) fn test_write_zeroed_area<S: WritableStorage>(
        nodestore: &NodeStore<Committed, S>,
        size: u64,
        offset: u64,
    ) {
        let area_content = vec![0u8; size as usize];
        nodestore.storage.write(offset, &area_content).unwrap();
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used, clippy::indexing_slicing)]
mod tests {
    use super::*;
    use crate::linear::memory::MemStore;
    use crate::test_utils::seeded_rng;
    use rand::Rng;
    use rand::seq::IteratorRandom;
    use test_case::test_case;

    #[test_case(&[0x01, 0x01, 0x01, 0x2a], Some((1, 42)); "old format")]
    // StoredArea::new(12, Area::<Node, _>::Free(FreeArea::new(None)));
    #[test_case(&[0x02, 0x01, 0x00], Some((2, 0)); "none")]
    #[test_case(&[0x03, 0xff, 0x2b], Some((3, 43)); "new format")]
    #[test_case(&[0x03, 0x44, 0x55], None; "garbage")]
    #[test_case(
        &[0x03, 0x01, 0x01, 0xfd, 0xe0, 0xa2, 0x6d, 0x27, 0x6e, 0x00, 0x00, 0x00, 0x0d, 0x09, 0x03, 0x00],
        Some((3, 0x6e_276d_a2e0));
        "old format with u64 address (issue #1146)"
    )]
    fn test_free_list_format(reader: &[u8], expected: Option<(AreaIndex, u64)>) {
        let expected =
            expected.map(|(index, addr)| (FreeArea::new(LinearAddress::new(addr)), index));
        let result = FreeArea::from_storage_reader(reader).ok();
        assert_eq!(result, expected, "Failed to parse FreeArea from {reader:?}");
    }

    #[test]
    // Create a random free list and test that `FreeListIterator` is able to traverse all the free areas
    fn free_list_iterator() {
        let mut rng = seeded_rng();
        let memstore = MemStore::new(vec![]);
        let nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let area_index = rng.random_range(0..NUM_AREA_SIZES as u8);
        let area_size = AREA_SIZES[area_index as usize];

        // create a random free list scattered across the storage
        let offsets = (1..100u64)
            .map(|i| i * area_size)
            .choose_multiple(&mut rng, 10);
        for (cur, next) in offsets.iter().zip(offsets.iter().skip(1)) {
            test_utils::test_write_free_area(
                &nodestore,
                Some(LinearAddress::new(*next).unwrap()),
                area_index,
                *cur,
            );
        }
        test_utils::test_write_free_area(&nodestore, None, area_index, *offsets.last().unwrap());

        // test iterator from a random starting point
        let skip = rng.random_range(0..offsets.len());
        let mut iterator = offsets.into_iter().skip(skip);
        let start = iterator.next().unwrap();
        let mut free_list_iter = FreeListIterator::new(
            nodestore.storage.as_ref(),
            LinearAddress::new(start),
            FreeListParent::FreeListHead(area_index),
        );
        assert_eq!(
            free_list_iter.next().unwrap().unwrap(),
            (LinearAddress::new(start).unwrap(), area_index)
        );

        for offset in iterator {
            assert_eq!(
                free_list_iter.next().unwrap().unwrap(),
                (LinearAddress::new(offset).unwrap(), area_index)
            );
        }

        assert!(free_list_iter.next().is_none());
    }

    // Create two free lists and check that `free_list_iter_with_metadata` correctly returns the free areas and their parents
    #[test]
    fn free_lists_iter_with_metadata() {
        let mut rng = seeded_rng();
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let mut free_lists = FreeLists::default();
        let mut offset = NodeStoreHeader::SIZE;

        // first free list
        let area_index1 = rng.random_range(0..NUM_AREA_SIZES as u8);
        let area_size1 = AREA_SIZES[area_index1 as usize];
        let mut next_free_block1 = None;

        test_utils::test_write_free_area(&nodestore, next_free_block1, area_index1, offset);
        let free_list1_area2 = LinearAddress::new(offset).unwrap();
        next_free_block1 = Some(free_list1_area2);
        offset += area_size1;

        test_utils::test_write_free_area(&nodestore, next_free_block1, area_index1, offset);
        let free_list1_area1 = LinearAddress::new(offset).unwrap();
        next_free_block1 = Some(free_list1_area1);
        offset += area_size1;

        free_lists[area_index1 as usize] = next_free_block1;

        // second free list
        let area_index2 =
            (area_index1 + rng.random_range(1..NUM_AREA_SIZES as u8)) % NUM_AREA_SIZES as u8; // make sure the second free list is different from the first
        assert_ne!(area_index1, area_index2);
        let area_size2 = AREA_SIZES[area_index2 as usize];
        let mut next_free_block2 = None;

        test_utils::test_write_free_area(&nodestore, next_free_block2, area_index2, offset);
        let free_list2_area2 = LinearAddress::new(offset).unwrap();
        next_free_block2 = Some(free_list2_area2);
        offset += area_size2;

        test_utils::test_write_free_area(&nodestore, next_free_block2, area_index2, offset);
        let free_list2_area1 = LinearAddress::new(offset).unwrap();
        next_free_block2 = Some(free_list2_area1);
        offset += area_size2;

        free_lists[area_index2 as usize] = next_free_block2;

        // write header
        test_utils::test_write_header(&mut nodestore, offset, None, free_lists);

        // test iterator
        let mut free_list_iter = nodestore.free_list_iter(0);

        // expected
        let expected_free_list1 = vec![
            FreeAreaWithMetadata {
                addr: free_list1_area1,
                area_index: area_index1,
                free_list_id: area_index1,
                parent: FreeListParent::FreeListHead(area_index1),
            },
            FreeAreaWithMetadata {
                addr: free_list1_area2,
                area_index: area_index1,
                free_list_id: area_index1,
                parent: FreeListParent::PrevFreeArea(free_list1_area1),
            },
        ];

        let expected_free_list2 = vec![
            FreeAreaWithMetadata {
                addr: free_list2_area1,
                area_index: area_index2,
                free_list_id: area_index2,
                parent: FreeListParent::FreeListHead(area_index2),
            },
            FreeAreaWithMetadata {
                addr: free_list2_area2,
                area_index: area_index2,
                free_list_id: area_index2,
                parent: FreeListParent::PrevFreeArea(free_list2_area1),
            },
        ];

        let mut expected_iterator = if area_index1 < area_index2 {
            expected_free_list1.into_iter().chain(expected_free_list2)
        } else {
            expected_free_list2.into_iter().chain(expected_free_list1)
        };

        loop {
            let next = free_list_iter.next_with_metadata();
            let expected = expected_iterator.next();

            if expected.is_none() {
                assert!(next.is_none());
                break;
            }

            assert_eq!(next.unwrap().unwrap(), expected.unwrap());
        }
    }

    #[test]
    fn free_lists_iter_skip_to_next_free_list() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let mut free_lists = FreeLists::default();
        let mut offset = NodeStoreHeader::SIZE;

        // first free list
        let area_index1 = 3;
        let area_size1 = AREA_SIZES[area_index1 as usize];
        let mut next_free_block1 = None;

        test_utils::test_write_free_area(&nodestore, next_free_block1, area_index1, offset);
        let free_list1_area2 = LinearAddress::new(offset).unwrap();
        next_free_block1 = Some(free_list1_area2);
        offset += area_size1;

        test_utils::test_write_free_area(&nodestore, next_free_block1, area_index1, offset);
        let free_list1_area1 = LinearAddress::new(offset).unwrap();
        next_free_block1 = Some(free_list1_area1);
        offset += area_size1;

        free_lists[area_index1 as usize] = next_free_block1;

        // second free list
        let area_index2 = 5;
        assert_ne!(area_index1, area_index2);
        let area_size2 = AREA_SIZES[area_index2 as usize];
        let mut next_free_block2 = None;

        test_utils::test_write_free_area(&nodestore, next_free_block2, area_index2, offset);
        let free_list2_area2 = LinearAddress::new(offset).unwrap();
        next_free_block2 = Some(free_list2_area2);
        offset += area_size2;

        test_utils::test_write_free_area(&nodestore, next_free_block2, area_index2, offset);
        let free_list2_area1 = LinearAddress::new(offset).unwrap();
        next_free_block2 = Some(free_list2_area1);
        offset += area_size2;

        free_lists[area_index2 as usize] = next_free_block2;

        // write header
        test_utils::test_write_header(&mut nodestore, offset, None, free_lists);

        // test iterator
        let mut free_list_iter = nodestore.free_list_iter(0);

        // start at the first free list
        assert_eq!(free_list_iter.current_free_list_id, 0);
        assert_eq!(
            free_list_iter.next_with_metadata().unwrap().unwrap(),
            FreeAreaWithMetadata {
                addr: free_list1_area1,
                area_index: area_index1,
                free_list_id: area_index1,
                parent: FreeListParent::FreeListHead(area_index1),
            },
        );
        // `next_with_metadata` moves the iterator to the first free list that is not empty
        assert_eq!(free_list_iter.current_free_list_id, area_index1);
        free_list_iter.move_to_next_free_list();
        // `move_to_next_free_list` moves the iterator to the next free list
        assert_eq!(free_list_iter.current_free_list_id, area_index1 + 1);
        assert_eq!(
            free_list_iter.next_with_metadata().unwrap().unwrap(),
            FreeAreaWithMetadata {
                addr: free_list2_area1,
                area_index: area_index2,
                free_list_id: area_index2,
                parent: FreeListParent::FreeListHead(area_index2),
            },
        );
        // `next_with_metadata` moves the iterator to the first free list that is not empty
        assert_eq!(free_list_iter.current_free_list_id, area_index2);
        free_list_iter.move_to_next_free_list();
        // `move_to_next_free_list` moves the iterator to the next free list
        assert_eq!(free_list_iter.current_free_list_id, area_index2 + 1);
        assert!(free_list_iter.next_with_metadata().is_none());
        // since no more non-empty free lists, `move_to_next_free_list` moves the iterator to the last free list
        assert_eq!(
            free_list_iter.current_free_list_id,
            (NUM_AREA_SIZES - 1) as u8
        );
        free_list_iter.move_to_next_free_list();
        // `move_to_next_free_list` will move the iterator beyond the last free list
        assert_eq!(free_list_iter.current_free_list_id, NUM_AREA_SIZES as u8);
        assert!(free_list_iter.next_with_metadata().is_none());
    }

    #[test]
    fn const_expr_tests() {
        // these are const expr
        let _ = const { LinearAddress::new(0) };
        let _ = const { LinearAddress::new(1).unwrap().advance(1u64) };
    }
}
