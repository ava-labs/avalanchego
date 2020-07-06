package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errSpendOverflow = errors.New("spent amount overflows uint64")
)

// return the inputs and outputs resulting from [keys] paying spending [toSpend]
// TODO comment better
func (vm *VM) spend(
	db database.Database,
	toSpend uint64,
	keys []*crypto.PrivateKeySECP256K1R,
) ([]*ava.TransferableInput, []*ava.TransferableOutput, [][]*crypto.PrivateKeySECP256K1R, error) {
	if len(keys) == 0 {
		return nil, nil, nil, fmt.Errorf("no keys provided")
	}
	addrs := ids.ShortSet{} // The addresses controlled by [keys]
	for _, key := range keys {
		addrs.Add(key.PublicKey().Address())
	}
	utxos, err := vm.getUTXOs(db, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		return nil, nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}
	kc := secp256k1fx.NewKeychain() // Keychain spends UTXOs and creates outputs
	for _, key := range keys {
		kc.Add(key)
	}
	now := uint64(vm.clock.Time().Unix())
	txSigners := [][]*crypto.PrivateKeySECP256K1R{} // Element i is set of keys to spend input i
	amountSpent := uint64(0)
	ins := []*ava.TransferableInput{}
	for _, utxo := range utxos { // Calculate inputs to this tx
		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			vm.Ctx.Log.Warn("UTXO has unexpected asset ID %s", assetID) // should never happen
			continue
		}
		inputIntf, signers, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(ava.Transferable)
		if !ok { // should never happen
			vm.Ctx.Log.Warn("input has unexpected type")
			continue
		}
		amountSpent, err = safemath.Add64(amountSpent, input.Amount())
		if err != nil { // Should never happen
			return nil, nil, nil, fmt.Errorf("overflow while calculating amount spent")
		}
		ins = append(ins, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     input,
		})
		txSigners = append(txSigners, signers)
		if amountSpent >= toSpend { // We have enough AVAX; stop.
			break
		}
	}
	if amountSpent < toSpend {
		return nil, nil, nil, fmt.Errorf("provided keys don't have %d AVAX", toSpend)
	}
	ava.SortTransferableInputsWithSigners(ins, txSigners) // sort inputs

	// This transaction has at most one output, which, if necessary, gives change back to the 1st key
	// You may be wondering, "what about the stake amount and transaction fee?"
	// Those are burned. When the staker gets their staked AVAX back, it will be in the form of a new UTXO.
	outs := make([]*ava.TransferableOutput, 0, 1)
	if amountSpent > toSpend {
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:      amountSpent - toSpend,
				Locktime: 0,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs.CappedList(1)[0]}, // Change goes to 1st key
				},
			},
		})
	}
	ava.SortTransferableOutputs(outs, vm.codec) //sort outputs

	return ins, outs, txSigners, nil
}

// Verify that:
// * inputs and outputs are sorted
// * inputs and outputs are all AVAX
// * sum(inputs) >= sum(outputs) + burnAmount
func syntacticVerifySpend(tx SpendTx, burnAmount uint64, avaxAssetID ids.ID) error {
	ins := tx.Ins()
	outs := tx.Outs()
	creds := tx.Creds()
	if len(ins) != len(creds) {
		return fmt.Errorf("there are %d inputs but %d credentials. Should be same number", len(ins), len(outs))
	}

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
	avaxProducedAndBurnt := uint64(burnAmount) // AVAX produced in this tx, plus the burned amount
	for _, out := range outs {
		if !out.AssetID().Equals(avaxAssetID) { // all outputs must be AVAX
			return fmt.Errorf("output has unexpected asset ID %s", out.AssetID())
		} else if err = out.Verify(); err != nil {
			return err
		} else if avaxProducedAndBurnt, err = safemath.Add64(avaxProducedAndBurnt, out.Output().Amount()); err != nil {
			return errors.New("outputs overflowed uint64")
		}
	}
	if avaxProducedAndBurnt > avaxConsumed {
		return fmt.Errorf("tx outputs (%d) + burn amount (%d) > inputs (%d)",
			avaxProducedAndBurnt-burnAmount, burnAmount, avaxConsumed)
	} else if !ava.IsSortedTransferableOutputs(outs, Codec) {
		return errOutputsNotSorted
	} else if !ava.IsSortedAndUniqueTransferableInputs(ins) {
		return errInputsNotSortedUnique
	}
	return nil
}

// Verify that the UTXOs spent by [tx] exist and are spendable with the given credentials
// Adds/removes the new/old UTXOs
// [db] should not be committed if an error is returned
// Precondition: [tx] has already been semantically verified
// TODO: Is this right?
func (vm *VM) semanticVerifySpend(db database.Database, tx SpendTx) error {
	creds := tx.Creds()
	for index, in := range tx.Ins() {
		if utxo, err := vm.getUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		} else if err := vm.fx.VerifyTransfer(tx, in.In, creds[index], utxo.Out); err != nil {
			return err
		} else if err := vm.removeUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		}
	}
	txID := tx.ID()
	for index, out := range tx.Outs() {
		if err := vm.putUTXO(db, &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out:   out,
		}); err != nil {
			return err
		}
	}
	return nil
}
