// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{HashType, PathBuf, PathComponent, TrieNode, TriePath, tries::TrieEdgeState};

/// A marker type for [`TrieEdgeIter`] that indicates that the iterator traverses
/// the trie in ascending order.
#[derive(Debug)]
pub struct IterAscending;

/// A marker type for [`TrieEdgeIter`] that indicates that the iterator traverses
/// the trie in descending order.
#[derive(Debug)]
pub struct IterDescending;

/// An iterator over the edges in a key-value trie in a specified order.
///
/// Use [`TrieNode::iter_edges`] or [`TrieNode::iter_edges_desc`] to
/// create an instance of this iterator in ascending or descending order,
/// respectively.
#[derive(Debug)]
#[must_use = "iterators are lazy and do nothing unless consumed"]
pub struct TrieEdgeIter<'a, N: ?Sized, V: ?Sized, D> {
    leading_path: PathBuf,
    stack: Vec<Frame<'a, N, V>>,
    marker: std::marker::PhantomData<D>,
}

/// An iterator over the key-value pairs in a key-value trie.
#[derive(Debug)]
#[must_use = "iterators are lazy and do nothing unless consumed"]
pub struct TrieValueIter<'a, N: ?Sized, V: ?Sized, D> {
    edges: TrieEdgeIter<'a, N, V, D>,
}

#[derive(Debug)]
struct Frame<'a, N: ?Sized, V: ?Sized> {
    node: &'a N,
    hash: Option<&'a HashType>,
    leading_path_len: usize,
    children: Option<std::array::IntoIter<PathComponent, { PathComponent::LEN }>>,
    marker: std::marker::PhantomData<V>,
}

impl<'a, N, V, D> TrieEdgeIter<'a, N, V, D>
where
    N: TrieNode<V> + ?Sized,
    V: AsRef<[u8]> + ?Sized,
{
    /// Creates a new iterator over the given key-value trie.
    pub fn new(root: &'a N, root_hash: Option<&'a HashType>) -> Self {
        let mut this = Self {
            leading_path: PathBuf::new_const(),
            stack: Vec::new(),
            marker: std::marker::PhantomData,
        };
        this.push_frame(None, root, root_hash);
        this
    }

    /// Transforms this iterator into an iterator over the key-value pairs in
    /// the trie.
    pub const fn node_values(self) -> TrieValueIter<'a, N, V, D> {
        TrieValueIter { edges: self }
    }

    fn push_frame(
        &mut self,
        leading_component: Option<PathComponent>,
        node: &'a N,
        hash: Option<&'a HashType>,
    ) {
        let frame = Frame {
            node,
            hash,
            leading_path_len: self.leading_path.len(),
            children: None,
            marker: std::marker::PhantomData,
        };
        self.stack.push(frame);
        self.leading_path.extend(leading_component);
        self.leading_path.extend(node.partial_path().components());
    }
}

/// Both iterators share this logic to descend into a node's children.
///
/// The passed in `children_iter` should be an iterator over the indices into
/// the children array in the desired order (e.g. ascending or descending).
macro_rules! descend {
    (
        $self:expr,
        $node:expr,
        $children_iter:expr
    ) => {
        if let Some((pc, state)) =
            $children_iter.find_map(|pc| $node.child_state(pc).map(|state| (pc, state)))
        {
            match state {
                TrieEdgeState::LocalChild { node, hash } => {
                    $self.push_frame(Some(pc), node, Some(hash));
                }
                TrieEdgeState::RemoteChild { hash } => {
                    let mut path = $self.leading_path.clone();
                    path.push(pc);
                    return Some((path, TrieEdgeState::RemoteChild { hash }));
                }
                TrieEdgeState::UnhashedChild { node } => {
                    $self.push_frame(Some(pc), node, None);
                }
            }

            continue;
        }
    };
}

impl<'a, N, V> Iterator for TrieEdgeIter<'a, N, V, IterAscending>
where
    N: TrieNode<V> + ?Sized,
    V: AsRef<[u8]> + ?Sized,
{
    type Item = (PathBuf, TrieEdgeState<'a, N>);

    fn next(&mut self) -> Option<Self::Item> {
        while let Some(&mut Frame {
            node,
            hash,
            leading_path_len,
            ref mut children,
            marker: _,
        }) = self.stack.last_mut()
        {
            // ascending iterator yields the node before iterating its children
            let mut do_yield = false;

            let children = children.get_or_insert_with(|| {
                do_yield = true;
                PathComponent::ALL.into_iter()
            });

            if do_yield {
                return Some((
                    self.leading_path.clone(),
                    TrieEdgeState::from_node(node, hash),
                ));
            }

            descend!(self, node, children);

            // we've exhausted this node's children, so pop its frame
            self.stack.pop();
            self.leading_path.truncate(leading_path_len);
        }

        None
    }
}

impl<'a, N, V> Iterator for TrieEdgeIter<'a, N, V, IterDescending>
where
    N: TrieNode<V> + ?Sized,
    V: AsRef<[u8]> + ?Sized,
{
    type Item = (PathBuf, TrieEdgeState<'a, N>);

    fn next(&mut self) -> Option<Self::Item> {
        while let Some(&mut Frame {
            node,
            hash,
            leading_path_len,
            ref mut children,
            marker: _,
        }) = self.stack.last_mut()
        {
            // descending iterator yields the node after iterating its children
            let children = children.get_or_insert_with(|| PathComponent::ALL.into_iter());

            descend!(self, node, children.rev());

            // clone the path before we pop the frame
            let leading_path = self.leading_path.clone();

            // we've exhausted this node's children, so pop its frame and yield the node
            self.stack.pop();
            self.leading_path.truncate(leading_path_len);

            return Some((leading_path, TrieEdgeState::from_node(node, hash)));
        }

        None
    }
}

impl<'a, N, V> Iterator for TrieValueIter<'a, N, V, IterAscending>
where
    N: TrieNode<V> + ?Sized,
    V: AsRef<[u8]> + ?Sized + 'a,
{
    type Item = (PathBuf, &'a V);

    fn next(&mut self) -> Option<Self::Item> {
        self.edges
            .find_map(|(path, node)| node.value().map(|v| (path, v)))
    }
}

impl<'a, N, V> Iterator for TrieValueIter<'a, N, V, IterDescending>
where
    N: TrieNode<V> + ?Sized,
    V: AsRef<[u8]> + ?Sized + 'a,
{
    type Item = (PathBuf, &'a V);

    fn next(&mut self) -> Option<Self::Item> {
        self.edges
            .find_map(|(path, node)| node.value().map(|v| (path, v)))
    }
}

// auto-derived implementations would require N: Clone, V: Clone which is too much

impl<N: ?Sized, V: ?Sized, D> Clone for TrieEdgeIter<'_, N, V, D> {
    fn clone(&self) -> Self {
        Self {
            leading_path: self.leading_path.clone(),
            stack: self.stack.clone(),
            marker: std::marker::PhantomData,
        }
    }

    fn clone_from(&mut self, source: &Self) {
        self.leading_path.clone_from(&source.leading_path);
        self.stack.clone_from(&source.stack);
    }
}

impl<N: ?Sized, V: ?Sized, D> Clone for TrieValueIter<'_, N, V, D> {
    fn clone(&self) -> Self {
        Self {
            edges: self.edges.clone(),
        }
    }

    fn clone_from(&mut self, source: &Self) {
        self.edges.clone_from(&source.edges);
    }
}

impl<N: ?Sized, V: ?Sized> Clone for Frame<'_, N, V> {
    fn clone(&self) -> Self {
        Self {
            node: self.node,
            hash: self.hash,
            leading_path_len: self.leading_path_len,
            children: self.children.clone(),
            marker: std::marker::PhantomData,
        }
    }

    fn clone_from(&mut self, source: &Self) {
        self.node = source.node;
        self.hash = source.hash;
        self.leading_path_len = source.leading_path_len;
        self.children.clone_from(&source.children);
    }
}
