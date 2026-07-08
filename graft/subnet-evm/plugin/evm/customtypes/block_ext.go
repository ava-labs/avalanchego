// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

func BlockGasCost(b *ethtypes.Block) *big.Int {
	cost := GetHeaderExtra(b.Header()).BlockGasCost
	if cost == nil {
		return nil
	}
	return new(big.Int).Set(cost)
}

func BlockTimeMilliseconds(b *ethtypes.Block) *uint64 {
	time := GetHeaderExtra(b.Header()).TimeMilliseconds
	if time == nil {
		return nil
	}
	cp := *time
	return &cp
}

func BlockMinDelayExponent(b *ethtypes.Block) *dynamic.DelayExponent {
	e := GetHeaderExtra(b.Header()).MinDelayExponent
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

func BlockTime(eth *ethtypes.Header) time.Time {
	if t := GetHeaderExtra(eth).TimeMilliseconds; t != nil {
		return time.UnixMilli(int64(*t))
	}
	return time.Unix(int64(eth.Time), 0)
}
