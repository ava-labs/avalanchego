// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

var errCustom = errors.New("custom")

func newClient(t testing.TB, state validators.State) *Client {
	t.Helper()

	require := require.New(t)

	listener, err := grpcutils.NewListener()
	require.NoError(err)

	server := grpcutils.NewServer()
	pb.RegisterValidatorStateServer(server, NewServer(state))

	go grpcutils.Serve(listener, server)

	conn, err := grpcutils.Dial(listener.Addr().String())
	require.NoError(err)

	t.Cleanup(func() {
		server.Stop()
		_ = conn.Close()
		_ = listener.Close()
	})

	return NewClient(pb.NewValidatorStateClient(conn))
}

type testState struct {
	client *Client
	server *validatorsmock.State
}

func setupState(t testing.TB, ctrl *gomock.Controller) *testState {
	t.Helper()

	state := validatorsmock.NewState(ctrl)
	return &testState{
		client: newClient(t, state),
		server: state,
	}
}

func TestGetMinimumHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	state := setupState(t, ctrl)

	// Happy path
	expectedHeight := uint64(1337)
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, nil)

	height, err := state.client.GetMinimumHeight(t.Context())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetMinimumHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetMinimumHeight(t.Context())
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

	height, err := state.client.GetCurrentHeight(t.Context())
	require.NoError(err)
	require.Equal(expectedHeight, height)

	// Error path
	state.server.EXPECT().GetCurrentHeight(gomock.Any()).Return(expectedHeight, errCustom)

	_, err = state.client.GetCurrentHeight(t.Context())
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

	subnetID, err := state.client.GetSubnetID(t.Context(), chainID)
	require.NoError(err)
	require.Equal(expectedSubnetID, subnetID)

	// Error path
	state.server.EXPECT().GetSubnetID(gomock.Any(), chainID).Return(expectedSubnetID, errCustom)

	_, err = state.client.GetSubnetID(t.Context(), chainID)
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
	pk0 := sk0.PublicKey()
	vdr0 := &validators.GetValidatorOutput{
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: pk0,
		Weight:    1,
	}

	sk1, err := localsigner.New()
	require.NoError(err)
	pk1 := sk1.PublicKey()
	vdr1 := &validators.GetValidatorOutput{
		NodeID:    ids.GenerateTestNodeID(),
		PublicKey: pk1,
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

	vdrs, err := state.client.GetValidatorSet(t.Context(), height, subnetID)
	require.NoError(err)
	require.Equal(expectedVdrs, vdrs)

	// Error path
	state.server.EXPECT().GetValidatorSet(gomock.Any(), height, subnetID).Return(expectedVdrs, errCustom)

	_, err = state.client.GetValidatorSet(t.Context(), height, subnetID)
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
		_, err := state.client.GetValidatorSet(b.Context(), height, subnetID)
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

func TestGetWarpValidatorSets(t *testing.T) {
	const height uint64 = 1337
	t.Run("error", func(t *testing.T) {
		state := &validatorstest.State{
			CantGetWarpValidatorSets: true,
		}
		c := newClient(t, state)

		_, err := c.GetWarpValidatorSets(t.Context(), height)
		require.Error(t, err) //nolint:forbidigo // returns grpc error
	})

	t.Run("valid", func(t *testing.T) {
		require := require.New(t)

		expectedVdrSets := map[ids.ID]validators.WarpSet{
			ids.GenerateTestID(): validatorstest.NewWarpSet(t, 1),
			ids.GenerateTestID(): validatorstest.NewWarpSet(t, 2),
			ids.GenerateTestID(): validatorstest.NewWarpSet(t, 3),
		}
		state := &validatorstest.State{
			GetWarpValidatorSetsF: func(_ context.Context, h uint64) (map[ids.ID]validators.WarpSet, error) {
				require.Equal(height, h)
				return expectedVdrSets, nil
			},
		}
		c := newClient(t, state)

		vdrSets, err := c.GetWarpValidatorSets(t.Context(), height)
		require.NoError(err)
		require.Equal(expectedVdrSets, vdrSets)
	})
}

func TestGetWarpValidatorSet(t *testing.T) {
	const height uint64 = 1337
	t.Run("error", func(t *testing.T) {
		state := &validatorstest.State{
			CantGetWarpValidatorSets: true,
		}
		c := newClient(t, state)

		_, err := c.GetWarpValidatorSet(t.Context(), height, ids.GenerateTestID())
		require.Error(t, err) //nolint:forbidigo // returns grpc error
	})

	t.Run("valid", func(t *testing.T) {
		require := require.New(t)

		subnetID := ids.GenerateTestID()
		expectedVdrSet := validatorstest.NewWarpSet(t, 3)
		state := &validatorstest.State{
			GetWarpValidatorSetF: func(_ context.Context, h uint64, s ids.ID) (validators.WarpSet, error) {
				require.Equal(height, h)
				require.Equal(subnetID, s)
				return expectedVdrSet, nil
			},
		}
		c := newClient(t, state)

		vdrSet, err := c.GetWarpValidatorSet(t.Context(), height, subnetID)
		require.NoError(err)
		require.Equal(expectedVdrSet, vdrSet)
	})
}
