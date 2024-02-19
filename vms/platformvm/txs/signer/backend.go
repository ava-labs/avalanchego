// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

type Backend interface {
	GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
	GetSubnetOwner(ctx context.Context, subnetID ids.ID) (fx.Owner, error)
}
