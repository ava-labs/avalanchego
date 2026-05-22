// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestClient_Send(t *testing.T) {
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	wantResp := &syncpb.GetLeafResponse{
		Keys:      [][]byte{{1, 2, 3}},
		Values:    [][]byte{{4, 5, 6}},
		ProofVals: [][]byte{{7, 8}},
	}
	c := synctest.NewClient[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse](t, ctx, nodeID, wantResp)

	resp := &syncpb.GetLeafResponse{}
	outcome, err := c.Send(ctx, &syncpb.GetLeafRequest{
		RootHash: []byte{0xab},
		KeyLimit: 1024,
	}, resp)
	require.NoError(t, err)
	outcome.Success()

	require.Empty(t, cmp.Diff(wantResp, resp, protocmp.Transform()))
}
