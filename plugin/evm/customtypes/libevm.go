// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	ethtypes "github.com/ava-labs/libevm/core/types"
)

var extras ethtypes.ExtraPayloads[*HeaderExtra, *BlockBodyExtra, isMultiCoin]

// Register registers the types with libevm. It MUST NOT be called more than
// once and therefore is only allowed to be used in tests and `package main`, to
// avoid polluting other packages that transitively depend on this one but don't
// need registration.
//
// Without a call to Register, none of the functionality of this package will
// work, and most will simply panic.
func Register() {
	extras = ethtypes.RegisterExtras[
		HeaderExtra, *HeaderExtra,
		BlockBodyExtra, *BlockBodyExtra,
		isMultiCoin,
	]()
	IsMultiCoinPayloads = extras.StateAccount
}
