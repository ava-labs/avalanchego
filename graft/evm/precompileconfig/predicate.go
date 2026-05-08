// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package precompileconfig defines types and helpers shared by Coreth and
// Subnet-EVM precompile configurations.
package precompileconfig

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	cmath "github.com/ava-labs/libevm/common/math"
	ethcore "github.com/ava-labs/libevm/core"
	ethparams "github.com/ava-labs/libevm/params"
)

// PredicateContext is the context passed in to the Predicater interface to verify
// a precompile predicate within a specific ProposerVM wrapper.
type PredicateContext struct {
	SnowCtx *snow.Context
	// ProposerVMBlockCtx defines the ProposerVM context the predicate is verified within
	ProposerVMBlockCtx *block.Context
}

// Predicater is an optional interface for StatefulPrecompileContracts to implement.
// If implemented, the predicate will be called for each predicate included in the
// access list of a transaction.
// PredicateGas will be called while calculating the IntrinsicGas of a transaction
// causing it to be dropped if the total gas goes above the tx gas limit.
// VerifyPredicate is used to populate a bit set of predicates verified prior to
// block execution, which can be accessed via the StateDB during execution.
// The bitset is stored in the block, so that historical blocks can be re-verified
// without calling VerifyPredicate.
type Predicater interface {
	PredicateGas(pred predicate.Predicate, rules Rules) (uint64, error)
	VerifyPredicate(predicateContext *PredicateContext, pred predicate.Predicate) error
}

// Rules defines the interface that provides information about the current rules of the chain.
type Rules interface {
	IsGraniteActivated() bool
	IsDurangoActivated() bool
}

// AccessListGasWithPredicates calculates intrinsic gas for an access list,
// delegating to a Predicater for any address that has one registered and
// using the standard per-tuple cost otherwise.
func AccessListGasWithPredicates(
	rules Rules,
	predicaters map[common.Address]Predicater,
	accessList libevm.AccessList,
) (uint64, error) {
	var gas uint64
	for _, accessTuple := range accessList {
		address := accessTuple.Address
		predicaterContract, ok := predicaters[address]
		if !ok {
			// Previous access list gas calculation does not use safemath because an overflow would not be possible with
			// the size of access lists that could be included in a block and standard access list gas costs.
			// Therefore, we only check for overflow when adding to [totalGas], which could include the sum of values
			// returned by a predicate.
			accessTupleGas := ethparams.TxAccessListAddressGas + uint64(len(accessTuple.StorageKeys))*ethparams.TxAccessListStorageKeyGas
			totalGas, overflow := cmath.SafeAdd(gas, accessTupleGas)
			if overflow {
				return 0, ethcore.ErrGasUintOverflow
			}
			gas = totalGas
		} else {
			predicateGas, err := predicaterContract.PredicateGas(predicate.Predicate(accessTuple.StorageKeys), rules)
			if err != nil {
				return 0, err
			}
			totalGas, overflow := cmath.SafeAdd(gas, predicateGas)
			if overflow {
				return 0, ethcore.ErrGasUintOverflow
			}
			gas = totalGas
		}
	}
	return gas, nil
}
