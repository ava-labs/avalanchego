// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Snapshot tests for `firewood-storage` node serialization.
//!
//! These tests lock down the binary wire format for three `Serializable`
//! implementations and the `Node::as_bytes` / `Node::from_reader` functions.
//!
//! Snapshot files live in `storage/src/node/snapshots/`. Run
//! `just snapshot-nodes` to write them on the first run or to regenerate
//! them after an intentional format change.
//!
//! # Structure
//!
//! Each group of structurally-identical tests is a single
//! [`test_case`](test_case::test_case)-parametrized function. The first
//! argument of every case is the explicit `insta` snapshot name (an explicit
//! name is required: `test_case` would otherwise make all cases of one function
//! resolve to the same auto-derived name).
//!
//! Tests whose bytes differ between hash modes (branch child encoding) build in
//! both modes from a single function; [`hash_mode_name`] prefixes their
//! snapshot name with `merkledb__` or `ethhash__` so each mode gets its own
//! snapshot file. Tests whose bytes are mode-independent share a single file.
//!
//! # Test coverage
//!
//! ## `Serializable` implementations (feature-agnostic)
//!
//! | Function | Case | Subject |
//! |----------|------|---------|
//! | [`trie_hash`] | `zero` | All-zero 32-byte hash |
//! | [`trie_hash`] | `sequential` | Sequential-byte hash `[0x00..0x1f]` |
//! | [`free_area`] | `no_next` | `FreeArea::new(None)` → `[0xFF, 0x00]` |
//! | [`free_area`] | `with_addr` | Single-byte varint address |
//! | [`free_area`] | `with_large_addr` | Multi-byte varint address |
//!
//! ## `Node` serialization
//!
//! Leaf nodes produce identical bytes under both hash modes (no child hashes),
//! so the [`leaf`] cases share a single snapshot file. Branch nodes encode
//! child hashes differently per mode, so [`branch`] cases are prefixed via
//! [`hash_mode_name`].
//!
//! | Function | Case | Subject |
//! |----------|------|---------|
//! | [`leaf`] | `with_value` | Leaf: path `[0,1,2,3]`, value `[4,5,6,7]` |
//! | [`leaf`] | `long_partial_path` | Leaf with a path that triggers varint overflow |
//! | [`branch`] | `one_child` | Branch: one child (`merkledb__`/`ethhash__`) |
//! | [`branch`] | `with_value` | Branch: value + one child (`merkledb__`/`ethhash__`) |
//!
//! ## `HashType` serialization (ethhash feature only)
//!
//! | Function | Case | Subject |
//! |----------|------|---------|
//! | `hash_or_rlp` | `keccak_hash` | Full 32-byte hash: `[0x00] + 32 bytes` |
//! | `hash_or_rlp` | `rlp_bytes` | Short RLP: length byte + payload bytes |

use test_case::test_case;

use crate::node::branch::Serializable;
use crate::node::{BranchNode, LeafNode, Node};
use crate::nodestore::alloc::FreeArea;
use crate::{Child, Children, LinearAddress, NibblesIterator, Path, PathComponent, TrieHash};

/// Serializes a [`Serializable`] value into a fresh `Vec<u8>`.
fn write_to_vec<T: Serializable>(t: &T) -> Vec<u8> {
    let mut buf = Vec::new();
    t.write_to(&mut buf);
    buf
}

/// Calls [`Node::as_bytes`] and returns the full encoded bytes (including the
/// leading `AreaIndex` byte at position 0).
fn node_as_bytes(node: &Node) -> Vec<u8> {
    let mut buf = Vec::new();
    node.as_bytes(&mut buf).unwrap();
    buf
}

/// Prefixes a snapshot name with the active hash mode so feature-split tests
/// write distinct snapshot files. Bytes differ between modes, so `merkledb` and
/// `ethhash` snapshots must not share a filename.
fn hash_mode_name(name: &str) -> String {
    let mode = if cfg!(feature = "ethhash") {
        "ethhash"
    } else {
        "merkledb"
    };
    format!("{mode}__{name}")
}

/// Builds a zero-hash child entry at the given nibble index using
/// [`TrieHash::from`] + `.into()`, which works under both hash modes.
fn child_at(nibble: u8) -> impl Fn(PathComponent) -> Option<Child> {
    let hash: crate::HashType = TrieHash::from([0u8; 32]).into();
    move |i| {
        if i.as_u8() == nibble {
            Some(Child::AddressWithHash(
                LinearAddress::new(1).unwrap(),
                hash.clone(),
            ))
        } else {
            None
        }
    }
}

/// [`TrieHash`] serialization — always 32 flat bytes, no framing.
#[test_case("zero", [0u8; 32] ; "zero")]
#[test_case("sequential", std::array::from_fn::<u8, 32, _>(|i| i as u8) ; "sequential")]
fn trie_hash(name: &str, bytes: [u8; 32]) {
    let h = TrieHash::from(bytes);
    insta::assert_snapshot!(name, hex::encode(write_to_vec(&h)));
}

/// [`FreeArea`] serialization — `0xFF` marker + varint-encoded optional address.
///
/// `no_next` encodes `None` as `[0xFF, 0x00]`. `with_addr` (42) fits in a single
/// LEB128 byte; `with_large_addr` (1024) requires two (`0x80 0x08`).
#[test_case("no_next", None ; "no_next")]
#[test_case("with_addr", Some(42) ; "with_addr")]
#[test_case("with_large_addr", Some(1024) ; "with_large_addr")]
fn free_area(name: &str, addr: Option<u64>) {
    let fa = FreeArea::new(addr.map(|a| LinearAddress::new(a).unwrap()));
    insta::assert_snapshot!(name, hex::encode(write_to_vec(&fa)));
}

/// Leaf node serialization. Leaf encoding is identical under both hash modes
/// (no child hashes), so these cases are not prefixed.
///
/// Encoded first byte: `is_leaf=1 | path_len_low_bits`. Followed by path
/// nibbles, then varint value length, then value bytes. `long_partial_path` has
/// a path long enough to trigger the varint overflow for the path-length field
/// (nibble count > the inline capacity), see #1056. The value is fixed at
/// `[4,5,6,7]` for both cases.
#[test_case("with_value", Path::from(vec![0, 1, 2, 3]) ; "with_value")]
#[test_case(
    "long_partial_path",
    Path::from_nibbles_iterator(NibblesIterator::new(
        b"this is a really long partial path, like so long it's more than 63 nibbles long which triggers #1056."
    ))
    ; "long_partial_path"
)]
fn leaf(name: &str, partial_path: Path) {
    let node = Node::Leaf(LeafNode {
        partial_path,
        value: vec![4, 5, 6, 7].into(),
    });
    insta::assert_snapshot!(name, hex::encode(node_as_bytes(&node)));
}

/// Branch node serialization. Child hashes encode differently per hash mode
/// (flat 32-byte `TrieHash` for MerkleDB; a 1-byte discriminant prefix for
/// ethhash), so [`hash_mode_name`] keeps the two modes in separate files.
///
/// `one_child` places a single child at nibble 15 (`0xF`) with no value.
/// `with_value` exercises the has-value bit and value-length varint with a
/// child at nibble 0 and a longer partial path.
#[test_case("one_child", vec![0, 1], None, 15 ; "one_child")]
#[test_case("with_value", vec![0, 1, 2, 3], Some(vec![4, 5, 6, 7]), 0 ; "with_value")]
fn branch(name: &str, path: Vec<u8>, value: Option<Vec<u8>>, child: u8) {
    let node = Node::Branch(Box::new(BranchNode {
        partial_path: Path::from(path),
        value: value.map(Into::into),
        children: Children::from_fn(child_at(child)),
    }));
    insta::assert_snapshot!(hash_mode_name(name), hex::encode(node_as_bytes(&node)));
}

/// [`crate::HashType`] serialization — available only when the
/// `ethhash` feature is enabled. Each value is prefixed with a 1-byte
/// discriminant: `0x00` for a full 32-byte Keccak hash, non-zero (the RLP
/// length) for an inline RLP value.
///
/// `keccak_hash` is discriminant `0x00` followed by 32 zero bytes. `rlp_bytes`
/// is a single-byte RLP payload `0xC0` (empty RLP list): length byte `0x01`
/// followed by `0xC0`.
#[cfg(feature = "ethhash")]
#[test_case("keccak_hash", crate::HashType::Hash(TrieHash::from([0u8; 32])) ; "keccak_hash")]
#[test_case("rlp_bytes", crate::HashType::Rlp(smallvec::SmallVec::from_slice(&[0xC0u8])) ; "rlp_bytes")]
fn hash_or_rlp(name: &str, h: crate::HashType) {
    insta::assert_snapshot!(name, hex::encode(write_to_vec(&h)));
}
