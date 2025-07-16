// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	pb "github.com/ava-labs/avalanchego/buf/proto/pb/validatorstate"
)

var errCustom = errors.New("custom")

type testState struct {
	client *Client
	server *validatorsmock.State
}

func setupState(t testing.TB, ctrl *gomock.Controller) *testState {
	require := require.New(t)

	t.Helper()

	state := &testState{
		server: validatorsmock.NewState(ctrl),
	}

	listener, err := grpcutils.NewListener()
	require.NoError(err)
	serverCloser := grpcutils.ServerCloser{}

	server := grpcutils.NewServer()
	pb.RegisterValidatorStateServer(server, NewServer(state.server))
	serverCloser.Add(server)

	go grpcutils.Serve(listener, server)

	conn, err := grpcutils.Dial(listener.Addr().String())
	require.NoError(err)

	state.client = NewClient(pb.NewValidatorStateClient(conn))

	t.Cleanup(func() {
		serverCloser.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	return state
}

func TestGetMinimumHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := setupState(t, ctrl)

	// Happy path
	expectedHeight := uint64(1337)
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, nil)

	height, err := state.client.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetMinimumHeight(context.Background())
	// TODO: require specific error
	require.Error(err) //nolint:forbidigo // currently returns grpc error
}

func TestGetCurrentHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := setupState(t, ctrl)

	// Happy path
	expectedHeight := uint64(1337)
	state.server.EXPECT().GetCurrentHeight(gomock.Any()).Return(expectedHeight, nil)

	height, err := state.client.GetCurrentHeight(context.Background())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetCurrentHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetCurrentHeight(context.Background())
	// TODO: require specific error
	require.Error(err) //nolint:forbidigo // currently returns grpc error
}

func TestGetSubnetID(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := setupState(t, ctrl)

	// Happy path
	chainID := ids.GenerateTestID()
	expectedSubnetID := ids.GenerateTestID()
	state.server.EXPECT().GetSubnetID(gomock.Any(), chainID).Return(expectedSubnetID, nil)

	subnetID, err := state.client.GetSubnetID(context.Background(), chainID)
	require.NoError(err)
	require.Equal(expectedSubnetID, subnetID)

	// Error path
	state.server.EXPECT().GetSubnetID(gomock.Any(), chainID).Return(expectedSubnetID, errCustom)

	_, err = state.client.GetSubnetID(context.Background(), chainID)
	// TODO: require specific error
	require.Error(err) //nolint:forbidigo // currently returns grpc error
}

func TestGetValidatorSet(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := setupState(t, ctrl)

	// Happy path
	sk0, err := localsigner.New()
	require.NoError(err)
	vdr0 := &validators.GetValidatorOutput{
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: sk0.PublicKey(),
		Weight:    1,
	}

	sk1, err := localsigner.New()
	require.NoError(err)
	vdr1 := &validators.GetValidatorOutput{
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: sk1.PublicKey(),
		Weight:    2,
	}

	vdr2 := &validators.GetValidatorOutput{
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: nil,
		Weight:    3,
	}

	expectedVdrs := map[ids.NodeID]*validators.GetValidatorOutput{
		vdr0.NodeID: vdr0,
		vdr1.NodeID: vdr1,
		vdr2.NodeID: vdr2,
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
	// TODO: require specific error
	require.Error(err) //nolint:forbidigo // currently returns grpc error
}

func TestPublicKeyDeserialize(t *testing.T) {
	require := require.New(t)

	sk, err := localsigner.New()
	require.NoError(err)
	pk := sk.PublicKey()

	pkBytes := bls.PublicKeyToUncompressedBytes(pk)
	pkDe := bls.PublicKeyFromValidUncompressedBytes(pkBytes)
	require.NotNil(pkDe)
	require.Equal(pk, pkDe)
}

// BenchmarkGetValidatorSet measures the time it takes complete a gRPC client
// request based on a mocked validator set.
func BenchmarkGetValidatorSet(b *testing.B) {
	for _, size := range []int{1, 16, 32, 1024, 2048} {
		vs := setupValidatorSet(b, size)
		b.Run(fmt.Sprintf("get_validator_set_%d_validators", size), func(b *testing.B) {
			benchmarkGetValidatorSet(b, vs)
		})
	}
}

func benchmarkGetValidatorSet(b *testing.B, vs map[ids.NodeID]*validators.GetValidatorOutput) {
	require := require.New(b)
	ctrl := gomock.NewController(b)
	state := setupState(b, ctrl)

	height := uint64(1337)
	subnetID := ids.GenerateTestID()
	state.server.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(vs, nil).AnyTimes()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := state.client.GetValidatorSet(context.Background(), height, subnetID)
		require.NoError(err)
	}
	b.StopTimer()
}

func setupValidatorSet(b *testing.B, size int) map[ids.NodeID]*validators.GetValidatorOutput {
	b.Helper()

	set := make(map[ids.NodeID]*validators.GetValidatorOutput, size)
	sk, err := localsigner.New()
	require.NoError(b, err)
	pk := sk.PublicKey()
	for i := 0; i < size; i++ {
		id := ids.GenerateTestNodeID()
		set[id] = &validators.GetValidatorOutput{
			NodeID:    id,
			PublicKey: pk,
			Weight:    uint64(i),
		}
	}
	return set
}
