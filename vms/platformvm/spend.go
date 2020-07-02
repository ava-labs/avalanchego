package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
)

var (
	errSpendOverflow = errors.New("spent amount overflows uint64")
)

/*
// return the inputs and outputs resulting from [key] paying the tx fee
func (vm *VM) payFee(db database.Database, key *crypto.PrivateKeySECP256K1R) (ins []*ava.TransferableInput, outs []*ava.TransferableOutput, err error) {
	// Get UTXOs controlled by [key]
	addrSet := ids.ShortSet{}
	addrSet.Add(key.PublicKey().Address())
	utxos, err := vm.getUTXOs(db, addrSet)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	// Spend the tx fee
	kc := secp256k1fx.NewKeychain()
	kc.Add(key)
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	amountSpent := uint64(0)
	for _, utxo := range utxos {
		if !utxo.AssetID().Equals(vm.ava) { // should never happen. TODO: log
			continue
		}
		inputIntf, signers, err := kc.Spend(utxo.Out, 0)
		if err != nil {
			continue // should never happen. TODO: log
		}
		input, ok := inputIntf.(ava.Transferable)
		if !ok {
			continue // should never happen. TODO: log
		}
		amountSpent, err = safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			return nil, nil, errSpendOverflow
		}

		in := &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: assetID},
			In:     input,
		}

		ins = append(ins, in)
		keys = append(keys, signers)

		if amountSpent >= txFee {
			break
		}
	}

	if amountSpent < uint64(args.Amount) {
		return nil, nil, errInsufficientFunds
	}

	ava.SortTransferableInputsWithSigners(ins, keys)

	outs = append(outs, &ava.TransferableOutput{
		Asset: ava.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:      uint64(args.Amount),
			Locktime: 0,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	})

	if amountSpent > txFee {
		changeAddr := kc.Keys[0].PublicKey().Address()
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:      amountSpent - txFee,
				Locktime: 0,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}

	ava.SortTransferableOutputs(outs, service.vm.codec)
}
*/

// Verify that:
// * inputs and outputs are sorted
// * inputs and outputs are all AVAX
// * sum(inputs) >= sum(outputs) + txFee
func syntacticVerifySpend(ins []*ava.TransferableInput, outs []*ava.TransferableOutput, txFee uint64, avaxAssetID ids.ID) error {
	var err error
	avaxConsumed := uint64(0) // AVAX consumed in this tx
	for _, in := range ins {
		if !in.AssetID().Equals(avaxAssetID) { // all inputs must be AVAX
			return fmt.Errorf("input has unexpected asset ID %s", in.AssetID())
		} else if err := in.Verify(); err != nil {
			return err
		} else if avaxConsumed, err = safemath.Add64(avaxConsumed, in.Input().Amount()); err != nil {
			return errors.New("inputs overflowed uint64")
		}
	}
	avaxProduced := uint64(txFee) // AVAX produced in this tx, plus the txFee
	for _, out := range outs {
		if !out.AssetID().Equals(avaxAssetID) { // all outputs must be AVAX
			return fmt.Errorf("output has unexpected asset ID %s", out.AssetID())
		} else if err = out.Verify(); err != nil {
			return err
		} else if avaxProduced, err = safemath.Add64(avaxProduced, out.Output().Amount()); err != nil {
			return errors.New("outputs overflowed uint64")
		}
	}
	if avaxProduced > avaxConsumed {
		return fmt.Errorf("tx outputs (%d) + txFee (%d) > inputs (%d)", avaxProduced-txFee, txFee, avaxConsumed)
	} else if !ava.IsSortedTransferableOutputs(outs, Codec) {
		return errOutputsNotSorted
	} else if !ava.IsSortedAndUniqueTransferableInputs(ins) {
		return errInputsNotSortedUnique
	}
	return nil
}

// verify that the UTXOs spent by [tx] exist and are spendable with the given credentials
// TODO: Is this right?
func (vm *VM) semanticVerifySpend(db database.Database, tx SpendTx) error {
	creds := tx.Creds()
	for i, in := range tx.Ins() {
		if utxo, err := vm.getUTXO(db, &in.UTXOID); err != nil {
			return err
		} else if err := vm.fx.VerifyTransfer(tx, in.In, creds[i], utxo.Out); err != nil {
			return err
		}
	}
	return nil
}
