// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

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

#[cfg(not(feature = "branch_factor_256"))]
impl std::fmt::Binary for ChildrenMap {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{:016b}", u16::from_le_bytes(self.0))
    }
}

#[cfg(feature = "branch_factor_256")]
impl std::fmt::Binary for ChildrenMap {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let [a, b] = bytemuck::cast::<_, [[u8; 16]; 2]>(self.0);
        let a = u128::from_le_bytes(a);
        let b = u128::from_le_bytes(b);
        write!(f, "{a:0128b}{b:0128b}")
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
