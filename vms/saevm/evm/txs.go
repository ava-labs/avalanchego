// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"iter"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/strevm/hook"
	"github.com/holiman/uint256"
)

type txSlice []*atomic.Tx

func (t *txSlice) NextTx() (*atomic.Tx, bool) {
	if len(*t) == 0 {
		return nil, false
	}
	tx := (*t)[0]
	*t = (*t)[1:]
	return tx, true
}

func (*txSlice) CancelCurrentTx(ids.ID)  {}
func (*txSlice) DiscardCurrentTx(ids.ID) {}
func (*txSlice) DiscardCurrentTxs()      {}

// inputUTXOs returns the set of all UTXOIDs consumed by atomic txs in the
// iterator.
func inputUTXOs(blocks iter.Seq[*types.Block]) (set.Set[ids.ID], error) {
	var inputUTXOs set.Set[ids.ID]
	for block := range blocks {
		// Extract atomic transactions from the block
		txs, err := atomic.ExtractAtomicTxs(
			customtypes.BlockExtData(block),
			true,
			atomic.Codec,
		)
		if err != nil {
			return nil, err
		}

		for _, tx := range txs {
			inputUTXOs.Union(tx.InputUTXOs())
		}
	}
	return inputUTXOs, nil
}

func atomicTxOp(
	tx *atomic.Tx,
	avaxAssetID ids.ID,
	baseFee *big.Int,
) (hook.Op, error) {
	// We do not need to check if we are in ApricotPhase5 here because we assume
	// that this function will only be called when the block is in at least
	// ApricotPhase5.
	gasUsed, err := tx.GasUsed(true)
	if err != nil {
		return hook.Op{}, err
	}
	gasPrice, err := atomic.EffectiveGasPrice(tx.UnsignedAtomicTx, avaxAssetID, true)
	if err != nil {
		return hook.Op{}, err
	}

	op := hook.Op{
		Gas:      gas.Gas(gasUsed),
		GasPrice: gasPrice,
	}
	switch tx := tx.UnsignedAtomicTx.(type) {
	case *atomic.UnsignedImportTx:
		op.To = make(map[common.Address]uint256.Int)
		for _, output := range tx.Outs {
			if output.AssetID != avaxAssetID {
				continue
			}

			// TODO: This implementation assumes that the addresses are unique.
			var amount uint256.Int
			amount.SetUint64(output.Amount)
			amount.Mul(&amount, atomic.X2CRate)
			op.To[output.Address] = amount
		}
	case *atomic.UnsignedExportTx:
		op.From = make(map[common.Address]hook.Account)
		for _, input := range tx.Ins {
			if input.AssetID != avaxAssetID {
				continue
			}

			// TODO: This implementation assumes that the addresses are unique.
			var amount uint256.Int
			amount.SetUint64(input.Amount)
			amount.Mul(&amount, atomic.X2CRate)
			op.From[input.Address] = hook.Account{
				Nonce:  input.Nonce,
				Amount: amount,
			}
		}
	default:
		return hook.Op{}, fmt.Errorf("unexpected atomic tx type: %T", tx)
	}
	return op, nil
}

func marshalAtomicTxs(txs []*atomic.Tx) ([]byte, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	return atomic.Codec.Marshal(atomic.CodecVersion, txs)
}
