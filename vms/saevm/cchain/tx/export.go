// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
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

func (e *Export) InputUTXOs() set.Set[ids.ID] {
	set := set.NewSet[ids.ID](len(e.Ins))
	for _, in := range e.Ins {
		set.Add(NonceInputID(in.Address, in.Nonce))
	}
	return set
}

func NonceInputID(address common.Address, nonce uint64) ids.ID {
	var id ids.ID
	packer := wrappers.Packer{Bytes: id[:]} // 32 bytes long
	packer.PackLong(nonce)                  // add 8 bytes
	packer.PackBytes(address.Bytes())       // add 24 bytes
	return id
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

var errZeroAmount = errors.New("zero amount")

func (i Input) Compare(o Input) int {
	if c := i.Address.Cmp(o.Address); c != 0 {
		return c
	}
	return i.AssetID.Compare(o.AssetID)
}

func (i Input) Verify() error {
	if i.Amount == 0 {
		return errZeroAmount
	}
	return nil
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

var errOutputsNotSorted = errors.New("outputs not sorted")

func (e *Export) SanityCheck(ctx context.Context, snowCtx *snow.Context) error {
	switch {
	case e.NetworkID != snowCtx.NetworkID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongNetworkID, snowCtx.NetworkID, e.NetworkID)
	case e.BlockchainID != snowCtx.ChainID:
		return fmt.Errorf("%w: expected %d, got %d", errWrongChainID, snowCtx.ChainID, e.BlockchainID)
	case len(e.Ins) == 0:
		return errNoInputs
	case len(e.ExportedOutputs) == 0:
		return errNoOutputs
	}

	if err := verify.SameSubnet(ctx, snowCtx, e.DestinationChain); err != nil {
		return fmt.Errorf("%w: %w", errNotSameSubnet, err)
	}

	fc := avax.NewFlowChecker()
	for i, in := range e.Ins {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidInput, i, err)
		}
		if in.AssetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXInput, i, snowCtx.AVAXAssetID, in.AssetID)
		}
		fc.Consume(snowCtx.AVAXAssetID, in.Amount)
	}
	for i, out := range e.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidOutput, i, err)
		}
		if assetID := out.AssetID(); assetID != snowCtx.AVAXAssetID {
			return fmt.Errorf("%w (%d): expected %s, got %s", errNonAVAXOutput, i, snowCtx.AVAXAssetID, assetID)
		}
		fc.Produce(snowCtx.AVAXAssetID, out.Out.Amount())
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
	}

	if !utils.IsSortedAndUnique(e.Ins) {
		return errInputsNotSortedUnique
	}
	if !avax.IsSortedTransferableOutputs(e.ExportedOutputs, c) {
		return errOutputsNotSorted
	}

	return nil
}

var (
	sigCache = secp256k1.NewRecoverCache(1024)

	errIncorrectNumSignatures = errors.New("incorrect number of signatures")
	errAddressMismatch        = errors.New("address does not match signature")
)

func (e *Export) VerifyCredentials(_ atomic.SharedMemory, creds []Credential) error {
	if len(e.Ins) != len(creds) {
		return fmt.Errorf("%w: expected %d, got %d", errIncorrectNumCredentials, len(e.Ins), len(creds))
	}

	fxTx, err := toFxTx(e)
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertingToFxTx, err)
	}
	for i, in := range e.Ins {
		cred := creds[i].Self()
		if err := cred.Verify(); err != nil {
			return err
		}
		if len(cred.Sigs) != 1 {
			return fmt.Errorf("%w: expected 1, got %d", errIncorrectNumSignatures, len(cred.Sigs))
		}

		pk, err := sigCache.RecoverPublicKey(fxTx.Bytes(), cred.Sigs[0][:])
		if err != nil {
			return err
		}
		if in.Address != pk.EthAddress() {
			return fmt.Errorf("%w: expected %s, got %s", errAddressMismatch, in.Address, pk.EthAddress())
		}
	}
	return nil
}

var errNonceMismatch = errors.New("nonce mismatch")

func (e *Export) VerifyState(avaxAssetID ids.ID, reader libevm.StateReader) error {
	op, err := e.asOp(avaxAssetID)
	if err != nil {
		return fmt.Errorf("problem converting export to op: %w", err)
	}
	for address, debit := range op.burn {
		if nonce := reader.GetNonce(address); nonce != debit.Nonce {
			return fmt.Errorf("%w: address %s has nonce %d but needs %d", errNonceMismatch, address, nonce, debit.Nonce)
		}
		if balance := reader.GetBalance(address); balance.Lt(&debit.MinBalance) {
			return fmt.Errorf("%w: address %s has balance %s but needs %s", errInsufficientFunds, address, balance.String(), debit.MinBalance.String())
		}
	}
	return nil
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

func (e *Export) AtomicOps(txID ids.ID) (ids.ID, *atomic.Requests, error) {
	elems := make([]*atomic.Element, len(e.ExportedOutputs))
	for i, out := range e.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
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

var errInsufficientFunds = errors.New("insufficient funds")

func (e *Export) TransferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error {
	for _, in := range e.Ins {
		if in.AssetID == avaxAssetID {
			continue
		}

		coinID := common.Hash(in.AssetID)
		amount := new(big.Int).SetUint64(in.Amount)
		if statedb.GetBalanceMultiCoin(in.Address, coinID).Cmp(amount) < 0 {
			// Non-AVAX asset transfers are only allowed during bootstrapping,
			// which should have already verified that this error will not
			// occur.
			return errInsufficientFunds
		}
		statedb.SubBalanceMultiCoin(in.Address, coinID, amount)
	}
	return nil
}
