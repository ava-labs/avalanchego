// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// CoreTx represents fundamental property of a transaction to have ID, Inputs and Outputs
type CoreTx interface {
	ID() ids.ID
	Outputs() []*avax.TransferableOutput
	Inputs() []*avax.TransferableInput
	InitFx(vm *VM) error
}
