// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"

	atomicvm "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
)

var (
	// ID this VM should be referenced by
	ID = ids.ID{'e', 'v', 'm'}

	_ vms.Factory = (*Factory)(nil)
)

type Factory struct{}

func (*Factory) New(logging.Logger) (interface{}, error) {
	return atomicvm.WrapVM(&evm.VM{}), nil
}

func NewPluginVM() block.ChainVM {
	return atomicvm.WrapVM(&evm.VM{IsPlugin: true})
}
