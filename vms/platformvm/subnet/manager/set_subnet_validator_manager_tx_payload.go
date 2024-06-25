// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import "github.com/ava-labs/avalanchego/ids"

type SetSubnetManagerTxWarpMessagePayload struct {
	SubnetID ids.ID `serialize:"true" json:"subnetID"`
	ChainID  ids.ID `serialize:"true" json:"chainID"`
	Addr     []byte `serialize:"true" json:"address"`
}
