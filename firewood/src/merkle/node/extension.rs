// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use bincode::Options;

use super::{Encoded, Node};
use crate::{
    merkle::{from_nibbles, to_nibble_array, PartialPath, TRIE_HASH_LEN},
    shale::{DiskAddress, ShaleError::InvalidCacheView, ShaleStore, Storable},
};
use std::{
    fmt::{Debug, Error as FmtError, Formatter},
    io::{Cursor, Read, Write},
    mem::size_of,
};

type PathLen = u8;
type DataLen = u8;

#[derive(PartialEq, Eq, Clone)]
pub struct ExtNode {
    pub(crate) path: PartialPath,
    pub(crate) child: DiskAddress,
    pub(crate) child_encoded: Option<Vec<u8>>,
}

impl Debug for ExtNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result<(), FmtError> {
        let Self {
            path,
            child,
            child_encoded,
        } = self;
        write!(f, "[Extension {path:?} {child:?} {child_encoded:?}]",)
    }
}

impl ExtNode {
    const PATH_LEN_SIZE: u64 = size_of::<PathLen>() as u64;
    const DATA_LEN_SIZE: u64 = size_of::<DataLen>() as u64;

    pub(super) fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut list = <[Encoded<Vec<u8>>; 2]>::default();
        list[0] = Encoded::Data(
            bincode::DefaultOptions::new()
                .serialize(&from_nibbles(&self.path.encode(false)).collect::<Vec<_>>())
                .unwrap(),
        );

        if !self.child.is_null() {
            let mut r = store.get_item(self.child).unwrap();

            if r.is_encoded_longer_than_hash_len(store) {
                list[1] = Encoded::Data(
                    bincode::DefaultOptions::new()
                        .serialize(&&(*r.get_root_hash(store))[..])
                        .unwrap(),
                );

                if r.is_dirty() {
                    r.write(|_| {}).unwrap();
                    r.set_dirty(false);
                }
            } else {
                list[1] = Encoded::Raw(r.get_encoded(store).to_vec());
            }
        } else {
            // Check if there is already a caclucated encoded value for the child, which
            // can happen when manually constructing a trie from proof.
            if let Some(v) = &self.child_encoded {
                if v.len() == TRIE_HASH_LEN {
                    list[1] = Encoded::Data(bincode::DefaultOptions::new().serialize(v).unwrap());
                } else {
                    list[1] = Encoded::Raw(v.clone());
                }
            }
        }

        bincode::DefaultOptions::new()
            .serialize(list.as_slice())
            .unwrap()
    }

    pub fn chd(&self) -> DiskAddress {
        self.child
    }

    pub fn chd_encoded(&self) -> Option<&[u8]> {
        self.child_encoded.as_deref()
    }

    pub fn chd_mut(&mut self) -> &mut DiskAddress {
        &mut self.child
    }

    pub fn chd_encoded_mut(&mut self) -> &mut Option<Vec<u8>> {
        &mut self.child_encoded
    }
}

impl Storable for ExtNode {
    fn serialized_len(&self) -> u64 {
        let path_len_size = Self::PATH_LEN_SIZE;
        let path_len = self.path.serialized_len();
        let child_len = DiskAddress::MSIZE;
        // TODO:
        // this seems wrong to always include this byte even if there isn't a child
        // but it matches the original implementation
        let encoded_len_size = Self::DATA_LEN_SIZE;
        let encoded_len = self
            .child_encoded
            .as_ref()
            .map(|v| v.len() as u64)
            .unwrap_or(0);

        path_len_size + path_len + child_len + encoded_len_size + encoded_len
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), crate::shale::ShaleError> {
        let mut cursor = Cursor::new(to);

        let path: Vec<u8> = from_nibbles(&self.path.encode(false)).collect();

        cursor.write_all(&[path.len() as PathLen])?;
        cursor.write_all(&self.child.to_le_bytes())?;
        cursor.write_all(&path)?;

        if let Some(encoded) = self.chd_encoded() {
            cursor.write_all(&[encoded.len() as DataLen])?;
            cursor.write_all(encoded)?;
        }

        Ok(())
    }

    fn deserialize<T: crate::shale::CachedStore>(
        mut offset: usize,
        mem: &T,
    ) -> Result<Self, crate::shale::ShaleError>
    where
        Self: Sized,
    {
        let header_size = Self::PATH_LEN_SIZE + DiskAddress::MSIZE;

        let path_and_disk_address = mem
            .get_view(offset, header_size)
            .ok_or(InvalidCacheView {
                offset,
                size: header_size,
            })?
            .as_deref();

        offset += header_size as usize;

        let mut cursor = Cursor::new(path_and_disk_address);
        let mut buf = [0u8; DiskAddress::MSIZE as usize];

        let path_len = {
            let buf = &mut buf[..Self::PATH_LEN_SIZE as usize];
            cursor.read_exact(buf)?;
            buf[0] as u64
        };

        let disk_address = {
            cursor.read_exact(buf.as_mut())?;
            DiskAddress::from(u64::from_le_bytes(buf) as usize)
        };

        let path = mem
            .get_view(offset, path_len)
            .ok_or(InvalidCacheView {
                offset,
                size: path_len,
            })?
            .as_deref();

        offset += path_len as usize;

        let path: Vec<u8> = path.into_iter().flat_map(to_nibble_array).collect();

        let path = PartialPath::decode(&path).0;

        let encoded_len_raw = mem
            .get_view(offset, Self::DATA_LEN_SIZE)
            .ok_or(InvalidCacheView {
                offset,
                size: Self::DATA_LEN_SIZE,
            })?
            .as_deref();

        offset += Self::DATA_LEN_SIZE as usize;

        let mut cursor = Cursor::new(encoded_len_raw);

        let encoded_len = {
            let mut buf = [0u8; Self::DATA_LEN_SIZE as usize];
            cursor.read_exact(buf.as_mut())?;
            DataLen::from_le_bytes(buf) as u64
        };

        let encoded = if encoded_len != 0 {
            let encoded = mem
                .get_view(offset, encoded_len)
                .ok_or(InvalidCacheView {
                    offset,
                    size: encoded_len,
                })?
                .as_deref();

            encoded.into()
        } else {
            None
        };

        Ok(ExtNode {
            path,
            child: disk_address,
            child_encoded: encoded,
        })
    }
}
