// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package primary

import (
	"context"
	"math/big"
)

type BaseFeeEstimator interface {
	EstimateBaseFee(ctx context.Context) (*big.Int, error)
}
