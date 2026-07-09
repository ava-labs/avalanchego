// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// MaxHashesPerRequest caps the hashes per request so a response of that many
// max-size contracts stays within the p2p message limit, with headroom for
// proto framing and the other message fields.
const MaxHashesPerRequest = constants.MaxContainersLen / params.MaxCodeSize

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
	for i, raw := range hashes {
		hash := common.BytesToHash(raw)
		data[i] = rawdb.ReadCode(r.codeReader, hash)
		if len(data[i]) == 0 {
			log.Debug("requested code not found, dropping request", "nodeID", nodeID, "hash", hash)
			return nil, nil
		}
	}

	return &syncpb.GetCodeResponse{Data: data}, nil
}
