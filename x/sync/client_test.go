// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"
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

func newModifiedResponseHandler(
	t *testing.T,
	db merkledb.MerkleDB,
	modifyResponse func(response *merkledb.RangeProof),
) p2p.Handler {
	rangeProofHandler := NewSyncGetRangeProofHandler(logging.NoLog{}, db)
	return &p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
			responseBytes, err := rangeProofHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
			require.NoError(t, err)

			response := &pb.RangeProof{}
			require.NoError(t, proto.Unmarshal(responseBytes, response))

			rangeProof := &merkledb.RangeProof{}
			require.NoError(t, rangeProof.UnmarshalProto(response))

			// Half of requests are modified
			if rand.Intn(2) < 1 {
				modifyResponse(rangeProof)
			}

			return proto.Marshal(rangeProof.ToProto())
		},
	}
}
