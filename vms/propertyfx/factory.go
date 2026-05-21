// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/vms/fx"
)

const Name = "propertyfx"

var (
	_ fx.Factory = (*Factory)(nil)

	// ID that this Fx uses when labeled
	ID = ids.ID{'p', 'r', 'o', 'p', 'e', 'r', 't', 'y', 'f', 'x'}
)

type Factory struct{}

func (*Factory) New() any {
	return &Fx{}
}
