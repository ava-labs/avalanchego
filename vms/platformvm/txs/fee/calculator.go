// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var ErrUnsupportedTx = errors.New("unsupported transaction type")

// Calculator calculates the minimum required fee, in nAVAX, that an unsigned
// transaction must pay for valid inclusion into a block.
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)

	// CalculateFeeForGas returns the fee for an explicit amount of gas. Eth
	// txs need it because their gas depends on how many inputs selection
	// consumes, which is decided during execution.
	CalculateFeeForGas(gas.Gas) (uint64, error)
}
