// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package msig

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func GetOwner(state state.Chain, addr ids.ShortID) (*secp256k1fx.OutputOwners, error) {
	msigOwner, err := state.GetMultisigOwner(addr)
	if err != nil && err != database.ErrNotFound {
		return nil, err
	}

	if msigOwner != nil {
		return &msigOwner.Owners, nil
	}

	return &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}, nil
}
