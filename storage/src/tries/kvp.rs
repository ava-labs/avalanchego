// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(not(feature = "branch_factor_256"))]
use crate::PackedPathRef;
use crate::{
    Children, HashType, PathBuf, PathComponent, PathGuard, SplitPath, TrieNode, TriePath,
    TriePathFromPackedBytes,
};

#[cfg(feature = "branch_factor_256")]
type PackedPathRef<'a> = &'a [PathComponent];

/// A duplicate key error when merging two key-value tries.
#[non_exhaustive]
#[derive(Debug, Clone, PartialEq, Eq, Hash, thiserror::Error)]
#[error("duplicate keys found at path {}", path.display())]
pub struct DuplicateKeyError {
    /// The path at which the duplicate keys were found.
    pub path: PathBuf,
}

/// The root of a key-value trie.
///
/// The trie maps byte string keys to values of type `T`.
///
/// The trie is a compressed prefix tree, where each node contains:
/// - A partial path (a sequence of path components) representing the path from
///   its parent to itself.
/// - An optional value of type `T` if the path to this node corresponds to a
///   key in the trie.
/// - A set of children nodes, each corresponding to a different path component
///   extending the current node's path.
pub struct KeyValueTrieRoot<'a, T: ?Sized> {
    /// The partial path from this node's parent to itself.
    pub partial_path: PackedPathRef<'a>,
    /// The value associated with the path to this node, if any.
    ///
    /// If [`None`], this node does not correspond to a key in the trie and is
    /// expected to have two or more children. If [`Some`], this node may have
    /// zero or more children.
    pub value: Option<&'a T>,
    /// The children of this node, indexed by their leading path component.
    pub children: Children<Option<Box<Self>>>,
}

impl<T: AsRef<[u8]> + ?Sized> std::fmt::Debug for KeyValueTrieRoot<'_, T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("KeyValueTrieRoot")
            .field("partial_path", &self.partial_path.display())
            .field("value", &DebugValue::new(self.value))
            .field("children", &DebugChildren::new(&self.children))
            .finish()
    }
}

impl<'a, T: AsRef<[u8]> + ?Sized> KeyValueTrieRoot<'a, T> {
    /// Constructs a new leaf node with the given path and value.
    #[must_use]
    pub fn new_leaf(path: &'a [u8], value: &'a T) -> Box<Self> {
        Box::new(Self {
            partial_path: PackedPathRef::path_from_packed_bytes(path),
            value: Some(value),
            children: Children::new(),
        })
    }

    /// Constructs a new key-value trie from the given slice of key-value pairs.
    ///
    /// For efficiency, the slice should be sorted by key; however, this is not
    /// explicitly required.
    ///
    /// # Errors
    ///
    /// If duplicate keys are found, a [`DuplicateKeyError`] is returned.
    pub fn from_slice<K, V>(slice: &'a [(K, V)]) -> Result<Option<Box<Self>>, DuplicateKeyError>
    where
        K: AsRef<[u8]>,
        V: AsRef<T>,
    {
        match slice {
            [] => Ok(None),
            [(k, v)] => Ok(Some(Self::new_leaf(k.as_ref(), v.as_ref()))),
            _ => {
                let mid = slice.len() / 2;
                let (lhs, rhs) = slice.split_at(mid);
                let lhs = Self::from_slice(lhs)?;
                let rhs = Self::from_slice(rhs)?;
                Self::merge_root(PathGuard::new(&mut PathBuf::new_const()), lhs, rhs)
            }
        }
    }

    /// Constructs a new internal node with the given two children.
    ///
    /// The two children must have different leading path components.
    #[must_use]
    fn new_siblings(
        path: PackedPathRef<'a>,
        lhs_path: PathComponent,
        lhs: Box<Self>,
        rhs_path: PathComponent,
        rhs: Box<Self>,
    ) -> Box<Self> {
        debug_assert_ne!(lhs_path, rhs_path);
        let mut children = Children::new();
        children[lhs_path] = Some(lhs);
        children[rhs_path] = Some(rhs);
        Box::new(Self {
            partial_path: path,
            value: None,
            children,
        })
    }

    /// Returns a new node with the same contents as `self` but with its path
    /// replaced by the given path.
    #[must_use]
    fn with_path(mut self: Box<Self>, path: PackedPathRef<'a>) -> Box<Self> {
        self.partial_path = path;
        self
    }

    /// Deeply merges two key-value tries, returning the merged trie.
    fn merge_root(
        leading_path: PathGuard<'_>,
        lhs: Option<Box<Self>>,
        rhs: Option<Box<Self>>,
    ) -> Result<Option<Box<Self>>, DuplicateKeyError> {
        match (lhs, rhs) {
            (Some(l), Some(r)) => Self::merge(leading_path, l, r).map(Some),
            (Some(node), None) | (None, Some(node)) => Ok(Some(node)),
            (None, None) => Ok(None),
        }
    }

    /// Merges two disjoint tries, returning the merged trie.
    fn merge(
        leading_path: PathGuard<'_>,
        lhs: Box<Self>,
        rhs: Box<Self>,
    ) -> Result<Box<Self>, DuplicateKeyError> {
        match lhs
            .partial_path
            .longest_common_prefix(rhs.partial_path)
            .split_first_parts()
        {
            (None, None, _) => {
                // both paths are identical, perform a deep merge of each child slot
                Self::deep_merge(leading_path, lhs, rhs)
            }
            (Some((l, l_rest)), Some((r, r_rest)), path) => {
                // the paths diverge at this component, create a new parent node
                // with the two nodes as children
                Ok(Self::new_siblings(
                    path,
                    l,
                    lhs.with_path(l_rest),
                    r,
                    rhs.with_path(r_rest),
                ))
            }
            (Some((l, l_rest)), None, _) => {
                // rhs is a prefix of lhs, so rhs becomes the parent of lhs
                rhs.merge_child(leading_path, l, lhs.with_path(l_rest))
            }
            (None, Some((r, r_rest)), _) => {
                // lhs is a prefix of rhs, so lhs becomes the parent of rhs
                lhs.merge_child(leading_path, r, rhs.with_path(r_rest))
            }
        }
    }

    /// Deeply merges two kvp nodes that have identical paths.
    fn deep_merge(
        mut leading_path: PathGuard<'_>,
        mut lhs: Box<Self>,
        #[expect(clippy::boxed_local)] mut rhs: Box<Self>,
    ) -> Result<Box<Self>, DuplicateKeyError> {
        leading_path.extend(lhs.partial_path.components());

        lhs.value = match (lhs.value.take(), rhs.value.take()) {
            (Some(v), None) | (None, Some(v)) => Some(v),
            (Some(_), Some(_)) => {
                return Err(DuplicateKeyError {
                    path: leading_path.as_slice().into(),
                });
            }
            (None, None) => None,
        };

        lhs.children = lhs.children.merge(rhs.children, |pc, lhs, rhs| {
            Self::merge_root(leading_path.fork_append(pc), lhs, rhs)
        })?;

        Ok(lhs)
    }

    /// Merges the given child into this node at the given prefix.
    ///
    /// If there is already a child at the given prefix, the two children
    /// are recursively merged.
    fn merge_child(
        mut self: Box<Self>,
        mut leading_path: PathGuard<'_>,
        prefix: PathComponent,
        child: Box<Self>,
    ) -> Result<Box<Self>, DuplicateKeyError> {
        leading_path.extend(self.partial_path.components());
        leading_path.push(prefix);

        self.children[prefix] = Some(match self.children.take(prefix) {
            Some(existing) => Self::merge(leading_path, existing, child)?,
            None => child,
        });

        Ok(self)
    }
}

impl<T: AsRef<[u8]> + ?Sized> TrieNode<T> for KeyValueTrieRoot<'_, T> {
    fn partial_path(&self) -> impl SplitPath + '_ {
        self.partial_path
    }

    fn value(&self) -> Option<&T> {
        self.value
    }

    fn child_hash(&self, pc: PathComponent) -> Option<&HashType> {
        let _ = pc;
        None
    }

    fn child_node(&self, pc: PathComponent) -> Option<&Self> {
        self.children[pc].as_deref()
    }
}

struct DebugValue<'a, T: ?Sized> {
    value: Option<&'a T>,
}

impl<'a, T: AsRef<[u8]> + ?Sized> DebugValue<'a, T> {
    const fn new(value: Option<&'a T>) -> Self {
        Self { value }
    }
}

impl<T: AsRef<[u8]> + ?Sized> std::fmt::Debug for DebugValue<'_, T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        #![expect(clippy::indexing_slicing)]

        const MAX_BYTES: usize = 32;

        let Some(value) = self.value else {
            return write!(f, "None");
        };

        let value = value.as_ref();
        let truncated = &value[..value.len().min(MAX_BYTES)];

        let mut hex_buf = [0u8; MAX_BYTES * 2];
        let hex_buf = &mut hex_buf[..truncated.len().wrapping_mul(2)];
        hex::encode_to_slice(truncated, hex_buf).expect("exact fit");
        let s = str::from_utf8(hex_buf).expect("valid hex");

        if truncated.len() < value.len() {
            write!(f, "0x{s}... (len {})", value.len())
        } else {
            write!(f, "0x{s}")
        }
    }
}

struct DebugChildren<'a, T> {
    children: &'a Children<Option<T>>,
}

impl<'a, T: std::fmt::Debug> DebugChildren<'a, T> {
    const fn new(children: &'a Children<Option<T>>) -> Self {
        Self { children }
    }
}

impl<T: std::fmt::Debug> std::fmt::Debug for DebugChildren<'_, T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        if f.alternate() {
            // if alternate, debug children as-is (which is pretty and recursive)
            self.children.fmt(f)
        } else {
            // otherwise, replace each child with a continuation marker
            self.children
                .each_ref()
                .map(
                    |_, child| {
                        if child.is_some() { "Some(...)" } else { "None" }
                    },
                )
                .fmt(f)
        }
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use test_case::test_case;

    use super::*;

    #[test_case(&[])]
    #[test_case(&[("a", "1")])]
    #[test_case(&[("a", "1"), ("b", "2")])]
    #[test_case(&[("a", "1"), ("ab", "2")])]
    #[test_case(&[("a", "1"), ("b", "2"), ("c", "3")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3")])]
    #[test_case(&[("a", "1"), ("b", "2"), ("ba", "3")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("abc", "3")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5"), ("bb", "6")])]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5"), ("bb", "6"), ("c", "7")])]
    fn test_trie_from_slice(slice: &[(&str, &str)]) {
        let root = KeyValueTrieRoot::<str>::from_slice(slice).unwrap();
        if slice.is_empty() {
            if let Some(root) = root {
                panic!("expected None, got {root:#?}");
            }
        } else {
            let root = root.unwrap();
            eprintln!("trie: {root:#?}");
            for ((kvp_path, kvp_value), &(slice_key, slice_value)) in root
                .iter_values()
                .zip(slice)
                .chain(root.iter_values_desc().zip(slice.iter().rev()))
            {
                let slice_key = PackedPathRef::path_from_packed_bytes(slice_key.as_bytes());
                assert!(
                    kvp_path.path_eq(&slice_key),
                    "expected path {} got {}",
                    slice_key.display(),
                    kvp_path.display(),
                );
                assert_eq!(kvp_value, slice_value);
            }
        }
    }

    #[test]
    fn test_trie_from_slice_duplicate_keys() {
        let slice = [("a", "1"), ("ab", "2"), ("a", "3")];
        let err = KeyValueTrieRoot::<str>::from_slice(&slice).unwrap_err();
        assert_eq!(
            err,
            DuplicateKeyError {
                path: PathBuf::path_from_packed_bytes(b"a")
            }
        );
    }

    #[test]
    fn test_trie_from_unsorted_slice() {
        let slice = [("b", "2"), ("a", "1"), ("ab", "3")];
        let expected = [("a", "1"), ("ab", "3"), ("b", "2")];
        let root = KeyValueTrieRoot::<str>::from_slice(&slice)
            .unwrap()
            .unwrap();
        eprintln!("trie: {root:#?}");

        for ((kvp_path, kvp_value), &(slice_key, slice_value)) in root
            .iter_values()
            .zip(&expected)
            .chain(root.iter_values_desc().zip(expected.iter().rev()))
        {
            let slice_key = PackedPathRef::path_from_packed_bytes(slice_key.as_bytes());
            assert!(
                kvp_path.path_eq(&slice_key),
                "expected path {} got {}",
                slice_key.display(),
                kvp_path.display(),
            );
            assert_eq!(kvp_value, slice_value);
        }
    }
}
