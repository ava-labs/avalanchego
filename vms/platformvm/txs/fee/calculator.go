// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var ErrUnsupportedTx = errors.New("unsupported transaction type")

// Calculator calculates the minimum required fee, in nAVAX, that an unsigned
// transaction must pay for valid inclusion into a block.
type Calculator interface {
	CalculateFee(tx txs.UnsignedTx) (uint64, error)
}
