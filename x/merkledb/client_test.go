// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ p2p.Handler = (*xsync.GetChangeProofHandler[*RangeProof, *ChangeProof])(nil)
	_ p2p.Handler = (*xsync.GetRangeProofHandler[*RangeProof, *ChangeProof])(nil)
	_ p2p.Handler = (*flakyHandler)(nil)
)

func newDefaultDBConfig() Config {
	return Config{
		IntermediateWriteBatchSize:  100,
		HistoryLength:               xsync.DefaultRequestKeyLimit,
		ValueNodeCacheSize:          xsync.DefaultRequestKeyLimit,
		IntermediateWriteBufferSize: xsync.DefaultRequestKeyLimit,
		IntermediateNodeCacheSize:   xsync.DefaultRequestKeyLimit,
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
	handler := xsync.NewGetRangeProofHandler(db, RangeProofMarshaler{})

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			proof, err := RangeProofMarshaler{}.Unmarshal(responseBytes)
			require.NoError(t, err)

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			responseBytes, err = RangeProofMarshaler{}.Marshal(proof)
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
	handler := xsync.NewGetChangeProofHandler(db, RangeProofMarshaler{}, ChangeProofMarshaler{})

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			response := &pb.GetChangeProofResponse{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			proof, err := ChangeProofMarshaler{}.Unmarshal(response.GetChangeProof())
			require.NoError(t, err)

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			proofBytes, err := ChangeProofMarshaler{}.Marshal(proof)
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
