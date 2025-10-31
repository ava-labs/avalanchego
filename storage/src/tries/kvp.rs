// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(not(feature = "branch_factor_256"))]
use crate::PackedPathRef;
use crate::{
    Children, HashType, Hashable, HashableShunt, HashedTrieNode, JoinedPath, PathBuf,
    PathComponent, PathGuard, SplitPath, TrieNode, TriePath, TriePathFromPackedBytes, ValueDigest,
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

/// The root of a hashed key-value trie.
///
/// This is similar to [`KeyValueTrieRoot`], but includes the computed hash of
/// the node as well as its leading path components. Consequently, the hashed
/// trie is formed by hashing the un-hashed trie.
pub struct HashedKeyValueTrieRoot<'a, T: ?Sized> {
    computed: HashType,
    leading_path: PathBuf,
    partial_path: PackedPathRef<'a>,
    value: Option<&'a T>,
    children: Children<Option<Box<Self>>>,
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

impl<T: AsRef<[u8]> + ?Sized> std::fmt::Debug for HashedKeyValueTrieRoot<'_, T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("HashedKeyValueTrieRoot")
            .field("computed", &self.computed)
            .field("leading_path", &self.leading_path.display())
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

    /// Hashes this trie, returning a hashed trie.
    #[must_use]
    pub fn into_hashed_trie(self: Box<Self>) -> Box<HashedKeyValueTrieRoot<'a, T>> {
        HashedKeyValueTrieRoot::new(PathGuard::new(&mut PathBuf::new_const()), self)
    }
}

impl<'a, T: AsRef<[u8]> + ?Sized> HashedKeyValueTrieRoot<'a, T> {
    /// Constructs a new hashed key-value trie node from the given un-hashed
    /// node.
    #[must_use]
    pub fn new(
        mut leading_path: PathGuard<'_>,
        #[expect(clippy::boxed_local)] node: Box<KeyValueTrieRoot<'a, T>>,
    ) -> Box<Self> {
        let children = node
            .children
            .map(|pc, child| child.map(|child| Self::new(leading_path.fork_append(pc), child)));

        Box::new(Self {
            computed: HashableShunt::new(
                leading_path.as_slice(),
                node.partial_path,
                node.value.map(|v| ValueDigest::Value(v.as_ref())),
                children
                    .each_ref()
                    .map(|_, c| c.as_deref().map(|c| c.computed.clone())),
            )
            .to_hash(),
            leading_path: leading_path.as_slice().into(),
            partial_path: node.partial_path,
            value: node.value,
            children,
        })
    }
}

impl<T: AsRef<[u8]> + ?Sized> TrieNode<T> for KeyValueTrieRoot<'_, T> {
    type PartialPath<'a>
        = PackedPathRef<'a>
    where
        Self: 'a;

    fn partial_path(&self) -> Self::PartialPath<'_> {
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

    fn child_state(&self, pc: PathComponent) -> Option<super::TrieEdgeState<'_, Self>> {
        self.children[pc]
            .as_deref()
            .map(|node| super::TrieEdgeState::UnhashedChild { node })
    }
}

impl<T: AsRef<[u8]> + ?Sized> TrieNode<T> for HashedKeyValueTrieRoot<'_, T> {
    type PartialPath<'a>
        = PackedPathRef<'a>
    where
        Self: 'a;

    fn partial_path(&self) -> Self::PartialPath<'_> {
        self.partial_path
    }

    fn value(&self) -> Option<&T> {
        self.value
    }

    fn child_hash(&self, pc: PathComponent) -> Option<&HashType> {
        self.children[pc].as_deref().map(|c| &c.computed)
    }

    fn child_node(&self, pc: PathComponent) -> Option<&Self> {
        self.children[pc].as_deref()
    }

    fn child_state(&self, pc: PathComponent) -> Option<super::TrieEdgeState<'_, Self>> {
        self.children[pc]
            .as_deref()
            .map(|node| super::TrieEdgeState::from_node(node, Some(&node.computed)))
    }
}

impl<T: AsRef<[u8]> + ?Sized> HashedTrieNode<T> for HashedKeyValueTrieRoot<'_, T> {
    fn computed(&self) -> &HashType {
        &self.computed
    }
}

impl<'a, T: AsRef<[u8]> + ?Sized> Hashable for HashedKeyValueTrieRoot<'a, T> {
    type LeadingPath<'b>
        = &'b [PathComponent]
    where
        Self: 'b;

    type PartialPath<'b>
        = PackedPathRef<'a>
    where
        Self: 'b;

    type FullPath<'b>
        = JoinedPath<&'b [PathComponent], PackedPathRef<'a>>
    where
        Self: 'b;

    fn parent_prefix_path(&self) -> Self::LeadingPath<'_> {
        &self.leading_path
    }

    fn partial_path(&self) -> Self::PartialPath<'_> {
        self.partial_path
    }

    fn full_path(&self) -> Self::FullPath<'_> {
        self.parent_prefix_path().append(self.partial_path)
    }

    fn value_digest(&self) -> Option<ValueDigest<&[u8]>> {
        self.value.map(|v| ValueDigest::Value(v.as_ref()))
    }

    fn children(&self) -> Children<Option<HashType>> {
        self.children
            .each_ref()
            .map(|_, c| c.as_deref().map(|c| c.computed.clone()))
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

    /// In constant context, convert an ASCII hex string to a byte array.
    ///
    /// `FROM` must be exactly twice `TO`. This is workaround because generic
    /// parameters cannot be used in constant expressions yet (see [upstream]).
    ///
    /// The [`expected_hash`] macro is able to work around this limitation by
    /// evaluating the length at macro expansion time which provides a constant
    /// value for both generic parameters at compile time.
    ///
    /// # Panics
    ///
    /// Panics if the input is not valid hex. Because this panic occurs in constant
    /// context, this will result in an "erroneous constant error" during compilation.
    ///
    /// [upstream]: https://github.com/rust-lang/rust/issues/76560
    const fn from_ascii<const FROM: usize, const TO: usize>(hex: &[u8; FROM]) -> [u8; TO] {
        #![expect(clippy::arithmetic_side_effects, clippy::indexing_slicing)]

        const fn from_hex_char(c: u8) -> u8 {
            match c {
                b'0'..=b'9' => c - b'0',
                b'a'..=b'f' => c - b'a' + 10,
                b'A'..=b'F' => c - b'A' + 10,
                _ => panic!("invalid hex character"),
            }
        }

        const {
            assert!(FROM == TO.wrapping_mul(2));
        }

        let mut bytes = [0u8; TO];
        let mut i = 0_usize;
        while i < TO {
            let off = i.wrapping_mul(2);
            let hi = hex[off];
            let off = off.wrapping_add(1);
            let lo = hex[off];
            bytes[i] = (from_hex_char(hi) << 4) | from_hex_char(lo);
            i += 1;
        }

        bytes
    }

    /// A macro to select the expected hash based on the enabled cargo features.
    ///
    /// For both merkledb hash types, only a 64-character hex string (32 bytes)
    /// is expected. For the ethereum hash type, either a 64-character hex string
    /// or an RLP-encoded byte string of arbitrary length can be provided, if
    /// wrapped in `rlp(...)`.
    ///
    /// # Example
    ///
    /// ```ignore
    /// expected_hash!{
    ///     merkledb16: b"749390713e51d3e4e50ba492a669c1644a6d9cb7e48b2a14d556e7f953da92fc",
    ///     merkledb256: b"30dbf15b59c97d2997f4fbed1ae86d1eab8e7aa2dd84337029fe898f47aeb8e6",
    ///     ethereum: b"2e636399fae96dc07abaf21167a34b8a5514d6594e777635987e319c76f28a75",
    /// }
    /// ```
    ///
    /// or with RLP:
    ///
    /// ```ignore
    /// expected_hash!{
    ///     merkledb16: b"1ffe11ce995a9c07021d6f8a8c5b1817e6375dd0ea27296b91a8d48db2858bc9",
    ///     merkledb256: b"831a115e52af616bd2df8cd7a0993e21e544d7d201e151a7f61dcdd1a6bd557c",
    ///     ethereum: rlp(b"c482206131"),
    /// }
    /// ```
    macro_rules! expected_hash {
        (
            merkledb16: $hex16:expr,
            merkledb256: $hex256:expr,
            ethereum: rlp($hexeth:expr),
        ) => {
            match () {
                #[cfg(all(not(feature = "branch_factor_256"), not(feature = "ethhash")))]
                () => $crate::HashType::from(from_ascii($hex16)),
                #[cfg(all(feature = "branch_factor_256", not(feature = "ethhash")))]
                () => $crate::HashType::from(from_ascii($hex256)),
                #[cfg(all(not(feature = "branch_factor_256"), feature = "ethhash"))]
                () => $crate::HashType::Rlp(smallvec::SmallVec::from(
                    &from_ascii::<{ $hexeth.len() }, { $hexeth.len() / 2 }>($hexeth)[..],
                )),
                #[cfg(all(feature = "branch_factor_256", feature = "ethhash"))]
                () => compile_error!("branch_factor_256 and ethhash cannot both be enabled"),
            }
        };
        (
            merkledb16: $hex16:expr,
            merkledb256: $hex256:expr,
            ethereum: $hexeth:expr,
        ) => {
            $crate::HashType::from(from_ascii(match () {
                #[cfg(all(not(feature = "branch_factor_256"), not(feature = "ethhash")))]
                () => $hex16,
                #[cfg(all(feature = "branch_factor_256", not(feature = "ethhash")))]
                () => $hex256,
                #[cfg(all(not(feature = "branch_factor_256"), feature = "ethhash"))]
                () => $hexeth,
                #[cfg(all(feature = "branch_factor_256", feature = "ethhash"))]
                () => compile_error!("branch_factor_256 and ethhash cannot both be enabled"),
            }))
        };
    }

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

    #[test_case(&[("a", "1")], expected_hash!{
        merkledb16: b"1ffe11ce995a9c07021d6f8a8c5b1817e6375dd0ea27296b91a8d48db2858bc9",
        merkledb256: b"831a115e52af616bd2df8cd7a0993e21e544d7d201e151a7f61dcdd1a6bd557c",
        ethereum: rlp(b"c482206131"),
    }; "single key")]
    #[test_case(&[("a", "1"), ("b", "2")], expected_hash!{
        merkledb16: b"ff783ce73f7a5fa641991d76d626eefd7840a839590db4269e1e92359ae60593",
        merkledb256: b"301e9035ef0fe1b50788f9b5bca3a2c19bce9c798bdb1dda09fc71dd22564ce4",
        ethereum: rlp(b"d81696d580c22031c220328080808080808080808080808080"),
    }; "two disjoint keys")]
    #[test_case(&[("a", "1"), ("ab", "2")], expected_hash!{
        merkledb16: b"c5def8c64a2f3b8647283251732b68a2fb185f8bf92c0103f31d5ec69bb9a90c",
        merkledb256: b"2453f6f0b38fd36bcb66b145aff0f7ae3a6b96121fa1187d13afcffa7641b156",
        ethereum: rlp(b"d882006194d3808080808080c2323280808080808080808031"),
    }; "two nested keys")]
    #[test_case(&[("a", "1"), ("b", "2"), ("c", "3")], expected_hash!{
        merkledb16: b"95618fd79a0ca2d7612bf9fd60663b81f632c9a65e76bb5bc3ed5f3045cf1404",
        merkledb256: b"f5c185a96ed86da8da052a52f6c2e7368c90d342c272dd0e6c9e72c0071cdb0c",
        ethereum: rlp(b"da1698d780c22031c22032c2203380808080808080808080808080"),
    }; "three disjoint keys")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3")], expected_hash!{
        merkledb16: b"ee8a7a1409935f58ab6ce40a1e05ee2a587bdc06c201dbec7006ee1192e71f70",
        merkledb256: b"40c9cee60ac59e7926109137fbaa5d68642d4770863b150f98bd8ac00aedbff3",
        ethereum: b"6ffab67bf7096a9608b312b9b2459c17ec9429286b283a3b3cdaa64860182699",
    }; "two children of same parent")]
    #[test_case(&[("a", "1"), ("b", "2"), ("ba", "3")], expected_hash!{
        merkledb16: b"d3efab83a1a4dd193c8ae51dfe638bba3494d8b1917e7a9185d20301ff1c528b",
        merkledb256: b"e6f711e762064ffcc7276e9c6149fc8f1050e009a21e436e7b78a4a60079e3ba",
        ethereum: b"21a118e1765c556e505a8752a0fd5bbb4ea78fb21077f8488d42862ebabf0130",
    }; "nested sibling")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("abc", "3")], expected_hash!{
        merkledb16: b"af11454e2f920fb49041c9890c318455952d651b7d835f5731218dbc4bde4805",
        merkledb256: b"5dc43e88b3019050e741be52ed4afff621e1ac93cd2c68d37f82947d1d16cff5",
        ethereum: b"eabecb5e4efb9b5824cd926fac6350bdcb4a599508b16538afde303d72571169",
    }; "linear nested keys")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4")], expected_hash!{
        merkledb16: b"749390713e51d3e4e50ba492a669c1644a6d9cb7e48b2a14d556e7f953da92fc",
        merkledb256: b"30dbf15b59c97d2997f4fbed1ae86d1eab8e7aa2dd84337029fe898f47aeb8e6",
        ethereum: b"2e636399fae96dc07abaf21167a34b8a5514d6594e777635987e319c76f28a75",
    }; "four keys")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5")], expected_hash!{
        merkledb16: b"1c043978de0cd65fe2e75a74eaa98878b753f4ec20f6fbbb7232a39f02e88c6f",
        merkledb256: b"02bb75b5d5b81ba4c64464a5e39547de4e0d858c04da4a4aae9e63fc8385279d",
        ethereum: b"df930bafb34edb6d758eb5f4dd9461fc259c8c13abf38da8a0f63f289e107ecd",
    }; "five keys")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5"), ("bb", "6")], expected_hash!{
        merkledb16: b"c2c13c095f7f07ce9ef92401f73951b4846a19e2b092b8a527fe96fa82f55cfd",
        merkledb256: b"56d69386ad494d6be42bbdd78b3ad00c07c12e631338767efa1539d6720ce7a6",
        ethereum: b"8ca7c3b09aa0a8877122d67fd795051bd1e6ff169932e3b7a1158ed3d66fbedf",
    }; "six keys")]
    #[test_case(&[("a", "1"), ("ab", "2"), ("ac", "3"), ("b", "4"), ("ba", "5"), ("bb", "6"), ("c", "7")], expected_hash!{
        merkledb16: b"697e767d6f4af8236090bc95131220c1c94cadba3e66e0a8011c9beef7b255a5",
        merkledb256: b"2f083246b86da1e6e135f771ae712f271c1162c23ebfaa16178ea57f0317bf06",
        ethereum: b"3fa832b90f7f1a053a48a4528d1e446cc679fbcf376d0ef8703748d64030e19d",
    }; "seven keys")]
    fn test_hashed_trie(slice: &[(&str, &str)], root_hash: crate::HashType) {
        let root = KeyValueTrieRoot::<str>::from_slice(slice)
            .unwrap()
            .unwrap()
            .into_hashed_trie();

        assert_eq!(*root.computed(), root_hash);
        assert_eq!(*root.computed(), crate::Preimage::to_hash(&*root));
    }
}
