// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func retrieveSubnetOwnerFromTx(c Chain, subnetID ids.ID) (fx.Owner, error) {
	subnetIntf, _, err := c.GetTx(subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w %q: %w",
			ErrCantFindSubnet,
			subnetID,
			err,
		)
	}

	subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%q %w", subnetID, errIsNotSubnet)
	}

	return subnet.Owner, nil
}
