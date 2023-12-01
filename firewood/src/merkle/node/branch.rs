// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{Data, Encoded, Node};
use crate::{
    merkle::{PartialPath, TRIE_HASH_LEN},
    shale::{DiskAddress, Storable},
    shale::{ShaleError, ShaleStore},
};
use bincode::{Error, Options};
use std::{
    fmt::{Debug, Error as FmtError, Formatter},
    io::{Cursor, Read, Write},
    mem::size_of,
    ops::Deref,
};

pub type DataLen = u32;
pub type EncodedChildLen = u8;

const MAX_CHILDREN: usize = 16;

#[derive(PartialEq, Eq, Clone)]
pub struct BranchNode {
    // pub(crate) path: PartialPath,
    pub(crate) children: [Option<DiskAddress>; MAX_CHILDREN],
    pub(crate) value: Option<Data>,
    pub(crate) children_encoded: [Option<Vec<u8>>; MAX_CHILDREN],
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result<(), FmtError> {
        write!(f, "[Branch")?;
        // write!(f, " path={:?}", self.path)?;

        for (i, c) in self.children.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({i:x} {c:?})")?;
            }
        }

        for (i, c) in self.children_encoded.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({i:x} {:?})", c)?;
            }
        }

        write!(
            f,
            " v={}]",
            match &self.value {
                Some(v) => hex::encode(&**v),
                None => "nil".to_string(),
            }
        )
    }
}

impl BranchNode {
    pub const MAX_CHILDREN: usize = MAX_CHILDREN;
    pub const MSIZE: usize = Self::MAX_CHILDREN + 1;

    pub fn new(
        _path: PartialPath,
        chd: [Option<DiskAddress>; Self::MAX_CHILDREN],
        value: Option<Vec<u8>>,
        chd_encoded: [Option<Vec<u8>>; Self::MAX_CHILDREN],
    ) -> Self {
        BranchNode {
            // path,
            children: chd,
            value: value.map(Data),
            children_encoded: chd_encoded,
        }
    }

    pub fn value(&self) -> &Option<Data> {
        &self.value
    }

    pub fn chd(&self) -> &[Option<DiskAddress>; Self::MAX_CHILDREN] {
        &self.children
    }

    pub fn chd_mut(&mut self) -> &mut [Option<DiskAddress>; Self::MAX_CHILDREN] {
        &mut self.children
    }

    pub fn chd_encode(&self) -> &[Option<Vec<u8>>; Self::MAX_CHILDREN] {
        &self.children_encoded
    }

    pub fn chd_encoded_mut(&mut self) -> &mut [Option<Vec<u8>>; Self::MAX_CHILDREN] {
        &mut self.children_encoded
    }

    pub(crate) fn single_child(&self) -> (Option<(DiskAddress, u8)>, bool) {
        let mut has_chd = false;
        let mut only_chd = None;
        for (i, c) in self.children.iter().enumerate() {
            if c.is_some() {
                has_chd = true;
                if only_chd.is_some() {
                    only_chd = None;
                    break;
                }
                only_chd = (*c).map(|e| (e, i as u8))
            }
        }
        (only_chd, has_chd)
    }

    pub(super) fn decode(buf: &[u8]) -> Result<Self, Error> {
        let mut items: Vec<Encoded<Vec<u8>>> = bincode::DefaultOptions::new().deserialize(buf)?;

        // we've already validated the size, that's why we can safely unwrap
        let data = items.pop().unwrap().decode()?;
        // Extract the value of the branch node and set to None if it's an empty Vec
        let value = Some(data).filter(|data| !data.is_empty());

        // encode all children.
        let mut chd_encoded: [Option<Vec<u8>>; Self::MAX_CHILDREN] = Default::default();

        // we popped the last element, so their should only be NBRANCH items left
        for (i, chd) in items.into_iter().enumerate() {
            let data = chd.decode()?;
            chd_encoded[i] = Some(data).filter(|data| !data.is_empty());
        }

        // TODO: add path
        let path = Vec::new().into();

        Ok(BranchNode::new(
            path,
            [None; Self::MAX_CHILDREN],
            value,
            chd_encoded,
        ))
    }

    pub(super) fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        // TODO: add path to encoded node
        let mut list = <[Encoded<Vec<u8>>; Self::MAX_CHILDREN + 1]>::default();

        for (i, c) in self.children.iter().enumerate() {
            match c {
                Some(c) => {
                    let mut c_ref = store.get_item(*c).unwrap();

                    if c_ref.is_encoded_longer_than_hash_len::<S>(store) {
                        list[i] = Encoded::Data(
                            bincode::DefaultOptions::new()
                                .serialize(&&(*c_ref.get_root_hash::<S>(store))[..])
                                .unwrap(),
                        );

                        // See struct docs for ordering requirements
                        if c_ref.is_dirty() {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.set_dirty(false);
                        }
                    } else {
                        let child_encoded = &c_ref.get_encoded::<S>(store);
                        list[i] = Encoded::Raw(child_encoded.to_vec());
                    }
                }

                // TODO:
                // we need a better solution for this. This is only used for reconstructing a
                // merkle-tree in memory. The proper way to do it is to abstract a trait for nodes
                // but that's a heavy lift.
                // TODO:
                // change the data-structure children: [(Option<DiskAddress>, Option<Vec<u8>>); Self::MAX_CHILDREN]
                None => {
                    // Check if there is already a calculated encoded value for the child, which
                    // can happen when manually constructing a trie from proof.
                    if let Some(v) = &self.children_encoded[i] {
                        if v.len() == TRIE_HASH_LEN {
                            list[i] =
                                Encoded::Data(bincode::DefaultOptions::new().serialize(v).unwrap());
                        } else {
                            list[i] = Encoded::Raw(v.clone());
                        }
                    }
                }
            };
        }

        if let Some(Data(val)) = &self.value {
            list[Self::MAX_CHILDREN] =
                Encoded::Data(bincode::DefaultOptions::new().serialize(val).unwrap());
        }

        bincode::DefaultOptions::new()
            .serialize(list.as_slice())
            .unwrap()
    }
}

impl Storable for BranchNode {
    fn serialized_len(&self) -> u64 {
        let children_len = Self::MAX_CHILDREN as u64 * DiskAddress::MSIZE;
        let data_len = optional_data_len::<DataLen, _>(self.value.as_deref());
        let children_encoded_len = self.children_encoded.iter().fold(0, |len, child| {
            len + optional_data_len::<EncodedChildLen, _>(child.as_ref())
        });

        children_len + data_len + children_encoded_len
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), crate::shale::ShaleError> {
        let mut cursor = Cursor::new(to);

        for child in &self.children {
            let bytes = child.map(|addr| addr.to_le_bytes()).unwrap_or_default();
            cursor.write_all(&bytes)?;
        }

        let (value_len, value) = self
            .value
            .as_ref()
            .map(|val| (val.len() as DataLen, val.deref()))
            .unwrap_or((DataLen::MAX, &[]));

        cursor.write_all(&value_len.to_le_bytes())?;
        cursor.write_all(value)?;

        for child_encoded in &self.children_encoded {
            let (child_len, child) = child_encoded
                .as_ref()
                .map(|child| (child.len() as EncodedChildLen, child.as_slice()))
                .unwrap_or((EncodedChildLen::MIN, &[]));

            cursor.write_all(&child_len.to_le_bytes())?;
            cursor.write_all(child)?;
        }

        Ok(())
    }

    fn deserialize<T: crate::shale::CachedStore>(
        mut addr: usize,
        mem: &T,
    ) -> Result<Self, crate::shale::ShaleError> {
        const DATA_LEN_SIZE: usize = size_of::<DataLen>();
        const BRANCH_HEADER_SIZE: u64 =
            BranchNode::MAX_CHILDREN as u64 * DiskAddress::MSIZE + DATA_LEN_SIZE as u64;

        let node_raw =
            mem.get_view(addr, BRANCH_HEADER_SIZE)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: BRANCH_HEADER_SIZE,
                })?;

        addr += BRANCH_HEADER_SIZE as usize;

        let mut cursor = Cursor::new(node_raw.as_deref());
        let mut children = [None; BranchNode::MAX_CHILDREN];
        let mut buf = [0u8; DiskAddress::MSIZE as usize];

        for child in &mut children {
            cursor.read_exact(&mut buf)?;
            *child = Some(usize::from_le_bytes(buf))
                .filter(|addr| *addr != 0)
                .map(DiskAddress::from);
        }

        let raw_len = {
            let mut buf = [0; DATA_LEN_SIZE];
            cursor.read_exact(&mut buf)?;
            Some(DataLen::from_le_bytes(buf))
                .filter(|len| *len != DataLen::MAX)
                .map(|len| len as u64)
        };

        let value = match raw_len {
            Some(len) => {
                let data = mem
                    .get_view(addr, len)
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr,
                        size: len,
                    })?;

                addr += len as usize;

                Some(Data(data.as_deref()))
            }
            None => None,
        };

        let mut children_encoded: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();

        for child in &mut children_encoded {
            const ENCODED_CHILD_LEN_SIZE: u64 = size_of::<EncodedChildLen>() as u64;

            let len_raw = mem
                .get_view(addr, ENCODED_CHILD_LEN_SIZE)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: ENCODED_CHILD_LEN_SIZE,
                })?
                .as_deref();

            let mut cursor = Cursor::new(len_raw);

            let len = {
                let mut buf = [0; ENCODED_CHILD_LEN_SIZE as usize];
                cursor.read_exact(buf.as_mut())?;
                EncodedChildLen::from_le_bytes(buf) as u64
            };

            addr += ENCODED_CHILD_LEN_SIZE as usize;

            if len == 0 {
                continue;
            }

            let encoded = mem
                .get_view(addr, len)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: len,
                })?
                .as_deref();

            addr += len as usize;

            *child = Some(encoded);
        }

        let node = BranchNode {
            // TODO: add path
            // path: Vec::new().into(),
            children,
            value,
            children_encoded,
        };

        Ok(node)
    }
}

fn optional_data_len<Len, T: AsRef<[u8]>>(data: Option<T>) -> u64 {
    size_of::<Len>() as u64 + data.as_ref().map_or(0, |data| data.as_ref().len() as u64)
}
