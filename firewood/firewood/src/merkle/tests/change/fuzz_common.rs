// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Helpers shared by fuzz tests: proof reconstruction from tampered parts,
//! the rejected-by-either-check predicate, and serialization round-trips.

use super::verify_and_check;
use crate::api::{BatchOp, FrozenChangeProof, FrozenRangeProof, HashKey};
use crate::db::Db;
use crate::merkle::{Key, Value};
use crate::{ChangeProof, Proof, ProofNode, verify_change_proof_structure};
use firewood_storage::{NodeHashAlgorithm, SeededRng};

/// Build a `FrozenChangeProof` from mutated parts.
pub(in crate::merkle::tests) fn build_change_proof(
    start: Vec<ProofNode>,
    end: Vec<ProofNode>,
    ops: Vec<BatchOp<Key, Value>>,
) -> FrozenChangeProof {
    ChangeProof::new(
        Proof::new(start.into_boxed_slice()),
        Proof::new(end.into_boxed_slice()),
        ops.into_boxed_slice(),
    )
}

/// Build a `FrozenRangeProof` from mutated parts.
pub(in crate::merkle::tests) fn build_range_proof(
    start: Vec<ProofNode>,
    end: Vec<ProofNode>,
    kvs: Vec<(Key, Value)>,
) -> FrozenRangeProof {
    FrozenRangeProof::new(
        Proof::new(start.into_boxed_slice()),
        Proof::new(end.into_boxed_slice()),
        kvs.into_boxed_slice(),
    )
}

/// Whether a change proof is rejected by the structural check or, failing
/// that, the root-hash check (its proposal won't match `end_root`).
pub(in crate::merkle::tests) fn change_proof_rejected(
    db: &Db,
    proof: &FrozenChangeProof,
    start_root: &HashKey,
    end_root: &HashKey,
    first: Option<&[u8]>,
    last: Option<&[u8]>,
) -> bool {
    // This module is ethhash-gated (only the ethhash-shaped fuzzer uses it),
    // so every proof it sees is Ethereum-mode.
    match verify_change_proof_structure(
        proof,
        end_root.clone(),
        first,
        last,
        NodeHashAlgorithm::Ethereum,
        None,
    ) {
        Err(_) => true,
        Ok(ctx) => verify_and_check(db, proof, &ctx, start_root.clone()).is_err(),
    }
}

/// Serialize and deserialize one proof in ten before verification, so the
/// serialization path is fuzzed alongside verification.
pub(in crate::merkle::tests) fn maybe_serialize_round_trip_change(
    rng: &SeededRng,
    proof: FrozenChangeProof,
) -> FrozenChangeProof {
    if rng.random_range(0..10_u32) == 0 {
        let mut buf = Vec::new();
        proof.write_to_vec(&mut buf);
        FrozenChangeProof::from_slice(&buf).expect("round-trip deserialization should succeed")
    } else {
        proof
    }
}

/// Range-proof twin of [`maybe_serialize_round_trip_change`].
pub(in crate::merkle::tests) fn maybe_serialize_round_trip_range(
    rng: &SeededRng,
    proof: FrozenRangeProof,
) -> FrozenRangeProof {
    if rng.random_range(0..10_u32) == 0 {
        let mut buf = Vec::new();
        proof.write_to_vec(&mut buf);
        FrozenRangeProof::from_slice(&buf).expect("round-trip deserialization should succeed")
    } else {
        proof
    }
}
