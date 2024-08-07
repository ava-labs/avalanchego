// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errInvalidTargetLockState    = errors.New("invalid target lock state")
	errLockingLockedUTXO         = errors.New("utxo consumed for locking are already locked")
	errUnlockingUnlockedUTXO     = errors.New("utxo consumed for unlocking are already unlocked")
	errInsufficientBalance       = errors.New("insufficient balance")
	errWrongInType               = errors.New("wrong input type")
	errWrongOutType              = errors.New("wrong output type")
	errWrongUTXOOutType          = errors.New("wrong utxo output type")
	errWrongProducedAmount       = errors.New("produced more tokens, than input had")
	errInputsCredentialsMismatch = errors.New("number of inputs is different from number of credentials")
	errInputsUTXOsMismatch       = errors.New("number of inputs is different from number of utxos")
	errBadCredentials            = errors.New("bad credentials")
	errNotBurnedEnough           = errors.New("burned less tokens, than needed to")
	errAssetIDMismatch           = errors.New("utxo/input/output assetID is different from expected asset id")
	errLockIDsMismatch           = errors.New("input lock ids is different from utxo lock ids")
	errFailToGetDeposit          = errors.New("couldn't get deposit")
	errLockedUTXO                = errors.New("can't spend locked utxo")
	errNotLockedUTXO             = errors.New("can't spend unlocked utxo")
	errUTXOOutTypeOrAmtMismatch  = errors.New("inner out isn't *secp256k1fx.TransferOutput or inner out amount != input.Amt")
	errCantSpend                 = errors.New("can't spend utxo with given credential and input")
)

// Creates UTXOs from [outs] and adds them to the UTXO set.
// UTXOs with LockedOut will have 'thisTxID' replaced with [txID].
// [txID] is the ID of the tx that created [outs].
func ProduceLocked(
	utxoDB avax.UTXOAdder,
	txID ids.ID,
	outs []*avax.TransferableOutput,
	appliedLockState locked.State,
) error {
	if appliedLockState != locked.StateBonded && appliedLockState != locked.StateDeposited {
		return errInvalidTargetLockState
	}

	for index, output := range outs {
		out := output.Out
		if lockedOut, ok := out.(*locked.Out); ok {
			utxoLockedOut := *lockedOut
			utxoLockedOut.FixLockID(txID, appliedLockState)
			out = &utxoLockedOut
		}
		utxoDB.AddUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: output.Asset,
			Out:   out,
		})
	}

	return nil
}

type CaminoSpender interface {
	// Lock the provided amount while deducting the provided fee.
	// Arguments:
	// - [keys] are the owners of the funds
	// - [totalAmountToLock] is the amount of funds that are trying to be locked with [appliedLockState]
	// - [totalAmountToBurn] is the amount of AVAX that should be burned
	// - [appliedLockState] state to set (except BondDeposit)
	// - [to] owner of unlocked amounts if appliedLockState is Unlocked
	// - [change] owner of unlocked amounts resulting from splittig inputs
	// - [asOf] timestamp against LockTime is compared
	// Returns:
	// - [inputs] the inputs that should be consumed to fund the outputs
	// - [outputs] the outputs that should be returned to the UTXO set
	// - [signers] the proof of ownership of the funds being moved
	// - [owners] the owners used for proof of ownership, used e.g. for multiSig
	Lock(
		utxoDB avax.UTXOReader,
		keys []*secp256k1.PrivateKey,
		totalAmountToLock uint64,
		totalAmountToBurn uint64,
		appliedLockState locked.State,
		to *secp256k1fx.OutputOwners,
		change *secp256k1fx.OutputOwners,
		asOf uint64,
	) (
		[]*avax.TransferableInput, // inputs
		[]*avax.TransferableOutput, // outputs
		[][]*secp256k1.PrivateKey, // signers
		[]*secp256k1fx.OutputOwners, // owners
		error,
	)

	// Undeposit all deposited by [depositTxIDs] utxos owned by [keys]. Returned results are unsorted.
	// Arguments:
	// - [state] chainstate which will be used to fetch utxos and deposit data
	// - [keys] are the owners of the deposits
	// - [depositTxIDs] ids of deposit transactions
	// Returns:
	// - [inputs] unsorted inputs that should be consumed to fund the outputs
	// - [outputs] unsorted outputs that should be returned to the UTXO set
	// - [signers] the unsorted proof of ownership of the funds being moved
	UnlockDeposit(
		state state.Chain,
		keys []*secp256k1.PrivateKey,
		depositTxIDs []ids.ID,
	) (
		[]*avax.TransferableInput, // inputs
		[]*avax.TransferableOutput, // outputs
		[][]*secp256k1.PrivateKey, // signers
		error,
	)

	Unlocker
}

type CaminoVerifier interface {
	// Verify that lock [tx] is semantically valid.
	// Arguments:
	// - [ins] and [outs] are the inputs and outputs of [tx].
	// - [creds] are the credentials of [tx], which allow [ins] to be spent.
	// - [ins] must have at least ([mintedAmount] - [burnedAmount]) less than the [outs].
	// - [assetID] is id of allowed asset, ins/outs with other assets will return error
	// - [appliedLockState] are lockState that was applied to [ins] lockState to produce [outs]
	//
	// Precondition: [tx] has already been syntactically verified.
	VerifyLock(
		tx txs.UnsignedTx,
		utxoDB avax.UTXOGetter,
		ins []*avax.TransferableInput,
		outs []*avax.TransferableOutput,
		creds []verify.Verifiable,
		mintedAmount uint64,
		burnedAmount uint64,
		assetID ids.ID,
		appliedLockState locked.State,
	) error

	// Verify that deposit unlock [tx] is semantically valid.
	// Arguments:
	// - [ins] and [outs] are the inputs and outputs of [tx].
	// - [creds] are the credentials of [tx], which allow [ins] to be spent.
	// - [burnedAmount] if any of deposits are still active, then unlocked inputs must have at least [burnedAmount] more than unlocked outs.
	// - [assetID] is id of allowed asset, ins/outs with other assets will return error
	// - [verifyCreds] if false, [creds] will be ignored
	//
	// Precondition: [tx] has already been syntactically verified.
	VerifyUnlockDeposit(
		utxoDB avax.UTXOGetter,
		tx txs.UnsignedTx,
		ins []*avax.TransferableInput,
		outs []*avax.TransferableOutput,
		creds []verify.Verifiable,
		burnedAmount uint64,
		assetID ids.ID,
		verifyCreds bool,
	) error

	Unlocker
}

type Unlocker interface {
	// Unlock fetches utxos locked by [lockTxIDs] transactions
	// with lock state [removedLockState], and then spends them producing
	// [inputs] and [outputs].
	// Outputs will have their lock state [removedLockState] removed,
	// but could still be locked with other lock state.
	// Arguments:
	// - [state] are the state from which lock txs and locked utxos will be fetched.
	// - [lockTxIDs] is array of lock transaction ids.
	// - [removedLockState] is lock state that will be removed from result [outputs].
	// Returns:
	// - [inputs] the inputs that should be consumed to fund the outputs
	// - [outputs] the outputs that should be returned to the UTXO set
	Unlock(
		state state.Chain,
		lockTxIDs []ids.ID,
		removedLockState locked.State,
	) (
		[]*avax.TransferableInput, // inputs
		[]*avax.TransferableOutput, // outputs
		error,
	)
}

func (h *handler) Lock(
	utxoDB avax.UTXOReader,
	keys []*secp256k1.PrivateKey,
	totalAmountToLock uint64,
	totalAmountToBurn uint64,
	appliedLockState locked.State,
	to *secp256k1fx.OutputOwners,
	change *secp256k1fx.OutputOwners,
	asOf uint64,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	[][]*secp256k1.PrivateKey, // signers
	[]*secp256k1fx.OutputOwners, // owners
	error,
) {
	switch appliedLockState {
	case locked.StateBonded,
		locked.StateDeposited,
		locked.StateUnlocked:
	default:
		return nil, nil, nil, nil, errInvalidTargetLockState
	}

	addrs, signer := secp256k1fx.ExtractFromAndSigners(keys)

	utxos, err := avax.GetAllUTXOs(utxoDB, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	sortUTXOs(utxos, h.ctx.AVAXAssetID, appliedLockState)

	kc := secp256k1fx.NewKeychain(signer...) // Keychain consumes UTXOs and creates new ones

	// Minimum time this transaction will be issued at
	now := asOf
	if now == 0 {
		now = uint64(h.clk.Time().Unix())
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	signers := [][]*secp256k1.PrivateKey{}
	owners := []*secp256k1fx.OutputOwners{}

	// Amount of AVAX that has been locked
	totalAmountLocked := uint64(0)

	// Amount of AVAX that has been burned
	totalAmountBurned := uint64(0)

	type lockedAndRemainedAmounts struct {
		locked   uint64
		remained uint64
	}
	type OwnerID struct {
		owners   *secp256k1fx.OutputOwners
		ownersID *ids.ID
	}
	type OwnerAmounts struct {
		amounts map[ids.ID]lockedAndRemainedAmounts
		owners  secp256k1fx.OutputOwners
	}
	// Track the amount of transfers and their owners
	// if appliedLockState == bond, then otherLockTxID is depositTxID and vice versa
	// ownerID -> otherLockTxID -> AAAA
	insAmounts := make(map[ids.ID]OwnerAmounts)

	var toOwnerID *ids.ID
	if to != nil && appliedLockState == locked.StateUnlocked {
		id, err := txs.GetOwnerID(to)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		toOwnerID = &id
	}

	var changeOwnerID *ids.ID
	if change != nil {
		id, err := txs.GetOwnerID(change)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		changeOwnerID = &id
	}

	for _, utxo := range utxos {
		// If we have consumed more AVAX than we are trying to lock,
		// and we have burned more AVAX than we need to,
		// then we have no need to consume more AVAX
		if totalAmountBurned >= totalAmountToBurn && totalAmountLocked >= totalAmountToLock {
			break
		}

		// We only care about locking AVAX,
		// and because utxos are sorted we can skip other utxos
		if assetID := utxo.AssetID(); assetID != h.ctx.AVAXAssetID {
			break
		}

		out := utxo.Out
		lockIDs := locked.IDsEmpty
		if lockedOut, ok := out.(*locked.Out); ok {
			// Resolves to true for StateUnlocked
			if lockedOut.IsLockedWith(appliedLockState) {
				// This output can't be locked with target lockState,
				// and because utxos are sorted we can skip other utxos
				break
			}
			out = lockedOut.TransferableOut
			lockIDs = lockedOut.IDs
		}

		innerOut, ok := out.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			continue
		}

		outOwnerID, err := txs.GetOutputOwnerID(out)
		if err != nil {
			// We couldn't get owner of this output, so move on to the next one
			continue
		}

		inIntf, inSigners, err := kc.SpendMultiSig(innerOut, now, utxoDB)
		if err != nil {
			// We couldn't spend the output, so move on to the next one
			continue
		}
		in, ok := inIntf.(avax.TransferableIn)
		if !ok { // should never happen
			h.ctx.Log.Warn("wrong input type",
				zap.String("expectedType", "avax.TransferableIn"),
				zap.String("actualType", fmt.Sprintf("%T", inIntf)),
			)
			continue
		}

		remainingValue := in.Amount()

		lockedOwnerID := OwnerID{&innerOut.OutputOwners, &outOwnerID}
		remainingOwnerID := lockedOwnerID

		if !lockIDs.IsLocked() {
			// Burn any value that should be burned
			amountToBurn := math.Min(
				totalAmountToBurn-totalAmountBurned, // Amount we still need to burn
				remainingValue,                      // Amount available to burn
			)
			totalAmountBurned += amountToBurn
			remainingValue -= amountToBurn

			if toOwnerID != nil {
				lockedOwnerID = OwnerID{to, toOwnerID}
			}

			isNestedMsig, err := h.fx.IsNestedMultisig(&innerOut.OutputOwners, utxoDB)
			if err != nil {
				return nil, nil, nil, nil, err
			}

			if changeOwnerID != nil && !isNestedMsig {
				remainingOwnerID = OwnerID{change, changeOwnerID}
			}
		}

		// Lock any value that should be locked
		amountToLock := math.Min(
			totalAmountToLock-totalAmountLocked, // Amount we still need to lock
			remainingValue,                      // Amount available to lock
		)
		totalAmountLocked += amountToLock
		remainingValue -= amountToLock

		if amountToLock > 0 || totalAmountToBurn > 0 {
			if lockIDs.IsLocked() {
				in = &locked.In{
					IDs:            lockIDs,
					TransferableIn: in,
				}
			}

			ins = append(ins, &avax.TransferableInput{
				UTXOID: avax.UTXOID{
					TxID:        utxo.TxID,
					OutputIndex: utxo.OutputIndex,
				},
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				In:    in,
			})
			signers = append(signers, inSigners)
			owners = append(owners, &innerOut.OutputOwners)

			otherLockTxID := lockIDs.DepositTxID
			if appliedLockState == locked.StateDeposited {
				otherLockTxID = lockIDs.BondTxID
			}

			ownerAmounts, ok := insAmounts[*lockedOwnerID.ownersID]
			if !ok {
				ownerAmounts = OwnerAmounts{
					amounts: make(map[ids.ID]lockedAndRemainedAmounts),
					owners:  *lockedOwnerID.owners,
				}
			}

			amounts := ownerAmounts.amounts[otherLockTxID]
			newAmount, err := math.Add64(amounts.locked, amountToLock)
			if err != nil {
				return nil, nil, nil, nil, err
			}

			amounts.locked = newAmount
			ownerAmounts.amounts[otherLockTxID] = amounts
			if !ok {
				insAmounts[*lockedOwnerID.ownersID] = ownerAmounts
			}

			ownerAmounts, ok = insAmounts[*remainingOwnerID.ownersID]
			if !ok {
				ownerAmounts = OwnerAmounts{
					amounts: make(map[ids.ID]lockedAndRemainedAmounts),
					owners:  *remainingOwnerID.owners,
				}
			}

			amounts = ownerAmounts.amounts[otherLockTxID]
			newAmount, err = math.Add64(amounts.remained, remainingValue)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			amounts.remained = newAmount

			ownerAmounts.amounts[otherLockTxID] = amounts
			if !ok {
				insAmounts[*remainingOwnerID.ownersID] = ownerAmounts
			}
		}
	}

	for _, ownerAmounts := range insAmounts {
		addOut := func(amt uint64, lockIDs locked.IDs, collect bool) uint64 {
			if amt == 0 {
				return 0
			}
			if lockIDs.IsLocked() {
				outs = append(outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &locked.Out{
						IDs: lockIDs,
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt:          amt,
							OutputOwners: ownerAmounts.owners,
						},
					},
				})
			} else {
				if collect {
					return amt
				}
				outs = append(outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:          amt,
						OutputOwners: ownerAmounts.owners,
					},
				})
			}
			return 0
		}

		for otherLockTxID, amounts := range ownerAmounts.amounts {
			lockIDs := locked.IDs{}
			switch appliedLockState {
			case locked.StateBonded:
				lockIDs.DepositTxID = otherLockTxID
			case locked.StateDeposited:
				lockIDs.BondTxID = otherLockTxID
			}

			// If out is unlocked no UTXO is written instead the amount is returned.
			// We apply the unlocked amount in the remaining step to compact UTXOs
			unlockAmount := addOut(amounts.locked, lockIDs.Lock(appliedLockState), true)
			if unlockAmount, err = math.Add64(unlockAmount, amounts.remained); err != nil {
				return nil, nil, nil, nil, err
			}
			addOut(unlockAmount, lockIDs, false)
		}
	}

	if totalAmountBurned < totalAmountToBurn || totalAmountLocked < totalAmountToLock {
		return nil, nil, nil, nil, errInsufficientBalance
	}

	avax.SortTransferableInputsWithSigners(ins, signers) // sort inputs and keys
	avax.SortTransferableOutputs(outs, txs.Codec)        // sort outputs

	return ins, outs, signers, owners, nil
}

func (h *handler) Unlock(
	state state.Chain,
	lockTxIDs []ids.ID,
	removedLockState locked.State,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	error,
) {
	if removedLockState != locked.StateBonded && removedLockState != locked.StateDeposited {
		return nil, nil, errInvalidTargetLockState
	}

	lockTxIDsSet := set.NewSet[ids.ID](len(lockTxIDs))
	for _, lockTxID := range lockTxIDs {
		lockTxIDsSet.Add(lockTxID)
	}

	lockTxAddresses := set.NewSet[ids.ShortID](0)
	for lockTxID := range lockTxIDsSet {
		tx, s, err := state.GetTx(lockTxID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch lockedTx %s: %w", lockTxID, err)
		}
		if s != status.Committed {
			return nil, nil, fmt.Errorf("%s is not a committed tx", lockTxID)
		}

		outputs := tx.Unsigned.Outputs()
		for i, output := range outputs {
			lockedOut, ok := output.Out.(*locked.Out)
			if !ok || !lockedOut.IsNewlyLockedWith(removedLockState) {
				// we'r only interested in outs locked by this tx
				continue
			}
			innerOut, ok := lockedOut.TransferableOut.(*secp256k1fx.TransferOutput)
			if !ok {
				return nil, nil, fmt.Errorf("could not cast locked out no. %d to transerfableOut from tx %s", i, lockTxID)
			}
			lockTxAddresses.Add(innerOut.Addrs...)
		}
	}

	utxos, err := state.LockedUTXOs(lockTxIDsSet, lockTxAddresses, removedLockState)
	if err != nil {
		return nil, nil, err
	}

	return h.unlockUTXOs(utxos, removedLockState)
}

// utxos that are not locked with [removedLockState] will be ignored
func (h *handler) unlockUTXOs(
	utxos []*avax.UTXO,
	removedLockState locked.State,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	error,
) {
	if removedLockState != locked.StateBonded && removedLockState != locked.StateDeposited {
		return nil, nil, errInvalidTargetLockState
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}

	for _, utxo := range utxos {
		out, ok := utxo.Out.(*locked.Out)
		if !ok || !out.IsLockedWith(removedLockState) {
			// This output isn't locked or doesn't have required lockState
			return nil, nil, errNotLockedUTXO
		}

		innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			return nil, nil, errWrongOutType
		}

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: avax.UTXOID{
				TxID:        utxo.TxID,
				OutputIndex: utxo.OutputIndex,
			},
			Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
			In: &locked.In{
				IDs: out.IDs,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt:   out.Amount(),
					Input: secp256k1fx.Input{SigIndices: []uint32{}},
				},
			},
		})

		if newLockIDs := out.Unlock(removedLockState); newLockIDs.IsLocked() {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &locked.Out{
					IDs: newLockIDs,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          innerOut.Amount(),
						OutputOwners: innerOut.OutputOwners,
					},
				},
			})
		} else {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          innerOut.Amount(),
					OutputOwners: innerOut.OutputOwners,
				},
			})
		}
	}

	avax.SortTransferableInputs(ins)              // sort inputs
	avax.SortTransferableOutputs(outs, txs.Codec) // sort outputs

	return ins, outs, nil
}

func (h *handler) UnlockDeposit(
	state state.Chain,
	keys []*secp256k1.PrivateKey,
	depositTxIDs []ids.ID,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	[][]*secp256k1.PrivateKey, // signers
	error,
) {
	addrs := set.NewSet[ids.ShortID](len(keys)) // The addresses controlled by [keys]
	for _, key := range keys {
		addrs.Add(key.Address())
	}

	depositTxSet := set.NewSet[ids.ID](len(depositTxIDs))
	for _, depositTxID := range depositTxIDs {
		depositTxSet.Add(depositTxID)
	}

	// Minimum time this transaction will be issued at
	currentTimestamp := uint64(h.clk.Time().Unix())

	unlockableAmounts, err := getDepositUnlockableAmounts(
		state, depositTxSet, currentTimestamp,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	utxos, err := state.LockedUTXOs(depositTxSet, addrs, locked.StateDeposited)
	if err != nil {
		return nil, nil, nil, err
	}

	kc := secp256k1fx.NewKeychain(keys...) // Keychain consumes UTXOs and creates new ones

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	signers := [][]*secp256k1.PrivateKey{}

	for _, utxo := range utxos {
		out, ok := utxo.Out.(*locked.Out)
		if !ok {
			// This output isn't locked
			continue
		} else if !out.IDs.Match(locked.StateDeposited, depositTxSet) {
			// This output isn't deposited by one of give deposit tx ids
			continue
		}

		unlockableAmount := unlockableAmounts[out.DepositTxID]
		if unlockableAmount == 0 {
			// This deposit tx doesn't have tokens available for unlock
			continue
		}

		innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			continue
		}

		inIntf, inSigners, err := kc.SpendMultiSig(innerOut, currentTimestamp, state)
		if err != nil {
			// We couldn't spend the output, so move on to the next one
			continue
		}

		in, ok := inIntf.(avax.TransferableIn)
		if !ok { // should never happen
			h.ctx.Log.Warn("wrong input type",
				zap.String("expectedType", "avax.TransferableIn"),
				zap.String("actualType", fmt.Sprintf("%T", inIntf)),
			)
			continue
		}

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: avax.UTXOID{
				TxID:        utxo.TxID,
				OutputIndex: utxo.OutputIndex,
			},
			Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
			In: &locked.In{
				IDs:            out.IDs,
				TransferableIn: in,
			},
		})

		signers = append(signers, inSigners)

		remainingValue := in.Amount()
		amountToUnlock := math.Min(unlockableAmount, remainingValue)
		remainingValue -= amountToUnlock
		unlockableAmounts[out.DepositTxID] -= amountToUnlock

		if newLockIDs := out.Unlock(locked.StateDeposited); newLockIDs.IsLocked() {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &locked.Out{
					IDs: newLockIDs,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          amountToUnlock,
						OutputOwners: innerOut.OutputOwners,
					},
				},
			})
		} else {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt:          amountToUnlock,
					OutputOwners: innerOut.OutputOwners,
				},
			})
		}

		// This input had extra value, so some of it must be returned
		if remainingValue > 0 {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &locked.Out{
					IDs: out.IDs,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          remainingValue,
						OutputOwners: innerOut.OutputOwners,
					},
				},
			})
		}
	}

	return ins, outs, signers, nil
}

func (h *handler) VerifyLock(
	tx txs.UnsignedTx,
	utxoDB avax.UTXOGetter,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	mintedAmount uint64,
	burnedAmount uint64,
	assetID ids.ID,
	appliedLockState locked.State,
) error {
	msigState, ok := utxoDB.(secp256k1fx.AliasGetter)
	if !ok {
		return secp256k1fx.ErrNotAliasGetter
	}

	utxos := make([]*avax.UTXO, len(ins))
	for index, input := range ins {
		utxo, err := utxoDB.GetUTXO(input.InputID())
		if err != nil {
			return fmt.Errorf(
				"failed to read consumed UTXO %s due to: %w",
				&input.UTXOID,
				err,
			)
		}
		utxos[index] = utxo
	}

	return h.VerifyLockUTXOs(msigState, tx, utxos, ins, outs, creds, mintedAmount, burnedAmount, assetID, appliedLockState)
}

func (h *handler) VerifyLockUTXOs(
	msigState secp256k1fx.AliasGetter,
	tx txs.UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	mintedAmount uint64,
	burnedAmount uint64,
	assetID ids.ID,
	appliedLockState locked.State,
) error {
	if appliedLockState != locked.StateBonded &&
		appliedLockState != locked.StateDeposited &&
		appliedLockState != locked.StateUnlocked {
		return errInvalidTargetLockState
	}

	if len(ins) != len(creds) {
		return fmt.Errorf(
			"there are %d inputs and %d credentials: %w",
			len(ins),
			len(creds),
			errInputsCredentialsMismatch,
		)
	}

	if len(ins) != len(utxos) {
		return fmt.Errorf(
			"there are %d inputs and %d utxos: %w",
			len(ins),
			len(utxos),
			errInputsUTXOsMismatch,
		)
	}

	for _, cred := range creds {
		if err := cred.Verify(); err != nil {
			return errBadCredentials
		}
	}

	// Track the amount of transfers and their owners
	// if appliedLockState == bond, then otherLockTxID is depositTxID and vice versa
	// ownerID -> otherLockTxID -> amount
	consumed := make(map[ids.ID]map[ids.ID]uint64)
	consumed[ids.Empty] = map[ids.ID]uint64{ids.Empty: mintedAmount} // TODO @evlekth simplify with dedicated var

	for index, input := range ins {
		utxo := utxos[index] // The UTXO consumed by [input]

		if utxoAssetID := utxo.AssetID(); utxoAssetID != assetID {
			return fmt.Errorf(
				"utxo %d has asset ID %s but expect %s: %w",
				index,
				utxoAssetID,
				assetID,
				errAssetIDMismatch,
			)
		}

		if inputAssetID := input.AssetID(); inputAssetID != assetID {
			return fmt.Errorf(
				"input %d has asset ID %s but expect %s: %w",
				index,
				inputAssetID,
				assetID,
				errAssetIDMismatch,
			)
		}

		out := utxo.Out
		if _, ok := out.(*stakeable.LockOut); ok {
			return errWrongUTXOOutType
		}

		lockIDs := &locked.IDsEmpty
		if lockedOut, ok := out.(*locked.Out); ok {
			// can only spend unlocked utxos, if appliedLockState is unlocked
			if appliedLockState == locked.StateUnlocked {
				return errLockedUTXO
				// utxo is already locked with appliedLockState, so it can't be locked it again
			} else if lockedOut.IsLockedWith(appliedLockState) {
				return errLockingLockedUTXO
			}
			out = lockedOut.TransferableOut
			lockIDs = &lockedOut.IDs
		}

		in := input.In
		if _, ok := in.(*stakeable.LockIn); ok {
			return errWrongInType
		}

		if lockedIn, ok := in.(*locked.In); ok {
			// This input is locked, but its LockIDs is wrong
			if *lockIDs != lockedIn.IDs {
				return errLockIDsMismatch
			}
			in = lockedIn.TransferableIn
		} else if lockIDs.IsLocked() {
			// The UTXO says it's locked, but this input, which consumes it,
			// is not locked - this is invalid.
			return errLockedFundsNotMarkedAsLocked
		}

		if err := h.fx.VerifyMultisigTransfer(tx, in, creds[index], out, msigState); err != nil {
			return fmt.Errorf("failed to verify transfer: %w", err)
		}

		otherLockTxID := &lockIDs.DepositTxID
		if appliedLockState == locked.StateDeposited {
			otherLockTxID = &lockIDs.BondTxID
		}

		ownerID := &ids.Empty
		if *otherLockTxID != ids.Empty {
			id, err := txs.GetOutputOwnerID(out)
			if err != nil {
				return err
			}
			ownerID = &id
		}

		amount := in.Amount()
		consumedOwnerAmounts, ok := consumed[*ownerID]
		if !ok {
			consumedOwnerAmounts = make(map[ids.ID]uint64)
			consumed[*ownerID] = consumedOwnerAmounts
		}

		newAmount, err := math.Add64(consumedOwnerAmounts[*otherLockTxID], amount)
		if err != nil {
			return err
		}
		consumedOwnerAmounts[*otherLockTxID] = newAmount
	}

	for index, output := range outs {
		if outputAssetID := output.AssetID(); outputAssetID != assetID {
			return fmt.Errorf(
				"output %d has asset ID %s but expect %s: %w",
				index,
				outputAssetID,
				assetID,
				errAssetIDMismatch,
			)
		}

		out := output.Out
		if _, ok := out.(*stakeable.LockOut); ok {
			return errWrongOutType
		}

		lockIDs := &locked.IDsEmpty
		if lockedOut, ok := out.(*locked.Out); ok {
			lockIDs = &lockedOut.IDs
			out = lockedOut.TransferableOut
		}

		if err := h.fx.VerifyMultisigOwner(out, msigState); err != nil {
			return err
		}

		otherLockTxID := &lockIDs.DepositTxID
		if appliedLockState == locked.StateDeposited {
			otherLockTxID = &lockIDs.BondTxID
		}

		ownerID := &ids.Empty
		if *otherLockTxID != ids.Empty {
			id, err := txs.GetOutputOwnerID(out)
			if err != nil {
				return err
			}
			ownerID = &id
		}

		producedAmount := out.Amount()
		consumedAmount := uint64(0)
		consumedOwnerAmounts, ok := consumed[*ownerID]
		if ok {
			consumedAmount = consumedOwnerAmounts[*otherLockTxID]
		}

		if consumedAmount < producedAmount {
			return fmt.Errorf(
				"address %s produces %d and consumes %d for lockIDs %+v with lock '%s': %w",
				ownerID,
				producedAmount,
				consumedAmount,
				otherLockTxID,
				appliedLockState,
				errWrongProducedAmount,
			)
		}

		consumedOwnerAmounts[*otherLockTxID] = consumedAmount - producedAmount
	}

	amountToBurn := burnedAmount
	for _, consumedOwnerAmounts := range consumed {
		consumedUnlockedAmount := consumedOwnerAmounts[ids.Empty]
		if consumedUnlockedAmount >= amountToBurn {
			return nil
		}
		amountToBurn -= consumedUnlockedAmount
	}

	if amountToBurn > 0 {
		return fmt.Errorf(
			"asset %s burned %d unlocked, but needed to burn %d: %w",
			assetID,
			burnedAmount-amountToBurn,
			burnedAmount,
			errNotBurnedEnough,
		)
	}

	return nil
}

func (h *handler) VerifyUnlockDeposit(
	utxoDB avax.UTXOGetter,
	tx txs.UnsignedTx,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	burnedAmount uint64,
	assetID ids.ID,
	verifyCreds bool,
) error {
	msigState, ok := utxoDB.(secp256k1fx.AliasGetter)
	if !ok {
		return secp256k1fx.ErrNotAliasGetter
	}

	utxos := make([]*avax.UTXO, len(ins))
	for index, input := range ins {
		utxo, err := utxoDB.GetUTXO(input.InputID())
		if err != nil {
			return fmt.Errorf(
				"failed to read consumed UTXO %s due to: %w",
				&input.UTXOID,
				err,
			)
		}
		utxos[index] = utxo
	}

	return h.VerifyUnlockDepositedUTXOs(msigState, tx, utxos, ins, outs, creds, burnedAmount, assetID, verifyCreds)
}

func (h *handler) VerifyUnlockDepositedUTXOs(
	msigState secp256k1fx.AliasGetter,
	tx txs.UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	burnedAmount uint64,
	assetID ids.ID,
	verifyCreds bool,
) error {
	if len(ins) != len(utxos) {
		return fmt.Errorf(
			"there are %d inputs and %d utxos: %w",
			len(ins),
			len(utxos),
			errInputsUTXOsMismatch,
		)
	}

	if verifyCreds && len(ins) != len(creds) {
		return fmt.Errorf(
			"there are %d inputs and %d credentials: %w",
			len(ins),
			len(creds),
			errInputsCredentialsMismatch,
		)
	}

	if verifyCreds {
		for _, cred := range creds {
			if err := cred.Verify(); err != nil {
				return errBadCredentials
			}
		}
	}

	consumedUnlocked := uint64(0)
	consumed := make(map[ids.ID]map[ids.ID]uint64) // ownerID -> bondTxID -> amount

	// iterate over ins, get utxos, calculate consumed values
	for index, input := range ins {
		utxo := utxos[index] // The UTXO consumed by [input]

		if utxoAssetID := utxo.AssetID(); utxoAssetID != assetID {
			return fmt.Errorf(
				"utxo %d has asset ID %s but expect %s: %w",
				index,
				utxoAssetID,
				assetID,
				errAssetIDMismatch,
			)
		}

		if inputAssetID := input.AssetID(); inputAssetID != assetID {
			return fmt.Errorf(
				"input %d has asset ID %s but expect %s: %w",
				index,
				inputAssetID,
				assetID,
				errAssetIDMismatch,
			)
		}

		out := utxo.Out
		lockIDs := &locked.IDsEmpty
		if lockedOut, ok := out.(*locked.Out); ok {
			// utxo isn't deposited, so it can't be unlocked
			// bonded-not-deposited utxos are not allowed
			if lockedOut.DepositTxID == ids.Empty {
				return errUnlockingUnlockedUTXO
			}
			out = lockedOut.TransferableOut
			lockIDs = &lockedOut.IDs
		}

		in := input.In
		if lockedIn, ok := in.(*locked.In); ok {
			// Input is locked, but LockIDs isn't matching utxo's
			if *lockIDs != lockedIn.IDs {
				return errLockIDsMismatch
			}
			in = lockedIn.TransferableIn
		} else if lockIDs.IsLocked() {
			// The UTXO is locked, but consuming input isn't locked
			return errLockedFundsNotMarkedAsLocked
		}

		consumedAmount := in.Amount()

		if verifyCreds {
			if err := h.fx.VerifyMultisigTransfer(tx, in, creds[index], out, msigState); err != nil {
				return fmt.Errorf("failed to verify transfer: %w: %s", errCantSpend, err)
			}
		} else if innerOut, ok := out.(*secp256k1fx.TransferOutput); !ok || innerOut.Amt != consumedAmount {
			return fmt.Errorf("failed to verify transfer: %w", errUTXOOutTypeOrAmtMismatch)
		}

		// calculating consumed amounts
		if lockIDs.IsLocked() {
			ownerID, err := txs.GetOutputOwnerID(out)
			if err != nil {
				return err
			}

			consumedOwnerAmounts, ok := consumed[ownerID]
			if !ok {
				consumedOwnerAmounts = make(map[ids.ID]uint64)
				consumed[ownerID] = consumedOwnerAmounts
			}

			newAmount, err := math.Add64(consumedOwnerAmounts[lockIDs.BondTxID], consumedAmount)
			if err != nil {
				return err
			}
			consumedOwnerAmounts[lockIDs.BondTxID] = newAmount
		} else {
			newAmount, err := math.Add64(consumedUnlocked, consumedAmount)
			if err != nil {
				return err
			}
			consumedUnlocked = newAmount
		}
	}

	// iterating over outs, checking produced amounts with consumed map
	for _, output := range outs {
		out := output.Out
		lockIDs := &locked.IDsEmpty
		lockedOut, isLocked := out.(*locked.Out)
		if isLocked {
			lockIDs = &lockedOut.IDs
			out = lockedOut.TransferableOut
		} else {
			// unlocked tokens can be transferred and must be checked
			if err := h.fx.VerifyMultisigOwner(out, msigState); err != nil {
				return err
			}
		}

		producedAmount := out.Amount()

		ownerID, err := txs.GetOutputOwnerID(out)
		if err != nil {
			return err
		}

		consumedOwnerAmounts, ok := consumed[ownerID]
		if !ok {
			consumedOwnerAmounts = make(map[ids.ID]uint64)
			consumed[ownerID] = consumedOwnerAmounts
		}

		consumedAmount := consumedOwnerAmounts[lockIDs.BondTxID]
		amountToRemoveFromConsumed := producedAmount

		if consumedAmount < amountToRemoveFromConsumed {
			if isLocked {
				return fmt.Errorf(
					"address %s produces %d and consumes %d for lockIDs %+v with unlock '%s': %w",
					ownerID,
					producedAmount,
					consumedAmount,
					lockIDs,
					locked.StateDeposited,
					errWrongProducedAmount,
				)
			}

			amountToRemoveFromConsumed = consumedAmount
			amountToRemoveFromConsumedUnlocked := producedAmount - consumedAmount
			if consumedUnlocked < amountToRemoveFromConsumedUnlocked {
				return fmt.Errorf(
					"address %s produces %d and consumes %d unlocked and %d locked with %+v: %w",
					ownerID,
					producedAmount,
					consumedUnlocked,
					consumedAmount,
					lockIDs,
					errWrongProducedAmount,
				)
			}
			consumedUnlocked -= amountToRemoveFromConsumedUnlocked
		}
		consumedOwnerAmounts[lockIDs.BondTxID] -= amountToRemoveFromConsumed
	}

	// checking that we burned required amount
	if consumedUnlocked < burnedAmount {
		return fmt.Errorf(
			"asset %s burned %d unlocked, but needed to burn %d: %w",
			assetID,
			consumedUnlocked,
			burnedAmount,
			errNotBurnedEnough,
		)
	}

	return nil
}

type innerSortUTXOs struct {
	utxos          []*avax.UTXO
	allowedAssetID ids.ID
	lockState      locked.State
}

func (sort *innerSortUTXOs) Less(i, j int) bool {
	iUTXO := sort.utxos[i]
	jUTXO := sort.utxos[j]

	if iUTXO.AssetID() == sort.allowedAssetID && jUTXO.AssetID() != sort.allowedAssetID {
		return true
	}

	iOut := iUTXO.Out
	iLockIDs := &locked.IDsEmpty
	if lockedOut, ok := iOut.(*locked.Out); ok {
		iOut = lockedOut.TransferableOut
		iLockIDs = &lockedOut.IDs
	}

	jOut := jUTXO.Out
	jLockIDs := &locked.IDsEmpty
	if lockedOut, ok := jOut.(*locked.Out); ok {
		jOut = lockedOut.TransferableOut
		jLockIDs = &lockedOut.IDs
	}

	if sort.lockState == locked.StateUnlocked {
		// Sort all locks last
		iEmpty := *iLockIDs == locked.IDsEmpty
		if iEmpty != (*jLockIDs == locked.IDsEmpty) {
			return iEmpty
		}
	} else {
		iLockTxID := &iLockIDs.DepositTxID
		jLockTxID := &jLockIDs.DepositTxID
		iOtherLockTxID := &iLockIDs.BondTxID
		jOtherLockTxID := &jLockIDs.BondTxID
		if sort.lockState == locked.StateBonded {
			iLockTxID = &iLockIDs.BondTxID
			jLockTxID = &jLockIDs.BondTxID
			iOtherLockTxID = &iLockIDs.DepositTxID
			jOtherLockTxID = &jLockIDs.DepositTxID
		}

		if *iLockTxID == ids.Empty && *jLockTxID != ids.Empty {
			return true
		} else if *iLockTxID != ids.Empty && *jLockTxID == ids.Empty {
			return false
		}

		switch bytes.Compare(iOtherLockTxID[:], jOtherLockTxID[:]) {
		case -1:
			return false
		case 1:
			return true
		}
	}

	iAmount := uint64(0)
	if amounter, ok := iOut.(avax.Amounter); ok {
		iAmount = amounter.Amount()
	}

	jAmount := uint64(0)
	if amounter, ok := jOut.(avax.Amounter); ok {
		jAmount = amounter.Amount()
	}

	return iAmount < jAmount
}

func (sort *innerSortUTXOs) Len() int {
	return len(sort.utxos)
}

func (sort *innerSortUTXOs) Swap(i, j int) {
	u := sort.utxos
	u[j], u[i] = u[i], u[j]
}

func sortUTXOs(utxos []*avax.UTXO, allowedAssetID ids.ID, lockState locked.State) {
	sort.Sort(&innerSortUTXOs{utxos: utxos, allowedAssetID: allowedAssetID, lockState: lockState})
}

func getDepositUnlockableAmounts(
	chainState state.Chain,
	depositTxIDs set.Set[ids.ID],
	currentTimestamp uint64,
) (map[ids.ID]uint64, error) {
	unlockableAmounts := make(map[ids.ID]uint64, len(depositTxIDs))

	for depositTxID := range depositTxIDs {
		deposit, err := chainState.GetDeposit(depositTxID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", errFailToGetDeposit, err)
		}

		depositOffer, err := chainState.GetDepositOffer(deposit.DepositOfferID)
		if err != nil {
			return nil, err
		}

		unlockableAmounts[depositTxID] = deposit.UnlockableAmount(
			depositOffer,
			currentTimestamp,
		)
	}

	return unlockableAmounts, nil
}
