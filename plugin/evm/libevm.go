// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm/customtypes"
)

// RegisterAllLibEVMExtras is a convenience wrapper for calling
// [core.RegisterExtras], [customtypes.Register], and [params.RegisterExtras].
// Together these are necessary and sufficient for configuring libevm for
// C-Chain behaviour.
//
// It MUST NOT be called more than once and therefore is only allowed to be used
// in tests and `package main`, to avoid polluting other packages that
// transitively depend on this one but don't need registration.
func RegisterAllLibEVMExtras() {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
}
