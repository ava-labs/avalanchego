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

// maxHashesPerRequest caps the hashes per request, sized for contracts within
// MaxCodeSize. Oversized genesis code can still outgrow the message limit.
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
	errCodeTooLarge = &avacommon.AppError{
		Code:    1002,
		Message: "requested code exceeds the message size limit",
	}
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
	sizeBudget int // caps the response, so an outgrown batch is a prefix, not nothing
}

func newResponder(log logging.Logger, codeReader ethdb.KeyValueReader) *responder {
	return &responder{log: log, codeReader: codeReader, sizeBudget: constants.MaxContainersLen}
}

// Respond answers an in-order prefix of hashes, stopping before the response
// would outgrow sizeBudget. A shorter response is valid, the client resumes.
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

	var (
		data = make([][]byte, 0, len(hashes))
		size int
	)
	for _, raw := range hashes {
		hash := common.BytesToHash(raw)
		code := rawdb.ReadCode(r.codeReader, hash)
		if len(code) == 0 {
			r.log.Debug("rejecting request",
				zap.Stringer("nodeID", nodeID),
				zap.String("reason", "code not found"),
				zap.Stringer("hash", hash),
			)
			return nil, errHashNotFound
		}

		switch {
		case len(data) == 0 && len(code) > r.sizeBudget:
			r.log.Debug("rejecting request",
				zap.Stringer("nodeID", nodeID),
				zap.String("reason", "code too large"),
				zap.Stringer("hash", hash),
				zap.Int("size", len(code)),
			)
			return nil, errCodeTooLarge
		case len(data) > 0 && size+len(code) > r.sizeBudget:
			return &syncpb.GetCodeResponse{Data: data}, nil
		}

		data = append(data, code)
		size += len(code)
	}

	return &syncpb.GetCodeResponse{Data: data}, nil
}
