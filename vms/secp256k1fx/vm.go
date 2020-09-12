// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// VM that this Fx must be run by
type VM interface {
	Codec() codec.Codec
	Clock() *timer.Clock
	Logger() logging.Logger
}

// TestVM is a minimal implementation of a VM
type TestVM struct {
	CLK  *timer.Clock
	Code codec.Codec
	Log  logging.Logger
}

// Clock returns CLK
func (vm *TestVM) Clock() *timer.Clock { return vm.CLK }

// Codec returns Code
func (vm *TestVM) Codec() codec.Codec { return vm.Code }

// Logger returns Log
func (vm *TestVM) Logger() logging.Logger { return vm.Log }
