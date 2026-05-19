// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block_test

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

	wantResp := &syncpb.GetBlockResponse{Blocks: [][]byte{{0x01}, {0x02}, {0x03}}}
	c := synctest.NewClient[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse](t, ctx, nodeID, wantResp)

	resp := &syncpb.GetBlockResponse{}
	gotNodeID, outcome, err := c.Send(ctx, &syncpb.GetBlockRequest{Height: 100, Parents: 5}, resp)
	require.NoError(t, err)
	outcome.Success()

	require.Equal(t, nodeID, gotNodeID)
	require.Empty(t, cmp.Diff(wantResp, resp, protocmp.Transform()))
}
