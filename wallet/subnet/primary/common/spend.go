// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

// MatchOwners attempts to match a list of addresses up to the provided
// threshold.
func MatchOwners(
	owners *secp256k1fx.OutputOwners,
	addrs ids.ShortSet,
	minIssuanceTime uint64,
) ([]uint32, bool) {
	if owners.Locktime > minIssuanceTime {
		return nil, false
	}

	sigs := make([]uint32, 0, owners.Threshold)
	for i := uint32(0); i < uint32(len(owners.Addrs)) && uint32(len(sigs)) < owners.Threshold; i++ {
		addr := owners.Addrs[i]
		if addrs.Contains(addr) {
			sigs = append(sigs, i)
		}
	}
	return sigs, uint32(len(sigs)) == owners.Threshold
}
