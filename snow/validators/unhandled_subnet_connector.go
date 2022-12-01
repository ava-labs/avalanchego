// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var UnhandledSubnetConnector SubnetConnector = &unhandledSubnetConnector{}

type unhandledSubnetConnector struct{}

func (unhandledSubnetConnector) ConnectedSubnet(_ context.Context, nodeID ids.NodeID, subnetID ids.ID) error {
	return fmt.Errorf(
		"unhandled ConnectedSubnet with nodeID=%q and subnetID=%q",
		nodeID,
		subnetID,
	)
}
