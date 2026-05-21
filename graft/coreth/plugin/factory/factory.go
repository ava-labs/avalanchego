// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"github.com/MetalBlockchain/metalgo/graft/coreth/plugin/evm"
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms"

	atomicvm "github.com/MetalBlockchain/metalgo/graft/coreth/plugin/evm/atomic/vm"
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
