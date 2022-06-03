// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"time"

	"github.com/ava-labs/coreth/metrics"
)

// HandlerStats reports prometheus metrics for the state sync handlers
type HandlerStats interface {
	BlockRequestHandlerStats
	CodeRequestHandlerStats
	LeafsRequestHandlerStats
}

type BlockRequestHandlerStats interface {
	IncBlockRequest()
	IncMissingBlockHash()
	UpdateBlocksReturned(num uint16)
	UpdateBlockRequestProcessingTime(duration time.Duration)
}

type CodeRequestHandlerStats interface {
	IncCodeRequest()
	IncMissingCodeHash()
	IncTooManyHashesRequested()
	IncDuplicateHashesRequested()
	UpdateCodeReadTime(duration time.Duration)
	UpdateCodeBytesReturned(bytes uint32)
}

type LeafsRequestHandlerStats interface {
	IncLeafsRequest()
	IncInvalidLeafsRequest()
	UpdateLeafsReturned(numLeafs uint16)
	UpdateLeafsRequestProcessingTime(duration time.Duration)
	UpdateReadLeafsTime(duration time.Duration)
	UpdateSnapshotReadTime(duration time.Duration)
	UpdateGenerateRangeProofTime(duration time.Duration)
	UpdateRangeProofKeysReturned(numProofKeys int64)
	IncMissingRoot()
	IncTrieError()
	IncProofError()
	IncSnapshotReadError()
	IncSnapshotReadAttempt()
	IncSnapshotReadSuccess()
	IncSnapshotSegmentValid()
	IncSnapshotSegmentInvalid()
}

type handlerStats struct {
	// BlockRequestHandler metrics
	blockRequest               metrics.Counter
	missingBlockHash           metrics.Counter
	blocksReturned             metrics.Histogram
	blockRequestProcessingTime metrics.Timer

	// CodeRequestHandler stats
	codeRequest              metrics.Counter
	missingCodeHash          metrics.Counter
	tooManyHashesRequested   metrics.Counter
	duplicateHashesRequested metrics.Counter
	codeBytesReturned        metrics.Histogram
	codeReadDuration         metrics.Timer

	// LeafsRequestHandler stats
	leafsRequest               metrics.Counter
	invalidLeafsRequest        metrics.Counter
	leafsReturned              metrics.Histogram
	leafsRequestProcessingTime metrics.Timer
	leafsReadTime              metrics.Timer
	snapshotReadTime           metrics.Timer
	generateRangeProofTime     metrics.Timer
	proofKeysReturned          metrics.Histogram
	missingRoot                metrics.Counter
	trieError                  metrics.Counter
	proofError                 metrics.Counter
	snapshotReadError          metrics.Counter
	snapshotReadAttempt        metrics.Counter
	snapshotReadSuccess        metrics.Counter
	snapshotSegmentValid       metrics.Counter
	snapshotSegmentInvalid     metrics.Counter
}

func (h *handlerStats) IncBlockRequest() {
	h.blockRequest.Inc(1)
}

func (h *handlerStats) IncMissingBlockHash() {
	h.missingBlockHash.Inc(1)
}

func (h *handlerStats) UpdateBlocksReturned(num uint16) {
	h.blocksReturned.Update(int64(num))
}

func (h *handlerStats) UpdateBlockRequestProcessingTime(duration time.Duration) {
	h.blockRequestProcessingTime.Update(duration)
}

func (h *handlerStats) IncCodeRequest() {
	h.codeRequest.Inc(1)
}

func (h *handlerStats) IncMissingCodeHash() {
	h.missingCodeHash.Inc(1)
}

func (h *handlerStats) IncTooManyHashesRequested() {
	h.tooManyHashesRequested.Inc(1)
}

func (h *handlerStats) IncDuplicateHashesRequested() {
	h.duplicateHashesRequested.Inc(1)
}

func (h *handlerStats) UpdateCodeReadTime(duration time.Duration) {
	h.codeReadDuration.Update(duration)
}

func (h *handlerStats) UpdateCodeBytesReturned(bytesLen uint32) {
	h.codeBytesReturned.Update(int64(bytesLen))
}

func (h *handlerStats) IncLeafsRequest() {
	h.leafsRequest.Inc(1)
}

func (h *handlerStats) IncInvalidLeafsRequest() {
	h.invalidLeafsRequest.Inc(1)
}

func (h *handlerStats) UpdateLeafsRequestProcessingTime(duration time.Duration) {
	h.leafsRequestProcessingTime.Update(duration)
}

func (h *handlerStats) UpdateLeafsReturned(numLeafs uint16) {
	h.leafsReturned.Update(int64(numLeafs))
}

func (h *handlerStats) UpdateReadLeafsTime(duration time.Duration) {
	h.leafsReadTime.Update(duration)
}

func (h *handlerStats) UpdateSnapshotReadTime(duration time.Duration) {
	h.snapshotReadTime.Update(duration)
}

func (h *handlerStats) UpdateGenerateRangeProofTime(duration time.Duration) {
	h.generateRangeProofTime.Update(duration)
}

func (h *handlerStats) UpdateRangeProofKeysReturned(numProofKeys int64) {
	h.proofKeysReturned.Update(numProofKeys)
}

func (h *handlerStats) IncMissingRoot()            { h.missingRoot.Inc(1) }
func (h *handlerStats) IncTrieError()              { h.trieError.Inc(1) }
func (h *handlerStats) IncProofError()             { h.proofError.Inc(1) }
func (h *handlerStats) IncSnapshotReadError()      { h.snapshotReadError.Inc(1) }
func (h *handlerStats) IncSnapshotReadAttempt()    { h.snapshotReadAttempt.Inc(1) }
func (h *handlerStats) IncSnapshotReadSuccess()    { h.snapshotReadSuccess.Inc(1) }
func (h *handlerStats) IncSnapshotSegmentValid()   { h.snapshotSegmentValid.Inc(1) }
func (h *handlerStats) IncSnapshotSegmentInvalid() { h.snapshotSegmentInvalid.Inc(1) }

func NewHandlerStats(enabled bool) HandlerStats {
	if !enabled {
		return NewNoopHandlerStats()
	}
	return &handlerStats{
		// initialise block request stats
		blockRequest:               metrics.GetOrRegisterCounter("block_request_count", nil),
		missingBlockHash:           metrics.GetOrRegisterCounter("block_request_missing_block_hash", nil),
		blocksReturned:             metrics.GetOrRegisterHistogram("block_request_total_blocks", nil, metrics.NewExpDecaySample(1028, 0.015)),
		blockRequestProcessingTime: metrics.GetOrRegisterTimer("block_request_processing_time", nil),

		// initialize code request stats
		codeRequest:              metrics.GetOrRegisterCounter("code_request_count", nil),
		missingCodeHash:          metrics.GetOrRegisterCounter("code_request_missing_code_hash", nil),
		tooManyHashesRequested:   metrics.GetOrRegisterCounter("code_request_too_many_hashes", nil),
		duplicateHashesRequested: metrics.GetOrRegisterCounter("code_request_duplicate_hashes", nil),
		codeReadDuration:         metrics.GetOrRegisterTimer("code_request_read_time", nil),
		codeBytesReturned:        metrics.GetOrRegisterHistogram("code_request_bytes_returned", nil, metrics.NewExpDecaySample(1028, 0.015)),

		// initialise leafs request stats
		leafsRequest:               metrics.GetOrRegisterCounter("leafs_request_count", nil),
		invalidLeafsRequest:        metrics.GetOrRegisterCounter("leafs_request_invalid", nil),
		leafsRequestProcessingTime: metrics.GetOrRegisterTimer("leafs_request_processing_time", nil),
		leafsReturned:              metrics.GetOrRegisterHistogram("leafs_request_total_leafs", nil, metrics.NewExpDecaySample(1028, 0.015)),
		leafsReadTime:              metrics.GetOrRegisterTimer("leafs_read_time", nil),
		snapshotReadTime:           metrics.GetOrRegisterTimer("snapshot_read_time", nil),
		generateRangeProofTime:     metrics.GetOrRegisterTimer("generate_range_proof_time", nil),
		proofKeysReturned:          metrics.GetOrRegisterHistogram("proof_keys_returned", nil, metrics.NewExpDecaySample(1028, 0.015)),
		missingRoot:                metrics.GetOrRegisterCounter("leafs_request_missing_root", nil),
		trieError:                  metrics.GetOrRegisterCounter("leafs_request_trie_error", nil),
		proofError:                 metrics.GetOrRegisterCounter("leafs_request_proof_error", nil),
		snapshotReadError:          metrics.GetOrRegisterCounter("snapshot_read_error", nil),
		snapshotReadAttempt:        metrics.GetOrRegisterCounter("snapshot_read_attempt", nil),
		snapshotReadSuccess:        metrics.GetOrRegisterCounter("snapshot_read_success", nil),
		snapshotSegmentValid:       metrics.GetOrRegisterCounter("snapshot_segment_valid", nil),
		snapshotSegmentInvalid:     metrics.GetOrRegisterCounter("snapshot_segment_invalid", nil),
	}
}

// no op implementation
type noopHandlerStats struct{}

func NewNoopHandlerStats() HandlerStats {
	return &noopHandlerStats{}
}

// all operations are no-ops
func (n *noopHandlerStats) IncBlockRequest()                                    {}
func (n *noopHandlerStats) IncMissingBlockHash()                                {}
func (n *noopHandlerStats) UpdateBlocksReturned(uint16)                         {}
func (n *noopHandlerStats) UpdateBlockRequestProcessingTime(time.Duration)      {}
func (n *noopHandlerStats) IncCodeRequest()                                     {}
func (n *noopHandlerStats) IncMissingCodeHash()                                 {}
func (n *noopHandlerStats) IncTooManyHashesRequested()                          {}
func (n *noopHandlerStats) IncDuplicateHashesRequested()                        {}
func (n *noopHandlerStats) UpdateCodeReadTime(time.Duration)                    {}
func (n *noopHandlerStats) UpdateCodeBytesReturned(uint32)                      {}
func (n *noopHandlerStats) IncLeafsRequest()                                    {}
func (n *noopHandlerStats) IncInvalidLeafsRequest()                             {}
func (n *noopHandlerStats) UpdateLeafsRequestProcessingTime(time.Duration)      {}
func (n *noopHandlerStats) UpdateLeafsReturned(uint16)                          {}
func (n *noopHandlerStats) UpdateReadLeafsTime(duration time.Duration)          {}
func (n *noopHandlerStats) UpdateSnapshotReadTime(duration time.Duration)       {}
func (n *noopHandlerStats) UpdateGenerateRangeProofTime(duration time.Duration) {}
func (n *noopHandlerStats) UpdateRangeProofKeysReturned(numProofKeys int64)     {}
func (n *noopHandlerStats) IncMissingRoot()                                     {}
func (n *noopHandlerStats) IncTrieError()                                       {}
func (n *noopHandlerStats) IncProofError()                                      {}
func (n *noopHandlerStats) IncSnapshotReadError()                               {}
func (n *noopHandlerStats) IncSnapshotReadAttempt()                             {}
func (n *noopHandlerStats) IncSnapshotReadSuccess()                             {}
func (n *noopHandlerStats) IncSnapshotSegmentValid()                            {}
func (n *noopHandlerStats) IncSnapshotSegmentInvalid()                          {}
