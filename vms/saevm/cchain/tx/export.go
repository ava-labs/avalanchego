// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"

	// Imported for [atomic.UnsignedExportTx.Burned] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var _ Unsigned = (*Export)(nil)

// Export is the unsigned component of a transaction that transfers assets from
// the C-Chain to either the P-Chain or the X-Chain. It modifies the C-Chain
// state and produces UTXOs in the shared memory between the C-Chain and the
// destination chain.
type Export struct {
	NetworkID        uint32                     `serialize:"true" json:"networkID"`
	BlockchainID     ids.ID                     `serialize:"true" json:"blockchainID"`
	DestinationChain ids.ID                     `serialize:"true" json:"destinationChain"`
	Ins              []Input                    `serialize:"true" json:"inputs"`
	ExportedOutputs  []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// Input identifies an account + nonce pair on the C-Chain that authorizes the
// asset and quantity to deduct.
//
// If the AssetID is AVAX, the amount will be scaled up to account for the EVM's
// higher denomination.
type Input struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}

// Like [atomic.UnsignedExportTx.Burned], burned will error if the sum of the
// inputs exceeds MaxUint64, even if the total amount burned could be
// represented as a uint64.
//
// Because the total supply of AVAX fits in a uint64, this doesn't matter in
// practice and allows for easier fuzzing.
func (e *Export) burned(assetID ids.ID) (uint64, error) {
	var (
		burned uint64
		err    error
	)
	for _, in := range e.Ins {
		if in.AssetID == assetID {
			burned, err = math.Add(burned, in.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	for _, out := range e.ExportedOutputs {
		if out.Asset.ID == assetID {
			burned, err = math.Sub(burned, out.Out.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	return burned, nil
}

func (e *Export) numSigs() (uint64, error) {
	return uint64(len(e.Ins)), nil
}

var errMultipleNonces = errors.New("multiple nonces for address")

func (e *Export) asOp(avaxAssetID ids.ID) (op, error) {
	burn := make(map[common.Address]hook.AccountDebit, len(e.Ins))
	for _, in := range e.Ins {
		debit, ok := burn[in.Address]
		if ok && debit.Nonce != in.Nonce {
			return op{}, fmt.Errorf("%w: address %s has nonces %d and %d", errMultipleNonces, in.Address, debit.Nonce, in.Nonce)
		}

		// Even if no AVAX is debited, Non-AVAX inputs MUST increment the nonce.
		if in.AssetID == avaxAssetID {
			amount := scaleAVAX(in.Amount)
			if _, overflow := debit.Amount.AddOverflow(&debit.Amount, &amount); overflow {
				return op{}, fmt.Errorf("%w: for address %s", errOverflow, in.Address)
			}
		}

		debit.Nonce = in.Nonce
		debit.MinBalance = debit.Amount
		burn[in.Address] = debit
	}
	return op{
		burn: burn,
	}, nil
}

func (e *Export) atomicRequests(txID ids.ID) (ids.ID, *chainsatomic.Requests, error) {
	elems := make([]*chainsatomic.Element, len(e.ExportedOutputs))
	for i, out := range e.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i), //#nosec G115 -- Won't overflow
			},
			Asset: out.Asset,
			Out:   out.Out,
		}

		utxoBytes, err := c.Marshal(codecVersion, utxo)
		if err != nil {
			return ids.ID{}, nil, err
		}
		utxoID := utxo.InputID()
		elem := &chainsatomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	return e.DestinationChain, &chainsatomic.Requests{PutRequests: elems}, nil
}

var errInsufficientFunds = errors.New("insufficient funds")

func (e *Export) TransferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error {
	for _, in := range e.Ins {
		if in.AssetID == avaxAssetID {
			continue
		}

		coinID := common.Hash(in.AssetID)
		amount := new(big.Int).SetUint64(in.Amount)
		if statedb.GetBalanceMultiCoin(in.Address, coinID).Cmp(amount) < 0 {
			return errInsufficientFunds
		}
		statedb.SubBalanceMultiCoin(in.Address, coinID, amount)
	}
	return nil
}
