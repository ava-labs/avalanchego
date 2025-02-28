// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"math/big"
)

func BlockGasCost(b *Block) *big.Int {
	cost := GetHeaderExtra(b.Header()).BlockGasCost
	if cost == nil {
		return nil
	}
	return new(big.Int).Set(cost)
}
