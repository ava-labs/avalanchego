// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statstest

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/sync/handlers/stats"
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

func (t *TestHandlerStats) Reset() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.BlockRequestCount = 0
	t.MissingBlockHashCount = 0
	t.BlocksReturnedSum = 0
	t.BlockRequestProcessingTimeSum = 0
	t.CodeRequestCount = 0
	t.MissingCodeHashCount = 0
	t.TooManyHashesRequested = 0
	t.DuplicateHashesRequested = 0
	t.CodeBytesReturnedSum = 0
	t.CodeReadTimeSum = 0
	t.LeafsRequestCount = 0
	t.InvalidLeafsRequestCount = 0
	t.LeafsReturnedSum = 0
	t.MissingRootCount = 0
	t.TrieErrorCount = 0
	t.ProofErrorCount = 0
	t.SnapshotReadErrorCount = 0
	t.SnapshotReadAttemptCount = 0
	t.SnapshotReadSuccessCount = 0
	t.SnapshotSegmentValidCount = 0
	t.SnapshotSegmentInvalidCount = 0
	t.ProofValsReturned = 0
	t.LeafsReadTime = 0
	t.SnapshotReadTime = 0
	t.GenerateRangeProofTime = 0
	t.LeafRequestProcessingTimeSum = 0
}

func (t *TestHandlerStats) IncBlockRequest() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.BlockRequestCount++
}

func (t *TestHandlerStats) IncMissingBlockHash() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.MissingBlockHashCount++
}

func (t *TestHandlerStats) UpdateBlocksReturned(num uint16) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.BlocksReturnedSum += uint32(num)
}

func (t *TestHandlerStats) UpdateBlockRequestProcessingTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.BlockRequestProcessingTimeSum += duration
}

func (t *TestHandlerStats) IncCodeRequest() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.CodeRequestCount++
}

func (t *TestHandlerStats) IncMissingCodeHash() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.MissingCodeHashCount++
}

func (t *TestHandlerStats) IncTooManyHashesRequested() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.TooManyHashesRequested++
}

func (t *TestHandlerStats) IncDuplicateHashesRequested() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.DuplicateHashesRequested++
}

func (t *TestHandlerStats) UpdateCodeReadTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.CodeReadTimeSum += duration
}

func (t *TestHandlerStats) UpdateCodeBytesReturned(bytes uint32) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.CodeBytesReturnedSum += bytes
}

func (t *TestHandlerStats) IncLeafsRequest() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.LeafsRequestCount++
}

func (t *TestHandlerStats) IncInvalidLeafsRequest() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.InvalidLeafsRequestCount++
}

func (t *TestHandlerStats) UpdateLeafsReturned(numLeafs uint16) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.LeafsReturnedSum += uint32(numLeafs)
}

func (t *TestHandlerStats) UpdateLeafsRequestProcessingTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.LeafRequestProcessingTimeSum += duration
}

func (t *TestHandlerStats) UpdateReadLeafsTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.LeafsReadTime += duration
}

func (t *TestHandlerStats) UpdateGenerateRangeProofTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.GenerateRangeProofTime += duration
}

func (t *TestHandlerStats) UpdateSnapshotReadTime(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotReadTime += duration
}

func (t *TestHandlerStats) UpdateRangeProofValsReturned(numProofVals int64) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.ProofValsReturned += numProofVals
}

func (t *TestHandlerStats) IncMissingRoot() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.MissingRootCount++
}

func (t *TestHandlerStats) IncTrieError() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.TrieErrorCount++
}

func (t *TestHandlerStats) IncProofError() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.ProofErrorCount++
}

func (t *TestHandlerStats) IncSnapshotReadError() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotReadErrorCount++
}

func (t *TestHandlerStats) IncSnapshotReadAttempt() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotReadAttemptCount++
}

func (t *TestHandlerStats) IncSnapshotReadSuccess() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotReadSuccessCount++
}

func (t *TestHandlerStats) IncSnapshotSegmentValid() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotSegmentValidCount++
}

func (t *TestHandlerStats) IncSnapshotSegmentInvalid() {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.SnapshotSegmentInvalidCount++
}
