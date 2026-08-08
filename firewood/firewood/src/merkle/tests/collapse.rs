// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;

/// Tests `collapse_root_to_path` with no range check. The merkle trie
/// initially has \x01 and \x10, then `collapse_root_to_path` is called
/// with a target of \x10. Since \x01 is not on the path from the root
/// to the target, nibble 0 is stripped from the root. Path compression
/// changes the root's partial path from a length of 0 to a length of 2.
#[test]
fn test_collapse_root_to_path() {
    let mut merkle = create_in_memory_merkle();
    let pc = |n: u8| PathComponent::try_new(n).unwrap();

    // \x01 → nibbles [0, 1], \x10 → nibbles [1, 0]
    // Root has empty partial path with children at nibbles 0 and 1.
    merkle.insert(b"\x01", Box::from(b"a".as_slice())).unwrap();
    merkle.insert(b"\x10", Box::from(b"b".as_slice())).unwrap();

    let root = merkle.nodestore.root_mut().as_ref().unwrap();
    assert_eq!(root.partial_path().len(), 0);

    // Collapse toward [1, 0] with no range check.
    // Strips child at nibble 0, flattens single-child root.
    merkle.collapse_root_to_path(&[pc(1), pc(0)], None).unwrap();

    let root = merkle.nodestore.root_mut().as_ref().unwrap();
    assert_eq!(root.partial_path().len(), 2);
}

/// Tests `collapse_root_to_path` with a range parameter. Nodes from the merkle
/// trie on the path to the specified target will have their children stripped.
/// If any of these children are inside the range, then this is an error and
/// an `EndRootMismatch` error is returned. Compression is applied afterwards
/// which may eliminate empty branch nodes and increase the partial path length
/// of the remaining nodes. In this example, the target is \x10, the range is
/// [0x0], [0xf], and the root (which is on-path) has a child in the range.
/// Therefore we expect to receive an `EndRootMismatch` error.
#[test]
fn test_collapse_root_to_path_rejects() {
    let mut merkle = create_in_memory_merkle();
    let pc = |n: u8| PathComponent::try_new(n).unwrap();

    // \x01 → nibbles [0, 1], \x10 → nibbles [1, 0]
    // Root has empty partial path with children at nibbles 0 and 1.
    merkle.insert(b"\x01", Box::from(b"a".as_slice())).unwrap();
    merkle.insert(b"\x10", Box::from(b"b".as_slice())).unwrap();

    let root = merkle.nodestore.root_mut().as_ref().unwrap();
    assert_eq!(root.partial_path().len(), 0);

    // Collapse toward [1, 0] but nibble 0 is in range [0x0, 0xf].
    // Should reject because collapsing would strip a child that is within the
    // range, indicating tampered batch_ops.
    let result = merkle.collapse_root_to_path(&[pc(1), pc(0)], Some((&[0x0], &[0xf])));

    assert!(matches!(
        result,
        Err(api::Error::ProofError(ProofError::EndRootMismatch))
    ));
}

#[test]
fn test_collapse_branch_to_path() {
    let pc = |n: u8| PathComponent::try_new(n).unwrap();
    let mut merkle = create_in_memory_merkle();

    merkle
        .insert(b"\x10\x21", Box::from(b"a".as_slice()))
        .unwrap();
    merkle
        .insert(b"\x10\x22", Box::from(b"b".as_slice()))
        .unwrap();
    merkle
        .insert(b"\x10\x30", Box::from(b"c".as_slice()))
        .unwrap();
    merkle.insert(b"\x20", Box::from(b"d".as_slice())).unwrap();

    // from = [1, 0] (parent proof node), to = [1, 0, 2, 1] (child proof node)
    // We strip away the child pointer to \x10\x22 from the branch at [1, 0, 2].
    // This will cause the node [1, 0, 2] to collapse into a node at nibble 2
    // from the parent proof node (\x10) with a partial path of [1].
    merkle
        .collapse_branch_to_path(&[pc(1), pc(0)], &[pc(1), pc(0), pc(2), pc(1)], None)
        .unwrap();

    assert!(merkle.get_value(b"\x10\x21").unwrap().is_some());
    assert!(merkle.get_value(b"\x10\x22").unwrap().is_none());
    assert!(merkle.get_value(b"\x10\x30").unwrap().is_some());
}

#[test]
fn test_collapse_branch_to_path_rejects() {
    let pc = |n: u8| PathComponent::try_new(n).unwrap();
    let mut merkle = create_in_memory_merkle();

    merkle
        .insert(b"\x10\x21", Box::from(b"a".as_slice()))
        .unwrap();
    merkle
        .insert(b"\x10\x22", Box::from(b"b".as_slice()))
        .unwrap();
    merkle
        .insert(b"\x10\x30", Box::from(b"c".as_slice()))
        .unwrap();
    merkle.insert(b"\x20", Box::from(b"d".as_slice())).unwrap();

    // from = [1, 0] (parent proof node), to = [1, 0, 2, 1] (child proof node)
    // We try to strip away the child pointer to \x10\x22 but fail because
    // it is within the range.
    let result = merkle.collapse_branch_to_path(
        &[pc(1), pc(0)],
        &[pc(1), pc(0), pc(2), pc(1)],
        Some((&[0x1, 0x0, 0x2, 0x0], &[0x1, 0x0, 0x2, 0xf])),
    );
    assert!(matches!(
        result,
        Err(api::Error::ProofError(ProofError::EndRootMismatch))
    ));
}
