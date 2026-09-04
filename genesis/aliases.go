// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"path"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	pChainAlias = "platform"
	xChainAlias = "avm"
	cChainAlias = "evm"
)

var (
	PChainAliases = []string{"P", pChainAlias}
	XChainAliases = []string{"X", xChainAlias}
	CChainAliases = []string{"C", cChainAlias}
	VMAliases     = map[ids.ID][]string{
		constants.PlatformVMID: {pChainAlias},
		constants.AVMID:        {xChainAlias},
		constants.EVMID:        {cChainAlias},
		secp256k1fx.ID:         {"secp256k1fx"},
		nftfx.ID:               {"nftfx"},
		propertyfx.ID:          {"propertyfx"},
	}
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := map[string][]string{
		path.Join(constants.ChainAliasPrefix, constants.PlatformChainID.String()): {
			"P",
			pChainAlias,
			path.Join(constants.ChainAliasPrefix, "P"),
			path.Join(constants.ChainAliasPrefix, pChainAlias),
		},
	}
	chainAliases := map[ids.ID][]string{
		constants.PlatformChainID: PChainAliases,
	}

	genesis, err := genesis.Parse(genesisBytes) // TODO let's not re-create genesis to do aliasing
	if err != nil {
		return nil, nil, err
	}
	for _, chain := range genesis.Chains {
		uChain := chain.Unsigned.(*txs.CreateChainTx)
		chainID := chain.ID()
		endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
		switch uChain.VMID {
		case constants.AVMID:
			apiAliases[endpoint] = []string{
				"X",
				xChainAlias,
				path.Join(constants.ChainAliasPrefix, "X"),
				path.Join(constants.ChainAliasPrefix, xChainAlias),
			}
			chainAliases[chainID] = XChainAliases
		case constants.EVMID:
			apiAliases[endpoint] = []string{
				"C",
				cChainAlias,
				path.Join(constants.ChainAliasPrefix, "C"),
				path.Join(constants.ChainAliasPrefix, cChainAlias),
			}
			chainAliases[chainID] = CChainAliases
		}
	}
	return apiAliases, chainAliases, nil
}
