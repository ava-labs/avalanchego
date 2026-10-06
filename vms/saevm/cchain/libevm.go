// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
)

// RegisterLibEVMExtras registers the C-Chain hooks and payloads with libevm:
// EVM hooks, header and block-body extras, state-key normalization, and
// chain-config extras. Together these are necessary and sufficient for libevm
// to exhibit C-Chain behaviour.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func RegisterLibEVMExtras() {
	core.RegisterExtras()
	customtypes.Register()
	extstate.RegisterExtras()
	corethparams.RegisterExtras()
}
