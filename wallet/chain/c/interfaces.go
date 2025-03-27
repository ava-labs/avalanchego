// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"math/big"
)

type BaseFeeEstimator interface {
	EstimateBaseFee(ctx context.Context) (*big.Int, error)
}
