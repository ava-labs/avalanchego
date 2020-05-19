package platformvm

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// return the inputs and outputs resulting from [key] paying the tx fee
func (vm *VM) payFee(db database.Database, key *crypto.PrivateKeySECP256K1R) (ins []*ava.TransferableInput, outs []*ava.TransferableOutput, err error) {
	// Get UTXOs controlled by [key]
	addrSet := ids.ShortSet{}
	addrSet.Add(key.PublicKey().Address())
	utxos, err := vm.getUTXOs(db, addrSet)
	if err != nil {
		return nil.nfmt.Errorf("couldn't get UTXOs: %w", err)
	}

	// Spend the tx fee
	kc := secp256k1fx.NewKeychain()
	kc.Add(key)
	keys := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos {
		if !utxo.AssetID().Equals(vm.ava) { // should never happen
			continue
		}
		inputIntf, signers, err := kc.Spend(utxo.Out, time)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(ava.Transferable)
		if !ok {
			continue // should never happen
		}
		spent, err := safemath.Add64(amountSpent, input.Amount())
		if err != nil {
			return errSpendOverflow
		}
		amountSpent = spent

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

func (vm *VM) syntacticVerifySpend(db database.Database, ins []*secp256k1fx.TransferInput, outs []*secp256k1fx.TransferOutput) {
	fc := ava.NewFlowChecker()
	for _, out := range outs {
		if err := out.Verify(); err != nil {
			return err
		}
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	if !ava.IsSortedTransferableOutputs(outs, c) {
		return errOutputsNotSorted
	}

	for _, in := range ins {
		if err := in.Verify(); err != nil {
			return err
		}
		fc.Consume(in.AssetID(), in.Input().Amount())
	}
	if !ava.IsSortedAndUniqueTransferableInputs(ins) {
		return errInputsNotSortedUnique
	}

	if err := fc.Verify(); err != nil {
		return err
	}
}

func (vm *VM) semanticVerifySpend(vm *VM, uTx *UniqueTx, creds []verify.Verifiable) error {
	for i, in := range t.Ins {
		cred := creds[i]

		fxIndex, err := vm.getFx(cred)
		if err != nil {
			return err
		}
		fx := vm.fxs[fxIndex].Fx

		utxo, err := vm.getUTXO(&in.UTXOID)
		if err != nil {
			return err
		}

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if !utxoAssetID.Equals(inAssetID) {
			return errAssetIDMismatch
		}

		if !vm.verifyFxUsage(fxIndex, inAssetID) {
			return errIncompatibleFx
		}

		if err := fx.VerifyTransfer(uTx, in.In, cred, utxo.Out); err != nil {
			return err
		}
	}
	return nil
}
