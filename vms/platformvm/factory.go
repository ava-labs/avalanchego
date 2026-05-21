// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/vms"
	"github.com/MetalBlockchain/metalgo/vms/platformvm/config"
)

var _ vms.Factory = (*Factory)(nil)

// Factory can create new instances of the Platform Chain
type Factory struct {
	config.Internal
}

// New returns a new instance of the Platform Chain
func (f *Factory) New(logging.Logger) (interface{}, error) {
	return &VM{Internal: f.Internal}, nil
}
