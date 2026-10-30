// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompileconfig defines types and helpers shared by Coreth and
// Subnet-EVM precompile configurations.
package precompileconfig

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// PredicateContext is the context that Warp messages are verified within.
type PredicateContext struct {
	SnowCtx *snow.Context
	// ProposerVMBlockCtx defines the ProposerVM context the predicate is verified within
	ProposerVMBlockCtx *block.Context
}

// Rules defines the interface that provides information about the current rules of the chain.
type Rules interface {
	IsGraniteActivated() bool
	IsDurangoActivated() bool
}

// AccessListGas calculates the intrinsic gas of an access list. Each entry at
// warpAddress is a Warp message and costs warpGas. Every other entry costs the
// standard per-tuple amount.
func AccessListGas(
	accessList libevm.AccessList,
	warpAddress common.Address,
	warpGas func(predicate.Predicate) (uint64, error),
) (uint64, error) {
	var gas uint64
	for _, tuple := range accessList {
		var tupleGas uint64
		if tuple.Address == warpAddress {
			var err error
			tupleGas, err = warpGas(predicate.Predicate(tuple.StorageKeys))
			if err != nil {
				return 0, err
			}
		} else {
			// The standard cost of a tuple cannot overflow for any access list
			// that fits in a block, but the sum of Warp costs can.
			tupleGas = params.TxAccessListAddressGas + uint64(len(tuple.StorageKeys))*params.TxAccessListStorageKeyGas
		}
		var overflow bool
		gas, overflow = math.SafeAdd(gas, tupleGas)
		if overflow {
			return 0, core.ErrGasUintOverflow
		}
	}
	return gas, nil
}
