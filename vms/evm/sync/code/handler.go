// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

// maxHashesPerRequest caps the hashes per request so a response of that many
// max-size contracts stays within the p2p message limit, with headroom for
// proto framing and the other message fields.
const maxHashesPerRequest = constants.MaxContainersLen / params.MaxCodeSize

// RegisterHandler serves code-by-hash requests at [p2p.EVMCodeRequestHandlerID] on net.
func RegisterHandler(log logging.Logger, net *p2p.Network, codeReader ethdb.KeyValueReader) error {
	h := handlers.NewHandler(
		log,
		func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} },
		newResponder(log, codeReader),
	)
	return net.AddHandler(p2p.EVMCodeRequestHandlerID, h)
}

var _ handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse] = (*responder)(nil)

// responder reads code by hash via [rawdb.ReadCode].
type responder struct {
	log        logging.Logger
	codeReader ethdb.KeyValueReader
}

func newResponder(log logging.Logger, codeReader ethdb.KeyValueReader) *responder {
	return &responder{log: log, codeReader: codeReader}
}

func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, error) {
	hashes := req.GetHashes()
	if len(hashes) > maxHashesPerRequest {
		r.log.Debug("too many hashes requested, dropping request",
			zap.Stringer("nodeID", nodeID),
			zap.Int("numHashes", len(hashes)),
		)
		return nil, nil
	}

	data := make([][]byte, len(hashes))
	for i, raw := range hashes {
		hash := common.BytesToHash(raw)
		data[i] = rawdb.ReadCode(r.codeReader, hash)
		if len(data[i]) == 0 {
			r.log.Debug("requested code not found, dropping request",
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("hash", hash),
			)
			return nil, nil
		}
	}

	return &syncpb.GetCodeResponse{Data: data}, nil
}
