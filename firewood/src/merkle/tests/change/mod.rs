// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;
use crate::api::{self, BatchOp, Db as DbTrait, DbView, FrozenChangeProof, Proposal as _};
use crate::db::{Db, DbConfig};
use crate::merkle::verify_change_proof_root_hash;
use crate::{ChangeProofVerificationContext, verify_change_proof_structure};

// ── Test infrastructure ────────────────────────────────────────────────────

pub(super) fn new_db() -> (Db, tempfile::TempDir) {
    let dir = tempfile::tempdir().unwrap();
    let db = Db::new(dir.path(), DbConfig::builder().build()).unwrap();
    (db, dir)
}

/// Create a new database pre-populated with key/value pairs.
///
/// ```rust,no_run
/// let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1")];
/// ```
macro_rules! setup_db {
    [] => {{
        let (db, dir) = new_db();
        db.propose(vec![] as Vec<BatchOp<&[u8], &[u8]>>)
            .unwrap()
            .commit()
            .unwrap();
        (db, dir)
    }};
    [$(($key:expr, $val:expr)),+ $(,)?] => {{
        let (db, dir) = new_db();
        db.propose(vec![$(BatchOp::Put { key: $key as &[u8], value: $val as &[u8] }),+])
            .unwrap()
            .commit()
            .unwrap();
        (db, dir)
    }};
}

/// Commit a second batch of Puts to `$db`, returning `(root_before, root_after)`.
///
/// ```rust,no_run
/// let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);
/// ```
macro_rules! setup_2nd_commit {
    ($db:expr, [$(($key:expr, $val:expr)),+ $(,)?]) => {{
        let root1 = $db.root_hash().unwrap();
        $db.propose(vec![$(BatchOp::Put { key: $key as &[u8], value: $val as &[u8] }),+])
            .unwrap()
            .commit()
            .unwrap();
        let root2 = $db.root_hash().unwrap();
        (root1, root2)
    }};
}

/// Create two databases with the same initial key/value pairs, returning
/// both databases and the target's root hash.
///
/// ```rust,no_run
/// let (source, target, root1_target, _ds, _dt) =
///     setup_source_target![(b"\x10", b"v0"), (b"\x20", b"v1")];
/// ```
macro_rules! setup_source_target {
    [$(($key:expr, $val:expr)),+ $(,)?] => {{
        let (source, dir_s) = setup_db![$(($key, $val)),+];
        let (target, dir_t) = setup_db![$(($key, $val)),+];
        let root1 = target.root_hash().unwrap();
        (source, target, root1, dir_s, dir_t)
    }};
}

/// Verify a change proof end-to-end: structural check + root hash check.
///
/// Builds a proposal (`start_root` + `batch_ops`) and verifies its in-range
/// state matches `end_root` using the restructure approach: an in-memory
/// proving trie is built from the proposal's in-range keys, boundary
/// proof nodes are reconciled into it, and a hybrid root hash is computed.
pub(super) fn verify_and_check(
    db: &Db,
    proof: &FrozenChangeProof,
    verification: &ChangeProofVerificationContext,
    start_root: api::HashKey,
) -> Result<(), api::Error> {
    let parent = db.revision(start_root)?;
    let proposal = db.apply_change_proof_to_parent(proof, &*parent)?;
    verify_change_proof_root_hash(proof, verification, &proposal)
}

// ── Test modules ──────────────────────────────────────────────────────────

mod attack;
mod bounds;
mod edge_cases;
// Empty start trie tests require ethhash: without it, the empty trie has no
// root hash, so change proofs from an empty database can't be generated.
#[cfg(feature = "ethhash")]
mod empty;
mod partial;
mod structural;
