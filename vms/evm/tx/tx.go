// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"math/big"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// NewTx returns a types.DynamicFeeTx with the predicate tuple added to the
// access list of the transaction.
func NewTx(
	chainID *big.Int,
	nonce uint64,
	to *common.Address,
	gas uint64,
	gasFeeCap *big.Int,
	gasTipCap *big.Int,
	value *big.Int,
	data []byte,
	accessList types.AccessList,
	predicateAddress common.Address,
	predicateBytes []byte,
) *types.Transaction {
	// Convert predicate bytes to storage keys for access list
	predicateData := predicate.New(predicateBytes)
	numHashes := predicate.RoundUpTo32(len(predicateData)) / common.HashLength
	storageKeys := make([]common.Hash, numHashes)
	for i := range storageKeys {
		start := i * common.HashLength
		copy(storageKeys[i][:], predicateData[start:])
	}

	accessList = append(accessList, types.AccessTuple{
		Address:     predicateAddress,
		StorageKeys: storageKeys,
	})
	return types.NewTx(&types.DynamicFeeTx{
		ChainID:    chainID,
		Nonce:      nonce,
		To:         to,
		Gas:        gas,
		GasFeeCap:  gasFeeCap,
		GasTipCap:  gasTipCap,
		Value:      value,
		Data:       data,
		AccessList: accessList,
	})
}
