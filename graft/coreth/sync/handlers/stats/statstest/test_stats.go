// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statstest

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/sync/handlers/stats"
)

var _ stats.HandlerStats = (*TestHandlerStats)(nil)

// TestHandlerStats is test for capturing and asserting on handler metrics in test
type TestHandlerStats struct {
	lock sync.Mutex

	BlockRequestCount,
	MissingBlockHashCount,
	BlocksReturnedSum uint32
	BlockRequestProcessingTimeSum time.Duration

	CodeRequestCount,
	MissingCodeHashCount,
	TooManyHashesRequested,
	DuplicateHashesRequested,
	CodeBytesReturnedSum uint32
	CodeReadTimeSum time.Duration

	LeafsRequestCount,
	InvalidLeafsRequestCount,
	LeafsReturnedSum,
	MissingRootCount,
	TrieErrorCount,
	ProofErrorCount,
	SnapshotReadErrorCount,
	SnapshotReadAttemptCount,
	SnapshotReadSuccessCount,
	SnapshotSegmentValidCount,
	SnapshotSegmentInvalidCount uint32
	ProofValsReturned int64
	LeafsReadTime,
	SnapshotReadTime,
	GenerateRangeProofTime,
	LeafRequestProcessingTimeSum time.Duration
}

func (m *TestHandlerStats) Reset() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlockRequestCount = 0
	m.MissingBlockHashCount = 0
	m.BlocksReturnedSum = 0
	m.BlockRequestProcessingTimeSum = 0
	m.CodeRequestCount = 0
	m.MissingCodeHashCount = 0
	m.TooManyHashesRequested = 0
	m.DuplicateHashesRequested = 0
	m.CodeBytesReturnedSum = 0
	m.CodeReadTimeSum = 0
	m.LeafsRequestCount = 0
	m.InvalidLeafsRequestCount = 0
	m.LeafsReturnedSum = 0
	m.MissingRootCount = 0
	m.TrieErrorCount = 0
	m.ProofErrorCount = 0
	m.SnapshotReadErrorCount = 0
	m.SnapshotReadAttemptCount = 0
	m.SnapshotReadSuccessCount = 0
	m.SnapshotSegmentValidCount = 0
	m.SnapshotSegmentInvalidCount = 0
	m.ProofValsReturned = 0
	m.LeafsReadTime = 0
	m.SnapshotReadTime = 0
	m.GenerateRangeProofTime = 0
	m.LeafRequestProcessingTimeSum = 0
}

func (m *TestHandlerStats) IncBlockRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlockRequestCount++
}

func (m *TestHandlerStats) IncMissingBlockHash() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingBlockHashCount++
}

func (m *TestHandlerStats) UpdateBlocksReturned(num uint16) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlocksReturnedSum += uint32(num)
}

func (m *TestHandlerStats) UpdateBlockRequestProcessingTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlockRequestProcessingTimeSum += duration
}

func (m *TestHandlerStats) IncCodeRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeRequestCount++
}

func (m *TestHandlerStats) IncMissingCodeHash() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingCodeHashCount++
}

func (m *TestHandlerStats) IncTooManyHashesRequested() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.TooManyHashesRequested++
}

func (m *TestHandlerStats) IncDuplicateHashesRequested() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.DuplicateHashesRequested++
}

func (m *TestHandlerStats) UpdateCodeReadTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeReadTimeSum += duration
}

func (m *TestHandlerStats) UpdateCodeBytesReturned(bytes uint32) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeBytesReturnedSum += bytes
}

func (m *TestHandlerStats) IncLeafsRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsRequestCount++
}

func (m *TestHandlerStats) IncInvalidLeafsRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.InvalidLeafsRequestCount++
}

func (m *TestHandlerStats) UpdateLeafsReturned(numLeafs uint16) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsReturnedSum += uint32(numLeafs)
}

func (m *TestHandlerStats) UpdateLeafsRequestProcessingTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafRequestProcessingTimeSum += duration
}

func (m *TestHandlerStats) UpdateReadLeafsTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsReadTime += duration
}

func (m *TestHandlerStats) UpdateGenerateRangeProofTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.GenerateRangeProofTime += duration
}

func (m *TestHandlerStats) UpdateSnapshotReadTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadTime += duration
}

func (m *TestHandlerStats) UpdateRangeProofValsReturned(numProofVals int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.ProofValsReturned += numProofVals
}

func (m *TestHandlerStats) IncMissingRoot() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingRootCount++
}

func (m *TestHandlerStats) IncTrieError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.TrieErrorCount++
}

func (m *TestHandlerStats) IncProofError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.ProofErrorCount++
}

func (m *TestHandlerStats) IncSnapshotReadError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadErrorCount++
}

func (m *TestHandlerStats) IncSnapshotReadAttempt() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadAttemptCount++
}

func (m *TestHandlerStats) IncSnapshotReadSuccess() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadSuccessCount++
}

func (m *TestHandlerStats) IncSnapshotSegmentValid() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotSegmentValidCount++
}

func (m *TestHandlerStats) IncSnapshotSegmentInvalid() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotSegmentInvalidCount++
}
