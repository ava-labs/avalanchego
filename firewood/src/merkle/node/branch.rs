// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{Data, Encoded, Node};
use crate::{
    merkle::{PartialPath, TRIE_HASH_LEN},
    shale::DiskAddress,
    shale::ShaleStore,
};
use bincode::{Error, Options};
use std::{
    fmt::{Debug, Error as FmtError, Formatter},
    sync::atomic::Ordering,
};

pub const MAX_CHILDREN: usize = 16;
pub const SIZE: usize = MAX_CHILDREN + 1;

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
    pub fn new(
        _path: PartialPath,
        chd: [Option<DiskAddress>; MAX_CHILDREN],
        value: Option<Vec<u8>>,
        chd_encoded: [Option<Vec<u8>>; MAX_CHILDREN],
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

    pub fn chd(&self) -> &[Option<DiskAddress>; MAX_CHILDREN] {
        &self.children
    }

    pub fn chd_mut(&mut self) -> &mut [Option<DiskAddress>; MAX_CHILDREN] {
        &mut self.children
    }

    pub fn chd_encode(&self) -> &[Option<Vec<u8>>; MAX_CHILDREN] {
        &self.children_encoded
    }

    pub fn chd_encoded_mut(&mut self) -> &mut [Option<Vec<u8>>; MAX_CHILDREN] {
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
        let mut chd_encoded: [Option<Vec<u8>>; MAX_CHILDREN] = Default::default();

        // we popped the last element, so their should only be NBRANCH items left
        for (i, chd) in items.into_iter().enumerate() {
            let data = chd.decode()?;
            chd_encoded[i] = Some(data).filter(|data| !data.is_empty());
        }

        // TODO: add path
        let path = Vec::new().into();

        Ok(BranchNode::new(
            path,
            [None; MAX_CHILDREN],
            value,
            chd_encoded,
        ))
    }

    pub(super) fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        // TODO: add path to encoded node
        let mut list = <[Encoded<Vec<u8>>; MAX_CHILDREN + 1]>::default();

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
                        if c_ref.lazy_dirty.load(Ordering::Relaxed) {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.lazy_dirty.store(false, Ordering::Relaxed)
                        }
                    } else {
                        let child_encoded = &c_ref.get_encoded::<S>(store);
                        list[i] = Encoded::Raw(child_encoded.to_vec());
                    }
                }
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
            list[MAX_CHILDREN] =
                Encoded::Data(bincode::DefaultOptions::new().serialize(val).unwrap());
        }

        bincode::DefaultOptions::new()
            .serialize(list.as_slice())
            .unwrap()
    }
}
