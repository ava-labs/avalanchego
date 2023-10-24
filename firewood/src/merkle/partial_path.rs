// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::nibbles::NibblesIterator;
use std::fmt::{self, Debug};

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

impl PartialPath {
    pub fn into_inner(self) -> Vec<u8> {
        self.0
    }

    pub(super) fn encode(&self, term: bool) -> Vec<u8> {
        let odd_len = (self.0.len() & 1) as u8;
        let flags = if term { 2 } else { 0 } + odd_len;
        let mut res = if odd_len == 1 {
            vec![flags]
        } else {
            vec![flags, 0x0]
        };
        res.extend(&self.0);
        res
    }

    // TODO: remove all non `Nibbles` usages and delete this function.
    // I also think `PartialPath` could probably borrow instead of own data.
    //
    /// returns a tuple of the decoded partial path and whether the path is terminal
    pub fn decode(raw: &[u8]) -> (Self, bool) {
        let prefix = raw[0];
        let is_odd = (prefix & 1) as usize;
        let decoded = raw.iter().skip(1).skip(1 - is_odd).copied().collect();

        (Self(decoded), prefix > 1)
    }

    /// returns a tuple of the decoded partial path and whether the path is terminal
    pub fn from_nibbles<const N: usize>(mut nibbles: NibblesIterator<'_, N>) -> (Self, bool) {
        let prefix = nibbles.next().unwrap();
        let is_odd = (prefix & 1) as usize;
        let decoded = nibbles.skip(1 - is_odd).collect();

        (Self(decoded), prefix > 1)
    }

    pub(super) fn dehydrated_len(&self) -> u64 {
        let len = self.0.len() as u64;
        if len & 1 == 1 {
            (len + 1) >> 1
        } else {
            (len >> 1) + 1
        }
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
        let path = PartialPath(steps.to_vec()).encode(term);
        let (decoded, decoded_term) = PartialPath::decode(&path);
        assert_eq!(&decoded.0, &steps);
        assert_eq!(decoded_term, term);
    }
}
