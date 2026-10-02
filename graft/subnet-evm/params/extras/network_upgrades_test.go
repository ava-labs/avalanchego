// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"

	evmparams "github.com/ava-labs/avalanchego/graft/evm/params"
)

func TestMain(m *testing.M) {
	evmparams.RegisterJSON(SubnetEVMJSON{})
	os.Exit(m.Run())
}

func TestNetworkUpgradesEqual(t *testing.T) {
	testcases := []struct {
		name      string
		upgrades1 *NetworkUpgrades
		upgrades2 *NetworkUpgrades
		expected  bool
	}{
		{
			name: "EqualNetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			expected: true,
		},
		{
			name: "NotEqualNetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](3),
			}),
			expected: false,
		},
		{
			name: "NilNetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: nil,
			expected:  false,
		},
		{
			name: "NilNetworkUpgrade",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: nil,
			}),
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
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			time:  1,
			valid: true,
		},
		{
			name: "Compatible_different_NetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](3),
			}),
			time:  1,
			valid: true,
		},
		{
			name: "Compatible_nil_NetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: nil,
			}),
			time:  1,
			valid: true,
		},
		{
			name: "Incompatible_rewinded_NetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](1),
			}),
			time:  1,
			valid: false,
		},
		{
			name: "Incompatible_fastforward_NetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](3),
			}),
			time:  4,
			valid: false,
		},
		{
			name: "Incompatible_nil_NetworkUpgrades",
			upgrades1: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			upgrades2: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: nil,
			}),
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
			err := test.upgrades1.CheckCompatible(test.upgrades2, test.time)
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
		wantError     error
	}{
		{
			name: "Invalid_Durango_nil_upgrade",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: nil,
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errCannotBeNil,
		},
		{
			name: "Invalid_Subnet-EVM_non-zero",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](2),
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errTimestampTooEarly,
		},
		{
			name: "Invalid_Durango_before_default_upgrade",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.PointerTo[uint64](1),
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errTimestampTooEarly,
		},
		{
			name: "Invalid_Mainnet_Durango_reconfigured_to_Fuji",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.TimeToNewUint64(upgrade.GetConfig(constants.FujiID).DurangoTime),
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errTimestampTooEarly,
		},
		{
			name: "Valid_Fuji_Durango_reconfigured_to_Mainnet",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.TimeToNewUint64(upgrade.GetConfig(constants.MainnetID).DurangoTime),
			}),
			avagoUpgrades: upgrade.Fuji,
			wantError:     errCannotBeNil, // Etna is required but not specified
		},
		{
			name: "Invalid_Etna_nil",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime),
				EtnaTimestamp:         nil,
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errCannotBeNil,
		},
		{
			name: "Invalid_Etna_before_Durango",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime),
				EtnaTimestamp:         utils.TimeToNewUint64(upgrade.Mainnet.DurangoTime.Add(-1)),
			}),
			avagoUpgrades: upgrade.Mainnet,
			wantError:     errTimestampTooEarly,
		},
		{
			name: "Valid_Granite_After_nil_Fortuna",
			upgrades: withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
				DurangoBlockTimestamp: utils.TimeToNewUint64(upgrade.Fuji.DurangoTime),
				EtnaTimestamp:         utils.TimeToNewUint64(upgrade.Fuji.EtnaTime),
				FortunaTimestamp:      nil,
				GraniteTimestamp:      utils.TimeToNewUint64(upgrade.Fuji.GraniteTime),
			}),
			avagoUpgrades: upgradetest.GetConfig(upgradetest.Granite),
			wantError:     nil,
		},
	}
	for _, test := range testcases {
		t.Run(test.name, func(t *testing.T) {
			err := verifyNetworkUpgrades(test.upgrades, test.avagoUpgrades)
			require.ErrorIs(t, err, test.wantError)
		})
	}
}

func TestSetDefaultsTreatsZeroAsUnset(t *testing.T) {
	upgrades := withSubnetEVM(utils.PointerTo[uint64](0), &NetworkUpgrades{
		DurangoBlockTimestamp: utils.PointerTo[uint64](0),
		EtnaTimestamp:         nil,
		FortunaTimestamp:      utils.PointerTo[uint64](0),
		GraniteTimestamp:      utils.PointerTo[uint64](0),
		HeliconTimestamp:      utils.PointerTo[uint64](0),
		IglooTimestamp:        utils.PointerTo[uint64](0),
	})
	agoUpgrades := upgradetest.GetConfig(upgradetest.Latest)
	defaults := GetNetworkUpgrades(agoUpgrades)
	upgrades.SetDefaults(defaults)

	require.Equal(t, subnetEVMTimestamp(&defaults), subnetEVMTimestamp(upgrades))
	require.Equal(t, defaults.DurangoBlockTimestamp, upgrades.DurangoBlockTimestamp)
	require.Equal(t, defaults.EtnaTimestamp, upgrades.EtnaTimestamp)
	require.Equal(t, defaults.FortunaTimestamp, upgrades.FortunaTimestamp)
	require.Equal(t, defaults.GraniteTimestamp, upgrades.GraniteTimestamp)
	require.Equal(t, defaults.HeliconTimestamp, upgrades.HeliconTimestamp)
	require.Equal(t, defaults.IglooTimestamp, upgrades.IglooTimestamp)
}

// withSubnetEVM sets the Subnet-EVM timestamp of n and returns it.
func withSubnetEVM(subnetEVM *uint64, n *NetworkUpgrades) *NetworkUpgrades {
	n.ApricotPhase1BlockTimestamp = subnetEVM
	n.ApricotPhase2BlockTimestamp = subnetEVM
	n.ApricotPhase3BlockTimestamp = subnetEVM
	n.ApricotPhase4BlockTimestamp = subnetEVM
	n.ApricotPhase5BlockTimestamp = subnetEVM
	n.ApricotPhasePre6BlockTimestamp = subnetEVM
	n.ApricotPhase6BlockTimestamp = subnetEVM
	n.ApricotPhasePost6BlockTimestamp = subnetEVM
	n.BanffBlockTimestamp = subnetEVM
	n.CortinaBlockTimestamp = subnetEVM
	return n
}

func TestSubnetEVMJSON(t *testing.T) {
	upgrades := withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{
		DurangoBlockTimestamp: utils.PointerTo[uint64](2),
		HeliconTimestamp:      utils.PointerTo[uint64](3),
		IglooTimestamp:        utils.PointerTo[uint64](4),
	})
	const wantJSON = `{"subnetEVMTimestamp":1,"durangoTimestamp":2,"heliconTimestamp":3,"iglooTimestamp":4}`

	got, err := json.Marshal(upgrades)
	require.NoError(t, err)
	require.JSONEq(t, wantJSON, string(got))

	var decoded NetworkUpgrades
	require.NoError(t, json.Unmarshal([]byte(wantJSON), &decoded))
	require.Equal(t, upgrades, &decoded)
	require.True(t, decoded.IsCortina(1))
	require.False(t, decoded.IsApricotPhase1(0))
}

func TestSubnetEVMJSONPreDurangoMismatch(t *testing.T) {
	upgrades := withSubnetEVM(utils.PointerTo[uint64](1), &NetworkUpgrades{})
	upgrades.CortinaBlockTimestamp = utils.PointerTo[uint64](2)

	_, err := json.Marshal(upgrades)
	require.ErrorIs(t, err, errPreDurangoMismatch)
}
