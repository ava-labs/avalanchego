// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"sync"
	"time"
)

var _ HandlerStats = &MockHandlerStats{}

// MockHandlerStats is mock for capturing and asserting on handler metrics in test
type MockHandlerStats struct {
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

func (m *MockHandlerStats) Reset() {
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

func (m *MockHandlerStats) IncBlockRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlockRequestCount++
}

func (m *MockHandlerStats) IncMissingBlockHash() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingBlockHashCount++
}

func (m *MockHandlerStats) UpdateBlocksReturned(num uint16) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlocksReturnedSum += uint32(num)
}

func (m *MockHandlerStats) UpdateBlockRequestProcessingTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.BlockRequestProcessingTimeSum += duration
}

func (m *MockHandlerStats) IncCodeRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeRequestCount++
}

func (m *MockHandlerStats) IncMissingCodeHash() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingCodeHashCount++
}

func (m *MockHandlerStats) IncTooManyHashesRequested() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.TooManyHashesRequested++
}

func (m *MockHandlerStats) IncDuplicateHashesRequested() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.DuplicateHashesRequested++
}

func (m *MockHandlerStats) UpdateCodeReadTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeReadTimeSum += duration
}

func (m *MockHandlerStats) UpdateCodeBytesReturned(bytes uint32) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.CodeBytesReturnedSum += bytes
}

func (m *MockHandlerStats) IncLeafsRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsRequestCount++
}

func (m *MockHandlerStats) IncInvalidLeafsRequest() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.InvalidLeafsRequestCount++
}

func (m *MockHandlerStats) UpdateLeafsReturned(numLeafs uint16) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsReturnedSum += uint32(numLeafs)
}

func (m *MockHandlerStats) UpdateLeafsRequestProcessingTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafRequestProcessingTimeSum += duration
}

func (m *MockHandlerStats) UpdateReadLeafsTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.LeafsReadTime += duration
}

func (m *MockHandlerStats) UpdateGenerateRangeProofTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.GenerateRangeProofTime += duration
}

func (m *MockHandlerStats) UpdateSnapshotReadTime(duration time.Duration) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadTime += duration
}

func (m *MockHandlerStats) UpdateRangeProofValsReturned(numProofVals int64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.ProofValsReturned += numProofVals
}

func (m *MockHandlerStats) IncMissingRoot() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.MissingRootCount++
}

func (m *MockHandlerStats) IncTrieError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.TrieErrorCount++
}

func (m *MockHandlerStats) IncProofError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.ProofErrorCount++
}

func (m *MockHandlerStats) IncSnapshotReadError() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadErrorCount++
}

func (m *MockHandlerStats) IncSnapshotReadAttempt() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadAttemptCount++
}

func (m *MockHandlerStats) IncSnapshotReadSuccess() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotReadSuccessCount++
}

func (m *MockHandlerStats) IncSnapshotSegmentValid() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotSegmentValidCount++
}

func (m *MockHandlerStats) IncSnapshotSegmentInvalid() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.SnapshotSegmentInvalidCount++
}
