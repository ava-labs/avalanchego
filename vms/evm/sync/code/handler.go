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
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// RegisterHandler serves code-by-hash requests at [p2p.EVMCodeRequestHandlerID] on net.
func RegisterHandler(log logging.Logger, net *p2p.Network, codeReader ethdb.KeyValueReader) error {
	h := handlers.NewHandler(log, newResponder(log, codeReader))
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

// maxHashesPerRequest caps the hashes per request so that a response of
// contracts up to [params.MaxCodeSize] fits in one message.
//
// TODO(powerslider): Oversized genesis code can exceed the message limit.
// Either explicitly disallow genesis code larger than MaxCodeSize or
// accommodate large code.
const maxHashesPerRequest = constants.MaxContainersLen / params.MaxCodeSize

var (
	errTooManyHashes = &avacommon.AppError{
		Code:    1000,
		Message: "too many code hashes requested",
	}
	errHashNotFound = &avacommon.AppError{
		Code:    1001,
		Message: "requested code not found",
	}
)

// Respond answers every requested hash, or rejects the whole request.
func (r *responder) Respond(_ context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, *avacommon.AppError) {
	hashes := req.GetHashes()
	if len(hashes) > maxHashesPerRequest {
		r.log.Debug("rejecting request",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "too many hashes"),
			zap.Int("numHashes", len(hashes)),
		)
		return nil, errTooManyHashes
	}

	data := make([][]byte, len(hashes))
	for i, raw := range hashes {
		hash := common.BytesToHash(raw)
		data[i] = rawdb.ReadCode(r.codeReader, hash)
		if len(data[i]) == 0 {
			r.log.Debug("rejecting request",
				zap.Stringer("nodeID", nodeID),
				zap.String("reason", "code not found"),
				zap.Stringer("hash", hash),
			)
			return nil, errHashNotFound
		}
	}

	return &syncpb.GetCodeResponse{Data: data}, nil
}
