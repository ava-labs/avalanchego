// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/ava-labs/avalanchego/ids"
)

// CoreTx represents fundamental property of a transaction to have ID and Init method
type CoreTx interface {
	ID() ids.ID
	Init(vm *VM) error
}
