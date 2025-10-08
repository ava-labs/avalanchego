// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/libevm/libevm/pseudo"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

type isMultiCoin bool

var IsMultiCoinPayloads pseudo.Accessor[ethtypes.StateOrSlimAccount, isMultiCoin]

func IsMultiCoin(s ethtypes.StateOrSlimAccount) bool {
	return bool(IsMultiCoinPayloads.Get(s))
}
