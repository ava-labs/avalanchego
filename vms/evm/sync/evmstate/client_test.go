// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ *evmstate.Client = (*network.Dispatcher[*syncpb.GetLeafRequest, *syncpb.LeafResponse])(nil)

func TestClient_Send(t *testing.T) {
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	wantResp := &syncpb.LeafResponse{
		Keys:      [][]byte{{1, 2, 3}},
		Values:    [][]byte{{4, 5, 6}},
		ProofVals: [][]byte{{7, 8}},
	}
	wantBytes, err := proto.Marshal(wantResp)
	require.NoError(t, err)

	c := synctest.NewDispatcher[*syncpb.GetLeafRequest, *syncpb.LeafResponse](
		t, ctx, nodeID, synctest.EchoHandler(wantBytes), synctest.NewPeerTracker(t, nodeID),
	)

	resp := &syncpb.LeafResponse{}
	gotNodeID, outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{
		RootHash: []byte{0xab},
		KeyLimit: 1024,
		NodeType: syncpb.EVMNodeType_EVM_NODE_TYPE_STATE_TRIE,
	}, resp)
	require.NoError(t, err)
	outcome.Success()

	require.Equal(t, nodeID, gotNodeID)
	require.Empty(t, cmp.Diff(wantResp, resp, protocmp.Transform()))
}
