// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code_test

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

	wantResp := &syncpb.GetCodeResponse{Data: [][]byte{{0xde, 0xad}, {0xbe, 0xef}}}
	c := synctest.NewClient[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse](t, ctx, nodeID, wantResp)

	resp := &syncpb.GetCodeResponse{}
	gotNodeID, outcome, err := c.Send(ctx, &syncpb.GetCodeRequest{Hashes: [][]byte{{1}, {2}}}, resp)
	require.NoError(t, err)
	outcome.Success()

	require.Equal(t, nodeID, gotNodeID)
	require.Empty(t, cmp.Diff(wantResp, resp, protocmp.Transform()))
}
