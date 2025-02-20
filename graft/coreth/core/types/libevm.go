// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

var extras = ethtypes.RegisterExtras[
	ethtypes.NOOPHeaderHooks, *ethtypes.NOOPHeaderHooks,
	ethtypes.NOOPBlockBodyHooks, *ethtypes.NOOPBlockBodyHooks,
	isMultiCoin,
]()
