// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// MaxCodeHashesPerRequest caps the hashes per request.
const MaxCodeHashesPerRequest = 5

// CodeHandler serves [syncpb.GetCodeRequest] over [p2p.EVMCodeRequestHandlerID].
type CodeHandler = Handler[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// CodeResponder is the inner contract for code-by-hash requests.
type CodeResponder = Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// NewCodeHandler wires resp into a [CodeHandler].
func NewCodeHandler(resp CodeResponder) *CodeHandler {
	return NewHandler(func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} }, resp)
}

var _ CodeResponder = (*codeResponder)(nil)

// codeResponder reads code by hash via [rawdb.ReadCode].
type codeResponder struct {
	codeReader ethdb.KeyValueReader
	stats      CodeStats
}

func NewCodeResponder(codeReader ethdb.KeyValueReader, stats CodeStats) CodeResponder {
	return &codeResponder{codeReader: codeReader, stats: stats}
}

func (r *codeResponder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, error) {
	startTime := time.Now()
	r.stats.IncCodeRequest()
	defer func() { r.stats.UpdateCodeReadTime(time.Since(startTime)) }()

	hashes := req.GetHashes()
	if len(hashes) > MaxCodeHashesPerRequest {
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
