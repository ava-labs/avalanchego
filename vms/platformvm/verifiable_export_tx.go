// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errOverflowExport = errors.New("overflow when computing export amount + txFee")

	_ VerifiableUnsignedAtomicTx = VerifiableUnsignedExportTx{}
)

type VerifiableUnsignedExportTx struct {
	*transactions.UnsignedExportTx `serialize:"true"`
}

// SemanticVerify this transaction is valid.
func (tx VerifiableUnsignedExportTx) SemanticVerify(
	vm *VM,
	parentState MutableState,
	stx *transactions.SignedTx,
) (VersionedState, TxError) {
	syntacticCtx := transactions.AtomicTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          platformcodec.Codec,
		AvmID:      vm.ctx.XChainID,
		FeeAssetID: vm.ctx.AVAXAssetID,
		FeeAmount:  vm.TxFee,
	}
	if err := tx.SyntacticVerify(syntacticCtx); err != nil {
		return nil, permError{err}
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.ExportedOutputs)

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(parentState, tx, tx.Ins, outs, stx.Creds, vm.TxFee, vm.ctx.AVAXAssetID); err != nil {
		switch err.(type) {
		case permError:
			return nil, permError{
				fmt.Errorf("failed semanticVerifySpend: %w", err),
			}
		default:
			return nil, tempError{
				fmt.Errorf("failed semanticVerifySpend: %w", err),
			}
		}
	}

	// Set up the state if this tx is committed
	newState := newVersionedState(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	// Consume the UTXOS
	consumeInputs(newState, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(newState, txID, vm.ctx.AVAXAssetID, tx.Outs)
	return newState, nil
}

// Accept this transaction
func (tx VerifiableUnsignedExportTx) Accept(ctx *snow.Context, batch database.Batch) error {
	txID := tx.ID()

	elems := make([]*atomic.Element, len(tx.ExportedOutputs))
	for i, out := range tx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.Outs) + i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := platformcodec.Codec.Marshal(platformcodec.Version, utxo)
		if err != nil {
			return fmt.Errorf("failed to marshal UTXO: %w", err)
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

	return ctx.SharedMemory.Apply(map[ids.ID]*atomic.Requests{tx.DestinationChain: {PutRequests: elems}}, batch)
}

// Create a new transaction
func (vm *VM) newExportTx(
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*transactions.SignedTx, error) {
	if vm.ctx.XChainID != chainID {
		return nil, transactions.ErrWrongChainID
	}

	toBurn, err := safemath.Add64(amount, vm.TxFee)
	if err != nil {
		return nil, errOverflowExport
	}
	ins, outs, _, signers, err := vm.stake(keys, 0, toBurn, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	utx := VerifiableUnsignedExportTx{
		UnsignedExportTx: &transactions.UnsignedExportTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    vm.ctx.NetworkID,
				BlockchainID: vm.ctx.ChainID,
				Ins:          ins,
				Outs:         outs, // Non-exported outputs
			}},
			DestinationChain: chainID,
			ExportedOutputs: []*avax.TransferableOutput{{ // Exported to X-Chain
				Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{to},
					},
				},
			}},
		},
	}
	tx := &transactions.SignedTx{UnsignedTx: utx}
	if err := tx.Sign(platformcodec.Codec, signers); err != nil {
		return nil, err
	}

	syntacticCtx := transactions.AtomicTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          platformcodec.Codec,
		AvmID:      vm.ctx.XChainID,
		FeeAssetID: vm.ctx.AVAXAssetID,
		FeeAmount:  vm.TxFee,
	}
	return tx, utx.SyntacticVerify(syntacticCtx)
}
