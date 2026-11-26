// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warptest

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
)

// AccessList returns an access list carrying each of msgs for the warp
// precompile.
func AccessList(msgs ...*warp.Message) types.AccessList {
	al := make(types.AccessList, len(msgs))
	for i, msg := range msgs {
		al[i] = types.AccessTuple{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(msg.Bytes()),
		}
	}
	return al
}
