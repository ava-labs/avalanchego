// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use bincode::Options;

use super::{Encoded, Node};
use crate::{
    merkle::{from_nibbles, PartialPath, TRIE_HASH_LEN},
    shale::{DiskAddress, ShaleStore, Storable},
};
use std::{
    fmt::{Debug, Error as FmtError, Formatter},
    io::{Cursor, Write},
    mem::size_of,
};

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
        let path_len_size = size_of::<DataLen>() as u64;
        let path_len = self.path.serialized_len();
        let child_len = DiskAddress::MSIZE;
        let encoded_len_size = size_of::<DataLen>() as u64;
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

        cursor.write_all(&[path.len() as DataLen])?;
        cursor.write_all(&self.child.to_le_bytes())?;
        cursor.write_all(&path)?;

        if let Some(encoded) = self.chd_encoded() {
            cursor.write_all(&[encoded.len() as DataLen])?;
            cursor.write_all(encoded)?;
        }

        Ok(())
    }

    fn deserialize<T: crate::shale::CachedStore>(
        _addr: usize,
        _mem: &T,
    ) -> Result<Self, crate::shale::ShaleError>
    where
        Self: Sized,
    {
        todo!()
    }
}
