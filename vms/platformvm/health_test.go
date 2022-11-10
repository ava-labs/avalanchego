// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

const defaultMinConnectedStake = 0.8

func TestHealthCheckPrimaryNetwork(t *testing.T) {
	require := require.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()
	genesisState, _ := defaultGenesis()
	for index, validator := range genesisState.Validators {
		err := vm.Connected(context.Background(), validator.NodeID, version.CurrentApp)
		require.NoError(err)
		details, err := vm.HealthCheck(context.Background())
		if float64((index+1)*20) >= defaultMinConnectedStake*100 {
			require.NoError(err)
		} else {
			require.Contains(details, "primary-percentConnected")
			require.ErrorIs(err, errNotEnoughStake)
		}
	}
}

func TestHealthCheckSubnet(t *testing.T) {
	tests := map[string]struct {
		minStake   float64
		useDefault bool
	}{
		"default min stake": {
			useDefault: true,
			minStake:   0,
		},
		"custom min stake": {
			minStake: 0.40,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			vm, _, _ := defaultVM()
			vm.ctx.Lock.Lock()
			defer func() {
				require.NoError(vm.Shutdown(context.Background()))
				vm.ctx.Lock.Unlock()
			}()
			subnetID := ids.GenerateTestID()
			vm.WhitelistedSubnets.Add(subnetID)
			testVdrCount := 4
			for i := 0; i < testVdrCount; i++ {
				subnetVal := ids.GenerateTestNodeID()
				require.NoError(vm.Validators.AddWeight(subnetID, subnetVal, 100))
			}

			vals, ok := vm.Validators.GetValidators(subnetID)
			require.True(ok)

			// connect to all primary network validators first
			genesisState, _ := defaultGenesis()
			for _, validator := range genesisState.Validators {
				err := vm.Connected(context.Background(), validator.NodeID, version.CurrentApp)
				require.NoError(err)
			}
			var expectedMinStake float64
			if test.useDefault {
				expectedMinStake = defaultMinConnectedStake
			} else {
				expectedMinStake = test.minStake
				vm.MinPercentConnectedStakeHealthy = map[ids.ID]float64{
					subnetID: expectedMinStake,
				}
			}
			for index, validator := range vals.List() {
				err := vm.Connected(context.Background(), validator.ID(), version.CurrentApp)
				require.NoError(err)
				details, err := vm.HealthCheck(context.Background())
				connectedPerc := float64((index + 1) * (100 / testVdrCount))
				if connectedPerc >= expectedMinStake*100 {
					require.NoError(err)
				} else {
					require.Contains(details, fmt.Sprintf("%s-percentConnected", subnetID))
					require.ErrorIs(err, errNotEnoughStake)
				}
			}
		})
	}
}
