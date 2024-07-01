// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

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

func newModifiedRangeProofHandler(
	t *testing.T,
	db merkledb.MerkleDB,
	modifyResponse func(response *merkledb.RangeProof),
) p2p.Handler {
	rangeProofHandler := NewSyncGetRangeProofHandler(logging.NoLog{}, db)

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
			responseBytes, err := rangeProofHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
			require.NoError(t, err)

			response := &pb.RangeProof{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			proof := &merkledb.RangeProof{}
			require.NoError(t, proof.UnmarshalProto(response))

			// Half of requests are modified
			if c.Get() == 0 {
				modifyResponse(proof)
			}

			return proto.Marshal(proof.ToProto())
		},
	}
}

func newModifiedChangeProofHandler(
	t *testing.T,
	db merkledb.MerkleDB,
	modifyResponse func(response *merkledb.ChangeProof),
) p2p.Handler {
	rangeProofHandler := NewSyncGetChangeProofHandler(logging.NoLog{}, db)

	c := counter{m: 2}
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
			responseBytes, err := rangeProofHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
			require.NoError(t, err)

			response := &pb.SyncGetChangeProofResponse{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			changeProof := response.Response.(*pb.SyncGetChangeProofResponse_ChangeProof)
			proof := &merkledb.ChangeProof{}
			require.NoError(t, proof.UnmarshalProto(changeProof.ChangeProof))

			// Half of requests are modified
			if c.Get() == 0 {
				modifyResponse(proof)
			}

			return proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_ChangeProof{
					ChangeProof: proof.ToProto(),
				},
			})
		},
	}
}

type counter struct {
	i    int
	m    int
	lock sync.Mutex
}

func (c *counter) Get() int {
	c.lock.Lock()
	defer c.lock.Unlock()

	tmp := c.i
	result := tmp % c.m

	c.i++
	return result
}
