package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errSpendOverflow                = errors.New("spent amount overflows uint64")
	errNoKeys                       = errors.New("no keys provided")
	errLockedFundsNotMarkedAsLocked = errors.New("locked funds not marked as locked")
	errWrongLocktime                = errors.New("wrong locktime reported")
	errUnknownOwners                = errors.New("unknown owners")
)

// stake the provided amount while deducting the provided fee.
// Arguments:
// - [db] is the database that is used to attempt to fetch the funds from.
// - [keys] are the owners of the funds
// - [amount] is the amount of funds that are trying to be staked
// - [fee] is the amount of AVAX that should be burned
// Returns:
// - [inputs] the inputs that should be consumed to fund the outputs
// - [returnedOutputs] the outputs that should be immediately returned to the
//                     UTXO set
// - [stakedOutputs] the outputs that should be locked for the duration of the
//                   staking period
// - [signers] the proof of ownership of the funds being moved
func (vm *VM) stake(
	db database.Database,
	keys []*crypto.PrivateKeySECP256K1R,
	amount uint64,
	fee uint64,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // returnedOutputs
	[]*avax.TransferableOutput, // stakedOutputs
	[][]*crypto.PrivateKeySECP256K1R, // signers
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

	// Minimum time this transaction will be issued at
	now := uint64(vm.clock.Time().Unix())

	ins := []*avax.TransferableInput{}
	returnedOuts := []*avax.TransferableOutput{}
	stakedOuts := []*avax.TransferableOutput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	// Amount of AVAX that has been staked
	amountStaked := uint64(0)

	// Consume locked UTXOs
	for _, utxo := range utxos {
		// If we have consumed more AVAX than we are trying to stake, then we
		// have no need to consume more locked AVAX
		if amountStaked >= amount {
			break
		}

		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			continue // We only care about staking AVAX, so ignore other assets
		}

		out, ok := utxo.Out.(*StakeableLockOut)
		if !ok {
			// This output isn't locked, so it will be handled during the next
			// iteration of the UTXO set
			continue
		}
		if out.Locktime <= now {
			// This output is no longer locked, so it will be handled during the
			// next iteration of the UTXO set
			continue
		}

		inner, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			continue
		}

		inIntf, inSigners, err := kc.Spend(out.TransferableOut, now)
		if err != nil {
			// We couldn't spend the output, so move on to the next one
			continue
		}
		in, ok := inIntf.(avax.TransferableIn)
		if !ok { // should never happen
			vm.Ctx.Log.Warn("expected input to be avax.TransferableIn but is %T", inIntf)
			continue
		}

		// The remaining value is initially the full value of the input
		remainingValue := in.Amount()

		// Stake any value that should be staked
		amountToStake := safemath.Min64(
			amount-amountStaked, // Amount we still need to stake
			remainingValue,      // Amount available to stake
		)
		amountStaked += amountToStake
		remainingValue -= amountToStake

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: vm.avaxAssetID},
			In: &StakeableLockIn{
				Locktime:       out.Locktime,
				TransferableIn: in,
			},
		})

		// Add the output to the staked outputs
		stakedOuts = append(stakedOuts, &avax.TransferableOutput{
			Asset: avax.Asset{ID: vm.avaxAssetID},
			Out: &StakeableLockOut{
				Locktime: out.Locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          remainingValue,
					OutputOwners: inner.OutputOwners,
				},
			},
		})

		if remainingValue > 0 {
			// This input provided more value than was needed to be locked.
			// Some of it must be returned
			returnedOuts = append(returnedOuts, &avax.TransferableOutput{
				Asset: avax.Asset{ID: vm.avaxAssetID},
				Out: &StakeableLockOut{
					Locktime: out.Locktime,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          remainingValue,
						OutputOwners: inner.OutputOwners,
					},
				},
			})
		}

		// Add the signers needed for this input to the set of signers
		signers = append(signers, inSigners)
	}

	// Amount of AVAX that has been burned
	amountBurned := uint64(0)

	for _, utxo := range utxos {
		// If we have consumed more AVAX than we are trying to stake, and we
		// have burned more AVAX then we need to, then we have no need to
		// consume more AVAX
		if amountBurned >= fee && amountStaked >= amount {
			break
		}

		if assetID := utxo.AssetID(); !assetID.Equals(vm.avaxAssetID) {
			continue // We only care about burning AVAX, so ignore other assets
		}

		out := utxo.Out
		inner, ok := out.(*StakeableLockOut)
		if ok {
			if inner.Locktime > now {
				// This output is currently locked, so this output can't be
				// burned. Additionally, it may have already been consumed
				// above. Regardless, we skip to the next UTXO
				continue
			}
			out = inner.TransferableOut
		}

		inIntf, inSigners, err := kc.Spend(out, now)
		if err != nil {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}
		in, ok := inIntf.(avax.TransferableIn)
		if !ok {
			// Because we only use the secp Fx right now, this should never
			// happen
			continue
		}

		// The remaining value is initially the full value of the input
		remainingValue := in.Amount()

		// Burn any value that should be burned
		amountToBurn := safemath.Min64(
			fee-amountBurned, // Amount we still need to burn
			remainingValue,   // Amount available to burn
		)
		amountBurned += amountToBurn
		remainingValue -= amountToBurn

		// Stake any value that should be staked
		amountToStake := safemath.Min64(
			amount-amountStaked, // Amount we still need to stake
			remainingValue,      // Amount available to stake
		)
		amountStaked += amountToStake
		remainingValue -= amountToStake

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: vm.avaxAssetID},
			In:     in,
		})

		if amountToStake > 0 {
			// Some of this input was put for staking
			changeAddr := kc.Keys[0].PublicKey().Address()
			stakedOuts = append(stakedOuts, &avax.TransferableOutput{
				Asset: avax.Asset{ID: vm.avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: amountToStake,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}

		if remainingValue > 0 {
			// This input had extra value, so some of it must be returned
			changeAddr := kc.Keys[0].PublicKey().Address()
			returnedOuts = append(returnedOuts, &avax.TransferableOutput{
				Asset: avax.Asset{ID: vm.avaxAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: remainingValue,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs:     []ids.ShortID{changeAddr},
					},
				},
			})
		}

		// Add the signers needed for this input to the set of signers
		signers = append(signers, inSigners)
	}

	if amountBurned < fee || amountStaked < amount {
		return nil, nil, nil, nil, fmt.Errorf("provided keys have balance (unlocked, locked) (%d, %d) but need (%d, %d)",
			amountBurned, amountStaked, fee, amount)
	}

	avax.SortTransferableInputsWithSigners(ins, signers) // sort inputs and keys
	avax.SortTransferableOutputs(returnedOuts, vm.codec) // sort outputs
	avax.SortTransferableOutputs(stakedOuts, vm.codec)   // sort outputs

	return ins, returnedOuts, stakedOuts, signers, nil
}

var (
	errUnknownOwner   = errors.New("unknown owner type")
	errCantSign       = errors.New("can't sign")
	errInputOverflow  = errors.New("inputs overflowed uint64")
	errOutputOverflow = errors.New("outputs overflowed uint64")
)

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
	owner, ok := subnet.UnsignedTx.(*UnsignedCreateSubnetTx).Owner.(*secp256k1fx.OutputOwners)
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

// Verify that:
// * inputs and outputs are all AVAX
// * sum(inputs{unlocked}) >= sum(outputs{unlocked}) + burnAmount{unlocked}
// * sum(inputs{locked}) >= sum(outputs{locked})
func syntacticVerifySpend(
	ins []*avax.TransferableInput,
	unlockedOuts []*avax.TransferableOutput,
	lockedOuts []*avax.TransferableOutput,
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

// Removes the UTXOs consumed by [ins] from the UTXO set
func (vm *VM) consumeInputs(
	db database.Database,
	ins []*avax.TransferableInput,
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
	outs []*avax.TransferableOutput,
) error {
	for index, out := range outs {
		if err := vm.putUTXO(db, &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: avax.Asset{ID: vm.avaxAssetID},
			Out:   out.Output(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Verify that [tx] is semantically valid.
// [db] should not be committed if an error is returned
// [ins] and [outs] are the inputs and outputs of [tx].
// [creds] are the credentials of [tx], which allow [ins] to be spent.
// Precondition: [tx] has already been syntactically verified
func (vm *VM) semanticVerifySpend(
	db database.Database,
	tx UnsignedTx,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
) TxError {
	utxos := make([]*avax.UTXO, len(ins))
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
// [db] should not be committed if an error is returned
// [ins] and [outs] are the inputs and outputs of [tx].
// [creds] are the credentials of [tx], which allow [ins] to be spent.
// [utxos[i]] is the UTXO being consumed by [ins[i]]
// Precondition: [tx] has already been syntactically verified
func (vm *VM) semanticVerifySpendUTXOs(
	tx UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
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
