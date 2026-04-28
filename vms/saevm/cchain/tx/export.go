// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

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

var errMultipleNonces = errors.New("multiple inputs for address with different nonces")

func (e *Export) asOp(avaxAssetID ids.ID) (op, error) {
	burn := make(map[common.Address]hook.AccountDebit, len(e.Ins))
	for _, in := range e.Ins {
		debit, ok := burn[in.Address]
		if ok && debit.Nonce != in.Nonce {
			return op{}, fmt.Errorf("%w: address %s has nonces %d and %d", errMultipleNonces, in.Address, debit.Nonce, in.Nonce)
		}

		// Non-AVAX inputs still record the address+nonce so SAE will increment
		// the nonce, even though no AVAX is debited.
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

func (e *Export) atomicRequests(txID ids.ID) (ids.ID, *atomic.Requests, error) {
	elems := make([]*atomic.Element, len(e.ExportedOutputs))
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
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}
	return e.DestinationChain, &atomic.Requests{PutRequests: elems}, nil
}
