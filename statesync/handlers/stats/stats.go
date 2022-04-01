// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stats

import (
	"time"

	"github.com/ethereum/go-ethereum/metrics"
)

// HandlerStats reports prometheus metrics for the state sync handlers
type HandlerStats interface {
	// BlockRequestHandler stats
	IncBlockRequest()
	IncMissingBlockHash()
	UpdateBlocksReturned(num uint16)
	UpdateBlockRequestProcessingTime(duration time.Duration)

	// CodeRequestHandler stats
	IncCodeRequest()
	IncMissingCodeHash()
	UpdateCodeReadTime(duration time.Duration)
	UpdateCodeBytesReturned(bytes uint32)

	// LeafsRequestHandler stats
	IncLeafsRequest()
	UpdateLeafsReturned(numLeafs uint16)
	UpdateLeafsRequestProcessingTime(duration time.Duration)
	IncMissingRoot()
}

type handlerStats struct {
	// BlockRequestHandler metrics
	blockRequest               metrics.Counter
	missingBlockHash           metrics.Counter
	blocksReturned             metrics.Histogram
	blockRequestProcessingTime metrics.Timer

	// CodeRequestHandler stats
	codeRequest       metrics.Counter
	missingCodeHash   metrics.Counter
	codeBytesReturned metrics.Histogram
	codeReadDuration  metrics.Timer

	// LeafsRequestHandler stats
	leafsRequest               metrics.Counter
	leafsReturned              metrics.Histogram
	leafsRequestProcessingTime metrics.Timer
	missingRoot                metrics.Counter
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

func (h *handlerStats) UpdateCodeReadTime(duration time.Duration) {
	h.codeReadDuration.Update(duration)
}

func (h *handlerStats) UpdateCodeBytesReturned(bytesLen uint32) {
	h.codeBytesReturned.Update(int64(bytesLen))
}

func (h *handlerStats) IncLeafsRequest() {
	h.leafsRequest.Inc(1)
}

func (h *handlerStats) UpdateLeafsRequestProcessingTime(duration time.Duration) {
	h.leafsRequestProcessingTime.Update(duration)
}

func (h *handlerStats) UpdateLeafsReturned(numLeafs uint16) {
	h.leafsReturned.Update(int64(numLeafs))
}

func (h *handlerStats) IncMissingRoot() {
	h.missingRoot.Inc(1)
}

func NewHandlerStats() HandlerStats {
	return &handlerStats{
		// initialise block request stats
		blockRequest:               metrics.GetOrRegisterCounter("block_request", nil),
		missingBlockHash:           metrics.GetOrRegisterCounter("missing_block_hash", nil),
		blocksReturned:             metrics.GetOrRegisterHistogram("blocks_returned", nil, metrics.NewExpDecaySample(1028, 0.015)),
		blockRequestProcessingTime: metrics.GetOrRegisterTimer("block_request_processing_time", nil),

		// initialize code request stats
		codeRequest:       metrics.GetOrRegisterCounter("code_request", nil),
		missingCodeHash:   metrics.GetOrRegisterCounter("missing_code_hash", nil),
		codeReadDuration:  metrics.GetOrRegisterTimer("code_read_time", nil),
		codeBytesReturned: metrics.GetOrRegisterHistogram("code_bytes_returned", nil, metrics.NewExpDecaySample(1028, 0.015)),

		// initialise leafs request stats
		leafsRequest:               metrics.GetOrRegisterCounter("leafs_request", nil),
		leafsRequestProcessingTime: metrics.GetOrRegisterTimer("leafs_request_processing_time", nil),
		leafsReturned:              metrics.GetOrRegisterHistogram("leafs_returned", nil, metrics.NewExpDecaySample(1028, 0.015)),
		missingRoot:                metrics.GetOrRegisterCounter("missing_root", nil),
	}
}

// no op implementation
type noopHandlerStats struct{}

func NewNoopHandlerStats() HandlerStats {
	return &noopHandlerStats{}
}

// all operations are no-ops
func (n *noopHandlerStats) IncBlockRequest()                               {}
func (n *noopHandlerStats) IncMissingBlockHash()                           {}
func (n *noopHandlerStats) UpdateBlocksReturned(uint16)                    {}
func (n *noopHandlerStats) UpdateBlockRequestProcessingTime(time.Duration) {}
func (n *noopHandlerStats) IncCodeRequest()                                {}
func (n *noopHandlerStats) IncMissingCodeHash()                            {}
func (n *noopHandlerStats) UpdateCodeReadTime(time.Duration)               {}
func (n *noopHandlerStats) UpdateCodeBytesReturned(uint32)                 {}
func (n *noopHandlerStats) IncLeafsRequest()                               {}
func (n *noopHandlerStats) UpdateLeafsRequestProcessingTime(time.Duration) {}
func (n *noopHandlerStats) UpdateLeafsReturned(uint16)                     {}
func (n *noopHandlerStats) IncMissingRoot()                                {}
