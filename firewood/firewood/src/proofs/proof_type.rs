// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

/// The type of serialized proof.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ProofType {
    /// A proof for a single key/value pair.
    ///
    /// A proof is a sequence of nodes from the root to a specific node.
    /// Each node in the path includes the hash of its child nodes, allowing
    /// for verification of the integrity of the path.
    ///
    /// A single proof includes the full key and value (if present) of the target
    /// node.
    Single = 0,
    /// A range proof for all key/value pairs over a specific key range.
    ///
    /// A range proof includes a key proof for the beginning and end of the
    /// range, as well as all key/value pairs in the range.
    Range = 1,
    /// A change proof for all key/value pairs that changed between two
    /// versions of the tree.
    ///
    /// A change proof includes a key proof for the beginning and end of the
    /// changed range, as well as all key/value pairs that changed.
    Change = 2,
}

impl ProofType {
    /// Parse a byte into a [`ProofType`].
    #[must_use]
    pub const fn new(v: u8) -> Option<Self> {
        match v {
            0 => Some(ProofType::Single),
            1 => Some(ProofType::Range),
            2 => Some(ProofType::Change),
            _ => None,
        }
    }

    /// Human readable name for the [`ProofType`]
    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            ProofType::Single => "single",
            ProofType::Range => "range",
            ProofType::Change => "change",
        }
    }
}
