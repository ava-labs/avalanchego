// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	// replayPathEnv is the environment variable that controls recording.
	// When set, FFI operations are recorded to the specified path.
	// This must match REPLAY_PATH_ENV in ffi/src/replay.rs.
	replayPathEnv = "FIREWOOD_BLOCK_REPLAY_PATH"

	// replayLogEnv is the environment variable for the replay log path.
	replayLogEnv = "REPLAY_LOG"

	// replayMaxCommitsEnv is the environment variable for limiting
	// number of executed commits.
	// If empty, defaults to 10000. Set to 0 for unlimited.
	replayMaxCommitsEnv = "REPLAY_MAX_COMMITS"
)

// replayLog mirrors the Rust ReplayLog type.
type replayLog struct {
	Operations []dbOperation `msgpack:"operations"`
}

// dbOperation is the Go representation of the Rust DbOperation enum.
// msgpack will populate exactly one of the pointer fields below.
type dbOperation struct {
	GetLatest         *getLatest         `msgpack:"GetLatest,omitempty"`
	GetFromProposal   *getFromProposal   `msgpack:"GetFromProposal,omitempty"`
	Batch             *replayBatch       `msgpack:"Batch,omitempty"`
	ProposeOnDB       *proposeOnDB       `msgpack:"ProposeOnDB,omitempty"`
	ProposeOnProposal *proposeOnProposal `msgpack:"ProposeOnProposal,omitempty"`
	Commit            *commit            `msgpack:"Commit,omitempty"`
}

// getLatest represents a read from the latest revision.
type getLatest struct {
	Key []byte `msgpack:"key"`
}

// getFromProposal represents a read from an uncommitted proposal.
type getFromProposal struct {
	ProposalID uint64 `msgpack:"proposal_id"`
	Key        []byte `msgpack:"key"`
}

// keyValueOp represents a single key/value mutation.
type keyValueOp struct {
	Key   []byte `msgpack:"key"`
	Value []byte `msgpack:"value"` // nil represents delete-range
}

// replayBatch represents a batch operation that commits immediately.
type replayBatch struct {
	Pairs []keyValueOp `msgpack:"pairs"`
}

// proposeOnDB represents a proposal created on the database.
type proposeOnDB struct {
	Pairs              []keyValueOp `msgpack:"pairs"`
	ReturnedProposalID uint64       `msgpack:"returned_proposal_id"`
}

// proposeOnProposal represents a proposal created on another proposal.
type proposeOnProposal struct {
	ProposalID         uint64       `msgpack:"proposal_id"`
	Pairs              []keyValueOp `msgpack:"pairs"`
	ReturnedProposalID uint64       `msgpack:"returned_proposal_id"`
}

// commit represents a commit operation for a proposal.
type commit struct {
	ProposalID   uint64 `msgpack:"proposal_id"`
	ReturnedHash []byte `msgpack:"returned_hash"` // nil when absent
}

// TestReplayLogExecution reads a length-prefixed MessagePack replay log
// and replays it against a fresh Firewood database using the Go FFI bindings.
//
// Environment variables:
//   - REPLAY_LOG: path to the replay log (required)
//   - REPLAY_MAX_COMMITS: max commits to replay (default: 10000, 0 for unlimited)
func TestReplayLogExecution(t *testing.T) {
	r := require.New(t)

	logPath := os.Getenv(replayLogEnv)
	if logPath == "" {
		t.Skipf("%s not set; skipping replay execution test", replayLogEnv)
	}

	maxCommits := 10000
	if v := os.Getenv(replayMaxCommitsEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxCommits = n
		}
	}

	logs, err := loadReplayLogs(filepath.Clean(logPath), maxCommits)
	if err != nil {
		t.Skipf("unable to read replay log %q: %v", logPath, err)
	}
	r.NotEmpty(logs, "expected at least one replay segment")

	db := newTestDatabase(t, WithTruncate(true))

	start := time.Now()
	commits, err := applyReplayLogs(db, logs, replayConfig{MaxCommits: maxCommits, VerifyHashes: true})
	r.NoError(err, "replay logs against database")
	elapsed := time.Since(start)

	root := db.Root()
	r.NotEqual(EmptyRoot, root, "root should not be EmptyRoot after replay")

	t.Logf("Replay completed in %v (%d commits), final root: %x", elapsed, commits, root)
}

// BenchmarkReplayLog benchmarks the replay of recorded operations.
//
// Environment variables:
//   - REPLAY_LOG: path to the replay log (required)
//   - REPLAY_MAX_COMMITS: max commits to replay (default: 10000, 0 for unlimited)
//
// Run with: REPLAY_LOG=/path/to/log go test -bench=BenchmarkReplayLog -benchtime=1x
func BenchmarkReplayLog(b *testing.B) {
	r := require.New(b)
	logPath := os.Getenv(replayLogEnv)
	if logPath == "" {
		b.Skipf("%s not set; skipping replay benchmark", replayLogEnv)
	}

	maxCommits := 10000
	if v := os.Getenv(replayMaxCommitsEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxCommits = n
		}
	}

	logs, err := loadReplayLogs(filepath.Clean(logPath), maxCommits)
	if err != nil {
		b.Skipf("unable to read replay log: %v", err)
	}
	r.NotEmpty(logs, "expected at least one replay segment")

	commits := 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := newTestDatabase(b, WithTruncate(true))
		b.StartTimer()

		commits, err = applyReplayLogs(db, logs, replayConfig{MaxCommits: maxCommits})
		r.NoError(err, "replay failed")
	}

	b.ReportMetric(float64(commits), "commits")
}

// TestBlockReplayRoundTrip records operations, then replays them to a new database.
func TestBlockReplayRoundTrip(t *testing.T) {
	r := require.New(t)

	replayLogPath := filepath.Join(t.TempDir(), "roundtrip.log")

	t.Setenv(replayPathEnv, replayLogPath)

	// Phase 1: Record operations
	db1 := newTestDatabase(t)

	_, _, batch := kvForTest(20)

	p1, err := db1.Propose(batch[:10])
	r.NoError(err)
	r.NoError(p1.Commit())

	p2, err := db1.Propose(batch[10:])
	r.NoError(err)
	r.NoError(p2.Commit())

	originalRoot := db1.Root()

	r.NoError(FlushBlockReplay())
	r.NoError(db1.Close(oneSecCtx(t)))

	// Verify log was created
	_, err = os.Stat(replayLogPath)
	if os.IsNotExist(err) {
		t.Skip("Replay log not created - block-replay feature may not be enabled")
	}
	r.NoError(err)

	// Phase 2: Replay to new database
	r.NoError(os.Unsetenv(replayPathEnv)) // Don't record replay operations

	logs, err := loadReplayLogs(replayLogPath, 0)
	r.NoError(err)
	r.NotEmpty(logs)

	db2 := newTestDatabase(t)

	_, err = applyReplayLogs(db2, logs, replayConfig{VerifyHashes: true})
	r.NoError(err)

	replayedRoot := db2.Root()
	r.Equal(originalRoot, replayedRoot, "replayed database should have same root hash")
}

// decodeReplayLogs decodes length-prefixed MessagePack segments from data.
// If maxCommits > 0, stops loading once enough commits are found.
func decodeReplayLogs(data []byte, maxCommits int) ([]replayLog, error) {
	var logs []replayLog
	buf := bytes.NewReader(data)
	totalCommits := 0

	for {
		var segLen uint64
		if err := binary.Read(buf, binary.LittleEndian, &segLen); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("reading segment length: %w", err)
		}

		if segLen == 0 {
			continue
		}
		if uint64(buf.Len()) < segLen {
			return nil, fmt.Errorf("segment length %d exceeds remaining buffer %d", segLen, buf.Len())
		}

		seg := make([]byte, segLen)
		if _, err := io.ReadFull(buf, seg); err != nil {
			return nil, fmt.Errorf("reading segment payload: %w", err)
		}

		var log replayLog
		if err := msgpack.Unmarshal(seg, &log); err != nil {
			return nil, fmt.Errorf("decoding segment as replayLog: %w", err)
		}
		logs = append(logs, log)

		// Check if we have enough commits
		if maxCommits > 0 {
			for _, op := range log.Operations {
				if op.Commit != nil {
					totalCommits++
				}
			}
			if totalCommits >= maxCommits {
				break
			}
		}
	}

	return logs, nil
}

// replayConfig controls replay behavior.
type replayConfig struct {
	// MaxCommits limits the number of commits to replay. 0 means unlimited.
	MaxCommits int
	// VerifyHashes enables verification of returned hashes after commits.
	VerifyHashes bool
}

// applyReplayLogs applies replay logs to a database.
// Returns the number of commits applied and any error encountered.
func applyReplayLogs(db *Database, logs []replayLog, cfg replayConfig) (int, error) {
	proposals := make(map[uint64]*Proposal)
	totalCommits := 0

	for _, segment := range logs {
		for _, op := range segment.Operations {
			switch {
			case op.GetLatest != nil:
				if _, err := db.Get(op.GetLatest.Key); err != nil {
					return totalCommits, fmt.Errorf("GetLatest: %w", err)
				}

			case op.GetFromProposal != nil:
				prop, ok := proposals[op.GetFromProposal.ProposalID]
				if !ok {
					return totalCommits, fmt.Errorf("GetFromProposal: unknown proposal id %d", op.GetFromProposal.ProposalID)
				}
				if _, err := prop.Get(op.GetFromProposal.Key); err != nil {
					return totalCommits, fmt.Errorf("GetFromProposal: %w", err)
				}

			case op.Batch != nil:
				batch := batchFromReplayPairs(op.Batch.Pairs)
				if _, err := db.Update(batch); err != nil {
					return totalCommits, fmt.Errorf("Batch: %w", err)
				}

			case op.ProposeOnDB != nil:
				batch := batchFromReplayPairs(op.ProposeOnDB.Pairs)
				prop, err := db.Propose(batch)
				if err != nil {
					return totalCommits, fmt.Errorf("ProposeOnDB: %w", err)
				}
				proposals[op.ProposeOnDB.ReturnedProposalID] = prop

			case op.ProposeOnProposal != nil:
				parent, ok := proposals[op.ProposeOnProposal.ProposalID]
				if !ok {
					return totalCommits, fmt.Errorf("ProposeOnProposal: unknown parent proposal id %d", op.ProposeOnProposal.ProposalID)
				}
				batch := batchFromReplayPairs(op.ProposeOnProposal.Pairs)
				prop, err := parent.Propose(batch)
				if err != nil {
					return totalCommits, fmt.Errorf("ProposeOnProposal: %w", err)
				}
				proposals[op.ProposeOnProposal.ReturnedProposalID] = prop

			case op.Commit != nil:
				prop, ok := proposals[op.Commit.ProposalID]
				if !ok {
					return totalCommits, fmt.Errorf("Commit: unknown proposal id %d", op.Commit.ProposalID)
				}
				delete(proposals, op.Commit.ProposalID)
				if err := prop.Commit(); err != nil {
					return totalCommits, fmt.Errorf("Commit: %w", err)
				}

				if cfg.VerifyHashes && op.Commit.ReturnedHash != nil {
					root := db.Root()
					if !bytes.Equal(op.Commit.ReturnedHash, root[:]) {
						return totalCommits, fmt.Errorf("root hash mismatch: expected %x, got %x", op.Commit.ReturnedHash, root[:])
					}
				}
				totalCommits++
				if cfg.MaxCommits > 0 && totalCommits >= cfg.MaxCommits {
					return totalCommits, nil
				}

			default:
				return totalCommits, fmt.Errorf("unknown or empty DbOperation: %+v", op)
			}
		}
	}

	return totalCommits, nil
}

// loadReplayLogs reads and decodes replay logs from a file path.
// If maxCommits > 0, stops loading once enough commits are found.
func loadReplayLogs(logPath string, maxCommits int) ([]replayLog, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}
	return decodeReplayLogs(data, maxCommits)
}

func batchFromReplayPairs(pairs []keyValueOp) []BatchOp {
	batch := make([]BatchOp, len(pairs))
	for i, p := range pairs {
		if p.Value == nil {
			batch[i] = PrefixDelete(p.Key)
		} else {
			batch[i] = Put(p.Key, p.Value)
		}
	}
	return batch
}
