// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::v2::api::{KeyType, ValueType};

/// A key/value pair operation. Only put (upsert) and delete are
/// supported
#[derive(Debug, Clone, Copy)]
pub enum BatchOp<K: KeyType, V: ValueType> {
    /// Upsert a key/value pair
    Put {
        /// the key
        key: K,
        /// the value
        value: V,
    },

    /// Delete a key
    Delete {
        /// The key
        key: K,
    },

    /// Delete a range of keys by prefix
    DeleteRange {
        /// The prefix of the keys to delete
        prefix: K,
    },
}

impl<K: KeyType, V: ValueType> BatchOp<K, V> {
    /// Get the key of this operation
    #[must_use]
    pub const fn key(&self) -> &K {
        match self {
            BatchOp::Put { key, .. }
            | BatchOp::Delete { key }
            | BatchOp::DeleteRange { prefix: key } => key,
        }
    }

    /// Get the value of this operation
    #[must_use]
    pub const fn value(&self) -> Option<&V> {
        match self {
            BatchOp::Put { value, .. } => Some(value),
            _ => None,
        }
    }

    /// Convert this operation into a borrowed version, where the key and value
    /// are references to the original data.
    #[must_use]
    pub const fn borrowed(&self) -> BatchOp<&K, &V> {
        match self {
            BatchOp::Put { key, value } => BatchOp::Put { key, value },
            BatchOp::Delete { key } => BatchOp::Delete { key },
            BatchOp::DeleteRange { prefix } => BatchOp::DeleteRange { prefix },
        }
    }

    /// Erases the key and value types, returning a [`BatchOp`] with the key
    /// and value dereferenced to `&[u8]`.
    #[inline]
    #[must_use]
    pub fn as_ref(&self) -> BatchOp<&[u8], &[u8]> {
        match self {
            BatchOp::Put { key, value } => BatchOp::Put {
                key: key.as_ref(),
                value: value.as_ref(),
            },
            BatchOp::Delete { key } => BatchOp::Delete { key: key.as_ref() },
            BatchOp::DeleteRange { prefix } => BatchOp::DeleteRange {
                prefix: prefix.as_ref(),
            },
        }
    }
}

impl BatchOp<&[u8], &[u8]> {
    fn eq_impl(self, other: Self) -> bool {
        std::mem::discriminant(&self) == std::mem::discriminant(&other)
            && self.key() == other.key()
            && self.value() == other.value()
    }

    fn hash_impl<H: std::hash::Hasher>(self, state: &mut H) {
        use std::hash::Hash;
        std::mem::discriminant(&self).hash(state);
        self.key().hash(state);
        if let Some(value) = self.value() {
            value.hash(state);
        }
    }
}

impl<K1, V1, K2, V2> PartialEq<BatchOp<K2, V2>> for BatchOp<K1, V1>
where
    K1: KeyType,
    K2: KeyType,
    V1: ValueType,
    V2: ValueType,
{
    fn eq(&self, other: &BatchOp<K2, V2>) -> bool {
        BatchOp::eq_impl(self.as_ref(), other.as_ref())
    }
}

impl<K: KeyType, V: ValueType> Eq for BatchOp<K, V> {}

impl<K: KeyType, V: ValueType> std::hash::Hash for BatchOp<K, V> {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        BatchOp::hash_impl(self.as_ref(), state);
    }
}

/// A key/value pair that can be used in a batch.
pub trait KeyValuePair {
    /// The key type
    type Key: KeyType;

    /// The value type
    type Value: ValueType;

    /// Convert this key-value pair into a [`BatchOp`].
    #[must_use]
    fn into_batch(self) -> BatchOp<Self::Key, Self::Value>;
}

impl<'a, K: KeyType, V: ValueType> KeyValuePair for &'a (K, V) {
    type Key = &'a K;
    type Value = &'a V;

    #[inline]
    fn into_batch(self) -> BatchOp<Self::Key, Self::Value> {
        // this converting `&'a (K, V)` into `(&'a K, &'a V)`
        let (key, value) = self;
        (key, value).into_batch()
    }
}

impl<K: KeyType, V: ValueType> KeyValuePair for (K, V) {
    type Key = K;
    type Value = V;

    #[inline]
    fn into_batch(self) -> BatchOp<Self::Key, Self::Value> {
        let (key, value) = self;
        if value.as_ref().is_empty() {
            BatchOp::DeleteRange { prefix: key }
        } else {
            BatchOp::Put { key, value }
        }
    }
}

impl<K: KeyType, V: ValueType> KeyValuePair for BatchOp<K, V> {
    type Key = K;
    type Value = V;

    fn into_batch(self) -> BatchOp<Self::Key, Self::Value> {
        self
    }
}

impl<'a, K: KeyType, V: ValueType> KeyValuePair for &'a BatchOp<K, V> {
    type Key = &'a K;
    type Value = &'a V;

    fn into_batch(self) -> BatchOp<Self::Key, Self::Value> {
        self.borrowed()
    }
}

/// An extension trait for iterators that yield [`KeyValuePair`]s.
pub trait KeyValuePairIter:
    Iterator<Item: KeyValuePair<Key = Self::Key, Value = Self::Value>>
{
    /// An associated type for the iterator item's key type. This is a convenience
    /// requirement to avoid needing to build up nested generic associated types.
    /// E.g., `<<Self as Iterator>::Item as KeyValuePair>::Key`
    type Key: KeyType;

    /// An associated type for the iterator item's value type. This is a convenience
    /// requirement to avoid needing to build up nested generic associated types.
    /// E.g., `<<Self as Iterator>::Item as KeyValuePair>::Value`
    type Value: ValueType;

    /// Maps the items of this iterator into [`BatchOp`]s.
    #[inline]
    fn map_into_batch(self) -> MapIntoBatch<Self>
    where
        Self: Sized,
        Self::Item: KeyValuePair,
    {
        self.map(KeyValuePair::into_batch)
    }
}

impl<I: Iterator<Item: KeyValuePair>> KeyValuePairIter for I {
    type Key = <I::Item as KeyValuePair>::Key;
    type Value = <I::Item as KeyValuePair>::Value;
}

/// An iterator that maps a [`KeyValuePair`] into a [`BatchOp`] on yielded items.
pub type MapIntoBatch<I> = std::iter::Map<
    I,
    fn(
        <I as Iterator>::Item,
    ) -> BatchOp<<I as KeyValuePairIter>::Key, <I as KeyValuePairIter>::Value>,
>;
