// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	cmath "github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	ethparams "github.com/ava-labs/libevm/params"
)

// ErrAccessListGasDeltaUnderflow is returned when the predicate-aware access
// list gas from Coreth is less than libevm's flat access-list intrinsic term.
// That should not happen for valid chain rules; it indicates a logic mismatch.
var ErrAccessListGasDeltaUnderflow = errors.New("access list intrinsic gas delta underflow")

// AccessListIntrinsicGasDelta returns the gas that must be charged on top of
// libevm's flat access-list intrinsic gas so the access-list portion matches
// Coreth's predicate-aware accessListGas.
//
// When the active rules define no predicaters, or accessList is empty, it returns (0, nil).
func AccessListIntrinsicGasDelta(rules params.Rules, accessList types.AccessList) (uint64, error) {
	if len(accessList) == 0 {
		return 0, nil
	}
	rulesExtra := params.GetRulesExtra(rules)
	if !rulesExtra.PredicatersExist() {
		return 0, nil
	}

	// i.e coreth's accessListGas
	var accessListGas uint64
	for _, tuple := range accessList {
		address := tuple.Address
		predicaterContract, ok := rulesExtra.Predicaters[address]
		if !ok {
			tupleGas := ethparams.TxAccessListAddressGas + uint64(len(tuple.StorageKeys))*ethparams.TxAccessListStorageKeyGas
			total, overflow := cmath.SafeAdd(accessListGas, tupleGas)
			if overflow {
				return 0, core.ErrGasUintOverflow
			}
			accessListGas = total
		} else {
			predicateGas, err := predicaterContract.PredicateGas(predicate.Predicate(tuple.StorageKeys), rulesExtra)
			if err != nil {
				return 0, err
			}
			total, overflow := cmath.SafeAdd(accessListGas, predicateGas)
			if overflow {
				return 0, core.ErrGasUintOverflow
			}
			accessListGas = total
		}
	}

	// These are already charged by libevm's intrinsic gas calculation. Whereas
	// Coreth does not charge these.
	libevmGas := uint64(len(accessList)) * ethparams.TxAccessListAddressGas
	libevmGas += uint64(accessList.StorageKeys()) * ethparams.TxAccessListStorageKeyGas

	if accessListGas < libevmGas {
		return 0, ErrAccessListGasDeltaUnderflow
	}
	return accessListGas - libevmGas, nil
}
