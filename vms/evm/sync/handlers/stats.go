// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import "time"

// Stats interfaces are invoked from deferred hot-path code.
// Implementations must NOT panic.

// LeafStats is the metrics surface for [leafResponder].
type LeafStats interface {
	IncLeafRequest()
	IncInvalidLeafRequest()
	IncMissingRoot()
	IncTrieError()
	IncProofError()
	IncSnapshotReadAttempt()
	IncSnapshotReadSuccess()
	IncSnapshotReadError()
	IncSnapshotSegmentValid()
	IncSnapshotSegmentInvalid()
	UpdateSnapshotReadTime(time.Duration)
	UpdateLeafRequestProcessingTime(time.Duration)
	UpdateLeafReturned(uint16)
	UpdateRangeProofValsReturned(int64)
	UpdateGenerateRangeProofTime(time.Duration)
	UpdateReadLeafTime(time.Duration)
}

// CodeStats is the metrics surface for [codeResponder].
type CodeStats interface {
	IncCodeRequest()
	IncMissingCodeHash()
	IncTooManyHashesRequested()
	IncDuplicateHashesRequested()
	UpdateCodeReadTime(time.Duration)
	UpdateCodeBytesReturned(uint32)
}

// BlockStats is the metrics surface for [blockResponder].
type BlockStats interface {
	IncBlockRequest()
	IncMissingBlockHash()
	UpdateBlocksReturned(uint16)
	UpdateBlockRequestProcessingTime(time.Duration)
}

// NoopLeafStats discards every [LeafStats] event.
type NoopLeafStats struct{}

func (NoopLeafStats) IncLeafRequest()                               {}
func (NoopLeafStats) IncInvalidLeafRequest()                        {}
func (NoopLeafStats) IncMissingRoot()                               {}
func (NoopLeafStats) IncTrieError()                                 {}
func (NoopLeafStats) IncProofError()                                {}
func (NoopLeafStats) IncSnapshotReadAttempt()                       {}
func (NoopLeafStats) IncSnapshotReadSuccess()                       {}
func (NoopLeafStats) IncSnapshotReadError()                         {}
func (NoopLeafStats) IncSnapshotSegmentValid()                      {}
func (NoopLeafStats) IncSnapshotSegmentInvalid()                    {}
func (NoopLeafStats) UpdateSnapshotReadTime(time.Duration)          {}
func (NoopLeafStats) UpdateLeafRequestProcessingTime(time.Duration) {}
func (NoopLeafStats) UpdateLeafReturned(uint16)                     {}
func (NoopLeafStats) UpdateRangeProofValsReturned(int64)            {}
func (NoopLeafStats) UpdateGenerateRangeProofTime(time.Duration)    {}
func (NoopLeafStats) UpdateReadLeafTime(time.Duration)              {}

// NoopCodeStats discards every [CodeStats] event.
type NoopCodeStats struct{}

func (NoopCodeStats) IncCodeRequest()                  {}
func (NoopCodeStats) IncMissingCodeHash()              {}
func (NoopCodeStats) IncTooManyHashesRequested()       {}
func (NoopCodeStats) IncDuplicateHashesRequested()     {}
func (NoopCodeStats) UpdateCodeReadTime(time.Duration) {}
func (NoopCodeStats) UpdateCodeBytesReturned(uint32)   {}

// NoopBlockStats discards every [BlockStats] event.
type NoopBlockStats struct{}

func (NoopBlockStats) IncBlockRequest()                               {}
func (NoopBlockStats) IncMissingBlockHash()                           {}
func (NoopBlockStats) UpdateBlocksReturned(uint16)                    {}
func (NoopBlockStats) UpdateBlockRequestProcessingTime(time.Duration) {}
