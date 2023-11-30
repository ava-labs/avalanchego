// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{
    fmt::{Debug, Error as FmtError, Formatter},
    io::{Cursor, Read, Write},
    mem::size_of,
};

use bincode::Options;

use super::{Data, Encoded};
use crate::{
    merkle::{from_nibbles, to_nibble_array, PartialPath},
    shale::{ShaleError::InvalidCacheView, Storable},
};

pub const SIZE: usize = 2;

type PathLen = u8;
type DataLen = u32;

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
    const PATH_LEN_SIZE: u64 = size_of::<PathLen>() as u64;
    const DATA_LEN_SIZE: u64 = size_of::<DataLen>() as u64;

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

impl Storable for LeafNode {
    fn serialized_len(&self) -> u64 {
        let path_len_size = size_of::<PathLen>() as u64;
        let path_len = self.path.serialized_len();
        let data_len_size = size_of::<DataLen>() as u64;
        let data_len = self.data.len() as u64;

        path_len_size + path_len + data_len_size + data_len
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), crate::shale::ShaleError> {
        let mut cursor = Cursor::new(to);

        let path: Vec<u8> = from_nibbles(&self.path.encode(true)).collect();

        cursor.write_all(&[path.len() as PathLen])?;

        let data_len = self.data.len() as DataLen;
        cursor.write_all(&data_len.to_le_bytes())?;

        cursor.write_all(&path)?;
        cursor.write_all(&self.data)?;

        Ok(())
    }

    fn deserialize<T: crate::shale::CachedStore>(
        mut offset: usize,
        mem: &T,
    ) -> Result<Self, crate::shale::ShaleError>
    where
        Self: Sized,
    {
        let header_size = Self::PATH_LEN_SIZE + Self::DATA_LEN_SIZE;

        let node_header_raw = mem
            .get_view(offset, header_size)
            .ok_or(InvalidCacheView {
                offset,
                size: header_size,
            })?
            .as_deref();

        offset += header_size as usize;

        let mut cursor = Cursor::new(node_header_raw);

        let path_len = {
            let mut buf = [0u8; Self::PATH_LEN_SIZE as usize];
            cursor.read_exact(buf.as_mut())?;
            PathLen::from_le_bytes(buf) as u64
        };

        let data_len = {
            let mut buf = [0u8; Self::DATA_LEN_SIZE as usize];
            cursor.read_exact(buf.as_mut())?;
            DataLen::from_le_bytes(buf) as u64
        };

        let size = path_len + data_len;
        let remainder = mem
            .get_view(offset, size)
            .ok_or(InvalidCacheView { offset, size })?
            .as_deref();

        let (path, data) = remainder.split_at(path_len as usize);

        let path = {
            let nibbles: Vec<u8> = path.iter().copied().flat_map(to_nibble_array).collect();
            PartialPath::decode(&nibbles).0
        };

        let data = Data(data.to_vec());

        Ok(Self::new(path, data))
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
