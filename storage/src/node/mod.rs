// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 4 occurrences after enabling the lint."
)]
#![expect(
    clippy::indexing_slicing,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![expect(
    clippy::items_after_statements,
    reason = "Found 2 occurrences after enabling the lint."
)]
#![expect(
    clippy::missing_errors_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![expect(
    clippy::missing_panics_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]

use bitfield::bitfield;
use branch::Serializable as _;
use enum_as_inner::EnumAsInner;
use integer_encoding::{VarInt, VarIntReader as _, VarIntWriter as _};
use std::fmt::Debug;
use std::io::{Error, Read, Write};
use std::num::NonZero;
use std::vec;

pub mod branch;
mod leaf;
pub mod path;
pub mod persist;

pub use branch::{BranchNode, Child};
pub use leaf::LeafNode;

use crate::{HashType, Path, SharedNode};

/// A node, either a Branch or Leaf

// TODO: explain why Branch is boxed but Leaf is not
#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner)]
#[repr(C)]
pub enum Node {
    /// This node is a [`BranchNode`]
    Branch(Box<BranchNode>),
    /// This node is a [`LeafNode`]
    Leaf(LeafNode),
}

impl Default for Node {
    fn default() -> Self {
        Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: Box::default(),
        })
    }
}

impl From<BranchNode> for Node {
    fn from(branch: BranchNode) -> Self {
        Node::Branch(Box::new(branch))
    }
}

impl From<LeafNode> for Node {
    fn from(leaf: LeafNode) -> Self {
        Node::Leaf(leaf)
    }
}

#[cfg(not(feature = "branch_factor_256"))]
bitfield! {
    struct BranchFirstByte(u8);
    impl Debug;
    impl new;
    u8;
    has_value, set_has_value: 1, 1;
    number_children, set_number_children: 5, 2;
    partial_path_length, set_partial_path_length: 7, 6;
}
#[cfg(not(feature = "branch_factor_256"))]
const MAX_ENCODED_PARTIAL_PATH_LEN: usize = 2;

#[cfg(feature = "branch_factor_256")]
bitfield! {
    struct BranchFirstByte(u8);
    impl Debug;
    impl new;
    u8;
    has_value, set_has_value: 1, 1;
    partial_path_length, set_partial_path_length: 7, 2;
}
#[cfg(feature = "branch_factor_256")]
const MAX_ENCODED_PARTIAL_PATH_LEN: usize = 63;

bitfield! {
    struct LeafFirstByte(u8);
    impl Debug;
    impl new;
    u8;
    is_leaf, set_is_leaf: 0, 0;
    partial_path_length, set_partial_path_length: 7, 1;
}

impl Default for LeafFirstByte {
    fn default() -> Self {
        LeafFirstByte(1)
    }
}

// TODO: Unstable extend_reserve re-implemented here
// Extend<A>::extend_reserve is unstable so we implement it here
// see https://github.com/rust-lang/rust/issues/72631
pub trait ExtendableBytes: Write {
    fn extend<T: IntoIterator<Item = u8>>(&mut self, other: T);
    fn reserve(&mut self, reserve: usize) {
        let _ = reserve;
    }
    fn push(&mut self, value: u8);

    fn extend_from_slice(&mut self, other: &[u8]) {
        self.extend(other.iter().copied());
    }

    /// Write a variable-length integer to the buffer without allocating an
    /// intermediate buffer on the heap.
    ///
    /// This uses a stack buffer for holding the encoded integer and copies it
    /// into the buffer.
    #[expect(clippy::indexing_slicing)]
    fn extend_var_int<VI: VarInt>(&mut self, int: VI) {
        let mut buf = [0u8; 10];
        let len = VarInt::encode_var(int, &mut buf);
        self.extend_from_slice(&buf[..len]);
    }
}

impl ExtendableBytes for Vec<u8> {
    fn extend<T: IntoIterator<Item = u8>>(&mut self, other: T) {
        std::iter::Extend::extend(self, other);
    }
    fn reserve(&mut self, reserve: usize) {
        self.reserve(reserve);
    }
    fn push(&mut self, value: u8) {
        Vec::push(self, value);
    }
    fn extend_from_slice(&mut self, other: &[u8]) {
        Vec::extend_from_slice(self, other);
    }
}

pub struct ByteCounter(u64);

impl ByteCounter {
    pub const fn new() -> Self {
        ByteCounter(0)
    }

    pub const fn count(&self) -> u64 {
        self.0
    }
}

impl Write for ByteCounter {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.0 += buf.len() as u64;
        Ok(buf.len())
    }

    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

impl ExtendableBytes for ByteCounter {
    fn extend<T: IntoIterator<Item = u8>>(&mut self, other: T) {
        self.0 += other.into_iter().count() as u64;
    }
    fn push(&mut self, _value: u8) {
        self.0 += 1;
    }
}

impl Node {
    /// Returns the partial path of the node.
    #[must_use]
    pub fn partial_path(&self) -> &Path {
        match self {
            Node::Branch(b) => &b.partial_path,
            Node::Leaf(l) => &l.partial_path,
        }
    }

    /// Updates the partial path of the node to `partial_path`.
    pub fn update_partial_path(&mut self, partial_path: Path) {
        match self {
            Node::Branch(b) => b.partial_path = partial_path,
            Node::Leaf(l) => l.partial_path = partial_path,
        }
    }

    /// Updates the value of the node to `value`.
    pub fn update_value(&mut self, value: Box<[u8]>) {
        match self {
            Node::Branch(b) => b.value = Some(value),
            Node::Leaf(l) => l.value = value,
        }
    }

    /// Returns Some(value) inside the node, or None if the node is a branch
    /// with no value.
    #[must_use]
    pub fn value(&self) -> Option<&[u8]> {
        match self {
            Node::Branch(b) => b.value.as_deref(),
            Node::Leaf(l) => Some(&l.value),
        }
    }

    /// Given a [Node], returns a set of bytes to write to storage
    /// The format is as follows:
    ///
    /// For a branch:
    ///  - Byte 0:
    ///   - Bit 0: always 0
    ///   - Bit 1: indicates if the branch has a value
    ///   - Bits 2-5: the number of children (unless `branch_factor_256`, which stores it in the next byte)
    ///   - Bits 6-7: 0: empty `partial_path`, 1: 1 nibble, 2: 2 nibbles, 3: length is encoded in the next byte
    ///     (for `branch_factor_256`, bits 2-7 are used for `partial_path` length, up to 63 nibbles)
    ///
    /// The remaining bytes are in the following order:
    ///   - The partial path, possibly preceeded by the length if it is longer than 3 nibbles (varint encoded)
    ///   - The number of children, if the branch factor is 256
    ///   - The children. If the number of children == [`BranchNode::MAX_CHILDREN`], then the children are just
    ///     addresses with hashes. Otherwise, they are offset, address, hash tuples.
    ///
    /// For a leaf:
    ///  - Byte 0:
    ///    - Bit 0: always 1
    ///    - Bits 1-7: the length of the partial path. If the partial path is longer than 126 nibbles, this is set to
    ///      126 and the length is encoded in the next byte.
    ///
    /// The remaining bytes are in the following order:
    ///    - The partial path, possibly preceeded by the length if it is longer than 126 nibbles (varint encoded)
    ///    - The value, always preceeded by the length, varint encoded
    ///
    /// Note that this means the first byte cannot be 255, which would be a leaf with 127 nibbles. We save this extra
    /// value to mark this as a freed area.
    ///
    /// Note that there is a "prefix" byte which is the size of the area when serializing this object. Since
    /// we always have one of those, we include it as a parameter for serialization.
    ///
    /// TODO: We could pack two bytes of the partial path into one and handle the odd byte length
    pub fn as_bytes<T: ExtendableBytes>(&self, prefix: u8, encoded: &mut T) {
        match self {
            Node::Branch(b) => {
                let child_iter = b
                    .children
                    .iter()
                    .enumerate()
                    .filter_map(|(offset, child)| child.as_ref().map(|c| (offset, c)));
                let childcount = child_iter.clone().count();

                // encode the first byte
                let pp_len = if b.partial_path.0.len() <= MAX_ENCODED_PARTIAL_PATH_LEN {
                    b.partial_path.0.len() as u8
                } else {
                    MAX_ENCODED_PARTIAL_PATH_LEN as u8 + 1
                };
                #[cfg(not(feature = "branch_factor_256"))]
                let first_byte: BranchFirstByte = BranchFirstByte::new(
                    u8::from(b.value.is_some()),
                    (childcount % BranchNode::MAX_CHILDREN) as u8,
                    pp_len,
                );
                #[cfg(feature = "branch_factor_256")]
                let first_byte: BranchFirstByte =
                    BranchFirstByte::new(u8::from(b.value.is_some()), pp_len);

                // create an output stack item, which can overflow to memory for very large branch nodes
                const OPTIMIZE_BRANCHES_FOR_SIZE: usize = 1024;
                encoded.reserve(OPTIMIZE_BRANCHES_FOR_SIZE);
                encoded.push(prefix);
                encoded.push(first_byte.0);
                #[cfg(feature = "branch_factor_256")]
                encoded.push((childcount % BranchNode::MAX_CHILDREN) as u8);

                // encode the partial path, including the length if it didn't fit above
                if b.partial_path.0.len() > MAX_ENCODED_PARTIAL_PATH_LEN {
                    encoded
                        .write_varint(b.partial_path.len())
                        .expect("writing to vec should succeed");
                }
                encoded.extend_from_slice(&b.partial_path);

                // encode the value. For tries that have the same length keys, this is always empty
                if let Some(v) = &b.value {
                    encoded
                        .write_varint(v.len())
                        .expect("writing to vec should succeed");
                    encoded.extend_from_slice(v);
                }

                // encode the children
                if childcount == BranchNode::MAX_CHILDREN {
                    for (_, child) in child_iter {
                        if let Child::AddressWithHash(address, hash) = child {
                            encoded.extend_from_slice(&address.get().to_ne_bytes());
                            hash.write_to(encoded);
                        } else {
                            panic!(
                                "attempt to serialize to persist a branch with a child that is not an AddressWithHash"
                            );
                        }
                    }
                } else {
                    for (position, child) in child_iter {
                        encoded
                            .write_varint(position)
                            .expect("writing to vec should succeed");
                        if let Child::AddressWithHash(address, hash) = child {
                            encoded.extend_from_slice(&address.get().to_ne_bytes());
                            hash.write_to(encoded);
                        } else {
                            panic!(
                                "attempt to serialize to persist a branch with a child that is not an AddressWithHash"
                            );
                        }
                    }
                }
            }
            Node::Leaf(l) => {
                let first_byte: LeafFirstByte = LeafFirstByte::new(1, l.partial_path.0.len() as u8);

                const OPTIMIZE_LEAVES_FOR_SIZE: usize = 128;
                encoded.reserve(OPTIMIZE_LEAVES_FOR_SIZE);
                encoded.push(prefix);
                encoded.push(first_byte.0);

                // encode the partial path, including the length if it didn't fit above
                if l.partial_path.0.len() >= 127 {
                    encoded
                        .write_varint(l.partial_path.len())
                        .expect("write to array should succeed");
                }
                encoded.extend_from_slice(&l.partial_path);

                // encode the value
                encoded
                    .write_varint(l.value.len())
                    .expect("write to array should succeed");
                encoded.extend_from_slice(&l.value);
            }
        }
    }

    /// Given a reader, return a [Node] from those bytes
    pub fn from_reader(mut serialized: impl Read) -> Result<Self, Error> {
        let mut first_byte: [u8; 1] = [0];
        serialized.read_exact(&mut first_byte)?;
        match first_byte[0] {
            255 => {
                // this is a freed area
                Err(Error::other("attempt to read freed area"))
            }
            leaf_first_byte if leaf_first_byte & 1 == 1 => {
                let partial_path_len = if leaf_first_byte < 255 {
                    // less than 126 nibbles
                    LeafFirstByte(leaf_first_byte).partial_path_length() as usize
                } else {
                    serialized.read_varint()?
                };

                let mut partial_path = vec![0u8; partial_path_len];
                serialized.read_exact(&mut partial_path)?;

                let mut value_len_buf = [0u8; 1];
                serialized.read_exact(&mut value_len_buf)?;
                let value_len = value_len_buf[0] as usize;

                let mut value = vec![0u8; value_len];
                serialized.read_exact(&mut value)?;

                Ok(Node::Leaf(LeafNode {
                    partial_path: Path::from(partial_path),
                    value: value.into(),
                }))
            }
            branch_first_byte => {
                let branch_first_byte = BranchFirstByte(branch_first_byte);

                let has_value = branch_first_byte.has_value() == 1;
                #[cfg(not(feature = "branch_factor_256"))]
                let childcount = branch_first_byte.number_children() as usize;
                #[cfg(feature = "branch_factor_256")]
                let childcount = {
                    let mut childcount_buf = [0u8; 1];
                    serialized.read_exact(&mut childcount_buf)?;
                    childcount_buf[0] as usize
                };

                let mut partial_path_len = branch_first_byte.partial_path_length() as usize;
                if partial_path_len > MAX_ENCODED_PARTIAL_PATH_LEN {
                    partial_path_len = serialized.read_varint()?;
                }

                let mut partial_path = vec![0u8; partial_path_len];
                serialized.read_exact(&mut partial_path)?;

                let value = if has_value {
                    let mut value_len_buf = [0u8; 1];
                    serialized.read_exact(&mut value_len_buf)?;
                    let value_len = value_len_buf[0] as usize;

                    let mut value = vec![0u8; value_len];
                    serialized.read_exact(&mut value)?;
                    Some(value.into())
                } else {
                    None
                };

                let mut children = [const { None }; BranchNode::MAX_CHILDREN];
                if childcount == 0 {
                    // branch is full of all children
                    for child in &mut children {
                        // TODO: we can read them all at once
                        let mut address_buf = [0u8; 8];
                        serialized.read_exact(&mut address_buf)?;
                        let address = u64::from_ne_bytes(address_buf);

                        let hash = HashType::from_reader(&mut serialized)?;

                        *child = Some(Child::AddressWithHash(
                            NonZero::new(address).ok_or(Error::other("zero address in child"))?,
                            hash,
                        ));
                    }
                } else {
                    for _ in 0..childcount {
                        let mut position_buf = [0u8; 1];
                        serialized.read_exact(&mut position_buf)?;
                        let position = position_buf[0] as usize;

                        let mut address_buf = [0u8; 8];
                        serialized.read_exact(&mut address_buf)?;
                        let address = u64::from_ne_bytes(address_buf);

                        let hash = HashType::from_reader(&mut serialized)?;

                        children[position] = Some(Child::AddressWithHash(
                            NonZero::new(address).ok_or(Error::other("zero address in child"))?,
                            hash,
                        ));
                    }
                }

                Ok(Node::Branch(Box::new(BranchNode {
                    partial_path: partial_path.into(),
                    value,
                    children,
                })))
            }
        }
    }
}

/// A path iterator item, which has the key nibbles up to this point,
/// a node, the address of the node, and the nibble that points to the
/// next child down the list
#[derive(Debug)]
pub struct PathIterItem {
    /// The key of the node at `address` as nibbles.
    pub key_nibbles: Box<[u8]>,
    /// A reference to the node
    pub node: SharedNode,
    /// The next item returned by the iterator is a child of `node`.
    /// Specifically, it's the child at index `next_nibble` in `node`'s
    /// children array.
    /// None if `node` is the last node in the path.
    pub next_nibble: Option<u8>,
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]

    use crate::node::{BranchNode, LeafNode, Node};
    use crate::{Child, LinearAddress, Path};
    use test_case::test_case;

    #[test_case(
        Node::Leaf(LeafNode {
            partial_path: Path::from(vec![0, 1, 2, 3]),
            value: vec![4, 5, 6, 7].into()
        }), 11; "leaf node with value")]
    #[test_case(Node::Branch(Box::new(BranchNode {
        partial_path: Path::from(vec![0, 1]),
        value: None,
        children: std::array::from_fn(|i| {
            if i == 15 {
                Some(Child::AddressWithHash(LinearAddress::new(1).unwrap(), std::array::from_fn::<u8, 32, _>(|i| i as u8).into()))
            } else {
                None
            }
        })})), 45; "one child branch node with short partial path and no value"
    )]
    #[test_case(Node::Branch(Box::new(BranchNode {
        partial_path: Path::from(vec![0, 1, 2, 3]),
        value: Some(vec![4, 5, 6, 7].into()),
        children: std::array::from_fn(|_|
                Some(Child::AddressWithHash(LinearAddress::new(1).unwrap(), std::array::from_fn::<u8, 32, _>(|i| i as u8).into()))
        )})), 652; "full branch node with long partial path and value"
    )]
    // When ethhash is enabled, we don't actually check the `expected_length`
    fn test_serialize_deserialize(
        node: Node,
        #[cfg_attr(
            any(feature = "branch_factor_256", feature = "ethhash"),
            expect(unused_variables)
        )]
        expected_length: usize,
    ) {
        use crate::node::Node;
        use std::io::Cursor;

        let mut serialized = Vec::new();
        node.as_bytes(0, &mut serialized);
        #[cfg(not(any(feature = "branch_factor_256", feature = "ethhash")))]
        assert_eq!(serialized.len(), expected_length);
        let mut cursor = Cursor::new(&serialized);
        cursor.set_position(1);
        let deserialized = Node::from_reader(cursor).unwrap();

        assert_eq!(node, deserialized);
    }
}
