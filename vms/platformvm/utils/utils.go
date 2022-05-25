// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

// Create the blockchain described in [tx], but only if this node is a member of
// the subnet that validates the chain
func CreateChain(vmCfg config.Config, utx unsigned.Tx, txID ids.ID) error {
	unsignedTx, ok := utx.(*unsigned.CreateChainTx)
	if !ok {
		return unsigned.ErrWrongTxType
	}

	if vmCfg.StakingEnabled && // Staking is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != unsignedTx.SubnetID && // All nodes must validate the primary network
		!vmCfg.WhitelistedSubnets.Contains(unsignedTx.SubnetID) { // This node doesn't validate this blockchain
		return nil
	}

	chainParams := chains.ChainParameters{
		ID:          txID,
		SubnetID:    unsignedTx.SubnetID,
		GenesisData: unsignedTx.GenesisData,
		VMAlias:     unsignedTx.VMID.String(),
	}
	for _, fxID := range unsignedTx.FxIDs {
		chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
	}
	vmCfg.Chains.CreateChain(chainParams)
	return nil
}
