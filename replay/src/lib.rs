// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Replay log types and engine for applying them to a Database.
//!
//! This crate provides:
//! - Serializable types representing database operations ([`DbOperation`])
//! - An engine to replay operations against a [`Db`] instance
//!
//! The log format uses length-prefixed `MessagePack` segments, allowing streaming
//! writes and reads without loading the entire log into memory.
//!
//! # Wire Format
//!
//! Each segment is written as:
//! ```text
//! [len: u64 little-endian][msgpack-serialized ReplayLog bytes]
//! ```
//!
//! A file may contain multiple segments appended sequentially.

use std::collections::HashMap;
use std::io::{self, Read};
use std::time::Instant;

use firewood::db::{BatchOp, Db, Proposal};
use firewood::v2::api::{self, Db as DbApi, DbView as DbViewApi, Proposal as ProposalApi};
use firewood_metrics::firewood_increment;
use firewood_storage::InvalidTrieHashLength;

pub mod registry;
use serde::{Deserialize, Serialize};
use serde_with::serde_as;
use thiserror::Error;

/// Strongly-typed proposal identifier.
///
/// Wraps a `u64` to prevent accidental misuse of raw integers as proposal IDs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct ProposalId(pub u64);

/// Operation that reads a key from the latest committed revision.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GetLatest {
    /// The key to read.
    #[serde(with = "serde_bytes")]
    pub key: Box<[u8]>,
}

/// Operation that reads a key from an uncommitted proposal.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GetFromProposal {
    /// The proposal ID assigned during recording.
    pub proposal_id: ProposalId,
    /// The key to read.
    #[serde(with = "serde_bytes")]
    pub key: Box<[u8]>,
}

/// A single key/value mutation within a batch or proposal.
#[serde_as]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KeyValueOp {
    /// The key being mutated.
    #[serde(with = "serde_bytes")]
    pub key: Box<[u8]>,
    /// The value to set, or `None` for a delete-range operation.
    #[serde_as(as = "Option<serde_with::Bytes>")]
    pub value: Option<Box<[u8]>>,
}

/// Batch operation that commits immediately.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Batch {
    /// The key/value operations in this batch.
    pub pairs: Vec<KeyValueOp>,
}

/// Proposal created directly on the database.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProposeOnDB {
    /// The key/value operations in this proposal.
    pub pairs: Vec<KeyValueOp>,
    /// The proposal ID assigned to the returned handle.
    pub returned_proposal_id: ProposalId,
}

/// Proposal created on top of an existing uncommitted proposal.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProposeOnProposal {
    /// The parent proposal ID.
    pub proposal_id: ProposalId,
    /// The key/value operations in this proposal.
    pub pairs: Vec<KeyValueOp>,
    /// The proposal ID assigned to the returned handle.
    pub returned_proposal_id: ProposalId,
}

/// Commit operation for a proposal.
#[serde_as]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Commit {
    /// The proposal ID being committed.
    pub proposal_id: ProposalId,
    /// The root hash returned by the commit, if any.
    #[serde_as(as = "Option<serde_with::Bytes>")]
    pub returned_hash: Option<Box<[u8]>>,
}

/// All supported database operations that can be recorded and replayed.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum DbOperation {
    /// Read from the latest revision.
    GetLatest(GetLatest),
    /// Read from an uncommitted proposal.
    GetFromProposal(GetFromProposal),
    /// Batch write (immediate commit).
    Batch(Batch),
    /// Create a proposal on the database.
    ProposeOnDB(ProposeOnDB),
    /// Create a proposal on another proposal.
    ProposeOnProposal(ProposeOnProposal),
    /// Commit a proposal.
    Commit(Commit),
}

/// A container for a sequence of database operations.
///
/// Multiple `ReplayLog` instances can be serialized sequentially into a file,
/// each prefixed with its byte length.
#[derive(Debug, Serialize, Deserialize)]
pub struct ReplayLog {
    /// The operations in this log segment.
    pub operations: Vec<DbOperation>,
}

impl ReplayLog {
    /// Creates a new replay log with the given operations.
    #[must_use]
    pub const fn new(operations: Vec<DbOperation>) -> Self {
        Self { operations }
    }

    /// Serializes the log to `MessagePack` format with named fields.
    ///
    /// Uses named field serialization for Go compatibility.
    ///
    /// # Errors
    ///
    /// Returns an error if serialization fails.
    pub fn to_msgpack(&self) -> Result<Vec<u8>, rmp_serde::encode::Error> {
        rmp_serde::to_vec_named(self)
    }
}

/// Errors that can occur when replaying a log against a database.
#[derive(Debug, Error)]
pub enum ReplayError {
    /// An I/O error occurred while reading the replay log.
    #[error("I/O error: {0}")]
    Io(#[from] io::Error),

    /// The log segment could not be deserialized.
    #[error("failed to decode replay segment: {0}")]
    Decode(#[from] rmp_serde::decode::Error),

    /// A database error occurred while applying an operation.
    #[error("database error: {0}")]
    Db(#[from] api::Error),

    /// A root hash in the log had an invalid length.
    #[error("invalid root hash: {0}")]
    InvalidHash(#[from] InvalidTrieHashLength),

    /// The log referenced a proposal ID that was not created or already consumed.
    #[error("unknown proposal ID {}", .0.0)]
    UnknownProposal(ProposalId),
}

/// Alias for the batch operation type used in replay.
type BoxedBatchOp = BatchOp<Box<[u8]>, Box<[u8]>>;

/// Consumes a `Vec` of [`KeyValueOp`] and converts them to firewood batch operations.
///
/// Takes ownership to avoid cloning keys and values in the hot path.
fn into_batch_ops(pairs: Vec<KeyValueOp>) -> Vec<BoxedBatchOp> {
    pairs
        .into_iter()
        .map(|op| match op.value {
            Some(value) => BatchOp::Put { key: op.key, value },
            None => BatchOp::DeleteRange { prefix: op.key },
        })
        .collect()
}

/// Retrieves a proposal reference from the map, returning an error if not found.
fn get_proposal<'a, 'db>(
    proposals: &'a HashMap<ProposalId, Proposal<'db>>,
    id: ProposalId,
) -> Result<&'a Proposal<'db>, ReplayError> {
    proposals.get(&id).ok_or(ReplayError::UnknownProposal(id))
}

/// Removes and returns a proposal from the map, returning an error if not found.
fn take_proposal<'db>(
    proposals: &mut HashMap<ProposalId, Proposal<'db>>,
    id: ProposalId,
) -> Result<Proposal<'db>, ReplayError> {
    proposals
        .remove(&id)
        .ok_or(ReplayError::UnknownProposal(id))
}

/// Applies a single operation to the database.
///
/// Returns the root hash if the operation was a commit that produced one.
fn apply_operation<'db>(
    db: &'db Db,
    proposals: &mut HashMap<ProposalId, Proposal<'db>>,
    operation: DbOperation,
) -> Result<Option<Box<[u8]>>, ReplayError> {
    match operation {
        DbOperation::GetLatest(GetLatest { key }) => {
            if let Some(root) = DbApi::root_hash(db) {
                let view = DbApi::revision(db, root)?;
                let _ = DbViewApi::val(&*view, key)?;
            }
            Ok(None)
        }

        DbOperation::GetFromProposal(GetFromProposal { proposal_id, key }) => {
            let proposal = get_proposal(proposals, proposal_id)?;
            let _ = DbViewApi::val(proposal, key)?;
            Ok(None)
        }

        DbOperation::Batch(Batch { pairs }) => {
            let ops = into_batch_ops(pairs);
            let proposal = DbApi::propose(db, ops)?;
            proposal.commit()?;
            Ok(None)
        }

        DbOperation::ProposeOnDB(ProposeOnDB {
            pairs,
            returned_proposal_id,
        }) => {
            let ops = into_batch_ops(pairs);
            let start = Instant::now();
            let proposal = DbApi::propose(db, ops)?;
            firewood_increment!(registry::PROPOSE_NS, start.elapsed().as_nanos() as u64);
            firewood_increment!(registry::PROPOSE_COUNT, 1);
            proposals.insert(returned_proposal_id, proposal);
            Ok(None)
        }

        DbOperation::ProposeOnProposal(ProposeOnProposal {
            proposal_id,
            pairs,
            returned_proposal_id,
        }) => {
            let ops = into_batch_ops(pairs);
            let start = Instant::now();
            let parent = get_proposal(proposals, proposal_id)?;
            let new_proposal = ProposalApi::propose(parent, ops)?;
            firewood_increment!(registry::PROPOSE_NS, start.elapsed().as_nanos() as u64);
            firewood_increment!(registry::PROPOSE_COUNT, 1);
            proposals.insert(returned_proposal_id, new_proposal);
            Ok(None)
        }

        DbOperation::Commit(Commit {
            proposal_id,
            returned_hash,
        }) => {
            let proposal = take_proposal(proposals, proposal_id)?;
            let start = Instant::now();
            proposal.commit()?;
            firewood_increment!(registry::COMMIT_NS, start.elapsed().as_nanos() as u64);
            firewood_increment!(registry::COMMIT_COUNT, 1);
            Ok(returned_hash)
        }
    }
}

/// Replays all operations from a length-prefixed replay log.
///
/// The log is expected to be a sequence of segments, each formatted as:
/// `[len: u64 LE][message_pack(ReplayLog) bytes]`
///
/// # Arguments
///
/// * `reader` - A reader positioned at the start of the replay log.
/// * `db` - The database to replay operations against.
/// * `max_commits` - Optional limit on the number of commits to replay.
///
/// # Returns
///
/// The root hash from the last commit operation, if any.
///
/// # Errors
///
/// Returns an error if:
/// - An I/O error occurs while reading the log
/// - A log segment cannot be deserialized
/// - A database operation fails
/// - The log references an unknown proposal ID
pub fn replay_from_reader<R: Read>(
    mut reader: R,
    db: &Db,
    max_commits: Option<u64>,
) -> Result<Option<Box<[u8]>>, ReplayError> {
    let mut proposals: HashMap<ProposalId, Proposal<'_>> = HashMap::new();
    let mut last_commit_hash = None;
    let mut commit_count = 0u64;
    let max = max_commits.unwrap_or(u64::MAX);

    loop {
        // Read segment length
        let mut len_buf = [0u8; 8];
        match reader.read_exact(&mut len_buf) {
            Ok(()) => {}
            Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => break,
            Err(e) => return Err(ReplayError::Io(e)),
        }

        let len = u64::from_le_bytes(len_buf);
        if len == 0 {
            continue;
        }

        // Read segment payload
        let mut buf = vec![0u8; len as usize];
        reader.read_exact(&mut buf)?;

        let log: ReplayLog = rmp_serde::from_slice(&buf)?;

        for op in log.operations {
            let is_commit = matches!(op, DbOperation::Commit(_));
            if let Some(hash) = apply_operation(db, &mut proposals, op)? {
                last_commit_hash = Some(hash);
            }
            if is_commit {
                commit_count = commit_count.saturating_add(1);
                if commit_count >= max {
                    return Ok(last_commit_hash);
                }
            }
        }
    }

    Ok(last_commit_hash)
}

/// Replays a log file against the provided database.
///
/// This is a convenience wrapper around [`replay_from_reader`].
///
/// # Errors
///
/// Returns an error if the file cannot be opened or if replay fails.
/// See [`replay_from_reader`] for detailed error conditions.
pub fn replay_from_file(
    path: impl AsRef<std::path::Path>,
    db: &Db,
    max_commits: Option<u64>,
) -> Result<Option<Box<[u8]>>, ReplayError> {
    let file = std::fs::File::open(path)?;
    replay_from_reader(file, db, max_commits)
}

#[cfg(test)]
mod tests {
    use super::*;
    use firewood::db::DbConfig;
    use firewood::manager::RevisionManagerConfig;
    use firewood_storage::NodeHashAlgorithm;
    use std::io::Cursor;
    use tempfile::tempdir;

    fn create_test_db() -> (tempfile::TempDir, Db) {
        let tmpdir = tempdir().expect("create tempdir");
        let db_path = tmpdir.path().join("test.db");
        let cfg = DbConfig::builder()
            .node_hash_algorithm(NodeHashAlgorithm::compile_option())
            .truncate(true)
            .manager(RevisionManagerConfig::builder().build())
            .build();
        let db = Db::new(&db_path, cfg).expect("db creation should succeed");
        (tmpdir, db)
    }

    fn serialize_log(log: &ReplayLog) -> Vec<u8> {
        let bytes = rmp_serde::to_vec(log).expect("serialize");
        let len: u64 = bytes.len().try_into().expect("fits");
        let mut buf = Vec::new();
        buf.extend_from_slice(&len.to_le_bytes());
        buf.extend_from_slice(&bytes);
        buf
    }

    #[test]
    fn replay_batch_inserts_keys() {
        let (_tmpdir, db) = create_test_db();

        let pairs: Vec<KeyValueOp> = (0u8..5)
            .map(|i| KeyValueOp {
                key: vec![i].into_boxed_slice(),
                value: Some(vec![i.wrapping_add(100)].into_boxed_slice()),
            })
            .collect();

        let log = ReplayLog::new(vec![DbOperation::Batch(Batch { pairs })]);
        let buf = serialize_log(&log);

        replay_from_reader(Cursor::new(buf), &db, None).expect("replay");

        let root = DbApi::root_hash(&db).expect("non-empty");
        let view = DbApi::revision(&db, root).expect("revision");

        for i in 0u8..5 {
            let val = DbViewApi::val(&*view, vec![i].as_slice())
                .expect("val")
                .expect("exists");
            assert_eq!(*val, [i.wrapping_add(100)]);
        }
    }

    #[test]
    fn replay_propose_and_commit() {
        let (_tmpdir, db) = create_test_db();

        let pairs: Vec<KeyValueOp> = (0u8..3)
            .map(|i| KeyValueOp {
                key: vec![i].into_boxed_slice(),
                value: Some(vec![i.wrapping_mul(2)].into_boxed_slice()),
            })
            .collect();

        let ops = vec![
            DbOperation::ProposeOnDB(ProposeOnDB {
                pairs,
                returned_proposal_id: ProposalId(1),
            }),
            DbOperation::Commit(Commit {
                proposal_id: ProposalId(1),
                returned_hash: None,
            }),
        ];

        let log = ReplayLog::new(ops);
        let buf = serialize_log(&log);

        replay_from_reader(Cursor::new(buf), &db, None).expect("replay");

        let root = DbApi::root_hash(&db).expect("non-empty");
        let view = DbApi::revision(&db, root).expect("revision");

        for i in 0u8..3 {
            let val = DbViewApi::val(&*view, vec![i].as_slice())
                .expect("val")
                .expect("exists");
            assert_eq!(*val, [i.wrapping_mul(2)]);
        }
    }

    #[test]
    fn replay_chained_proposals() {
        let (_tmpdir, db) = create_test_db();

        let ops = vec![
            DbOperation::ProposeOnDB(ProposeOnDB {
                pairs: vec![KeyValueOp {
                    key: vec![1].into_boxed_slice(),
                    value: Some(vec![10].into_boxed_slice()),
                }],
                returned_proposal_id: ProposalId(1),
            }),
            DbOperation::ProposeOnProposal(ProposeOnProposal {
                proposal_id: ProposalId(1),
                pairs: vec![KeyValueOp {
                    key: vec![2].into_boxed_slice(),
                    value: Some(vec![20].into_boxed_slice()),
                }],
                returned_proposal_id: ProposalId(2),
            }),
            DbOperation::Commit(Commit {
                proposal_id: ProposalId(1),
                returned_hash: None,
            }),
            DbOperation::Commit(Commit {
                proposal_id: ProposalId(2),
                returned_hash: None,
            }),
        ];

        let log = ReplayLog::new(ops);
        let buf = serialize_log(&log);

        replay_from_reader(Cursor::new(buf), &db, None).expect("replay");

        let root = DbApi::root_hash(&db).expect("non-empty");
        let view = DbApi::revision(&db, root).expect("revision");

        let v1 = DbViewApi::val(&*view, [1]).expect("val").expect("exists");
        let v2 = DbViewApi::val(&*view, [2]).expect("val").expect("exists");
        assert_eq!(*v1, [10]);
        assert_eq!(*v2, [20]);
    }

    #[test]
    fn replay_empty_log_succeeds() {
        let (_tmpdir, db) = create_test_db();
        let result = replay_from_reader(Cursor::new(Vec::new()), &db, None);
        assert!(result.is_ok());
        assert!(result.expect("ok").is_none());
    }
}
