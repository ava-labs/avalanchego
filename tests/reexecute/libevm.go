// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

// RegisterLibEVMExtras configures libevm for C-Chain behaviour. It MUST NOT be
// called more than once.
//
// TODO(JonathanOppenheimer): Delete this once production code no longer
// registers the extras through coreth.
func RegisterLibEVMExtras() {
	core.RegisterExtras()
	customtypes.Register()
	extstate.RegisterExtras()
	params.RegisterExtras()
}
