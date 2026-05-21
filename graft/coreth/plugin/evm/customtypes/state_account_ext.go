// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/MetalBlockchain/libevm/common"
	"github.com/MetalBlockchain/libevm/core/state"

	ethtypes "github.com/MetalBlockchain/libevm/core/types"
)

type isMultiCoin bool

func IsMultiCoin(s *state.StateDB, addr common.Address) bool {
	return bool(state.GetExtra(s, extras.StateAccount, addr))
}

func SetMultiCoin(s *state.StateDB, addr common.Address, to bool) {
	state.SetExtra(s, extras.StateAccount, addr, isMultiCoin(to))
}

func IsAccountMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(extras.StateAccount.Get(s))
}
