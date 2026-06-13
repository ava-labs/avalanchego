// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Ethereum MPT path encoding
//!
//! Encoding primitives for the Ethereum Modified Merkle Patricia Trie that are
//! needed by both the ethhash hasher and the `eth_getProof`-compatible proof
//! emitter. These helpers are unconditionally compiled so the proof emitter
//! (which is itself always compiled) can call them without pulling in the
//! `ethhash` feature gate.

use crate::TriePath;
use bitfield::bitfield;
use smallvec::SmallVec;
use std::iter::once;

/// Hex-prefix encoding of a nibble path (Ethereum Yellow Paper, appendix C).
///
/// Produces the byte sequence used as the first element of leaf and extension
/// nodes when RLP-encoding an MPT node. The first output byte is a header of
/// the form `00ABCCCC`:
///
/// - `A` is 1 iff `is_leaf` — distinguishes leaf (`2x`/`3x`) from extension
///   (`0x`/`1x`) nodes.
/// - `B` is 1 iff the input has an odd number of nibbles, in which case
///   `CCCC` is the first (orphan) nibble.
/// - If `B` is 0, `CCCC` is zero and the remaining nibbles pack evenly into
///   bytes.
///
/// Remaining nibble pairs are packed high-nibble-first into subsequent bytes.
/// This must match geth's `hexToCompact` exactly; any deviation breaks root
/// hash compatibility.
pub fn nibbles_to_eth_compact<T: TriePath>(nibbles: T, is_leaf: bool) -> SmallVec<[u8; 32]> {
    // This is a bitfield that represents the first byte of the output, documented above
    bitfield! {
        struct CompactFirstByte(u8);
        impl Debug;
        impl new;
        u8;
        is_leaf, set_is_leaf: 5;
        odd_nibbles, set_odd_nibbles: 4;
        low_nibble, set_low_nibble: 3, 0;
    }

    let nibbles = nibbles.as_component_slice();

    let mut first_byte = CompactFirstByte(0);
    first_byte.set_is_leaf(is_leaf);

    let (maybe_low_nibble, nibble_pairs) = nibbles.as_rchunks::<2>();
    if let &[low_nibble] = maybe_low_nibble {
        // we have an odd number of nibbles
        first_byte.set_odd_nibbles(true);
        first_byte.set_low_nibble(low_nibble.as_u8());
    } else {
        // as_rchunks can only return 0 or 1 element in the first slice if N is 2
        debug_assert!(maybe_low_nibble.is_empty());
    }

    // now assemble everything: the first byte, and the nibble pairs compacted back together
    once(first_byte.0)
        .chain(nibble_pairs.iter().map(|&[hi, lo]| hi.join(lo)))
        .collect()
}

#[cfg(test)]
mod test {
    use test_case::test_case;

    use crate::{PathComponent, TriePathFromUnpackedBytes};

    #[test_case(&[], false, &[0x00])]
    #[test_case(&[], true, &[0x20])]
    #[test_case(&[1, 2, 3, 4, 5], false, &[0x11, 0x23, 0x45])]
    #[test_case(&[0, 1, 2, 3, 4, 5], false, &[0x00, 0x01, 0x23, 0x45])]
    #[test_case(&[15, 1, 12, 11, 8], true, &[0x3f, 0x1c, 0xb8])]
    #[test_case(&[0, 15, 1, 12, 11, 8], true, &[0x20, 0x0f, 0x1c, 0xb8])]
    fn test_hex_to_compact(hex: &[u8], has_value: bool, expected_compact: &[u8]) {
        let path = <&[PathComponent]>::path_from_unpacked_bytes(hex).expect("valid path");
        assert_eq!(
            &*super::nibbles_to_eth_compact(path, has_value),
            expected_compact
        );
    }
}
