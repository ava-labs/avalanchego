// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

const bufSize = 1024 * 1024

var errCustom = errors.New("custom")

type testState struct {
	client  *Client
	server  *validators.MockState
	closeFn func()
}

func setupState(t testing.TB, ctrl *gomock.Controller) *testState {
	t.Helper()

	state := &testState{
		server: validators.NewMockState(ctrl),
	}

	listener := bufconn.Listen(bufSize)
	serverCloser := grpcutils.ServerCloser{}

	serverFunc := func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		pb.RegisterValidatorStateServer(server, NewServer(state.server))
		serverCloser.Add(server)
		return server
	}

	go grpcutils.Serve(listener, serverFunc)

	dialer := grpc.WithContextDialer(
		func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		},
	)

	dopts := grpcutils.DefaultDialOptions
	dopts = append(dopts, dialer)
	conn, err := grpcutils.Dial("", dopts...)
	if err != nil {
		t.Fatalf("Failed to dial: %s", err)
	}

	state.client = NewClient(pb.NewValidatorStateClient(conn))
	state.closeFn = func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
	return state
}

func TestGetMinimumHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := setupState(t, ctrl)
	defer state.closeFn()

	// Happy path
	expectedHeight := uint64(1337)
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, nil)

	height, err := state.client.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetMinimumHeight(context.Background())
	require.Error(err)
}

func TestGetCurrentHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := setupState(t, ctrl)
	defer state.closeFn()

	// Happy path
	expectedHeight := uint64(1337)
	state.server.EXPECT().GetCurrentHeight(gomock.Any()).Return(expectedHeight, nil)

	height, err := state.client.GetCurrentHeight(context.Background())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetCurrentHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetCurrentHeight(context.Background())
	require.Error(err)
}

func TestGetValidatorSet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := setupState(t, ctrl)
	defer state.closeFn()

	// Happy path
	nodeID1, nodeID2 := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	nodeID1Weight, nodeID2Weight := uint64(1), uint64(2)
	expectedVdrs := map[ids.NodeID]uint64{
		nodeID1: nodeID1Weight,
		nodeID2: nodeID2Weight,
	}
	height := uint64(1337)
	subnetID := ids.GenerateTestID()
	state.server.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(expectedVdrs, nil)

	vdrs, err := state.client.GetValidatorSet(context.Background(), height, subnetID)
	require.NoError(err)
	require.Equal(expectedVdrs, vdrs)

	// Error path
	state.server.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(expectedVdrs, errCustom)

	_, err = state.client.GetValidatorSet(context.Background(), height, subnetID)
	require.Error(err)
}
