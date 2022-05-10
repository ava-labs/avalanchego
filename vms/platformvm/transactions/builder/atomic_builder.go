// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	ErrNoFunds        = errors.New("no spendable funds were found")
	errOverflowExport = errors.New("overflow when computing export amount + txFee")
)

// Max number of items allowed in a page
const MaxPageSize = 1024

type AtomicTxBuilder interface {
	NewImportTx(
		from ids.ID, // chain to import from
		to ids.ShortID, // Address of recipient
		keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)

	NewExportTx(
		amount uint64, // Amount of tokens to export
		chainID ids.ID, // Chain to send the UTXOs to
		to ids.ShortID, // Address of chain recipient
		keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
		changeAddr ids.ShortID, // Address to send change to, if there is any
	) (*signed.Tx, error)
}

func (b *builder) NewImportTx(
	from ids.ID, // chain to import from
	to ids.ShortID, // Address of recipient
	keys []*crypto.PrivateKeySECP256K1R, // Keys to import the funds
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	kc := secp256k1fx.NewKeychain(keys...)

	atomicUTXOs, _, _, err := b.GetAtomicUTXOs(from, kc.Addresses(), ids.ShortEmpty, ids.Empty, MaxPageSize)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving atomic UTXOs: %w", err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := uint64(0)
	now := b.clk.Unix()
	for _, utxo := range atomicUTXOs {
		if utxo.AssetID() != b.ctx.AVAXAssetID {
			continue
		}
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		importedAmount, err = math.Add64(importedAmount, input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)

	if importedAmount == 0 {
		return nil, ErrNoFunds // No imported UTXOs were spendable
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	if importedAmount < b.cfg.TxFee { // imported amount goes toward paying tx fee
		var baseSigners [][]*crypto.PrivateKeySECP256K1R
		ins, outs, _, baseSigners, err = b.Stake(keys, 0, b.cfg.TxFee-importedAmount, changeAddr)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
		signers = append(baseSigners, signers...)
	} else if importedAmount > b.cfg.TxFee {
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: importedAmount - b.cfg.TxFee,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		})
	}

	// Create the transaction
	utx := &unsigned.ImportTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Outs:         outs,
			Ins:          ins,
		}},
		SourceChain:    from,
		ImportedInputs: importedInputs,
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}

func (b *builder) NewExportTx(
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*signed.Tx, error) {
	toBurn, err := math.Add64(amount, b.cfg.TxFee)
	if err != nil {
		return nil, errOverflowExport
	}
	ins, outs, _, signers, err := b.Stake(keys, 0, toBurn, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	utx := &unsigned.ExportTx{
		BaseTx: unsigned.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.ctx.NetworkID,
			BlockchainID: b.ctx.ChainID,
			Ins:          ins,
			Outs:         outs, // Non-exported outputs
		}},
		DestinationChain: chainID,
		ExportedOutputs: []*avax.TransferableOutput{{ // Exported to X-Chain
			Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
	}
	tx := &signed.Tx{Unsigned: utx}
	if err := tx.Sign(unsigned.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(b.ctx)
}
