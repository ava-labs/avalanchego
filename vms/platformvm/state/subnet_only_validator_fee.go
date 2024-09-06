// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

type ValidatorState struct {
	Current                  gas.Gas
	Target                   gas.Gas
	Capacity                 gas.Gas
	Excess                   gas.Gas
	MinFee                   gas.Price
	ExcessConversionConstant gas.Gas
}

func (v ValidatorState) AdvanceTime(seconds uint64) ValidatorState {
	return ValidatorState{
		Current:                  v.Current,
		Target:                   v.Target,
		Capacity:                 v.Capacity,
		Excess:                   fee.CalculateExcess(v.Target, v.Current, v.Excess, seconds),
		MinFee:                   v.MinFee,
		ExcessConversionConstant: v.ExcessConversionConstant,
	}
}

func (v ValidatorState) CalculateContinuousFee(seconds uint64) uint64 {
	return fee.CalculateCost(v.Target, v.Current, v.MinFee, v.ExcessConversionConstant, v.Excess, seconds)
}

// Returns the first number n where CalculateContinuousFee(n) >= balance
func (v ValidatorState) CalculateTimeTillContinuousFee(balance uint64) uint64 {
	return fee.CalculateDuration(v.Target, v.Current, v.MinFee, v.ExcessConversionConstant, v.Excess, math.MaxUint64, balance)
}
