// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms"
	"github.com/MetalBlockchain/metalgo/vms/avm/config"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct {
	config.Config
}

func (f *Factory) New(logging.Logger) (interface{}, error) {
	return &VM{Config: f.Config}, nil
}
