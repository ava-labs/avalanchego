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
	"github.com/ava-labs/avalanchego/vms/timestampvm"
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := getAPIAliases()
	chainAliases := map[ids.ID][]string{
		constants.PlatformChainID: {"P", "platform"},
	}
	genesis := &platformvm.Genesis{} // TODO let's not re-create genesis to do aliasing
	if _, err := platformvm.GenesisCodec.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, nil, err
	}
	if err := genesis.Initialize(); err != nil {
		return nil, nil, err
	}

	for _, chain := range genesis.Chains {
		uChain := chain.UnsignedTx.(*platformvm.UnsignedCreateChainTx)
		switch uChain.VMID {
		case avm.ID:
			apiAliases[ids.ChainAliasPrefix+chain.ID().String()] = []string{"X", "avm", ids.ChainAliasPrefix + "X", ids.ChainAliasPrefix + "/avm"}
			chainAliases[chain.ID()] = GetXChainAliases()
		case evm.ID:
			apiAliases[ids.ChainAliasPrefix+chain.ID().String()] = []string{"C", "evm", ids.ChainAliasPrefix + "C", ids.ChainAliasPrefix + "evm"}
			chainAliases[chain.ID()] = GetCChainAliases()
		case timestampvm.ID:
			apiAliases[ids.ChainAliasPrefix+chain.ID().String()] = []string{ids.ChainAliasPrefix + "timestamp"}
			chainAliases[chain.ID()] = []string{"timestamp"}
		}
	}
	return apiAliases, chainAliases, nil
}

func GetCChainAliases() []string {
	return []string{"C", "evm"}
}

func GetXChainAliases() []string {
	return []string{"X", "avm"}
}

func getAPIAliases() map[string][]string {
	return map[string][]string{
		ids.VMAliasPrefix + platformvm.ID.String():                {ids.VMAliasPrefix + "platform"},
		ids.VMAliasPrefix + avm.ID.String():                       {ids.VMAliasPrefix + "avm"},
		ids.VMAliasPrefix + evm.ID.String():                       {ids.VMAliasPrefix + "evm"},
		ids.VMAliasPrefix + timestampvm.ID.String():               {ids.VMAliasPrefix + "timestamp"},
		ids.ChainAliasPrefix + constants.PlatformChainID.String(): {"P", "platform", ids.ChainAliasPrefix + "P", ids.ChainAliasPrefix + "platform"},
	}
}

func GetVMAliases() map[ids.ID][]string {
	return map[ids.ID][]string{
		platformvm.ID:  {"platform"},
		avm.ID:         {"avm"},
		evm.ID:         {"evm"},
		timestampvm.ID: {"timestamp"},
		secp256k1fx.ID: {"secp256k1fx"},
		nftfx.ID:       {"nftfx"},
		propertyfx.ID:  {"propertyfx"},
	}
}
