// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt::{Debug, Error as FmtError, Formatter};

use crate::Path;

/// A leaf node
#[derive(PartialEq, Eq, Clone)]
pub struct LeafNode {
    /// The path of this leaf, but only the remaining nibbles
    pub partial_path: Path,

    /// The value associated with this leaf
    pub value: Box<[u8]>,
}

impl Debug for LeafNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result<(), FmtError> {
        write!(
            f,
            "[Leaf {:?} {}]",
            self.partial_path,
            hex::encode(&*self.value)
        )
    }
}
