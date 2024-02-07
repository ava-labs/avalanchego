// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	subnet, ok := subnets.Get(constants.PrimaryNetworkID)
	require.True(ok)
	require.Equal(subnet.Config(), config[constants.PrimaryNetworkID])
}

func TestNewSubnets_NoPrimaryNetworkConfig(t *testing.T) {
	require := require.New(t)
	config := map[ids.ID]subnets.Config{}

	_, err := NewSubnets(ids.EmptyNodeID, config)
	require.ErrorIs(err, ErrNoPrimaryNetworkConfig)
}

func TestSubnetsAdd(t *testing.T) {
	testSubnetID := ids.GenerateTestID()

	type add struct {
		subnetID ids.ID
		want     bool
	}

	tests := []struct {
		name string
		adds []add
	}{
		{
			name: "adding duplicate subnet is a noop",
			adds: []add{
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
			adds: []add{
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

			for _, add := range tt.adds {
				got := subnets.Add(add.subnetID)
				require.Equal(got, add.want)
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
					GossipConfig: subnets.GossipConfig{
						AcceptedFrontierValidatorSize: 123456789,
					},
				},
			},
			subnetID: testSubnetID,
			want: subnets.Config{
				GossipConfig: subnets.GossipConfig{
					AcceptedFrontierValidatorSize: 123456789,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			subnets, err := NewSubnets(ids.EmptyNodeID, tt.config)
			require.NoError(err)

			ok := subnets.Add(tt.subnetID)
			require.True(ok)

			subnet, ok := subnets.Get(tt.subnetID)
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

	subnets.Add(subnetID)
	subnet, ok := subnets.Get(subnetID)
	require.True(ok)

	// Start bootstrapping
	subnet.AddChain(chainID)
	bootstrapping := subnets.Bootstrapping()
	require.Contains(bootstrapping, subnetID)

	// Finish bootstrapping
	subnet.Bootstrapped(chainID)
	require.Empty(subnets.Bootstrapping())
}
