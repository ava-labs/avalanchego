// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package dynamic preserves the former C-Chain-local import path.
//
// Deprecated: use github.com/ava-labs/avalanchego/vms/evm/dynamic.
package dynamic

import (
	"github.com/ava-labs/avalanchego/vms/components/gas"

	evmdynamic "github.com/ava-labs/avalanchego/vms/evm/dynamic"
)

type (
	DelayExponent  = evmdynamic.DelayExponent
	PriceExponent  = evmdynamic.PriceExponent
	TargetExponent = evmdynamic.TargetExponent
)

const (
	InitialDelayExponent  = evmdynamic.InitialDelayExponent
	InitialPriceExponent  = evmdynamic.InitialPriceExponent
	InitialTargetExponent = evmdynamic.InitialTargetExponent
	MinTarget             = evmdynamic.MinTarget
)

func DesiredDelayExponent(desired uint64) DelayExponent {
	return evmdynamic.DesiredDelayExponent(desired)
}

func DesiredPriceExponent(desired gas.Price) PriceExponent {
	return evmdynamic.DesiredPriceExponent(desired)
}

func DesiredTargetExponent(desired gas.Gas) TargetExponent {
	return evmdynamic.DesiredTargetExponent(desired)
}
