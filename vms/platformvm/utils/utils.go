// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Create the blockchain described in [tx], but only if this node is a member of
// the subnet that validates the chain
func CreateChain(vmCfg config.Config, tx *txs.CreateChainTx, txID ids.ID) {
	if vmCfg.StakingEnabled && // Staking is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != tx.SubnetID && // All nodes must validate the primary network
		!vmCfg.WhitelistedSubnets.Contains(tx.SubnetID) { // This node doesn't validate this blockchain
		return
	}

	chainParams := chains.ChainParameters{
		ID:          txID,
		SubnetID:    tx.SubnetID,
		GenesisData: tx.GenesisData,
		VMAlias:     tx.VMID.String(),
	}
	for _, fxID := range tx.FxIDs {
		chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
	}
	vmCfg.Chains.CreateChain(chainParams)
}
