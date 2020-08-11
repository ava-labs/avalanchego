package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errSpendOverflow = errors.New("spent amount overflows uint64")
	errNoKeys        = errors.New("no keys provided")
)

// Spends UTXOs controlled by [keys].
// The outputs that are created by this method are defined by [spends].
// Each element of [spends] describes an output.
// Change is sent according to [changeSpend].
func (vm *VM) spend(
	db database.Database,
	keys []*crypto.PrivateKeySECP256K1R,
	amount uint64,
	fee uint64,
) (
	[]*ava.TransferableInput,
	[]*ava.TransferableOutput, // unlocked
	[]*ava.TransferableOutput, // locked
	[][]*crypto.PrivateKeySECP256K1R,
	error,
) {
	addrs := ids.ShortSet{} // The addresses controlled by [keys]
	for _, key := range keys {
		addrs.Add(key.PublicKey().Address())
	}
	utxos, _, _, err := vm.GetUTXOs(db, addrs, ids.ShortEmpty, ids.Empty, -1) // The UTXOs controlled by [keys]
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	kc := secp256k1fx.NewKeychain() // Keychain consumes UTXOs and creates new ones
	for _, key := range keys {
		kc.Add(key)
	}

	// Consume locked UTXOs
	now := uint64(vm.clock.Time().Unix())
	spentLocked := uint64(0) // amount of AVAX that has been placed into the locked outputs
	ins := []*ava.TransferableInput{}
	returnedOuts := []*ava.TransferableOutput{}
	lockedOuts := []*ava.TransferableOutput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}
	for _, utxo := range utxos { // See which UTXOs we can spend
		// If we have enough locked AVAX; stop.
		if spentLocked >= amount {
			break
		}
		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			vm.Ctx.Log.Warn("UTXO has unexpected asset ID %s", assetID) // should never happen
			continue
		}

		out, ok := utxo.Out.(*StakeableLockOut)
		if !ok {
			// not locked
			continue
		}
		if out.Locktime <= now {
			// no longer locked
			continue
		}

		inner, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			// unknown output type can't be cloned
			continue
		}

		inIntf, inSigners, err := kc.Spend(out.TransferableOut, now)
		if err != nil {
			continue
		}
		in, ok := inIntf.(ava.TransferableIn)
		if !ok { // should never happen
			vm.Ctx.Log.Warn("expected input to be ava.TransferableIn but is %T", inIntf)
			continue
		}

		value := in.Amount()
		newSpentLocked, err := safemath.Add64(spentLocked, value)
		if err != nil { // Should never happen
			return nil, nil, nil, nil, err
		}
		spentLocked = newSpentLocked

		toReturn := uint64(0)
		if spentLocked > amount {
			toReturn = spentLocked - amount
		}
		value -= toReturn

		lockedOuts = append(lockedOuts, &ava.TransferableOutput{
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out: &StakeableLockOut{
				Locktime: out.Locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          value,
					OutputOwners: inner.OutputOwners,
				},
			},
		})
		if toReturn > 0 {
			returnedOuts = append(returnedOuts, &ava.TransferableOutput{
				Asset: ava.Asset{ID: vm.avaxAssetID},
				Out: &StakeableLockOut{
					Locktime: out.Locktime,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          toReturn,
						OutputOwners: inner.OutputOwners,
					},
				},
			})
		}
		ins = append(ins, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In: &StakeableLockIn{
				Locktime:       out.Locktime,
				TransferableIn: in,
			},
		})
		signers = append(signers, inSigners)
	}

	burned := uint64(0)          // amount of AVAX that has been burned
	for _, utxo := range utxos { // See which UTXOs we can spend
		// If we have enough unlocked AVAX; stop.
		if burned >= fee && spentLocked >= amount {
			break
		}
		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			vm.Ctx.Log.Warn("UTXO has unexpected asset ID %s", assetID) // should never happen
			continue
		}

		out := utxo.Out
		inner, ok := utxo.Out.(*StakeableLockOut)
		if ok {
			if inner.Locktime > now {
				// currently locked
				continue
			}
			out = inner.TransferableOut
		}

		inIntf, inSigners, err := kc.Spend(out, now)
		if err != nil {
			continue
		}
		in, ok := inIntf.(ava.TransferableIn)
		if !ok { // should never happen
			vm.Ctx.Log.Warn("expected input to be ava.TransferableIn but is %T", inIntf)
			continue
		}

		value := in.Amount()
		amountToLock := uint64(0)
		amountToReturn := uint64(0)
		if spentLocked < amount {
			// We need to lock more funds
			if value > amount-spentLocked {
				// We don't need to lock all of this input
				amountToLock = amount - spentLocked
				// Reduce the value of this input to be able to be put towards
				// the fee or being returned
				value -= amount - spentLocked
				spentLocked = amount
			} else {
				// We need to lock all of this input
				amountToLock = value
				spentLocked += value
				value = 0
			}
		}
		if burned < fee {
			// We need to burn more funds
			if value > fee-burned {
				// We don't need to burn all of this input
				amountToReturn = value - (fee - burned)
				burned = fee
			} else {
				// We need to burn all of this input
				burned += value
			}
		} else {
			// We don't need to burn more funds
			amountToReturn = value
		}

		if amountToLock > 0 {
			changeAddr := kc.Keys[0].PublicKey().Address()
			lockedOuts = append(lockedOuts, &ava.TransferableOutput{
				Asset: ava.Asset{ID: vm.avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountToLock,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}
		if amountToReturn > 0 {
			changeAddr := kc.Keys[0].PublicKey().Address()
			returnedOuts = append(returnedOuts, &ava.TransferableOutput{
				Asset: ava.Asset{ID: vm.avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountToReturn,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}
		ins = append(ins, &ava.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  ava.Asset{ID: vm.avaxAssetID},
			In:     in,
		})
		signers = append(signers, inSigners)
	}

	if burned < fee || spentLocked < amount {
		return nil, nil, nil, nil, fmt.Errorf("provided keys have balance (unlocked, locked) (%d, %d) but need (%d, %d)",
			burned, spentLocked, fee, amount)
	}

	ava.SortTransferableInputsWithSigners(ins, signers) // sort inputs and keys
	ava.SortTransferableOutputs(returnedOuts, vm.codec) // sort outputs
	ava.SortTransferableOutputs(lockedOuts, vm.codec)   // sort outputs

	return ins, returnedOuts, lockedOuts, signers, nil
}

// authorize ...
func (vm *VM) authorize(
	db database.Database,
	subnetID ids.ID,
	keys []*crypto.PrivateKeySECP256K1R,
) (
	verify.Verifiable,
	[]*crypto.PrivateKeySECP256K1R,
	error,
) {
	// Get information about the subnet we're adding a chain to
	subnet, err := vm.getSubnet(db, subnetID)
	if err != nil {
		return nil, nil, fmt.Errorf("subnet %s doesn't exist", subnetID)
	}

	// Make sure the owners of the subnet match the provided keys
	owner, ok := subnet.UnsignedDecisionTx.(*UnsignedCreateSubnetTx).Owner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, nil, errUnknownOwner
	}

	// Add the keys to a keychain
	kc := secp256k1fx.NewKeychain()
	for _, key := range keys {
		kc.Add(key)
	}

	// Attempt to prove ownership of the subnet
	now := uint64(vm.clock.Time().Unix())
	indices, signers, matches := kc.Match(owner, now)
	if !matches {
		return nil, nil, errCantSign
	}

	return &secp256k1fx.Input{SigIndices: indices}, signers, nil
}

var (
	errInputOverflow  = errors.New("inputs overflowed uint64")
	errOutputOverflow = errors.New("outputs overflowed uint64")
)

// Verify that:
// * inputs and outputs are all AVAX
// * sum(inputs{unlocked}) >= sum(outputs{unlocked}) + burnAmount{unlocked}
// * sum(inputs{locked}) >= sum(outputs{locked})
func syntacticVerifySpend(
	ins []*ava.TransferableInput,
	unlockedOuts []*ava.TransferableOutput,
	lockedOuts []*ava.TransferableOutput,
	locked uint64,
	burnedUnlocked uint64,
	avaxAssetID ids.ID,
) error {
	// AVAX consumed in this tx
	consumedLocked := uint64(0)
	consumedUnlocked := uint64(0)
	for _, in := range ins {
		if assetID := in.AssetID(); !assetID.Equals(avaxAssetID) { // all inputs must be AVAX
			return fmt.Errorf("input has unexpected asset ID %s expected %s", assetID, avaxAssetID)
		}

		in := in.Input()
		consumed := in.Amount()
		if _, ok := in.(*StakeableLockIn); ok {
			newConsumed, err := safemath.Add64(consumedLocked, consumed)
			if err != nil {
				return errInputOverflow
			}
			consumedLocked = newConsumed
		} else {
			newConsumed, err := safemath.Add64(consumedUnlocked, consumed)
			if err != nil {
				return errInputOverflow
			}
			consumedUnlocked = newConsumed
		}
	}

	// AVAX produced in this tx
	producedUnlocked := burnedUnlocked
	for _, out := range unlockedOuts {
		if assetID := out.AssetID(); !assetID.Equals(avaxAssetID) { // all outputs must be AVAX
			return fmt.Errorf("output has unexpected asset ID %s expected %s", assetID, avaxAssetID)
		}

		out := out.Output()
		produced := out.Amount()
		if _, ok := out.(*StakeableLockOut); !ok {
			newProduced, err := safemath.Add64(producedUnlocked, produced)
			if err != nil {
				return errOutputOverflow
			}
			producedUnlocked = newProduced
		}
	}

	// AVAX produced in this tx
	producedLocked := uint64(0)
	for _, out := range lockedOuts {
		if assetID := out.AssetID(); !assetID.Equals(avaxAssetID) { // all outputs must be AVAX
			return fmt.Errorf("output has unexpected asset ID %s expected %s", assetID, avaxAssetID)
		}

		out := out.Output()
		produced := out.Amount()
		newProduced, err := safemath.Add64(producedLocked, produced)
		if err != nil {
			return errOutputOverflow
		}
		producedLocked = newProduced
	}
	if producedLocked < locked {
		return fmt.Errorf("tx locked outputs (%d) < required locked (%d)",
			producedLocked, locked)
	}
	if producedUnlocked > consumedUnlocked {
		return fmt.Errorf("tx unlocked outputs (%d) + burn amount (%d) > inputs (%d)",
			producedUnlocked-burnedUnlocked, burnedUnlocked, consumedUnlocked)
	}
	return nil
}

var (
	errLockedFundsNotMarkedAsLocked = errors.New("locked funds not marked as locked")
	errWrongLocktime                = errors.New("wrong locktime reported")
	errUnknownOwners                = errors.New("unknown owners")
)

// Removes the UTXOs consumed by [ins] from the UTXO set
func (vm *VM) consumeInputs(
	db database.Database,
	ins []*ava.TransferableInput,
) error {
	for _, input := range ins {
		utxoID := input.UTXOID.InputID()
		if err := vm.removeUTXO(db, utxoID); err != nil {
			return tempError{err}
		}
	}
	return nil
}

// Adds the UTXOs created by [outs] to the UTXO set.
// [txID] is the ID of the tx that created [outs].
func (vm *VM) produceOutputs(
	db database.Database,
	txID ids.ID,
	outs []*ava.TransferableOutput,
) error {
	for index, out := range outs {
		if err := vm.putUTXO(db, &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: ava.Asset{ID: vm.avaxAssetID},
			Out:   out.Output(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Verify that [tx] is semantically valid.
// [ins] and [outs] are the inputs and outputs of [tx].
// [creds] are the credentials of [tx], which allow [ins] to be spent.
// [db] should not be committed if an error is returned
// Precondition: [tx] has already been syntactically verified
func (vm *VM) semanticVerifySpend(
	db database.Database,
	tx SpendTx,
	ins []*ava.TransferableInput,
	outs []*ava.TransferableOutput,
	creds []verify.Verifiable,
) TxError {
	utxos := make([]*ava.UTXO, len(ins))
	for index, input := range ins {
		utxoID := input.UTXOID.InputID()
		utxo, err := vm.getUTXO(db, utxoID)
		if err != nil {
			return tempError{err}
		}
		utxos[index] = utxo
	}

	return vm.semanticVerifySpendUTXOs(tx, utxos, ins, outs, creds)
}

// Verify that [tx] is semantically valid.
// [ins] and [outs] are the inputs and outputs of [tx].
// [creds] are the credentials of [tx], which allow [ins] to be spent.
// [utxos[i]] is the UTXO being consumed by [ins[i]]
// [db] should not be committed if an error is returned
// Precondition: [tx] has already been syntactically verified
func (vm *VM) semanticVerifySpendUTXOs(
	tx SpendTx,
	utxos []*ava.UTXO,
	ins []*ava.TransferableInput,
	outs []*ava.TransferableOutput,
	creds []verify.Verifiable,
) TxError {
	if len(ins) != len(creds) {
		return permError{fmt.Errorf("there are %d inputs but %d credentials. Should be same number",
			len(ins), len(creds))}
	}
	if len(ins) != len(utxos) {
		return permError{fmt.Errorf("there are %d inputs but %d utxos. Should be same number",
			len(ins), len(utxos))}
	}
	for _, cred := range creds { // Verify credentials are well-formed.
		if err := cred.Verify(); err != nil {
			return permError{err}
		}
	}

	now := uint64(vm.clock.Time().Unix())
	// locktime -> owners -> amount
	produced := make(map[uint64]map[[32]byte]uint64)
	consumed := make(map[uint64]map[[32]byte]uint64)
	for index, input := range ins {
		utxo := utxos[index] // The UTXO consumed by [input]

		out := utxo.Out
		locktime := uint64(0)
		// Set [locktime] to this UTXO's locktime, if applicable
		if inner, ok := out.(*StakeableLockOut); ok {
			out = inner.TransferableOut
			locktime = inner.Locktime
		}

		in := input.In
		// The UTXO says it's locked until [locktime], but this input, which consumes it,
		// is not locked even though [locktime] hasn't passed. This is invalid.
		if inner, ok := in.(*StakeableLockIn); now < locktime && !ok {
			return permError{errLockedFundsNotMarkedAsLocked}
		} else if ok {
			if inner.Locktime != locktime {
				// This input is locked, but its locktime is wrong
				return permError{errWrongLocktime}
			}
			in = inner.TransferableIn
		}

		// Verify that this tx's credentials allow [in] to be spent
		if err := vm.fx.VerifyTransfer(tx, in, creds[index], out); err != nil {
			return permError{err}
		}

		owned, ok := out.(Owned)
		if !ok {
			return permError{errUnknownOwners}
		}
		owner := owned.Owners()
		ownerBytes, err := vm.codec.Marshal(owner)
		if err != nil {
			return tempError{err}
		}
		ownerID := hashing.ComputeHash256Array(ownerBytes)
		owners, ok := consumed[locktime]
		if !ok {
			owners = make(map[[32]byte]uint64)
			consumed[locktime] = owners
		}
		newAmount, err := safemath.Add64(owners[ownerID], in.Amount())
		if err != nil {
			return permError{err}
		}
		owners[ownerID] = newAmount
	}
	for _, out := range outs {
		output := out.Output()
		locktime := uint64(0)
		// Set [locktime] to this output's locktime, if applicable
		if inner, ok := output.(*StakeableLockOut); ok {
			output = inner.TransferableOut
			locktime = inner.Locktime
		}

		owned, ok := output.(Owned)
		if !ok {
			return permError{errUnknownOwners}
		}
		owner := owned.Owners()
		ownerBytes, err := vm.codec.Marshal(owner)
		if err != nil {
			return tempError{err}
		}
		ownerID := hashing.ComputeHash256Array(ownerBytes)
		owners, ok := produced[locktime]
		if !ok {
			owners = make(map[[32]byte]uint64)
			produced[locktime] = owners
		}
		newAmount, err := safemath.Add64(owners[ownerID], output.Amount())
		if err != nil {
			return permError{err}
		}
		owners[ownerID] = newAmount
	}

	// [unlockedProduced] is the amount of unlocked tokens produced by [outs]
	unlockedProduced := uint64(0)
	for _, amount := range produced[0] {
		newAmount, err := safemath.Add64(unlockedProduced, amount)
		if err != nil {
			return permError{err}
		}
		unlockedProduced = newAmount
	}
	delete(produced, 0)

	// [unlockedConsumed] is the amount of unlocked tokens consumed by [ins]
	unlockedConsumed := uint64(0)
	for _, amount := range consumed[0] {
		newAmount, err := safemath.Add64(unlockedConsumed, amount)
		if err != nil {
			return permError{err}
		}
		unlockedConsumed = newAmount
	}
	delete(consumed, 0)

	if unlockedProduced > unlockedConsumed { // More unlocked tokens produced than consumed. Invalid.
		return permError{errInvalidAmount}
	}

	// Make sure that for each locktime, tokens produced <= tokens consumed
	for locktime, producedAmounts := range produced {
		consumedAmounts := consumed[locktime]
		for ownerID, amount := range producedAmounts {
			if amount > consumedAmounts[ownerID] {
				return permError{errInvalidAmount}
			}
		}
	}

	return nil
}
