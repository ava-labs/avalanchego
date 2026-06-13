// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{Children, HashType, Hashable, JoinedPath, SplitPath, ValueDigest};

/// A shunt for a hasheable trie that we can use to compute the hash of a node
/// using component parts.
pub struct HashableShunt<'a, P1, P2> {
    parent_prefix: P1,
    partial_path: P2,
    value: Option<ValueDigest<&'a [u8]>>,
    child_hashes: Children<Option<HashType>>,
}

impl<'a, P1: SplitPath, P2: SplitPath> HashableShunt<'a, P1, P2> {
    /// Creates a new [`HashableShunt`].
    #[must_use]
    pub const fn new(
        parent_prefix: P1,
        partial_path: P2,
        value: Option<ValueDigest<&'a [u8]>>,
        child_hashes: Children<Option<HashType>>,
    ) -> Self {
        Self {
            parent_prefix,
            partial_path,
            value,
            child_hashes,
        }
    }

    /// Calculates the hash of this shunt.
    pub fn to_hash(&self) -> HashType {
        crate::Preimage::to_hash(self)
    }
}

impl<P1: SplitPath, P2: SplitPath> std::fmt::Debug for HashableShunt<'_, P1, P2> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("HashableShunt")
            .field("parent_prefix", &self.parent_prefix.display())
            .field("partial_path", &self.partial_path.display())
            .field(
                "value",
                &self.value.as_ref().map(|v| v.as_ref().map(hex::encode)),
            )
            .field("child_hashes", &self.child_hashes)
            .field("hash", &self.to_hash())
            .finish()
    }
}

impl<P1: SplitPath, P2: SplitPath> Hashable for HashableShunt<'_, P1, P2> {
    type LeadingPath<'a>
        = P1
    where
        Self: 'a;

    type PartialPath<'a>
        = P2
    where
        Self: 'a;

    type FullPath<'a>
        = JoinedPath<P1, P2>
    where
        Self: 'a;

    fn parent_prefix_path(&self) -> Self::LeadingPath<'_> {
        self.parent_prefix
    }

    fn partial_path(&self) -> Self::PartialPath<'_> {
        self.partial_path
    }

    fn full_path(&self) -> Self::FullPath<'_> {
        self.parent_prefix_path().append(self.partial_path())
    }

    fn value_digest(&self) -> Option<ValueDigest<&[u8]>> {
        self.value.clone()
    }

    fn children(&self) -> Children<Option<HashType>> {
        self.child_hashes.clone()
    }
}
