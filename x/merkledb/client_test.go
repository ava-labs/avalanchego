// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"

	merklesync "github.com/ava-labs/avalanchego/database/merkle/sync"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	_ p2p.Handler = (*merklesync.GetChangeProofHandler[*RangeProof, *ChangeProof])(nil)
	_ p2p.Handler = (*merklesync.GetRangeProofHandler[*RangeProof, *ChangeProof])(nil)
	_ p2p.Handler = (*flakyHandler)(nil)
)

func newDefaultDBConfig() Config {
	return Config{
		IntermediateWriteBatchSize:  100,
		HistoryLength:               merklesync.DefaultRequestKeyLimit,
		ValueNodeCacheSize:          merklesync.DefaultRequestKeyLimit,
		IntermediateWriteBufferSize: merklesync.DefaultRequestKeyLimit,
		IntermediateNodeCacheSize:   merklesync.DefaultRequestKeyLimit,
		Reg:                         prometheus.NewRegistry(),
		Tracer:                      trace.Noop,
		BranchFactor:                BranchFactor16,
	}
}

func newFlakyRangeProofHandler(
	t *testing.T,
	db MerkleDB,
	modifyResponse func(response *RangeProof),
) p2p.Handler {
	var (
		c                   = counter{m: 2}
		rangeProofMarshaler = rangeProofMarshaler
		handler             = merklesync.NewGetRangeProofHandler(db, rangeProofMarshaler)
	)

	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			proof, err := rangeProofMarshaler.Unmarshal(responseBytes)
			require.NoError(t, err)

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			responseBytes, err = rangeProofMarshaler.Marshal(proof)
			if err != nil {
				return nil, &common.AppError{Code: 123, Message: err.Error()}
			}

			return responseBytes, nil
		},
	}
}

func newFlakyChangeProofHandler(
	t *testing.T,
	db MerkleDB,
	modifyResponse func(response *ChangeProof),
) p2p.Handler {
	var (
		c                    = counter{m: 2}
		rangeProofMarshaler  = rangeProofMarshaler
		changeProofMarshaler = changeProofMarshaler
		handler              = merklesync.NewGetChangeProofHandler(db, rangeProofMarshaler, changeProofMarshaler)
	)

	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			response := &pb.GetChangeProofResponse{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			proof, err := changeProofMarshaler.Unmarshal(response.GetChangeProof())
			require.NoError(t, err)

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			proofBytes, err := changeProofMarshaler.Marshal(proof)
			require.NoError(t, err)
			responseBytes, err = proto.Marshal(&pb.GetChangeProofResponse{
				Response: &pb.GetChangeProofResponse_ChangeProof{
					ChangeProof: proofBytes,
				},
			})
			if err != nil {
				return nil, &common.AppError{Code: 123, Message: err.Error()}
			}

			return responseBytes, nil
		},
	}
}

type flakyHandler struct {
	p2p.Handler
	c *counter
}

func (f *flakyHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	if f.c.Inc() == 0 {
		return nil, &common.AppError{Code: 123, Message: "flake error"}
	}

	return f.Handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type counter struct {
	i    int
	m    int
	lock sync.Mutex
}

func (c *counter) Inc() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	tmp := c.i
	result := tmp % c.m

	c.i++
	return result
}
