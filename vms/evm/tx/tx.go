// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// TxOption is a functional option for configuring a transaction
type TxOption func(*txConfig)

// txConfig holds the configuration for creating a transaction
type txConfig struct {
	chainID          *big.Int
	nonce            uint64
	to               *common.Address
	gas              uint64
	gasFeeCap        *big.Int
	gasTipCap        *big.Int
	value            *big.Int
	data             []byte
	accessList       types.AccessList
	predicateAddress *common.Address
	predicateBytes   []byte
}

// WithPredicate adds predicate information to the transaction
func WithPredicate(address common.Address, bytes []byte) TxOption {
	return func(c *txConfig) {
		c.predicateAddress = &address
		c.predicateBytes = bytes
	}
}

// NewTx returns a types.DynamicFeeTx with optional predicate support
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
	opts ...TxOption,
) *types.Transaction {
	config := &txConfig{
		chainID:    chainID,
		nonce:      nonce,
		to:         to,
		gas:        gas,
		gasFeeCap:  gasFeeCap,
		gasTipCap:  gasTipCap,
		value:      value,
		data:       data,
		accessList: accessList,
	}

	// Apply all options
	for _, opt := range opts {
		opt(config)
	}

	// Add predicate to access list if specified
	if config.predicateAddress != nil && config.predicateBytes != nil {
		// Convert predicate bytes to storage keys for access list
		predicateData := predicate.New(config.predicateBytes)
		numHashes := predicate.RoundUpTo32(len(predicateData)) / common.HashLength
		storageKeys := make([]common.Hash, numHashes)
		for i := range storageKeys {
			start := i * common.HashLength
			copy(storageKeys[i][:], predicateData[start:])
		}

		config.accessList = append(config.accessList, types.AccessTuple{
			Address:     *config.predicateAddress,
			StorageKeys: storageKeys,
		})
	}

	return types.NewTx(&types.DynamicFeeTx{
		ChainID:    config.chainID,
		Nonce:      config.nonce,
		To:         config.to,
		Gas:        config.gas,
		GasFeeCap:  config.gasFeeCap,
		GasTipCap:  config.gasTipCap,
		Value:      config.value,
		Data:       config.data,
		AccessList: config.accessList,
	})
}
