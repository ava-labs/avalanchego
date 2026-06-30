// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood_storage::{Children, PathComponent, U4};

/// Compact u16 bitmap for tracking which of a branch node's 16 children
/// are present or "outside" the proven range.
///
/// Used in two contexts:
/// - **Proof serialization**: tracks which children are present in a node,
///   enabling efficient bitmap-based encoding.
/// - **Proof verification**: tracks which children are "outside" the proven
///   range and should use the proof's original hashes.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct ChildMask(u16);

impl ChildMask {
    /// A mask with all bits set.
    pub(crate) const ALL: Self = Self(u16::MAX);

    /// Sets all bits for indices strictly less than `nibble`.
    pub(crate) const fn set_below(mut self, nibble: U4) -> Self {
        self.0 |= 1u16.wrapping_shl(nibble.as_u8() as u32).wrapping_sub(1);
        self
    }

    /// Sets all bits for indices strictly greater than `nibble`.
    pub(crate) const fn set_above(mut self, nibble: U4) -> Self {
        self.0 |= u16::MAX.wrapping_shl(nibble.as_u8() as u32).wrapping_shl(1);
        self
    }

    /// Returns `true` if the bit at `nibble` is set.
    pub(crate) const fn is_set(self, nibble: U4) -> bool {
        self.0 & 1u16.wrapping_shl(nibble.as_u8() as u32) != 0
    }

    /// Sets the bit at `nibble`, returning the updated mask.
    pub(crate) const fn set(self, nibble: U4) -> Self {
        Self(self.0 | 1u16.wrapping_shl(nibble.as_u8() as u32))
    }

    /// Create a new `ChildMask` from the given children array, setting a bit
    /// for each child that is `Some`.
    pub(crate) fn from_children<T>(children: &Children<Option<T>>) -> Self {
        Self(children.iter_present().fold(0, |bits, (i, _)| {
            bits | 1u16.wrapping_shl(u32::from(i.as_u8()))
        }))
    }

    /// Iterates over the indices of all set bits, yielding `PathComponent`s.
    ///
    /// Uses bit-scanning (`trailing_zeros` + clear-lowest-bit) so the cost is
    /// proportional to the number of set bits, not the total width.
    pub(crate) fn iter_indices(self) -> impl Iterator<Item = PathComponent> {
        PathComponent::ALL
            .into_iter()
            .filter(move |&i| self.is_set(i.0))
    }

    /// Serialize as little-endian bytes (for proof wire format).
    pub(crate) const fn to_le_bytes(self) -> [u8; 2] {
        self.0.to_le_bytes()
    }

    /// Deserialize from little-endian bytes (for proof wire format).
    pub(crate) const fn from_le_bytes(bytes: [u8; 2]) -> Self {
        Self(u16::from_le_bytes(bytes))
    }
}

impl std::ops::BitOr for ChildMask {
    type Output = Self;

    fn bitor(self, rhs: Self) -> Self {
        Self(self.0 | rhs.0)
    }
}

impl std::ops::BitOrAssign for ChildMask {
    fn bitor_assign(&mut self, rhs: Self) {
        self.0 |= rhs.0;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const fn u4(v: u8) -> U4 {
        U4::new_masked(v)
    }

    #[test]
    fn child_mask_set_below() {
        // set_below(n) should set bits 0..n (indices < n)
        let expected: [(U4, u16); 16] = [
            (u4(0), 0b0000_0000_0000_0000),
            (u4(1), 0b0000_0000_0000_0001),
            (u4(2), 0b0000_0000_0000_0011),
            (u4(3), 0b0000_0000_0000_0111),
            (u4(4), 0b0000_0000_0000_1111),
            (u4(5), 0b0000_0000_0001_1111),
            (u4(6), 0b0000_0000_0011_1111),
            (u4(7), 0b0000_0000_0111_1111),
            (u4(8), 0b0000_0000_1111_1111),
            (u4(9), 0b0000_0001_1111_1111),
            (u4(10), 0b0000_0011_1111_1111),
            (u4(11), 0b0000_0111_1111_1111),
            (u4(12), 0b0000_1111_1111_1111),
            (u4(13), 0b0001_1111_1111_1111),
            (u4(14), 0b0011_1111_1111_1111),
            (u4(15), 0b0111_1111_1111_1111),
        ];

        for (nibble, bits) in expected {
            let mask = ChildMask::default().set_below(nibble);
            assert_eq!(mask.0, bits, "set_below({nibble})");
        }
    }

    #[test]
    fn child_mask_set_above() {
        // set_above(n) should set bits (n+1)..16 (indices > n)
        let expected: [(U4, u16); 16] = [
            (u4(0), 0b1111_1111_1111_1110),
            (u4(1), 0b1111_1111_1111_1100),
            (u4(2), 0b1111_1111_1111_1000),
            (u4(3), 0b1111_1111_1111_0000),
            (u4(4), 0b1111_1111_1110_0000),
            (u4(5), 0b1111_1111_1100_0000),
            (u4(6), 0b1111_1111_1000_0000),
            (u4(7), 0b1111_1111_0000_0000),
            (u4(8), 0b1111_1110_0000_0000),
            (u4(9), 0b1111_1100_0000_0000),
            (u4(10), 0b1111_1000_0000_0000),
            (u4(11), 0b1111_0000_0000_0000),
            (u4(12), 0b1110_0000_0000_0000),
            (u4(13), 0b1100_0000_0000_0000),
            (u4(14), 0b1000_0000_0000_0000),
            (u4(15), 0b0000_0000_0000_0000),
        ];

        for (nibble, bits) in expected {
            let mask = ChildMask::default().set_above(nibble);
            assert_eq!(mask.0, bits, "set_above({nibble})");
        }
    }

    #[test]
    fn child_mask_is_set() {
        // Set bits below 3 and above 12
        let mask = ChildMask::default().set_below(u4(3)).set_above(u4(12));

        for i in 0..16u8 {
            let nibble = u4(i);
            let expected = !(3..=12).contains(&i);

            assert_eq!(mask.is_set(nibble), expected, "is_set({nibble})");
        }
    }

    #[test]
    fn child_mask_bitor() {
        let combined = ChildMask::default().set_below(u4(5)) // bits 0..5
            | ChildMask::default().set_above(u4(10)); // bits 11..16

        for i in 0..16u8 {
            let nibble = u4(i);
            let expected = !(5..=10).contains(&i);

            assert_eq!(combined.is_set(nibble), expected, "union is_set({nibble})");
        }
    }

    #[test]
    fn child_mask_default_is_empty() {
        let mask = ChildMask::default();

        for i in 0..16u8 {
            assert!(!mask.is_set(u4(i)), "default should have no bits set");
        }
    }

    #[test]
    fn child_mask_all_is_full() {
        let mask = ChildMask::ALL;
        for i in 0..16u8 {
            assert!(mask.is_set(u4(i)), "ALL should have all bits set");
        }
    }

    #[test]
    fn child_mask_set_single() {
        for i in 0..16u8 {
            let mask = ChildMask::default().set(u4(i));
            assert_eq!(mask.0, 1u16 << i, "set({i})");
            assert!(mask.is_set(u4(i)));
        }
    }

    #[test]
    fn child_mask_set_accumulates() {
        let mask = ChildMask::default().set(u4(0)).set(u4(4)).set(u4(15));
        assert!(mask.is_set(u4(0)));
        assert!(!mask.is_set(u4(1)));
        assert!(mask.is_set(u4(4)));
        assert!(mask.is_set(u4(15)));
    }

    #[test]
    fn child_mask_from_children_empty() {
        let children: Children<Option<()>> = Children::new();
        let mask = ChildMask::from_children(&children);
        assert_eq!(mask.0, 0);
    }

    #[test]
    fn child_mask_from_children_some() {
        let mut children: Children<Option<()>> = Children::new();
        children[PathComponent::ALL[0]] = Some(());
        children[PathComponent::ALL[5]] = Some(());
        children[PathComponent::ALL[15]] = Some(());
        let mask = ChildMask::from_children(&children);
        assert!(mask.is_set(u4(0)));
        assert!(!mask.is_set(u4(1)));
        assert!(mask.is_set(u4(5)));
        assert!(mask.is_set(u4(15)));
    }

    #[test]
    fn child_mask_from_children_all() {
        let children: Children<Option<()>> = Children::from_fn(|_| Some(()));
        let mask = ChildMask::from_children(&children);
        assert_eq!(mask, ChildMask::ALL);
    }

    #[test]
    fn child_mask_iter_indices() {
        let mask = ChildMask::default().set(u4(1)).set(u4(7)).set(u4(14));
        let indices: Vec<u8> = mask.iter_indices().map(PathComponent::as_u8).collect();
        assert_eq!(indices, vec![1, 7, 14]);
    }

    #[test]
    fn child_mask_iter_indices_empty() {
        let indices: Vec<u8> = ChildMask::default()
            .iter_indices()
            .map(PathComponent::as_u8)
            .collect();
        assert!(indices.is_empty());
    }

    #[test]
    fn child_mask_le_bytes_roundtrip() {
        let mask = ChildMask::default().set(u4(0)).set(u4(8)).set(u4(15));
        let bytes = mask.to_le_bytes();
        assert_eq!(ChildMask::from_le_bytes(bytes), mask);
    }

    #[test]
    fn child_mask_le_bytes_encoding() {
        // bit 0 in low byte, bit 9 in high byte
        let mask = ChildMask::default().set(u4(0)).set(u4(9));
        assert_eq!(mask.to_le_bytes(), [0x01, 0x02]);
    }

    #[test]
    fn child_mask_bitor_assign() {
        let mut mask = ChildMask::default().set(u4(1));
        mask |= ChildMask::default().set(u4(14));
        assert!(mask.is_set(u4(1)));
        assert!(mask.is_set(u4(14)));
        assert!(!mask.is_set(u4(0)));
    }
}
