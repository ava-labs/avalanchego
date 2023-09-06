// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"path"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constant"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := map[string][]string{
		path.Join(constant.ChainAliasPrefix, constant.PlatformChainID.String()): {
			"P",
			"platform",
			path.Join(constant.ChainAliasPrefix, "P"),
			path.Join(constant.ChainAliasPrefix, "platform"),
		},
	}
	chainAliases := map[ids.ID][]string{
		constant.PlatformChainID: {"P", "platform"},
	}

	genesis, err := genesis.Parse(genesisBytes) // TODO let's not re-create genesis to do aliasing
	if err != nil {
		return nil, nil, err
	}
	for _, chain := range genesis.Chains {
		uChain := chain.Unsigned.(*txs.CreateChainTx)
		chainID := chain.ID()
		endpoint := path.Join(constant.ChainAliasPrefix, chainID.String())
		switch uChain.VMID {
		case constant.AVMID:
			apiAliases[endpoint] = []string{
				"X",
				"avm",
				path.Join(constant.ChainAliasPrefix, "X"),
				path.Join(constant.ChainAliasPrefix, "avm"),
			}
			chainAliases[chainID] = GetXChainAliases()
		case constant.EVMID:
			apiAliases[endpoint] = []string{
				"C",
				"evm",
				path.Join(constant.ChainAliasPrefix, "C"),
				path.Join(constant.ChainAliasPrefix, "evm"),
			}
			chainAliases[chainID] = GetCChainAliases()
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

func GetVMAliases() map[ids.ID][]string {
	return map[ids.ID][]string{
		constant.PlatformVMID: {"platform"},
		constant.AVMID:        {"avm"},
		constant.EVMID:        {"evm"},
		secp256k1fx.ID:        {"secp256k1fx"},
		nftfx.ID:              {"nftfx"},
		propertyfx.ID:         {"propertyfx"},
	}
}
