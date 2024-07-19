// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/fee"
)

// Calculator is the interfaces that any fee Calculator must implement
type Calculator interface {
	CalculateFee(tx *txs.Tx) (uint64, error)

	GetFee() uint64
	ResetFee(newFee uint64)
	AddFeesFor(complexity fee.Dimensions) (uint64, error)
	RemoveFeesFor(unitsToRm fee.Dimensions) (uint64, error)
	GetGasPrice() fee.GasPrice
	GetBlockGas() (fee.Gas, error)
	GetGasCap() fee.Gas
	setCredentials(creds []*fxs.FxCredential)
	IsEActive() bool
}
