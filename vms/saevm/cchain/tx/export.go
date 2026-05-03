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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
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

// Compare orders [Input] values by [Input.Address] and [Input.AssetID].
func (i Input) Compare(other Input) int {
	if c := i.Address.Cmp(other.Address); c != 0 {
		return c
	}
	return i.AssetID.Compare(other.AssetID)
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

var errOutputsNotSorted = errors.New("outputs not sorted")

func (e *Export) SanityCheck(ctx *snow.Context) error {
	switch {
	case e.NetworkID != ctx.NetworkID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongNetworkID, ctx.NetworkID, e.NetworkID)
	case e.BlockchainID != ctx.ChainID:
		return fmt.Errorf("%w: expected %s, got %s", errWrongChainID, ctx.ChainID, e.BlockchainID)
	case e.DestinationChain != constants.PlatformChainID && e.DestinationChain != ctx.XChainID:
		return fmt.Errorf("%w: expected %s or %s, got %s", errNotSameSubnet, constants.PlatformChainID, ctx.XChainID, e.DestinationChain)
	case len(e.Ins) == 0:
		return errNoInputs
	case len(e.ExportedOutputs) == 0:
		return errNoOutputs
	}

	fc := avax.NewFlowChecker()
	for i, in := range e.Ins {
		if in.Amount == 0 {
			return fmt.Errorf("%w (%d): zero amount", errInvalidInput, i)
		}
		if in.AssetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXInput, i, ctx.AVAXAssetID, in.AssetID)
		}
		fc.Consume(ctx.AVAXAssetID, in.Amount)
	}
	for i, out := range e.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidOutput, i, err)
		}
		if assetID := out.Asset.ID; assetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXOutput, i, ctx.AVAXAssetID, assetID)
		}
		fc.Produce(ctx.AVAXAssetID, out.Out.Amount())
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
	}

	if !utils.IsSortedAndUnique(e.Ins) {
		return errInputsNotSortedUnique
	}
	// Like [atomic.UnsignedExportTx.Verify], outputs aren't enforced to be
	// unique. This is safe because each output's UTXO is keyed by txID and
	// outputIndex, so duplicate outputs still produce distinct UTXOs.
	if !avax.IsSortedTransferableOutputs(e.ExportedOutputs, c) {
		return errOutputsNotSorted
	}

	return nil
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

		// Even if no AVAX is debited, non-AVAX inputs MUST increment the nonce.
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
		if o, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = o.Addresses()
		}

		elems[i] = elem
	}
	return e.DestinationChain, &chainsatomic.Requests{PutRequests: elems}, nil
}

var errInsufficientFunds = errors.New("insufficient funds")

// TransferNonAVAX subtracts the non-AVAX balances from the statedb.
func (e *Export) TransferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error {
	for _, in := range e.Ins {
		if in.AssetID == avaxAssetID {
			continue
		}

		coinID := common.Hash(in.AssetID)
		amount := new(big.Int).SetUint64(in.Amount)
		if balance := statedb.GetBalanceMultiCoin(in.Address, coinID); balance.Cmp(amount) < 0 {
			return fmt.Errorf("%w: address %s asset %s has %d want %d", errInsufficientFunds, in.Address, coinID, balance, amount)
		}
		statedb.SubBalanceMultiCoin(in.Address, coinID, amount)
	}
	return nil
}
