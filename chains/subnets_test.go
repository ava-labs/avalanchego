// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestNewSubnets(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(err)

	subnet, ok := subnets.GetOrCreate(constants.PrimaryNetworkID)
	require.False(ok)
	require.Equal(config[constants.PrimaryNetworkID], subnet.Config())
}

func TestNewSubnetsNoPrimaryNetworkConfig(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]subnets.Config{}

	_, err := NewSubnets(ids.EmptyNodeID, config)
	require.ErrorIs(err, ErrNoPrimaryNetworkConfig)
}

func TestSubnetsGetOrCreate(t *testing.T) {
	testSubnetID := ids.GenerateTestID()

	type args struct {
		subnetID ids.ID
		want     bool
	}

	tests := []struct {
		name string
		args []args
	}{
		{
			name: "adding duplicate subnet is a noop",
			args: []args{
				{
					subnetID: testSubnetID,
					want:     true,
				},
				{
					subnetID: testSubnetID,
				},
			},
		},
		{
			name: "adding unique subnets succeeds",
			args: []args{
				{
					subnetID: ids.GenerateTestID(),
					want:     true,
				},
				{
					subnetID: ids.GenerateTestID(),
					want:     true,
				},
				{
					subnetID: ids.GenerateTestID(),
					want:     true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			config := map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
			}
			subnets, err := NewSubnets(ids.EmptyNodeID, config)
			require.NoError(err)

			for _, arg := range tt.args {
				_, got := subnets.GetOrCreate(arg.subnetID)
				require.Equal(arg.want, got)
			}
		})
	}
}

func TestSubnetConfigs(t *testing.T) {
	testSubnetID := ids.GenerateTestID()

	tests := []struct {
		name     string
		config   map[ids.ID]subnets.Config
		subnetID ids.ID
		want     subnets.Config
	}{
		{
			name: "default to primary network config",
			config: map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
			},
			subnetID: testSubnetID,
			want:     subnets.Config{},
		},
		{
			name: "use subnet config",
			config: map[ids.ID]subnets.Config{
				constants.PrimaryNetworkID: {},
				testSubnetID: {
					ValidatorOnly: true,
				},
			},
			subnetID: testSubnetID,
			want: subnets.Config{
				ValidatorOnly: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			subnets, err := NewSubnets(ids.EmptyNodeID, tt.config)
			require.NoError(err)

			subnet, ok := subnets.GetOrCreate(tt.subnetID)
			require.True(ok)

			require.Equal(tt.want, subnet.Config())
		})
	}
}

func TestSubnetsBootstrapping(t *testing.T) {
	require := require.New(t)

	config := map[ids.ID]subnets.Config{
		constants.PrimaryNetworkID: {},
	}

	subnets, err := NewSubnets(ids.EmptyNodeID, config)
	require.NoError(err)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()

	subnet, ok := subnets.GetOrCreate(subnetID)
	require.True(ok)

	// Start bootstrapping
	subnet.AddChain(chainID)
	bootstrapping := subnets.Bootstrapping()
	require.Contains(bootstrapping, subnetID)

	// Finish bootstrapping
	subnet.Bootstrapped(chainID)
	require.Empty(subnets.Bootstrapping())
}
