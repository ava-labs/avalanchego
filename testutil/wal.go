// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/wal"
	"github.com/stretchr/testify/require"
)

type TestWAL struct {
	common.WriteAheadLog
	t      *testing.T
	lock   sync.Mutex
	signal sync.Cond
	// failNextAppend, if set, is the error returned by the next Append of a record of failNextRecordType.
	failNextAppend     error
	failNextRecordType uint16
}

func NewTestWAL(t *testing.T) *TestWAL {
	var tw TestWAL
	tw.WriteAheadLog = wal.NewMemWAL(t)
	tw.signal = sync.Cond{L: &tw.lock}
	tw.t = t
	return &tw
}

func (tw *TestWAL) Clone() *TestWAL {
	rawWAL, err := tw.ReadAll()
	require.NoError(tw.t, err)

	wal := NewTestWAL(tw.t)

	for _, entry := range rawWAL {
		require.NoError(tw.t, wal.Append(entry))
	}

	return wal
}

// Delete discards the entries of the WAL, as deleting a real WAL file would.
func (tw *TestWAL) Delete() error {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	tw.WriteAheadLog = wal.NewMemWAL(tw.t)
	return nil
}

// FailNextAppend makes the next Append of a record of the given type fail with err without writing it,
// simulating a transient disk fault. Later appends succeed.
func (tw *TestWAL) FailNextAppend(recordType uint16, err error) {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	tw.failNextAppend = err
	tw.failNextRecordType = recordType
}

func (tw *TestWAL) Append(b []byte) error {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	if tw.failNextAppend != nil && len(b) >= 2 && binary.BigEndian.Uint16(b[:2]) == tw.failNextRecordType {
		err := tw.failNextAppend
		tw.failNextAppend = nil
		return err
	}

	err := tw.WriteAheadLog.Append(b)
	tw.signal.Signal()
	return err
}

func (tw *TestWAL) ReadAll() ([][]byte, error) {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	return tw.WriteAheadLog.ReadAll()
}

func (tw *TestWAL) AssertWALSize(n int) {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	for {
		rawRecords, err := tw.WriteAheadLog.ReadAll()
		require.NoError(tw.t, err)

		if len(rawRecords) == n {
			return
		}

		tw.signal.Wait()
	}
}

func (tw *TestWAL) AssertNotarization(round uint64) uint16 {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	for {
		rawRecords, err := tw.WriteAheadLog.ReadAll()
		require.NoError(tw.t, err)

		for _, rawRecord := range rawRecords {
			if binary.BigEndian.Uint16(rawRecord[:2]) == common.NotarizationRecordType {
				_, vote, err := common.ParseNotarizationRecord(rawRecord)
				require.NoError(tw.t, err)

				if vote.Round == round {
					return common.NotarizationRecordType
				}
			}
			if binary.BigEndian.Uint16(rawRecord[:2]) == common.EmptyNotarizationRecordType {
				_, vote, err := common.ParseEmptyNotarizationRecord(rawRecord)
				require.NoError(tw.t, err)

				if vote.Round == round {
					return common.EmptyNotarizationRecordType
				}
			}
		}

		tw.signal.Wait()
	}

}

func (tw *TestWAL) AssertEmptyVote(round uint64) {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	for {
		rawRecords, err := tw.WriteAheadLog.ReadAll()
		require.NoError(tw.t, err)

		for _, rawRecord := range rawRecords {
			if binary.BigEndian.Uint16(rawRecord[:2]) == common.EmptyVoteRecordType {
				vote, err := common.ParseEmptyVoteRecord(rawRecord)
				require.NoError(tw.t, err)

				if vote.Round == round {
					return
				}
			}
		}

		tw.signal.Wait()
	}

}

func (tw *TestWAL) AssertBlockProposal(round uint64) {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	for {
		rawRecords, err := tw.WriteAheadLog.ReadAll()
		require.NoError(tw.t, err)

		for _, rawRecord := range rawRecords {
			if binary.BigEndian.Uint16(rawRecord[:2]) == common.BlockRecordType {
				bh, _, err := common.ParseBlockRecord(rawRecord)
				require.NoError(tw.t, err)

				if bh.Round == round {
					return
				}
			}
		}

		tw.signal.Wait()
	}

}

func (tw *TestWAL) ContainsNotarization(round uint64) bool {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	rawRecords, err := tw.WriteAheadLog.ReadAll()
	require.NoError(tw.t, err)

	for _, rawRecord := range rawRecords {
		if binary.BigEndian.Uint16(rawRecord[:2]) == common.NotarizationRecordType {
			_, vote, err := common.ParseNotarizationRecord(rawRecord)
			require.NoError(tw.t, err)

			if vote.Round == round {
				return true
			}
		}
	}

	return false
}

func (tw *TestWAL) ContainsEmptyVote(round uint64) bool {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	rawRecords, err := tw.WriteAheadLog.ReadAll()
	require.NoError(tw.t, err)

	for _, rawRecord := range rawRecords {
		if binary.BigEndian.Uint16(rawRecord[:2]) == common.EmptyVoteRecordType {
			vote, err := common.ParseEmptyVoteRecord(rawRecord)
			require.NoError(tw.t, err)

			if vote.Round == round {
				return true
			}
		}
	}

	return false
}

func (tw *TestWAL) ContainsEmptyNotarization(round uint64) bool {
	tw.lock.Lock()
	defer tw.lock.Unlock()

	rawRecords, err := tw.WriteAheadLog.ReadAll()
	require.NoError(tw.t, err)

	for _, rawRecord := range rawRecords {
		if binary.BigEndian.Uint16(rawRecord[:2]) == common.EmptyNotarizationRecordType {
			_, vote, err := common.ParseEmptyNotarizationRecord(rawRecord)
			require.NoError(tw.t, err)

			if vote.Round == round {
				return true
			}
		}
	}

	return false
}

type walRound struct {
	round                   uint64
	blockRecord             bool
	notarizationRecord      bool
	finalizationRecord      bool
	emptyNotarizationRecord bool
	emptyVoteRecord         bool
}

// AssertHealthy checks that the WAL has at most one of each record type per round.
func (tw *TestWAL) AssertHealthy(bd common.BlockDeserializer, qcd common.QCDeserializer) {
	ctx := context.Background()
	records, err := tw.ReadAll()
	require.NoError(tw.t, err)

	rounds := make(map[uint64]*walRound)

	for _, r := range records {
		recordType := binary.BigEndian.Uint16(r)

		switch recordType {
		case common.BlockRecordType:
			block, err := common.BlockFromRecord(ctx, bd, r)
			require.NoError(tw.t, err)
			round := block.BlockHeader().Round
			if _, exists := rounds[round]; !exists {
				rounds[round] = &walRound{round: round}
			}
			require.False(tw.t, rounds[round].blockRecord, "duplicate block record for round %d", round)
			rounds[round].blockRecord = true
		case common.NotarizationRecordType:
			_, vote, err := common.ParseNotarizationRecord(r)
			require.NoError(tw.t, err)
			round := vote.Round
			if _, exists := rounds[round]; !exists {
				rounds[round] = &walRound{round: round}
			}
			require.False(tw.t, rounds[round].notarizationRecord, "duplicate notarization record for round %d", round)
			rounds[round].notarizationRecord = true
		case common.EmptyNotarizationRecordType:
			_, vote, err := common.ParseEmptyNotarizationRecord(r)
			require.NoError(tw.t, err)
			round := vote.Round
			if _, exists := rounds[round]; !exists {
				rounds[round] = &walRound{round: round}
			}
			require.False(tw.t, rounds[round].emptyNotarizationRecord, "duplicate empty notarization record for round %d", round)
			rounds[round].emptyNotarizationRecord = true
		case common.EmptyVoteRecordType:
			vote, err := common.ParseEmptyVoteRecord(r)
			require.NoError(tw.t, err)
			round := vote.Round
			if _, exists := rounds[round]; !exists {
				rounds[round] = &walRound{round: round}
			}
			require.False(tw.t, rounds[round].emptyVoteRecord, "duplicate empty vote record for round %d", round)
			rounds[round].emptyVoteRecord = true
		case common.FinalizationRecordType:
			finalization, err := common.FinalizationFromRecord(r, qcd)
			require.NoError(tw.t, err)
			round := finalization.Finalization.Round
			if _, exists := rounds[round]; !exists {
				rounds[round] = &walRound{round: round}
			}
			require.False(tw.t, rounds[round].finalizationRecord, "duplicate finalization record for round %d", round)
			rounds[round].finalizationRecord = true
		default:
			tw.t.Fatalf("undefined record type: %d", recordType)
		}
	}
}
