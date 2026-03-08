// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Block replay recording for FFI operations.
//!
//! When the `block-replay` feature is enabled and the `FIREWOOD_BLOCK_REPLAY_PATH`
//! environment variable is set, this module records all database operations
//! passing through the FFI layer to a file for later replay.
//!
//! The recording is length-prefixed messagepack segments, compatible with the
//! `firewood-replay` crate's replay engine.

use std::collections::HashMap;
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::path::PathBuf;
use std::sync::OnceLock;

use firewood_replay::{
    Batch, Commit, DbOperation, GetFromProposal, GetLatest, KeyValueOp, ProposalId, ProposeOnDB,
    ProposeOnProposal, ReplayLog,
};
use parking_lot::Mutex;

use crate::value::{BatchOp, BorrowedBatchOps, BorrowedBytes};

/// Environment variable that controls the output path for the replay log.
const REPLAY_PATH_ENV: &str = "FIREWOOD_BLOCK_REPLAY_PATH";

/// Number of operations to buffer before flushing to disk.
const FLUSH_THRESHOLD: usize = 10_000;

/// The global recorder instance. `None` if recording is disabled.
static RECORDER: OnceLock<Option<Mutex<Recorder>>> = OnceLock::new();

/// Internal state for recording operations.
struct Recorder {
    /// Buffered operations awaiting flush.
    operations: Vec<DbOperation>,
    /// Counter for assigning proposal IDs.
    next_proposal_id: u64,
    /// Map from proposal handle pointer addresses to assigned IDs.
    proposal_ids: HashMap<usize, ProposalId>,
    /// Output path for the replay log.
    output_path: PathBuf,
}

impl Recorder {
    fn new(output_path: PathBuf) -> Self {
        Self {
            operations: Vec::new(),
            next_proposal_id: 1, // Start from 1 to make 0 stand out as invalid
            proposal_ids: HashMap::new(),
            output_path,
        }
    }

    /// Records a `GetLatest` operation.
    fn record_get_latest(&mut self, key: &[u8]) {
        self.operations
            .push(DbOperation::GetLatest(GetLatest { key: key.into() }));
        self.maybe_flush();
    }

    /// Records a `GetFromProposal` operation.
    fn record_get_from_proposal(&mut self, handle_ptr: usize, key: &[u8]) {
        let Some(&proposal_id) = self.proposal_ids.get(&handle_ptr) else {
            return;
        };
        self.operations
            .push(DbOperation::GetFromProposal(GetFromProposal {
                proposal_id,
                key: key.into(),
            }));
        self.maybe_flush();
    }

    /// Records a `Batch` operation.
    fn record_batch(&mut self, ops: &[BatchOp<'_>]) {
        let pairs = convert_ops(ops);
        self.operations.push(DbOperation::Batch(Batch { pairs }));
        self.maybe_flush();
    }

    /// Records a `ProposeOnDB` operation.
    fn record_propose_on_db(&mut self, handle_ptr: usize, ops: &[BatchOp<'_>]) {
        let proposal_id = ProposalId(self.next_proposal_id);
        self.next_proposal_id = self.next_proposal_id.saturating_add(1);
        self.proposal_ids.insert(handle_ptr, proposal_id);

        let pairs = convert_ops(ops);
        self.operations.push(DbOperation::ProposeOnDB(ProposeOnDB {
            pairs,
            returned_proposal_id: proposal_id,
        }));
        self.maybe_flush();
    }

    /// Records a `ProposeOnProposal` operation.
    fn record_propose_on_proposal(
        &mut self,
        parent_ptr: usize,
        new_ptr: usize,
        ops: &[BatchOp<'_>],
    ) {
        let Some(&parent_id) = self.proposal_ids.get(&parent_ptr) else {
            return;
        };

        let new_id = ProposalId(self.next_proposal_id);
        self.next_proposal_id = self.next_proposal_id.saturating_add(1);
        self.proposal_ids.insert(new_ptr, new_id);

        let pairs = convert_ops(ops);
        self.operations
            .push(DbOperation::ProposeOnProposal(ProposeOnProposal {
                proposal_id: parent_id,
                pairs,
                returned_proposal_id: new_id,
            }));
        self.maybe_flush();
    }

    /// Records a `Commit` operation.
    fn record_commit(&mut self, handle_ptr: usize, returned_hash: Option<&[u8]>) {
        let Some(&proposal_id) = self.proposal_ids.get(&handle_ptr) else {
            return;
        };
        // Remove the proposal from tracking since it's now committed
        self.proposal_ids.remove(&handle_ptr);

        self.operations.push(DbOperation::Commit(Commit {
            proposal_id,
            returned_hash: returned_hash.map(Into::into),
        }));
        self.maybe_flush();
    }

    /// Flushes if the buffer exceeds the threshold.
    fn maybe_flush(&mut self) {
        if self.operations.len() >= FLUSH_THRESHOLD {
            let _ = self.flush_to_disk();
        }
    }

    /// Flushes buffered operations to disk.
    fn flush_to_disk(&mut self) -> io::Result<()> {
        if self.operations.is_empty() {
            return Ok(());
        }

        let operations = std::mem::take(&mut self.operations);
        let log = ReplayLog::new(operations);

        let bytes = log
            .to_msgpack()
            .map_err(|e| io::Error::other(format!("failed to serialize replay log: {e}")))?;

        if let Some(parent) = self.output_path.parent() {
            fs::create_dir_all(parent)?;
        }

        let mut file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.output_path)?;

        let len: u64 = bytes
            .len()
            .try_into()
            .map_err(|_| io::Error::other("replay segment too large"))?;

        file.write_all(&len.to_le_bytes())?;
        file.write_all(&bytes)?;

        Ok(())
    }
}

/// Converts FFI batch operations to replay log format.
fn convert_ops(ops: &[BatchOp<'_>]) -> Vec<KeyValueOp> {
    ops.iter()
        .map(|op| match op {
            BatchOp::Put { key, value } => KeyValueOp {
                key: key.as_slice().into(),
                value: Some(value.as_slice().into()),
            },
            BatchOp::Delete { key } => KeyValueOp {
                key: key.as_slice().into(),
                value: None,
            },
            BatchOp::DeleteRange { prefix } => KeyValueOp {
                key: prefix.as_slice().into(),
                value: None,
            },
        })
        .collect()
}

/// Returns the recorder if recording is enabled.
fn recorder() -> Option<&'static Mutex<Recorder>> {
    RECORDER
        .get_or_init(|| {
            std::env::var(REPLAY_PATH_ENV)
                .ok()
                .map(|p| Mutex::new(Recorder::new(PathBuf::from(p))))
        })
        .as_ref()
}

/// Records a `fwd_get_latest` call.
pub(crate) fn record_get_latest(key: BorrowedBytes<'_>) {
    if let Some(rec) = recorder() {
        rec.lock().record_get_latest(key.as_slice());
    }
}

/// Records a `fwd_get_from_proposal` call.
pub(crate) fn record_get_from_proposal(
    handle: Option<&crate::ProposalHandle<'_>>,
    key: BorrowedBytes<'_>,
) {
    let Some(rec) = recorder() else { return };
    let Some(handle) = handle else { return };

    let ptr = std::ptr::from_ref(handle) as usize;
    rec.lock().record_get_from_proposal(ptr, key.as_slice());
}

/// Records a `fwd_batch` call.
pub(crate) fn record_batch(values: BorrowedBatchOps<'_>) {
    if let Some(rec) = recorder() {
        rec.lock().record_batch(values.as_slice());
    }
}

/// Records a `fwd_propose_on_db` call after the proposal is created.
pub(crate) fn record_propose_on_db(
    result: &crate::ProposalResult<'_>,
    values: BorrowedBatchOps<'_>,
) {
    let Some(rec) = recorder() else { return };
    let crate::ProposalResult::Ok { handle, .. } = result else {
        return;
    };

    let ptr = std::ptr::from_ref(&**handle) as usize;
    rec.lock().record_propose_on_db(ptr, values.as_slice());
}

/// Records a `fwd_propose_on_proposal` call after the proposal is created.
pub(crate) fn record_propose_on_proposal<'db>(
    parent: Option<&crate::ProposalHandle<'db>>,
    result: &crate::ProposalResult<'db>,
    values: BorrowedBatchOps<'_>,
) {
    let Some(rec) = recorder() else { return };
    let Some(parent) = parent else { return };
    let crate::ProposalResult::Ok { handle, .. } = result else {
        return;
    };

    let parent_ptr = std::ptr::from_ref(parent) as usize;
    let new_ptr = std::ptr::from_ref(&**handle) as usize;
    rec.lock()
        .record_propose_on_proposal(parent_ptr, new_ptr, values.as_slice());
}

/// Records a `fwd_commit_proposal` call after the commit completes.
pub(crate) fn record_commit(
    proposal_ptr: Option<*const crate::ProposalHandle<'_>>,
    result: &crate::HashResult,
) {
    let Some(rec) = recorder() else { return };
    let Some(ptr) = proposal_ptr else { return };

    let returned_hash_bytes = match result {
        crate::HashResult::Some(hash) => {
            let api_hash: firewood::v2::api::HashKey = (*hash).into();
            let bytes: [u8; 32] = api_hash.into();
            Some(bytes)
        }
        crate::HashResult::None => None,
        _ => return,
    };

    rec.lock().record_commit(
        ptr as usize,
        returned_hash_bytes.as_ref().map(AsRef::as_ref),
    );
}

/// Flushes any buffered operations to disk.
///
/// Called automatically when the database is closed, but can also be
/// invoked manually via `fwd_block_replay_flush`.
pub(crate) fn flush_to_disk() -> io::Result<()> {
    // we don't use recorder() here because flushing should not init
    // the recorder. TestMain opens and closes a db, that causes
    // some tests to fail.
    // TODO[AMIN]: this should change when we make record db-specific.
    if let Some(Some(rec)) = RECORDER.get() {
        rec.lock().flush_to_disk()
    } else {
        Ok(())
    }
}
