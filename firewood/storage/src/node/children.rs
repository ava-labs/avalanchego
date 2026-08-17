// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use arity_arrays::{Arity16, FixedArray, PackedArray};

use crate::PathComponent;

const MAX_CHILDREN: usize = PathComponent::LEN;

/// Type alias for an iterator over the slots of a branch node's children
/// with its corresponding [`PathComponent`].
pub type ChildrenSlots<T> = std::iter::Zip<
    std::array::IntoIter<PathComponent, MAX_CHILDREN>,
    std::array::IntoIter<T, MAX_CHILDREN>,
>;

/// The type of iterator returned by [`Children::iter_present`].
pub type IterPresentRef<'a, T> = std::iter::FilterMap<
    ChildrenSlots<&'a Option<T>>,
    fn((PathComponent, &'a Option<T>)) -> Option<(PathComponent, &'a T)>,
>;

/// The type of iterator returned by [`Children::iter_present`].
pub type IterPresentMut<'a, T> = std::iter::FilterMap<
    ChildrenSlots<&'a mut Option<T>>,
    fn((PathComponent, &'a mut Option<T>)) -> Option<(PathComponent, &'a mut T)>,
>;

/// Type alias for a collection of children in a branch node.
///
/// Backed by [`arity_arrays::FixedArray`], indexed by [`PathComponent`]. Unlike
/// the previous inline-array representation this type is not `Copy` (the backing
/// array is heap-free but move-only); clone explicitly where a copy is needed.
#[derive(Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Children<T>(FixedArray<T, Arity16>);

impl<T: std::fmt::Debug> std::fmt::Debug for Children<T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_map()
            .entries(self.iter().map(|(pc, child)| (pc.as_u8(), child)))
            .finish()
    }
}

impl<T> Children<T> {
    /// Creates a new [`Children`] by calling `f` for each possible
    /// [`PathComponent`].
    #[must_use]
    pub fn from_fn(mut f: impl FnMut(PathComponent) -> T) -> Self {
        Self(FixedArray::from_fn(|index| f(PathComponent(index))))
    }

    /// Borrows each element and returns a [`Children`] wrapping the references.
    #[must_use]
    pub fn each_ref(&self) -> Children<&T> {
        Children(FixedArray::from_fn(|index| self.0.get(index)))
    }

    /// Borrows each element mutably and returns a [`Children`] wrapping the
    /// mutable references.
    #[must_use]
    pub fn each_mut(&mut self) -> Children<&mut T> {
        // `iter_mut` (via `Deref<[T]>`) hands out the 16 disjoint `&mut T` in
        // ascending slot order; `FixedArray::from_fn` fills slot i with the i-th,
        // so slot i ends up borrowing element i.
        let mut slots = self.0.iter_mut();
        Children(FixedArray::from_fn(|_index| {
            slots
                .next()
                .expect("FixedArray::iter_mut yields exactly MAX_CHILDREN items")
        }))
    }

    /// Returns a reference to the element at `index`.
    ///
    /// This is infallible because `index` is guaranteed to be in-bounds.
    #[must_use]
    pub fn get(&self, index: PathComponent) -> &T {
        self.0.get(index.0)
    }

    /// Returns a mutable reference to the element at `index`.
    ///
    /// This is infallible because `index` is guaranteed to be in-bounds.
    #[must_use]
    pub fn get_mut(&mut self, index: PathComponent) -> &mut T {
        self.0.get_mut(index.0)
    }

    /// Replaces the element at `index` with `value`, returning the previous
    /// value.
    ///
    /// This is infallible because `index` is guaranteed to be in-bounds.
    pub fn replace(&mut self, index: PathComponent, value: T) -> T {
        self.0.replace(index.0, value)
    }

    /// Maps each element to another value using `f`, returning a new
    /// [`Children`] containing the results.
    #[must_use]
    pub fn map<O>(self, mut f: impl FnMut(PathComponent, T) -> O) -> Children<O> {
        Children(self.0.map(|index, value| f(PathComponent(index), value)))
    }

    /// Returns an iterator over each element with its corresponding
    /// [`PathComponent`].
    pub fn iter(&self) -> ChildrenSlots<&T> {
        self.into_iter()
    }

    /// Returns a mutable iterator over each element with its corresponding
    /// [`PathComponent`].
    pub fn iter_mut(&mut self) -> ChildrenSlots<&mut T> {
        self.into_iter()
    }

    /// Merges this collection of children with another collection of children
    /// using the given function.
    ///
    /// If the function returns an error, the merge is aborted and the error is
    /// returned. Because this method takes `self` and `other` by value, they
    /// will be dropped if the merge fails.
    pub fn merge<U, V, E>(
        self,
        other: Children<U>,
        mut merge: impl FnMut(PathComponent, T, U) -> Result<Option<V>, E>,
    ) -> Result<Children<Option<V>>, E> {
        let mut output = Children::<Option<V>>::new();
        for ((pc, a), (_, b)) in self.into_iter().zip(other) {
            output[pc] = merge(pc, a, b)?;
        }
        Ok(output)
    }
}

impl<T> Children<Option<T>> {
    /// Creates a new [`Children`] with all elements set to `None`.
    #[must_use]
    pub fn new() -> Self {
        Self(FixedArray::new())
    }

    /// Returns the number of [`Some`] elements in this collection.
    #[must_use]
    pub fn count(&self) -> usize {
        self.0.count()
    }

    /// Sets the element at `index` to `None`, returning the previous value.
    #[must_use]
    pub fn take(&mut self, index: PathComponent) -> Option<T> {
        self.0.take(index.0)
    }

    /// Returns an iterator over each [`Some`] element with its corresponding
    /// [`PathComponent`].
    pub fn iter_present(&self) -> IterPresentRef<'_, T> {
        self.into_iter()
            .filter_map(|(pc, opt)| opt.as_ref().map(|v| (pc, v)))
    }

    /// If exactly one child is present, takes and returns it along with its
    /// index. Returns `None` if zero or more than one child is present.
    pub fn take_only_child(&mut self) -> Option<(PathComponent, T)> {
        let mut children = self.iter_present();

        // get first child, return None if not present
        let (idx, _) = children.next()?;
        if children.next().is_some() {
            // second child found
            return None;
        }
        drop(children);

        self.take(idx).map(|child| (idx, child))
    }
}

impl<T> Default for Children<Option<T>> {
    fn default() -> Self {
        Self::new()
    }
}

impl<'a, T> Children<&'a Option<T>> {
    /// Returns the number of [`Some`] elements in this collection.
    #[must_use]
    pub fn count(&self) -> usize {
        self.0.iter().filter(|c| c.is_some()).count()
    }

    /// Returns an iterator over each [`Some`] element with its corresponding
    /// [`PathComponent`].
    pub fn iter_present(self) -> IterPresentRef<'a, T> {
        self.into_iter()
            .filter_map(|(pc, opt)| opt.as_ref().map(|v| (pc, v)))
    }
}

impl<'a, T> Children<&'a mut Option<T>> {
    /// Returns the number of [`Some`] elements in this collection.
    #[must_use]
    pub fn count(&self) -> usize {
        self.0.iter().filter(|c| c.is_some()).count()
    }

    /// Returns an iterator over each [`Some`] element with its corresponding
    /// [`PathComponent`].
    pub fn iter_present(self) -> IterPresentMut<'a, T> {
        self.into_iter()
            .filter_map(|(pc, opt)| opt.as_mut().map(|v| (pc, v)))
    }
}

impl<T> std::ops::Index<PathComponent> for Children<T> {
    type Output = T;

    fn index(&self, index: PathComponent) -> &Self::Output {
        self.get(index)
    }
}

impl<T> std::ops::IndexMut<PathComponent> for Children<T> {
    fn index_mut(&mut self, index: PathComponent) -> &mut Self::Output {
        self.get_mut(index)
    }
}

impl<T> IntoIterator for Children<T> {
    type Item = (PathComponent, T);
    type IntoIter = ChildrenSlots<T>;

    fn into_iter(self) -> Self::IntoIter {
        // Collect the owned values (ascending slot order) into an array so the
        // iterator type stays `Zip<array::IntoIter<PathComponent>, array::IntoIter<T>>`.
        let mut values = self.0.into_iter();
        let values: [T; MAX_CHILDREN] = std::array::from_fn(|_| {
            values
                .next()
                .expect("FixedArray yields exactly MAX_CHILDREN items")
                .1
        });
        PathComponent::ALL.into_iter().zip(values)
    }
}

impl<'a, T: 'a> IntoIterator for &'a Children<T> {
    type Item = (PathComponent, &'a T);
    type IntoIter = ChildrenSlots<&'a T>;

    fn into_iter(self) -> Self::IntoIter {
        let mut refs = self.0.iter();
        let refs: [&'a T; MAX_CHILDREN] =
            std::array::from_fn(|_| refs.next().expect("FixedArray has MAX_CHILDREN slots"));
        PathComponent::ALL.into_iter().zip(refs)
    }
}

impl<'a, T: 'a> IntoIterator for &'a mut Children<T> {
    type Item = (PathComponent, &'a mut T);
    type IntoIter = ChildrenSlots<&'a mut T>;

    fn into_iter(self) -> Self::IntoIter {
        let mut refs = self.0.iter_mut();
        let refs: [&'a mut T; MAX_CHILDREN] =
            std::array::from_fn(|_| refs.next().expect("FixedArray has MAX_CHILDREN slots"));
        PathComponent::ALL.into_iter().zip(refs)
    }
}

/// A pointer-sized, present-only container of a branch node's children, sized to
/// exactly the children that exist (zero heap when empty). Backed by
/// [`arity_arrays::PackedArray`], indexed by [`crate::U4`].
pub type DenseChildren<T> = PackedArray<T, Arity16>;

impl<T: Clone> Children<Option<T>> {
    /// Packs the present (`Some`) entries into a [`DenseChildren`], cloning each
    /// present value; empty slots are dropped (zero heap when empty).
    #[must_use]
    pub fn to_dense(&self) -> DenseChildren<T> {
        PackedArray::from(&self.0)
    }
}

impl<T: Clone> From<&DenseChildren<T>> for Children<Option<T>> {
    /// Expands a [`DenseChildren`] back into a full 16-slot [`Children`], cloning
    /// each present value; absent slots become `None`.
    fn from(dense: &DenseChildren<T>) -> Self {
        Children(FixedArray::from(dense))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dense_children_round_trips_with_children() {
        // Children -> DenseChildren -> Children is the identity on present slots.
        let mut c: Children<Option<u32>> = Children::new();
        c[PathComponent::ALL[1]] = Some(10);
        c[PathComponent::ALL[9]] = Some(90);
        c[PathComponent::ALL[15]] = Some(150);

        let dense = c.to_dense();
        assert_eq!(dense.count(), 3);
        assert_eq!(dense.get(PathComponent::ALL[1].0), Some(&10));
        assert_eq!(dense.get(PathComponent::ALL[9].0), Some(&90));
        assert_eq!(dense.get(PathComponent::ALL[0].0), None);

        let back = Children::from(&dense);
        assert_eq!(back, c);

        // Empty round-trips with zero allocation (the #2099 property).
        let empty: Children<Option<u32>> = Children::new();
        let dense_empty = empty.to_dense();
        assert!(dense_empty.is_empty());
        assert_eq!(Children::from(&dense_empty), empty);
    }
}
