// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// ExtractTxSubnetOwners returns a map of txID to current subnet's
// owner, for the subnets associated to [pChainTxs].
// Extraction is only done for txs of kind CreateSubnetTx
// or TransferSubnetOwnershipTx, noop for other ones.
func ExtractTxSubnetOwners(
	ctx context.Context,
	pClient platformvm.Client,
	pChainTxs map[ids.ID]*txs.Tx,
) (map[ids.ID]fx.Owner, error) {
	subnetIDs := []ids.ID{}
	for _, tx := range pChainTxs {
		switch unsignedTx := tx.Unsigned.(type) {
		case *txs.CreateSubnetTx:
			subnetIDs = append(subnetIDs, tx.ID())
		case *txs.TransferSubnetOwnershipTx:
			subnetIDs = append(subnetIDs, unsignedTx.Subnet)
		}
	}
	subnetOwners := map[ids.ID]fx.Owner{}
	for _, subnetID := range subnetIDs {
		subnetInfo, err := pClient.GetSubnet(ctx, subnetID)
		if err != nil {
			return nil, err
		}
		subnetOwners[subnetID] = &secp256k1fx.OutputOwners{
			Locktime:  subnetInfo.Locktime,
			Threshold: subnetInfo.Threshold,
			Addrs:     subnetInfo.ControlKeys,
		}
	}
	return subnetOwners, nil
}
