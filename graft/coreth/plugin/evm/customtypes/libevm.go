// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

var extras = ethtypes.RegisterExtras[
	HeaderExtra, *HeaderExtra,
	BlockBodyExtra, *BlockBodyExtra,
	isMultiCoin,
]()
