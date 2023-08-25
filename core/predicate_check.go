// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	"github.com/ethereum/go-ethereum/common"
)

// CheckPredicates checks that all precompile predicates are satisfied within the current [predicateContext] for [tx]
func CheckPredicates(rules params.Rules, predicateContext *precompileconfig.PredicateContext, tx *types.Transaction) error {
	// Short circuit early if there are no precompile predicates to verify
	if len(rules.Predicates) == 0 {
		return nil
	}

	// Check that the transaction can cover its IntrinsicGas (including the gas required by the predicate) before
	// verifying the predicate.
	intrinsicGas, err := IntrinsicGas(tx.Data(), tx.AccessList(), tx.To() == nil, rules)
	if err != nil {
		return err
	}
	if tx.Gas() < intrinsicGas {
		return fmt.Errorf("insufficient gas for predicate verification (%d) < intrinsic gas (%d)", tx.Gas(), intrinsicGas)
	}

	// Track addresses that we've performed a predicate check for
	precompileAddressChecks := make(map[common.Address]struct{})
	for _, accessTuple := range tx.AccessList() {
		address := accessTuple.Address
		predicater, ok := rules.Predicates[address]
		if !ok {
			continue
		}
		// Return an error if we've already checked a predicate for this address
		if _, ok := precompileAddressChecks[address]; ok {
			return fmt.Errorf("predicate %s failed verification for tx %s: specified %s in access list multiple times", address, tx.Hash(), address)
		}
		precompileAddressChecks[address] = struct{}{}
		predicateBytes := predicateutils.HashSliceToBytes(accessTuple.StorageKeys)
		if err := predicater.VerifyPredicate(predicateContext, predicateBytes); err != nil {
			return fmt.Errorf("predicate %s failed verification for tx %s: %w", address, tx.Hash(), err)
		}
	}

	return nil
}
