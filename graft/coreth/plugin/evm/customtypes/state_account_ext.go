// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
)

type isMultiCoin bool

func IsMultiCoin(s *state.StateDB, addr common.Address) bool {
	return bool(state.GetExtra(s, extras.StateAccount, addr))
}

func SetMultiCoin(s *state.StateDB, addr common.Address, to bool) {
	state.SetExtra(s, extras.StateAccount, addr, isMultiCoin(to))
}
