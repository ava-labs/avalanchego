// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/avm"
	"github.com/ava-labs/gecko/vms/nftfx"
	"github.com/ava-labs/gecko/vms/platformvm"
	"github.com/ava-labs/gecko/vms/propertyfx"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
	"github.com/ava-labs/gecko/vms/spchainvm"
	"github.com/ava-labs/gecko/vms/spdagvm"
	"github.com/ava-labs/gecko/vms/timestampvm"
)

// Aliases returns the default aliases based on the network ID
func Aliases(networkID uint32) (map[string][]string, map[[32]byte][]string, map[[32]byte][]string, error) {
	generalAliases := map[string][]string{
		"vm/" + platformvm.ID.String():  {"vm/platform"},
		"vm/" + avm.ID.String():         {"vm/avm"},
		"vm/" + EVMID.String():          {"vm/evm"},
		"vm/" + spdagvm.ID.String():     {"vm/spdag"},
		"vm/" + spchainvm.ID.String():   {"vm/spchain"},
		"vm/" + timestampvm.ID.String(): {"vm/timestamp"},
		"bc/" + ids.Empty.String():      {"P", "platform", "bc/P", "bc/platform"},
	}
	chainAliases := map[[32]byte][]string{
		ids.Empty.Key(): {"P", "platform"},
	}
	vmAliases := map[[32]byte][]string{
		platformvm.ID.Key():  {"platform"},
		avm.ID.Key():         {"avm"},
		EVMID.Key():          {"evm"},
		spdagvm.ID.Key():     {"spdag"},
		spchainvm.ID.Key():   {"spchain"},
		timestampvm.ID.Key(): {"timestamp"},
		secp256k1fx.ID.Key(): {"secp256k1fx"},
		nftfx.ID.Key():       {"nftfx"},
		propertyfx.ID.Key():  {"propertyfx"},
	}

	genesisBytes, _, err := Genesis(networkID)
	if err != nil {
		return nil, nil, nil, err
	}

	genesis := &platformvm.Genesis{} // TODO let's not re-create genesis to do aliasing
	if err := platformvm.Codec.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, nil, nil, err
	}
	if err := genesis.Initialize(); err != nil {
		return nil, nil, nil, err
	}

	for _, chain := range genesis.Chains {
		switch {
		case avm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"X", "avm", "bc/X", "bc/avm"}
			chainAliases[chain.ID().Key()] = []string{"X", "avm"}
		case EVMID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"C", "evm", "bc/C", "bc/evm"}
			chainAliases[chain.ID().Key()] = []string{"C", "evm"}
		case spdagvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spdag"}
			chainAliases[chain.ID().Key()] = []string{"spdag"}
		case spchainvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/spchain"}
			chainAliases[chain.ID().Key()] = []string{"spchain"}
		case timestampvm.ID.Equals(chain.VMID):
			generalAliases["bc/"+chain.ID().String()] = []string{"bc/timestamp"}
			chainAliases[chain.ID().Key()] = []string{"timestamp"}
		}
	}
	return generalAliases, chainAliases, vmAliases, nil
}
