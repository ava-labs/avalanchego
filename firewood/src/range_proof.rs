// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::proof::Proof;

/// A range proof proves that a given set of key-value pairs
/// are in the trie with a given root hash.
#[derive(Debug)]
pub struct RangeProof<K: AsRef<[u8]>, V: AsRef<[u8]>, H> {
    #[expect(dead_code)]
    pub(crate) start_proof: Option<Proof<H>>,
    #[expect(dead_code)]
    pub(crate) end_proof: Option<Proof<H>>,
    #[expect(dead_code)]
    pub(crate) key_values: Box<[(K, V)]>,
}
