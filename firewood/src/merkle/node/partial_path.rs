// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::Flags;
use crate::nibbles::NibblesIterator;
use std::{
    fmt::{self, Debug},
    iter::once,
};

// TODO: use smallvec
/// PartialPath keeps a list of nibbles to represent a path on the Trie.
#[derive(PartialEq, Eq, Clone)]
pub struct PartialPath(pub Vec<u8>);

impl Debug for PartialPath {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        for nib in self.0.iter() {
            write!(f, "{:x}", *nib & 0xf)?;
        }
        Ok(())
    }
}

impl std::ops::Deref for PartialPath {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

impl From<Vec<u8>> for PartialPath {
    fn from(value: Vec<u8>) -> Self {
        Self(value)
    }
}

impl PartialPath {
    pub fn into_inner(self) -> Vec<u8> {
        self.0
    }

    pub(crate) fn encode(&self, is_terminal: bool) -> Vec<u8> {
        let mut flags = Flags::empty();

        if is_terminal {
            flags.insert(Flags::TERMINAL);
        }

        let has_odd_len = self.0.len() & 1 == 1;

        let extra_byte = if has_odd_len {
            flags.insert(Flags::ODD_LEN);

            None
        } else {
            Some(0)
        };

        once(flags.bits())
            .chain(extra_byte)
            .chain(self.0.iter().copied())
            .collect()
    }

    // TODO: remove all non `Nibbles` usages and delete this function.
    // I also think `PartialPath` could probably borrow instead of own data.
    //
    /// returns a tuple of the decoded partial path and whether the path is terminal
    pub fn decode(raw: &[u8]) -> (Self, bool) {
        Self::from_iter(raw.iter().copied())
    }

    /// returns a tuple of the decoded partial path and whether the path is terminal
    pub fn from_nibbles<const N: usize>(nibbles: NibblesIterator<'_, N>) -> (Self, bool) {
        Self::from_iter(nibbles)
    }

    /// Assumes all bytes are nibbles, prefer to use `from_nibbles` instead.
    fn from_iter<Iter: Iterator<Item = u8>>(mut iter: Iter) -> (Self, bool) {
        let flags = Flags::from_bits_retain(iter.next().unwrap_or_default());

        if !flags.contains(Flags::ODD_LEN) {
            let _ = iter.next();
        }

        (Self(iter.collect()), flags.contains(Flags::TERMINAL))
    }

    pub(super) fn serialized_len(&self) -> u64 {
        let len = self.0.len();

        // if len is even the prefix takes an extra byte
        // otherwise is combined with the first nibble
        let len = if len & 1 == 1 {
            (len + 1) / 2
        } else {
            len / 2 + 1
        };

        len as u64
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use test_case::test_case;

    #[test_case(&[1, 2, 3, 4], true)]
    #[test_case(&[1, 2, 3], false)]
    #[test_case(&[0, 1, 2], false)]
    #[test_case(&[1, 2], true)]
    #[test_case(&[1], true)]
    fn test_encoding(steps: &[u8], term: bool) {
        let path = PartialPath(steps.to_vec());
        let encoded = path.encode(term);

        assert_eq!(encoded.len(), path.serialized_len() as usize * 2);

        let (decoded, decoded_term) = PartialPath::decode(&encoded);

        assert_eq!(&&*decoded, &steps);
        assert_eq!(decoded_term, term);
    }
}
