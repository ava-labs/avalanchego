package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/vms/components/verify"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errSpendOverflow = errors.New("spent amount overflows uint64")
	errNoKeys        = errors.New("no keys provided")
)

// spend spends [Amount] tokens.
// [Threshold] signatures from [Addrs] are required to spend the output.
// It can't be spent until [Locktime].
type spend struct {
	Amount    uint64
	Threshold uint32
	Locktime  uint64
	Addrs     []ids.ShortID
}

/*
// Return the inputs and outputs resulting from [keys] burning [toSpend],
// as well as the keys used to spend each input
// Any change is sent to the first key in [keys]
func (vm *VM) spend(
	db database.Database,
	toSpend uint64,
	keys []*crypto.PrivateKeySECP256K1R,
) (ins []*ava.TransferableInput, outs []*ava.TransferableOutput, signerKeys [][]*crypto.PrivateKeySECP256K1R, err error) {
	if len(keys) == 0 {
		err = errNoKeys
		return
	}
	addrs := make([][]byte, len(keys), len(keys)) // The addresses controlled by [keys]
	for i, key := range keys {
		addrs[i] = key.PublicKey().Address().Bytes()
	}
	utxos, err := vm.getUTXOs(db, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		err = fmt.Errorf("couldn't get UTXOs: %w", err)
		return
	}
	kc := secp256k1fx.NewKeychain() // Keychain spends UTXOs and creates outputs
	for _, key := range keys {
		kc.Add(key)
	}
	now := uint64(vm.clock.Time().Unix())
	amountSpent := uint64(0)
	for _, utxo := range utxos { // Calculate inputs to this tx
		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			vm.Ctx.Log.Warn("UTXO has unexpected asset ID %s", assetID) // should never happen
			continue
		}
		inputIntf, signers, err2 := kc.Spend(utxo.Out, now)
		if err2 != nil {
			vm.Ctx.Log.Warn("coudn't spend UTXO %s: %s", utxo.InputID(), err2) // shouldn't happen
			continue
		}
		input, ok := inputIntf.(ava.TransferableIn)
		if !ok { // should never happen
			vm.Ctx.Log.Warn("input has unexpected type")
			continue
		}
		amountSpent, err = safemath.Add64(amountSpent, input.Amount())
		if err != nil { // Should never happen
			err = errOverflow
			return
		}
		ins = append(ins, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     input,
		})
		signerKeys = append(signerKeys, signers)
		if amountSpent >= toSpend { // We have enough AVAX; stop.
			break
		}
	}
	if amountSpent < toSpend {
		err = fmt.Errorf("provided keys have %d nAVAX but need %d", amountSpent, toSpend)
		return
	}
	ava.SortTransferableInputsWithSigners(ins, signerKeys) // sort inputs

	// This transaction has at most one output, which, if necessary, gives change back to the 1st key
	// You may be wondering, "what about the stake amount and transaction fee?"
	// Those are burned. When the staker gets their staked AVAX back, it will be in the form of a new UTXO.
	if amountSpent > toSpend {
		var changeAddrBytes [20]byte
		copy(changeAddrBytes[:], addrs[0]) // Change goes to 1st key
		changeAddr := ids.NewShortID(changeAddrBytes)
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amountSpent - toSpend,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{changeAddr},
				},
			},
		})
	}
	ava.SortTransferableOutputs(outs, vm.codec) //sort outputs
	return
}
*/

// Spends UTXOs controlled by [keys].
// The outputs that are created by this method are defined by [spends].
// Each element of [spends] describes an output.
// Change is sent according to [changeSpend].
func (vm *VM) spend(
	db database.Database,
	keys []*crypto.PrivateKeySECP256K1R,
	spends []*spend,
	changeSpend *spend,
	burnAmt uint64,
) (ins []*ava.TransferableInput, outs []*ava.TransferableOutput, credKeys [][]*crypto.PrivateKeySECP256K1R, err error) {
	if len(keys) == 0 {
		err = errNoKeys
		return
	}

	toSpend := uint64(burnAmt) // Calculate the total amount we need to spend
	for _, spend := range spends {
		if toSpend, err = safemath.Add64(toSpend, spend.Amount); err != nil {
			return
		}
	}
	addrs := make([][]byte, len(keys), len(keys)) // The addresses controlled by [keys]
	for i, key := range keys {
		addrs[i] = key.PublicKey().Address().Bytes()
	}
	utxos, err := vm.getUTXOs(db, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		err = fmt.Errorf("couldn't get UTXOs: %w", err)
		return
	}
	kc := secp256k1fx.NewKeychain() // Keychain consumes UTXOs and creates new ones
	for _, key := range keys {
		kc.Add(key)
	}

	// Consume UTXOs
	now := uint64(vm.clock.Time().Unix())
	fundsAvailable := uint64(0)
	for _, utxo := range utxos { // See which UTXOs we can spend
		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			vm.Ctx.Log.Warn("UTXO has unexpected asset ID %s", assetID) // should never happen
			continue
		} else if inputIntf, signers, err2 := kc.Spend(utxo.Out, now); err2 != nil {
			continue
		} else if input, ok := inputIntf.(ava.Transferable); !ok { // should never happen
			vm.Ctx.Log.Warn("expected input to be ava.Transferable but is %T", inputIntf)
			continue
		} else if fundsAvailable, err = safemath.Add64(fundsAvailable, input.Amount()); err != nil { // Should never happen
			return
		} else {
			ins = append(ins, &ava.TransferableInput{
				UTXOID: utxo.UTXOID,
				Asset:  ava.Asset{ID: vm.avaxAssetID},
				In:     input,
			})
			credKeys = append(credKeys, signers)
			if fundsAvailable >= toSpend { // We have enough AVAX; stop.
				break
			}
		}
	}
	if fundsAvailable < toSpend {
		err = fmt.Errorf("provided keys have balance %d but need %d", fundsAvailable, toSpend)
		return
	}
	ava.SortTransferableInputsWithSigners(ins, credKeys) // sort inputs and keys

	// We have enough tokens. Create the outputs.
	for _, spend := range spends {
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: spend.Amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  spend.Locktime,
					Threshold: spend.Threshold,
					Addrs:     spend.Addrs,
				},
			},
		})
		fundsAvailable, err = safemath.Sub64(fundsAvailable, spend.Amount)
		if err != nil {
			return // Should never happen
		}
	}

	// If there is change, send it to [changeAddr]
	if fundsAvailable > 0 {
		outs = append(outs, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: fundsAvailable, // Amount is ignored for [changeSpend]. Just send change.
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  changeSpend.Locktime,
					Threshold: changeSpend.Threshold,
					Addrs:     changeSpend.Addrs,
				},
			},
		})
	}
	ava.SortTransferableOutputs(outs, vm.codec) //sort outputs
	return
}

// Verify that:
// * inputs and outputs are sorted
// * inputs and outputs are all AVAX
// * sum(inputs) >= sum(outputs) + burnAmount
func syntacticVerifySpend(
	ins []*ava.TransferableInput,
	outs []*ava.TransferableOutput,
	creds []verify.Verifiable,
	burnAmount uint64,
	avaxAssetID ids.ID,
) error {
	if len(ins) != len(creds) {
		return fmt.Errorf("there are %d inputs but %d credentials. Should be same number", len(ins), len(creds))
	}
	for _, cred := range creds {
		if err := cred.Verify(); err != nil {
			return err
		}
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
// Precondition: [tx] has already been syntactically verified
func (vm *VM) semanticVerifySpend(
	db database.Database,
	tx SpendTx,
	ins []*ava.TransferableInput,
	outs []*ava.TransferableOutput,
	creds []verify.Verifiable,
) TxError {
	for index, in := range ins {
		if utxo, err := vm.getUTXO(db, in.UTXOID.InputID()); err != nil {
			return tempError{err}
		} else if err := vm.fx.VerifyTransfer(tx, in.In, creds[index], utxo.Out); err != nil {
			return permError{err}
		} else if err := vm.removeUTXO(db, in.UTXOID.InputID()); err != nil {
			return tempError{err}
		}
	}
	txID := tx.ID()
	for index, out := range outs {
		if err := vm.putUTXO(db, &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out:   out.Output(),
		}); err != nil {
			return tempError{err}
		}
	}
	return nil
}
