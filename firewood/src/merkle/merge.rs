// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood_storage::{FileIoError, TrieReader};

use crate::{
    db::BatchOp,
    iter::MerkleKeyValueIter,
    merkle::Key,
    v2::api::{BatchIter, KeyType, KeyValuePair},
};

/// Serializes a sequence of key-value pairs merged with the base merkle trie
/// as a sequence of [`BatchOp`]s.
///
/// The key-value range is considered total, meaning keys within the specified
/// bounds that are present within the base trie but not in the key-value iterator
/// will be yielded as [`BatchOp::Delete`] or [`BatchOp::DeleteRange`] operations.
#[must_use = "iterators are lazy and do nothing unless consumed"]
pub(super) struct MergeKeyValueIter<'a, T, I, K>
where
    T: TrieReader,
    I: Iterator<Item: KeyValuePair>,
    K: KeyType,
{
    trie: ReturnableIterator<KeyRangeIter<MerkleKeyValueIter<'a, T>, K>>,
    kvp: ReturnableIterator<KeyRangeIter<I, K>>,
}

impl<'a, T, I, K> MergeKeyValueIter<'a, T, I, K>
where
    T: TrieReader,
    I: Iterator<Item: KeyValuePair>,
    K: KeyType,
{
    pub fn new(
        merkle: &'a crate::merkle::Merkle<T>,
        first_key: Option<impl KeyType>,
        last_key: Option<K>,
        kvp_iter: impl IntoIterator<IntoIter = I>,
    ) -> Self {
        let base_iter = match first_key {
            Some(k) => merkle.key_value_iter_from_key(k),
            None => merkle.key_value_iter(),
        };

        Self {
            trie: ReturnableIterator::new(KeyRangeIter::new(base_iter, last_key)),
            kvp: ReturnableIterator::new(KeyRangeIter::new(kvp_iter.into_iter(), None)),
        }
    }
}

impl<T, I, K> Iterator for MergeKeyValueIter<'_, T, I, K>
where
    T: TrieReader,
    I: Iterator<Item: KeyValuePair>,
    K: KeyType,
{
    type Item = Result<
        BatchOp<EitherKey<Key, <I as BatchIter>::Key>, <I as BatchIter>::Value>,
        FileIoError,
    >;

    fn next(&mut self) -> Option<Self::Item> {
        loop {
            break match (self.trie.next(), self.kvp.next()) {
                // we've exhausted both iterators
                (None, None) => None,

                (Some(Err(err)), kvp) => {
                    if let Some(kvp) = kvp {
                        self.kvp.set_next(kvp);
                    }

                    Some(Err(err))
                }
                (trie, Some(Err(err))) => {
                    if let Some(trie) = trie {
                        self.trie.set_next(trie);
                    }

                    Some(Err(err.into()))
                }

                // only the trie iterator has remaining items.
                (Some(Ok((key, _))), None) => Some(Ok(BatchOp::Delete {
                    key: EitherKey::Left(key),
                })),

                // only kvp iterator has remaining items.
                (None, Some(Ok((key, value)))) => Some(Ok(BatchOp::Put {
                    key: EitherKey::Right(key),
                    value,
                })),

                (Some(Ok((base_key, node_value))), Some(Ok((kvp_key, kvp_value)))) => {
                    match <[u8] as Ord>::cmp(&base_key, kvp_key.as_ref()) {
                        std::cmp::Ordering::Less => {
                            // retain the kvp iterator's current item.
                            self.kvp.set_next(Ok((kvp_key, kvp_value)));

                            // trie key is less than next kvp key, so it must be deleted.
                            Some(Ok(BatchOp::Delete {
                                key: EitherKey::Left(base_key),
                            }))
                        }
                        std::cmp::Ordering::Equal => {
                            // retain neither iterator's current item.

                            if *node_value == *kvp_value.as_ref() {
                                // skip since value is unchanged
                                continue;
                            }

                            // values differ, so we need to insert the new value
                            Some(Ok(BatchOp::Put {
                                key: EitherKey::Right(kvp_key),
                                value: kvp_value,
                            }))
                        }
                        std::cmp::Ordering::Greater => {
                            // retain the trie iterator's current item.
                            self.trie.set_next(Ok((base_key, node_value)));
                            // trie key is greater than next kvp key, so we need to insert it.
                            Some(Ok(BatchOp::Put {
                                key: EitherKey::Right(kvp_key),
                                value: kvp_value,
                            }))
                        }
                    }
                }
            };
        }
    }
}

/// Similar to a peekable iterator. Instead of peeking at the next item, it allows
/// you to put it back to be returned on the next call to `next()`.
struct ReturnableIterator<I: Iterator> {
    iter: I,
    next: Option<I::Item>,
}

impl<I: Iterator> ReturnableIterator<I> {
    const fn new(iter: I) -> Self {
        Self { iter, next: None }
    }

    const fn set_next(&mut self, head: I::Item) -> Option<I::Item> {
        self.next.replace(head)
    }
}

impl<I: Iterator> Iterator for ReturnableIterator<I> {
    type Item = I::Item;

    fn next(&mut self) -> Option<Self::Item> {
        self.next.take().or_else(|| self.iter.next())
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let (lower, upper) = self.iter.size_hint();
        let head_count = usize::from(self.next.is_some());
        (
            lower.saturating_add(head_count),
            upper.and_then(|u| u.checked_add(head_count)),
        )
    }
}

enum KeyRangeIter<I, K> {
    Unfiltered { iter: I },
    Filtered { iter: I, last_key: K },
    Exhausted,
}

impl<I, K> KeyRangeIter<I, K> {
    fn new(iter: I, last_key: Option<K>) -> Self {
        match last_key {
            Some(k) => KeyRangeIter::Filtered { iter, last_key: k },
            None => KeyRangeIter::Unfiltered { iter },
        }
    }
}

impl<I: Iterator<Item = T>, T: KeyValuePair, K: KeyType> Iterator for KeyRangeIter<I, K> {
    type Item = Result<(T::Key, T::Value), T::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        match self {
            KeyRangeIter::Unfiltered { iter } => iter.next().map(T::try_into_tuple),
            KeyRangeIter::Filtered { iter, last_key } => match iter.next().map(T::try_into_tuple) {
                Some(Ok((key, value))) if key.as_ref() <= last_key.as_ref() => {
                    Some(Ok((key, value)))
                }
                Some(Err(e)) => Some(Err(e)),
                _ => {
                    *self = KeyRangeIter::Exhausted;
                    None
                }
            },
            KeyRangeIter::Exhausted => None,
        }
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        match self {
            KeyRangeIter::Unfiltered { iter } => iter.size_hint(),
            KeyRangeIter::Filtered { iter, .. } => {
                let (_, upper) = iter.size_hint();
                (0, upper)
            }
            KeyRangeIter::Exhausted => (0, Some(0)),
        }
    }
}

#[derive(Debug)]
pub(super) enum EitherKey<L, R> {
    Left(L),
    Right(R),
}

impl<L: KeyType, R: KeyType> EitherKey<L, R> {
    fn as_key(&self) -> &[u8] {
        match self {
            EitherKey::Left(key) => key.as_ref(),
            EitherKey::Right(key) => key.as_ref(),
        }
    }
}

impl<L: KeyType, R: KeyType> AsRef<[u8]> for EitherKey<L, R> {
    fn as_ref(&self) -> &[u8] {
        self.as_key()
    }
}

impl<L: KeyType, R: KeyType> std::borrow::Borrow<[u8]> for EitherKey<L, R> {
    fn borrow(&self) -> &[u8] {
        self.as_key()
    }
}

impl<L: KeyType, R: KeyType> std::ops::Deref for EitherKey<L, R> {
    type Target = [u8];

    fn deref(&self) -> &Self::Target {
        self.as_key()
    }
}

impl<L: KeyType, R: KeyType, V: KeyType + ?Sized> PartialEq<V> for EitherKey<L, R> {
    fn eq(&self, other: &V) -> bool {
        self.as_key() == other.as_ref()
    }
}

impl<L: KeyType, R: KeyType> Eq for EitherKey<L, R> {}

impl<L: KeyType, R: KeyType, V: KeyType + ?Sized> PartialOrd<V> for EitherKey<L, R> {
    fn partial_cmp(&self, other: &V) -> Option<std::cmp::Ordering> {
        Some(self.as_key().cmp(other.as_ref()))
    }
}

impl<L: KeyType, R: KeyType> Ord for EitherKey<L, R> {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.as_key().cmp(other.as_key())
    }
}

impl<L: KeyType, R: KeyType> std::hash::Hash for EitherKey<L, R> {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.as_key().hash(state);
    }
}
