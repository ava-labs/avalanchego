// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood_storage::FileIoError;

use crate::v2::api::{KeyType, ValueType};

/// A key/value pair operation.
///
/// Put (upsert), Delete (single key), or Prefix Delete (range) are supported.
#[derive(Debug, Clone, Copy)]
#[non_exhaustive]
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

/// A key/value pair that can be converted into a batch operation.
///
/// The difference between this trait and [`TryIntoBatch`] is that this trait
/// is only used in places where the operations are expected to all be `Put`
/// operations. Empty values are not treated as `DeleteRange` operations here.
pub trait KeyValuePair: TryIntoBatch {
    /// Convert this into a tuple of key and value.
    ///
    /// # Errors
    ///
    /// Returns an error if the conversion fails. E.g., if a read from storage
    /// is required to obtain the key or value.
    fn try_into_tuple(self) -> Result<(Self::Key, Self::Value), Self::Error>;
}

impl<K: KeyType, V: ValueType> KeyValuePair for (K, V) {
    fn try_into_tuple(self) -> Result<(Self::Key, Self::Value), Self::Error> {
        Ok(self)
    }
}

impl<K: KeyType, V: ValueType> KeyValuePair for &(K, V) {
    fn try_into_tuple(self) -> Result<(Self::Key, Self::Value), Self::Error> {
        let (key, value) = self;
        Ok((key, value))
    }
}

impl<T: KeyValuePair<Error = std::convert::Infallible>, E: Into<FileIoError>> KeyValuePair
    for Result<T, E>
{
    fn try_into_tuple(self) -> Result<(Self::Key, Self::Value), Self::Error> {
        match self {
            Ok(t) => t.try_into_tuple().map_err(|e| match e {}),
            Err(e) => Err(e),
        }
    }
}

/// A key/value pair that can be used in a batch.
pub trait TryIntoBatch {
    /// The key type
    type Key: KeyType;

    /// The value type
    type Value: ValueType;

    /// The error type. It is preferable to use an error that is convertible into
    /// [`FileIoError`] instead of the type directly to allow for better compiler
    /// optimizations.
    ///
    /// E.g., [`std::convert::Infallible`] is preferred over [`FileIoError`] when
    /// there is no possibility of error so that the compiler can optimize away
    /// error handling.
    type Error: Into<FileIoError>;

    /// Convert this key-value pair into a [`BatchOp`].
    ///
    /// # Errors
    ///
    /// Returns an error if the conversion fails. E.g., if a read from storage
    /// is required to obtain the key or value.
    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error>;
}

impl<'a, K: KeyType, V: ValueType> TryIntoBatch for &'a (K, V) {
    type Key = &'a K;
    type Value = &'a V;
    type Error = std::convert::Infallible;

    #[inline]
    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error> {
        // this converting `&'a (K, V)` into `(&'a K, &'a V)`
        let (key, value) = self;
        (key, value).try_into_batch()
    }
}

impl<K: KeyType, V: ValueType> TryIntoBatch for (K, V) {
    type Key = K;
    type Value = V;
    type Error = std::convert::Infallible;

    #[inline]
    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error> {
        let (key, value) = self;
        if value.as_ref().is_empty() {
            Ok(BatchOp::DeleteRange { prefix: key })
        } else {
            Ok(BatchOp::Put { key, value })
        }
    }
}

impl<K: KeyType, V: ValueType> TryIntoBatch for BatchOp<K, V> {
    type Key = K;
    type Value = V;
    type Error = std::convert::Infallible;

    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error> {
        Ok(self)
    }
}

impl<'a, K: KeyType, V: ValueType> TryIntoBatch for &'a BatchOp<K, V> {
    type Key = &'a K;
    type Value = &'a V;
    type Error = std::convert::Infallible;

    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error> {
        Ok(self.borrowed())
    }
}

impl<T, E> TryIntoBatch for Result<T, E>
where
    T: TryIntoBatch<Error = std::convert::Infallible>,
    E: Into<FileIoError>,
{
    type Key = T::Key;
    type Value = T::Value;
    type Error = E;

    fn try_into_batch(self) -> Result<BatchOp<Self::Key, Self::Value>, Self::Error> {
        match self {
            Ok(t) => t.try_into_batch().map_err(|e| match e {}),
            Err(e) => Err(e),
        }
    }
}

/// An extension trait for iterators that yield [`TryIntoBatch`]s.
pub trait BatchIter:
    Iterator<Item: TryIntoBatch<Key = Self::Key, Value = Self::Value, Error = Self::Error>>
{
    /// An associated type for the iterator item's key type. This is a convenience
    /// requirement to avoid needing to build up nested generic associated types.
    /// E.g., `<<Self as Iterator>::Item as KeyValuePair>::Key`
    type Key: KeyType;

    /// An associated type for the iterator item's value type. This is a convenience
    /// requirement to avoid needing to build up nested generic associated types.
    /// E.g., `<<Self as Iterator>::Item as KeyValuePair>::Value`
    type Value: ValueType;

    /// An associated type for the iterator item's error type. This is a convenience
    /// requirement to avoid needing to build up nested generic associated types.
    /// E.g., `<<Self as Iterator>::Item as KeyValuePair>::Error`
    type Error: Into<FileIoError>;
}

impl<I: Iterator<Item: TryIntoBatch>> BatchIter for I {
    type Key = <I::Item as TryIntoBatch>::Key;
    type Value = <I::Item as TryIntoBatch>::Value;
    type Error = <I::Item as TryIntoBatch>::Error;
}

/// An extension trait for types that can be converted into an iterator of batch
/// operations.
pub trait IntoBatchIter: IntoIterator<IntoIter: BatchIter> {
    /// Convert this type into an iterator of batch operations.
    ///
    /// This is a convenience method that maps over the iterator returned by
    /// [`IntoIterator::into_iter`] and calls [`TryIntoBatch::try_into_batch`]
    /// on each item. Additionally, the error type is converted into the specified
    /// error type `E` using the `Into` trait.
    ///
    /// The pass-through of the error type `E` allows for better compiler optimizations
    /// by allowing error handling to be omitted during monomorphization when the error
    /// type is `Infallible` or another type that can be optimized away.
    fn into_batch_iter<E>(self) -> std::iter::Map<Self::IntoIter, MapIntoBatchFn<Self::Item, E>>
    where
        Self: Sized,
        FileIoError: Into<E>,
    {
        self.into_iter().map(|item| {
            item.try_into_batch()
                .map_err(Into::<FileIoError>::into)
                .map_err(Into::<E>::into)
        })
    }
}

impl<T: IntoIterator<IntoIter: BatchIter>> IntoBatchIter for T {}

/// Type alias for `fn(T) -> Result<BatchOp<K, V>, E>` where `T: TryIntoBatch<Key = K, Value = V>`.
pub type MapIntoBatchFn<T, E> =
    fn(T) -> Result<BatchOp<<T as TryIntoBatch>::Key, <T as TryIntoBatch>::Value>, E>;
