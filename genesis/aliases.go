// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/evm"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, map[ids.ID][]string, error) {
	generalAliases := getGeneralAliases()
	chainAliases := map[ids.ID][]string{
		constants.PlatformChainID: {"P", "platform"},
	}
	vmAliases := getChainAliases()
	genesis := &platformvm.Genesis{} // TODO let's not re-create genesis to do aliasing
	if _, err := platformvm.GenesisCodec.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, nil, nil, err
	}
	if err := genesis.Initialize(); err != nil {
		return nil, nil, nil, err
	}

	for _, chain := range genesis.Chains {
		uChain := chain.UnsignedTx.(*platformvm.UnsignedCreateChainTx)
		switch uChain.VMID {
		case avm.ID:
			generalAliases["bc/"+chain.ID().String()] = []string{"X", "avm", "bc/X", "bc/avm"}
			chainAliases[chain.ID()] = GetXChainAliases()
		case evm.ID:
			generalAliases["bc/"+chain.ID().String()] = []string{"C", "evm", "bc/C", "bc/evm"}
			chainAliases[chain.ID()] = GetCChainAliases()
		}
	}
	return generalAliases, chainAliases, vmAliases, nil
}

func GetCChainAliases() []string {
	return []string{"C", "evm"}
}

func GetXChainAliases() []string {
	return []string{"X", "avm"}
}

func getGeneralAliases() map[string][]string {
	return map[string][]string{
		"vm/" + platformvm.ID.String():             {"vm/platform"},
		"vm/" + avm.ID.String():                    {"vm/avm"},
		"vm/" + evm.ID.String():                    {"vm/evm"},
		"bc/" + constants.PlatformChainID.String(): {"P", "platform", "bc/P", "bc/platform"},
	}
}

func getChainAliases() map[ids.ID][]string {
	return map[ids.ID][]string{
		platformvm.ID:  {"platform"},
		avm.ID:         {"avm"},
		evm.ID:         {"evm"},
		secp256k1fx.ID: {"secp256k1fx"},
		nftfx.ID:       {"nftfx"},
		propertyfx.ID:  {"propertyfx"},
	}
}
