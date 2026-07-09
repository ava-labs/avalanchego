// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// MaxHashesPerRequest caps the hashes per request.
const MaxHashesPerRequest = 5

// RegisterHandler serves code-by-hash requests at [p2p.EVMCodeRequestHandlerID] on net.
func RegisterHandler(net *p2p.Network, log logging.Logger, codeReader ethdb.KeyValueReader) error {
	h := handlers.NewHandler(
		log,
		func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} },
		newResponder(codeReader),
	)
	return net.AddHandler(p2p.EVMCodeRequestHandlerID, h)
}

var _ handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse] = (*responder)(nil)

// responder reads code by hash via [rawdb.ReadCode].
type responder struct {
	codeReader ethdb.KeyValueReader
}

func newResponder(codeReader ethdb.KeyValueReader) *responder {
	return &responder{codeReader: codeReader}
}

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, error) {
	hashes := req.GetHashes()
	if len(hashes) > MaxHashesPerRequest {
		log.Debug("too many hashes requested, dropping request", "nodeID", nodeID, "numHashes", len(hashes))
		return nil, nil
	}

	data := make([][]byte, len(hashes))
	seen := make(map[common.Hash]struct{}, len(hashes))
	for i, raw := range hashes {
		hash := common.BytesToHash(raw)
		// A duplicate hash drops the whole request by design rather than being de-duplicated.
		if _, ok := seen[hash]; ok {
			log.Debug("duplicate code hashes requested, dropping request", "nodeID", nodeID)
			return nil, nil
		}
		seen[hash] = struct{}{}

		data[i] = rawdb.ReadCode(r.codeReader, hash)
		if len(data[i]) == 0 {
			log.Debug("requested code not found, dropping request", "nodeID", nodeID, "hash", hash)
			return nil, nil
		}
	}

	return &syncpb.GetCodeResponse{Data: data}, nil
}
