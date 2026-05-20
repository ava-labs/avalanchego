// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// MaxHashesPerRequest caps the hashes per request.
const MaxHashesPerRequest = 5

// Handler serves [syncpb.GetCodeRequest] over [p2p.EVMCodeRequestHandlerID].
type Handler = handlers.Handler[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// Responder serves code-by-hash requests.
type Responder = handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// NewHandler wires resp into a [Handler].
func NewHandler(resp Responder) *Handler {
	return handlers.NewHandler(func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} }, resp)
}

var _ Responder = (*responder)(nil)

// responder reads code by hash via [rawdb.ReadCode].
type responder struct {
	codeReader ethdb.KeyValueReader
	stats      Stats
}

func NewResponder(codeReader ethdb.KeyValueReader, stats Stats) Responder {
	return &responder{codeReader: codeReader, stats: stats}
}

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, error) {
	startTime := time.Now()
	r.stats.IncCodeRequest()
	defer func() { r.stats.UpdateCodeReadTime(time.Since(startTime)) }()

	hashes := req.GetHashes()
	if len(hashes) > MaxHashesPerRequest {
		r.stats.IncTooManyHashesRequested()
		log.Debug("too many hashes requested, dropping request", "nodeID", nodeID, "numHashes", len(hashes))
		return nil, nil
	}
	if !uniqueHashes(hashes) {
		r.stats.IncDuplicateHashesRequested()
		log.Debug("duplicate code hashes requested, dropping request", "nodeID", nodeID)
		return nil, nil
	}

	data := make([][]byte, len(hashes))
	totalBytes := 0
	for i, raw := range hashes {
		hash := common.BytesToHash(raw)
		data[i] = rawdb.ReadCode(r.codeReader, hash)
		if len(data[i]) == 0 {
			r.stats.IncMissingCodeHash()
			log.Debug("requested code not found, dropping request", "nodeID", nodeID, "hash", hash)
			return nil, nil
		}
		totalBytes += len(data[i])
	}

	r.stats.UpdateCodeBytesReturned(uint32(totalBytes))
	return &syncpb.GetCodeResponse{Data: data}, nil
}

// uniqueHashes reports whether hashes has no duplicates.
func uniqueHashes(hashes [][]byte) bool {
	if len(hashes) <= 1 {
		return true
	}
	seen := make(map[common.Hash]struct{}, len(hashes))
	for _, raw := range hashes {
		h := common.BytesToHash(raw)
		if _, ok := seen[h]; ok {
			return false
		}
		seen[h] = struct{}{}
	}
	return true
}

// Stats reports [responder] metrics.
type Stats interface {
	IncCodeRequest()
	IncMissingCodeHash()
	IncTooManyHashesRequested()
	IncDuplicateHashesRequested()
	UpdateCodeReadTime(time.Duration)
	UpdateCodeBytesReturned(uint32)
}

// NoopStats discards every [Stats] event.
type NoopStats struct{}

func (NoopStats) IncCodeRequest()                  {}
func (NoopStats) IncMissingCodeHash()              {}
func (NoopStats) IncTooManyHashesRequested()       {}
func (NoopStats) IncDuplicateHashesRequested()     {}
func (NoopStats) UpdateCodeReadTime(time.Duration) {}
func (NoopStats) UpdateCodeBytesReturned(uint32)   {}
