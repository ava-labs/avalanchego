// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/network"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ *code.Client = (*network.Dispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse])(nil)

func TestClient_Send(t *testing.T) {
	ctx := t.Context()
	nodeID := ids.GenerateTestNodeID()

	wantResp := &syncpb.CodeResponse{Data: [][]byte{{0xde, 0xad}, {0xbe, 0xef}}}
	wantBytes, err := proto.Marshal(wantResp)
	require.NoError(t, err)

	c := synctest.NewDispatcher[*syncpb.GetCodeRequest, *syncpb.CodeResponse](
		t, ctx, nodeID, synctest.EchoHandler(wantBytes), synctest.NewPeerTracker(t, nodeID),
	)

	resp := &syncpb.CodeResponse{}
	gotNodeID, outcome, err := c.Send(ctx, &syncpb.GetCodeRequest{Hashes: [][]byte{{1}, {2}}}, resp)
	require.NoError(t, err)
	outcome.Success()

	require.Equal(t, nodeID, gotNodeID)
	require.Empty(t, cmp.Diff(wantResp, resp, protocmp.Transform()))
}
