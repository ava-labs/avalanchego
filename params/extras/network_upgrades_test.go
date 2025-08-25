// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"testing"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/utils"
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
		valid     bool
	}{
		{
			name: "Compatible_same_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			time:  1,
			valid: true,
		},
		{
			name: "Compatible_different_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(3),
			},
			time:  1,
			valid: true,
		},
		{
			name: "Compatible_nil_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			time:  1,
			valid: true,
		},
		{
			name: "Incompatible_rewinded_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(1),
			},
			time:  1,
			valid: false,
		},
		{
			name: "Incompatible_fastforward_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(3),
			},
			time:  4,
			valid: false,
		},
		{
			name: "Incompatible_nil_NetworkUpgrades",
			upgrades1: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			upgrades2: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			time:  2,
			valid: false,
		},
		{
			name: "Incompatible_fastforward_nil_NetworkUpgrades",
			upgrades1: func() *NetworkUpgrades {
				upgrades := GetNetworkUpgrades(upgrade.Fuji)
				return &upgrades
			}(),
			upgrades2: func() *NetworkUpgrades {
				upgrades := GetNetworkUpgrades(upgrade.Fuji)
				upgrades.EtnaTimestamp = nil
				return &upgrades
			}(),
			time:  uint64(upgrade.Fuji.EtnaTime.Unix()),
			valid: false,
		},
		{
			name: "Compatible_Fortuna_fastforward_nil_NetworkUpgrades",
			upgrades1: func() *NetworkUpgrades {
				upgrades := GetNetworkUpgrades(upgrade.Fuji)
				return &upgrades
			}(),
			upgrades2: func() *NetworkUpgrades {
				upgrades := GetNetworkUpgrades(upgrade.Fuji)
				upgrades.FortunaTimestamp = nil
				return &upgrades
			}(),
			time:  uint64(upgrade.Fuji.FortunaTime.Unix()),
			valid: true,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := test.upgrades1.checkNetworkUpgradesCompatible(test.upgrades2, test.time)
			if test.valid {
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
		valid         bool
	}{
		{
			name: "ValidNetworkUpgrades_for_latest_network",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(1607144400),
				EtnaTimestamp:      utils.NewUint64(1607144400),
			},
			avagoUpgrades: upgradetest.GetConfig(upgradetest.Latest),
			valid:         true,
		},
		{
			name: "Invalid_Durango_nil_upgrade",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   nil,
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Invalid_Subnet-EVM_non-zero",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(1),
				DurangoTimestamp:   utils.NewUint64(2),
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Invalid_Durango_before_default_upgrade",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.NewUint64(1),
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Invalid_Mainnet_Durango_reconfigured_to_Fuji",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.GetConfig(constants.FujiID).DurangoTime),
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Valid_Fuji_Durango_reconfigured_to_Mainnet",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.GetConfig(constants.MainnetID).DurangoTime),
			},
			avagoUpgrades: upgrade.Fuji,
			valid:         false,
		},
		{
			name: "Invalid_Etna_nil",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime),
				EtnaTimestamp:      nil,
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Invalid_Etna_before_Durango",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime),
				EtnaTimestamp:      utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime.Add(-1)),
			},
			avagoUpgrades: upgrade.Mainnet,
			valid:         false,
		},
		{
			name: "Valid_Fortuna_nil",
			upgrades: &NetworkUpgrades{
				SubnetEVMTimestamp: utils.NewUint64(0),
				DurangoTimestamp:   utils.TimeToNewUint64(upgrade.Fuji.DurangoTime),
				EtnaTimestamp:      utils.TimeToNewUint64(upgrade.Fuji.EtnaTime),
				FortunaTimestamp:   nil,
			},
			avagoUpgrades: upgrade.Fuji,
			valid:         true,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := test.upgrades.verifyNetworkUpgrades(test.avagoUpgrades)
			if test.valid {
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
			err := checkForks(test.upgrades.forkOrder())
			if test.expectedErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
		})
	}
}
