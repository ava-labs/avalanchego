// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"testing"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/require"
)

func TestNetworkUpgradesEqual(t *testing.T) {
	testcases := []struct {
		name      string
		upgrades1 *NetworkUpgrades
		upgrades2 *NetworkUpgrades
		expected  bool
	}{
		{
			name: "EqualNetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			expected: true,
		},
		{
			name: "NotEqualNetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(3),
			},
			expected: false,
		},
		{
			name: "NilNetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: nil,
			expected:  false,
		},
		{
			name: "NilNetworkUpgrade",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			expected: false,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, test.upgrades1.Equal(test.upgrades2))
		})
	}
}

func TestCheckNetworkUpgradesCompatible(t *testing.T) {
	testcases := []struct {
		name      string
		upgrades1 *NetworkUpgrades
		upgrades2 *NetworkUpgrades
		time      uint64
		expected  bool
	}{
		{
			name: "Compatible same NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			time:     1,
			expected: true,
		},
		{
			name: "Compatible different NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(3),
			},
			time:     1,
			expected: true,
		},
		{
			name: "Compatible nil NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			time:     1,
			expected: true,
		},
		{
			name: "Incompatible rewinded NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(1),
			},
			time:     1,
			expected: false,
		},
		{
			name: "Incompatible fastforward NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(3),
			},
			time:     4,
			expected: false,
		},
		{
			name: "Incompatible nil NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			time:     2,
			expected: false,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := test.upgrades1.CheckNetworkUpgradesCompatible(test.upgrades2, test.time)
			if test.expected {
				require.Nil(t, err)
			} else {
				require.NotNil(t, err)
			}
		})
	}
}

func TestVerifyNetworkUpgrades(t *testing.T) {
	testcases := []struct {
		name          string
		upgrades      *NetworkUpgrades
		avagoUpgrades upgrade.Config
		expected      bool
	}{
		{
			name: "ValidNetworkUpgrades for custom network",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(1607144400),
				EtnaTimestamp:      utils.NewUint64(1607144400),
			},
			avagoUpgrades: upgrade.GetConfig(1111),
			expected:      true,
		},
		{
			name: "Invalid Durango nil upgrade",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
		{
			name: "Invalid Subnet-EVM non-zero",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
		{
			name: "Invalid Durango before default upgrade",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(1),
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
		{
			name: "Invalid Mainnet Durango reconfigured to Fuji",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.GetConfig(constants.FujiID).DurangoTime),
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
		{
			name: "Valid Fuji Durango reconfigured to Mainnet",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.GetConfig(constants.MainnetID).DurangoTime),
			},
			avagoUpgrades: upgrade.GetConfig(constants.FujiID),
			expected:      false,
		},
		{
			name: "Invalid Etna nil",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(2),
				EtnaTimestamp:      nil,
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
		{
			name: "Invalid Etna before Durango",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(2),
				EtnaTimestamp:      utils.NewUint64(1),
			},
			avagoUpgrades: upgrade.GetConfig(constants.MainnetID),
			expected:      false,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := test.upgrades.verifyNetworkUpgrades(test.avagoUpgrades)
			if test.expected {
				require.Nil(t, err)
			} else {
				require.NotNil(t, err)
			}
		})
	}
}

func TestForkOrder(t *testing.T) {
	testcases := []struct {
		name        string
		upgrades    *NetworkUpgrades
		expectedErr bool
	}{
		{
			name: "ValidNetworkUpgrades",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			expectedErr: false,
		},
		{
			name: "Invalid order",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(0),
			},
			expectedErr: true,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := checkForks(test.upgrades.forkOrder(), false)
			if test.expectedErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
		})
	}
}
