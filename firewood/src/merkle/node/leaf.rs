// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt::{Debug, Error as FmtError, Formatter};

use bincode::Options;

use super::{Data, Encoded};
use crate::merkle::{from_nibbles, PartialPath};

pub const SIZE: usize = 2;

#[derive(PartialEq, Eq, Clone)]
pub struct LeafNode {
    pub(crate) path: PartialPath,
    pub(crate) data: Data,
}

impl Debug for LeafNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result<(), FmtError> {
        write!(f, "[Leaf {:?} {}]", self.path, hex::encode(&*self.data))
    }
}

impl LeafNode {
    pub fn new<P: Into<PartialPath>, D: Into<Data>>(path: P, data: D) -> Self {
        Self {
            path: path.into(),
            data: data.into(),
        }
    }

    pub fn path(&self) -> &PartialPath {
        &self.path
    }

    pub fn data(&self) -> &Data {
        &self.data
    }

    pub(super) fn encode(&self) -> Vec<u8> {
        bincode::DefaultOptions::new()
            .serialize(
                [
                    Encoded::Raw(from_nibbles(&self.path.encode(true)).collect()),
                    Encoded::Raw(self.data.to_vec()),
                ]
                .as_slice(),
            )
            .unwrap()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use test_case::test_case;

    // these tests will fail if the encoding mechanism changes and should be updated accordingly
    #[test_case(0b10 << 4, vec![0x12, 0x34], vec![1, 2, 3, 4]; "even length")]
    // first nibble is part of the prefix
    #[test_case((0b11 << 4) + 2, vec![0x34], vec![2, 3, 4]; "odd length")]
    fn encode_regression_test(prefix: u8, path: Vec<u8>, nibbles: Vec<u8>) {
        let data = vec![5, 6, 7, 8];

        let serialized_path = [vec![prefix], path.clone()].concat();
        // 0 represents Encoded::Raw
        let serialized_path = [vec![0, serialized_path.len() as u8], serialized_path].concat();
        let serialized_data = [vec![0, data.len() as u8], data.clone()].concat();

        let serialized = [vec![2], serialized_path, serialized_data].concat();

        let node = LeafNode::new(nibbles, data.clone());

        assert_eq!(node.encode(), serialized);
    }
}
