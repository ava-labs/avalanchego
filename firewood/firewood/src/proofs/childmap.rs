// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Compact bitmap representation for tracking present children in proof nodes.
//!
//! This module provides the [`ChildrenMap`] type, which is used internally during
//! proof serialization to efficiently encode which children are present in a node.
//! Instead of serializing each child slot individually (which would require many bytes
//! for sparse nodes), the bitmap allows encoding the presence or absence of up to 256
//! children in just 32 bytes (for branch factor 256) or 2 bytes (for branch factor 16).
//!
//! # Serialization Efficiency
//!
//! The [`ChildrenMap`] provides significant space savings during proof serialization:
//!
//! - **Branch factor 16**: Uses 2 bytes to represent which of 16 children are present
//! - **Branch factor 256**: Uses 32 bytes to represent which of 256 children are present
//!
//! This is especially beneficial for sparse nodes where only a few children exist,
//! as the serialization size is fixed regardless of how many children are actually
//! present. The deserializer can quickly scan the bitmap to determine which child
//! hashes to expect in the serialized data.
//!
//! # Internal Use Only
//!
//! This type is not part of the public API and is only used internally by the proof
//! serialization and deserialization logic in the `ser` and `de` modules.

use firewood_storage::{Children, PathComponent};

#[derive(Clone, Copy, PartialEq, Eq, bytemuck_derive::Pod, bytemuck_derive::Zeroable)]
#[repr(C)]
/// A bitmap indicating which children are present in a node.
pub(super) struct ChildrenMap([u8; ChildrenMap::SIZE]);

impl ChildrenMap {
    const SIZE: usize = firewood_storage::BranchNode::MAX_CHILDREN / 8;

    /// Create a new `ChildrenMap` from the given children array.
    pub fn new<T>(children: &Children<Option<T>>) -> Self {
        let mut map = Self([0_u8; Self::SIZE]);

        for (i, _) in children.iter_present() {
            map.set(i);
        }

        map
    }

    #[cfg(test)]
    pub fn len(self) -> usize {
        self.0.iter().map(|b| b.count_ones() as usize).sum()
    }

    pub const fn get(self, index: PathComponent) -> bool {
        #![expect(clippy::indexing_slicing)]
        let i = index.as_usize();
        self.0[i / 8] & (1 << (i % 8)) != 0
    }

    pub const fn set(&mut self, index: PathComponent) {
        #![expect(clippy::indexing_slicing)]
        let i = index.as_usize();
        self.0[i / 8] |= 1 << (i % 8);
    }

    pub fn iter_indices(self) -> impl Iterator<Item = PathComponent> {
        PathComponent::ALL.into_iter().filter(move |&i| self.get(i))
    }
}

impl std::fmt::Display for ChildrenMap {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if f.alternate() {
            f.debug_list().entries(self.iter_indices()).finish()
        } else {
            write!(f, "{self:b}")
        }
    }
}

impl std::fmt::Binary for ChildrenMap {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{:016b}", u16::from_le_bytes(self.0))
    }
}

impl std::fmt::Debug for ChildrenMap {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(self, f)
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use super::*;

    use firewood_storage::{Children, PathComponent};
    use test_case::test_case;

    #[test_case(Children::new(), &[]; "empty")]
    #[test_case({
        let mut children = Children::new();
        children[PathComponent::ALL[0]] = Some(());
        children
    }, &[PathComponent::ALL[0]]; "first")]
    #[test_case({
        let mut children = Children::new();
        children[PathComponent::ALL[1]] = Some(());
        children
    }, &[PathComponent::ALL[1]]; "second")]
    #[test_case({
        let mut children = Children::new();
        children[PathComponent::ALL.last().copied().unwrap()] = Some(());
        children
    }, &[PathComponent::ALL.last().copied().unwrap()]; "last")]
    #[test_case({
        let mut children = Children::new();
        for (_, slot) in children.iter_mut().step_by(2) {
            *slot = Some(());
        }
        children
    }, &PathComponent::ALL.into_iter().step_by(2).collect::<Vec<_>>(); "evens")]
    #[test_case({
        let mut children = Children::new();
        for (_, slot) in children.iter_mut().skip(1).step_by(2) {
            *slot = Some(());
        }
        children
    }, &PathComponent::ALL.into_iter().skip(1).step_by(2).collect::<Vec<_>>(); "odds")]
    #[test_case(Children::from_fn(|_| Some(())), &PathComponent::ALL; "all")]
    fn test_children_map(children: Children<Option<()>>, indicies: &[PathComponent]) {
        let map = ChildrenMap::new(&children);
        assert_eq!(map.len(), indicies.len());

        assert!(
            indicies.iter().copied().eq(map.iter_indices()),
            "expected {:?}, got {:?}",
            indicies,
            map.iter_indices().collect::<Vec<_>>()
        );
    }
}
