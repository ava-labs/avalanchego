// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(not(feature = "branch_factor_256"))]
/// A path component in a hexary trie; which is only 4 bits (aka a nibble).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PathComponent(pub crate::u4::U4);

#[cfg(feature = "branch_factor_256")]
/// A path component in a 256-ary trie; which is 8 bits (aka a byte).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PathComponent(pub u8);

impl PathComponent {
    /// All possible path components.
    ///
    /// This makes it easy to iterate over all possible children of a branch
    /// in a type-safe way. It is preferrable to do:
    ///
    /// ```ignore
    /// for (idx, slot) in PathComponent::ALL.into_iter().zip(branch.children.each_ref()) {
    ///     /// use idx and slot
    /// }
    /// ```
    ///
    /// instead of using a raw range like (`0..16`) or  [`Iterator::enumerate`],
    /// which does not give a type-safe path component.
    #[cfg(not(feature = "branch_factor_256"))]
    pub const ALL: [Self; 16] = [
        Self(crate::u4::U4::new_masked(0x0)),
        Self(crate::u4::U4::new_masked(0x1)),
        Self(crate::u4::U4::new_masked(0x2)),
        Self(crate::u4::U4::new_masked(0x3)),
        Self(crate::u4::U4::new_masked(0x4)),
        Self(crate::u4::U4::new_masked(0x5)),
        Self(crate::u4::U4::new_masked(0x6)),
        Self(crate::u4::U4::new_masked(0x7)),
        Self(crate::u4::U4::new_masked(0x8)),
        Self(crate::u4::U4::new_masked(0x9)),
        Self(crate::u4::U4::new_masked(0xA)),
        Self(crate::u4::U4::new_masked(0xB)),
        Self(crate::u4::U4::new_masked(0xC)),
        Self(crate::u4::U4::new_masked(0xD)),
        Self(crate::u4::U4::new_masked(0xE)),
        Self(crate::u4::U4::new_masked(0xF)),
    ];

    /// All possible path components.
    ///
    /// This makes it easy to iterate over all possible children of a branch
    /// in a type-safe way. It is preferrable to do:
    ///
    /// ```ignore
    /// for (idx, slot) in PathComponent::ALL.into_iter().zip(branch.children.each_ref()) {
    ///     /// use idx and slot
    /// }
    /// ```
    ///
    /// instead of using a raw range like (`0..256`) or  [`Iterator::enumerate`],
    /// which does not give a type-safe path component.
    #[cfg(feature = "branch_factor_256")]
    pub const ALL: [Self; 256] = {
        let mut all = [Self(0); 256];
        let mut i = 0;
        #[expect(clippy::indexing_slicing)]
        while i < 256 {
            all[i] = Self(i as u8);
            i += 1;
        }
        all
    };
}

impl PathComponent {
    /// Returns the path component as a [`u8`].
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            self.0.as_u8()
        }
        #[cfg(feature = "branch_factor_256")]
        {
            self.0
        }
    }

    /// Returns the path component as a [`usize`].
    #[must_use]
    pub const fn as_usize(self) -> usize {
        self.as_u8() as usize
    }

    /// Tries to create a path component from the given [`u8`].
    ///
    /// For hexary tries, the input must be in the range 0x00 to 0x0F inclusive.
    /// Any value outside this range will result in [`None`].
    ///
    /// For 256-ary tries, any value is valid.
    #[must_use]
    pub const fn try_new(value: u8) -> Option<Self> {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            match crate::u4::U4::try_new(value) {
                Some(u4) => Some(Self(u4)),
                None => None,
            }
        }
        #[cfg(feature = "branch_factor_256")]
        {
            Some(Self(value))
        }
    }

    /// Creates a pair of path components from a single byte.
    #[cfg(not(feature = "branch_factor_256"))]
    #[must_use]
    pub const fn new_pair(v: u8) -> (Self, Self) {
        let (upper, lower) = crate::u4::U4::new_pair(v);
        (Self(upper), Self(lower))
    }
}

impl std::fmt::Display for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            write!(f, "{self:X}")
        }
        #[cfg(feature = "branch_factor_256")]
        {
            write!(f, "{self:02X}")
        }
    }
}

impl std::fmt::Binary for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Binary::fmt(&self.0, f)
    }
}

impl std::fmt::LowerHex for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::LowerHex::fmt(&self.0, f)
    }
}

impl std::fmt::UpperHex for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::UpperHex::fmt(&self.0, f)
    }
}
