// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

// NewPredicateTx returns a transaction with the predicateAddress/predicateBytes tuple
// packed and added to the access list of the transaction.
func NewPredicateTx(
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
	accessList = append(accessList, types.AccessTuple{
		Address:     predicateAddress,
		StorageKeys: utils.BytesToHashSlice(PackPredicate(predicateBytes)),
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
