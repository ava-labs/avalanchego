// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

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
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ p2p.Handler = (*flakyHandler)(nil)

func newDefaultDBConfig() merkledb.Config {
	return merkledb.Config{
		IntermediateWriteBatchSize:  100,
		HistoryLength:               defaultRequestKeyLimit,
		ValueNodeCacheSize:          defaultRequestKeyLimit,
		IntermediateWriteBufferSize: defaultRequestKeyLimit,
		IntermediateNodeCacheSize:   defaultRequestKeyLimit,
		Reg:                         prometheus.NewRegistry(),
		Tracer:                      trace.Noop,
		BranchFactor:                merkledb.BranchFactor16,
	}
}

func newFlakyRangeProofHandler(
	t *testing.T,
	proofCreator ProofCreator,
	modifyResponse func(response *merkledb.RangeProof),
) p2p.Handler {
	handler := NewGetRangeProofHandler(proofCreator)

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			response := &pb.RangeProof{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			proof := &merkledb.RangeProof{}
			require.NoError(t, proof.UnmarshalProto(response))

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			responseBytes, err := proto.Marshal(proof.ToProto())
			if err != nil {
				return nil, &common.AppError{Code: 123, Message: err.Error()}
			}

			return responseBytes, nil
		},
	}
}

func newFlakyChangeProofHandler(
	t *testing.T,
	proofCreator ProofCreator,
	modifyResponse func(response *merkledb.ChangeProof),
) p2p.Handler {
	handler := NewGetChangeProofHandler(proofCreator)

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
			var err error
			responseBytes, appErr := handler.AppRequest(ctx, nodeID, deadline, requestBytes)
			if appErr != nil {
				return nil, appErr
			}

			response := &pb.SyncGetChangeProofResponse{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			changeProof := response.Response.(*pb.SyncGetChangeProofResponse_ChangeProof)
			proof := &merkledb.ChangeProof{}
			require.NoError(t, proof.UnmarshalProto(changeProof.ChangeProof))

			// Half of requests are modified
			if c.Inc() == 0 {
				modifyResponse(proof)
			}

			responseBytes, err = proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_ChangeProof{
					ChangeProof: proof.ToProto(),
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

type waitingHandler struct {
	p2p.NoOpHandler
	handler         p2p.Handler
	updatedRootChan chan struct{}
}

func (w *waitingHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *common.AppError) {
	<-w.updatedRootChan
	return w.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}
