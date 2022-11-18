// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
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
	errNotEnoughBalance          = errors.New("not enough balance to lock")
	errWrongInType               = errors.New("wrong input type")
	errWrongOutType              = errors.New("wrong output type")
	errWrongUTXOOutType          = errors.New("wrong utxo output type")
	errWrongProducedAmount       = errors.New("produced more tokens, than input had")
	errInputsCredentialsMismatch = errors.New("number of inputs is different from number of credentials")
	errInputsUTXOSMismatch       = errors.New("number of inputs is different from number of utxos")
	errWrongCredentials          = errors.New("wrong credentials")
	errNotBurnedEnough           = errors.New("burned less tokens, than needed to")
	errAssetIDMismatch           = errors.New("input assetID is different from utxo asset id")
	errLockIDsMismatch           = errors.New("input lock ids is different from utxo lock ids")
	errLockAmountNotZero         = errors.New("lockAmount must be 0 for StateUnlocked")
)

// Creates UTXOs from [outs] and adds them to the UTXO set.
// UTXOs with LockedOut will have 'thisTxID' replaced with [txID].
// [txID] is the ID of the tx that created [outs].
func ProduceLocked(
	utxoDB state.UTXOAdder,
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
	keys []*crypto.PrivateKeySECP256K1R,
	totalAmountToLock uint64,
	totalAmountToBurn uint64,
	appliedLockState locked.State,
	changeAddr ids.ShortID,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	[][]*crypto.PrivateKeySECP256K1R, // signers
	error,
) {
	switch appliedLockState {
	case locked.StateBonded,
		locked.StateDeposited,
		locked.StateUnlocked:
	default:
		return nil, nil, nil, errInvalidTargetLockState
	}

	if appliedLockState == locked.StateUnlocked && totalAmountToLock > 0 {
		return nil, nil, nil, errLockAmountNotZero
	}

	addrs := ids.NewShortSet(len(keys)) // The addresses controlled by [keys]
	for _, key := range keys {
		addrs.Add(key.PublicKey().Address())
	}
	utxos, err := avax.GetAllUTXOs(h.utxosReader, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		return nil, nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	sortUTXOs(utxos, h.ctx.AVAXAssetID, appliedLockState)

	kc := secp256k1fx.NewKeychain(keys...) // Keychain consumes UTXOs and creates new ones

	// Minimum time this transaction will be issued at
	now := uint64(h.clk.Time().Unix())

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	// Amount of AVAX that has been locked
	totalAmountLocked := uint64(0)

	// Amount of AVAX that has been burned
	totalAmountBurned := uint64(0)

	type lockedAndRemainedAmounts struct {
		locked   uint64
		remained uint64
	}
	type OwnerAmounts struct {
		amounts map[ids.ID]lockedAndRemainedAmounts
		owners  secp256k1fx.OutputOwners
		signers []*crypto.PrivateKeySECP256K1R
	}
	// Track the amount of transfers and their owners
	// if appliedLockState == bond, then otherLockTxID is depositTxID and vice versa
	// ownerID -> otherLockTxID -> AAAA
	insAmounts := make(map[ids.ID]OwnerAmounts)

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
		if lockedOut, ok := utxo.Out.(*locked.Out); ok {
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

		inIntf, inSigners, err := kc.Spend(innerOut, now)
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

		if !lockIDs.IsLocked() {
			// Burn any value that should be burned
			amountToBurn := math.Min(
				totalAmountToBurn-totalAmountBurned, // Amount we still need to burn
				remainingValue,                      // Amount available to burn
			)
			totalAmountBurned += amountToBurn
			remainingValue -= amountToBurn
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
				UTXOID: utxo.UTXOID,
				Asset:  avax.Asset{ID: h.ctx.AVAXAssetID},
				In:     in,
			})
			signers = append(signers, inSigners)

			ownerID, err := GetOwnerID(out)
			if err != nil {
				// We couldn't get owner of this output, so move on to the next one
				continue
			}

			ownerAmounts, ok := insAmounts[ownerID]
			if !ok {
				ownerAmounts = OwnerAmounts{
					amounts: make(map[ids.ID]lockedAndRemainedAmounts),
					owners:  innerOut.OutputOwners,
					signers: inSigners,
				}
			}

			otherLockTxID := lockIDs.DepositTxID
			if appliedLockState == locked.StateDeposited {
				otherLockTxID = lockIDs.BondTxID
			}

			amounts := ownerAmounts.amounts[otherLockTxID]

			newAmount, err := math.Add64(amounts.locked, amountToLock)
			if err != nil {
				// We couldn't sum this input's amount, so move on to the next one
				continue
			}
			amounts.locked = newAmount

			newAmount, err = math.Add64(amounts.remained, remainingValue)
			if err != nil {
				// We couldn't sum this input's amount, so move on to the next one
				continue
			}
			amounts.remained = newAmount

			ownerAmounts.amounts[otherLockTxID] = amounts
			if !ok {
				insAmounts[ownerID] = ownerAmounts
			}
		}
	}

	for _, ownerAmounts := range insAmounts {
		for otherLockTxID, amounts := range ownerAmounts.amounts {
			lockIDs := locked.IDs{}
			switch appliedLockState {
			case locked.StateBonded:
				lockIDs.DepositTxID = otherLockTxID
			case locked.StateDeposited:
				lockIDs.BondTxID = otherLockTxID
			}

			if amounts.locked > 0 {
				// Some of this input was put for locking
				outs = append(outs, &avax.TransferableOutput{
					Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
					Out: &locked.Out{
						IDs: lockIDs.Lock(appliedLockState),
						TransferableOut: &secp256k1fx.TransferOutput{
							Amt:          amounts.locked,
							OutputOwners: ownerAmounts.owners,
						},
					},
				})
			}

			// This input had extra value, so some of it must be returned
			if amounts.remained > 0 {
				if lockIDs.IsLocked() {
					outs = append(outs, &avax.TransferableOutput{
						Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
						Out: &locked.Out{
							IDs: lockIDs,
							TransferableOut: &secp256k1fx.TransferOutput{
								Amt:          amounts.remained,
								OutputOwners: ownerAmounts.owners,
							},
						},
					})
				} else {
					var owners secp256k1fx.OutputOwners
					if changeAddr != ids.ShortEmpty {
						owners = secp256k1fx.OutputOwners{
							Locktime:  0,
							Threshold: 1,
							Addrs:     []ids.ShortID{changeAddr},
						}
					} else {
						owners = ownerAmounts.owners
					}
					outs = append(outs, &avax.TransferableOutput{
						Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt:          amounts.remained,
							OutputOwners: owners,
						},
					})
				}
			}
		}
	}

	if totalAmountBurned < totalAmountToBurn || totalAmountLocked < totalAmountToLock {
		return nil, nil, nil, errNotEnoughBalance
	}

	avax.SortTransferableInputsWithSigners(ins, signers) // sort inputs and keys
	avax.SortTransferableOutputs(outs, txs.Codec)        // sort outputs

	return ins, outs, signers, nil
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

	lockTxIDsSet := ids.NewSet(len(lockTxIDs))
	for _, lockTxID := range lockTxIDs {
		lockTxIDsSet.Add(lockTxID)
	}

	lockTxAddresses := ids.ShortSet{}
	for lockTxID := range lockTxIDsSet {
		tx, s, err := state.GetTx(lockTxID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch lockedTx %s: %v", lockTxID, err)
		}
		if s != status.Committed {
			return nil, nil, fmt.Errorf("%s is not a committed tx", lockTxID)
		}

		outputs := tx.Unsigned.Outputs()
		for i, output := range outputs {
			lockedOut, ok := output.Out.(*locked.Out)
			if !ok || !lockedOut.IsNewlyLockedWith(removedLockState) {
				// we'r only intersed in outs locked by this tx
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
		if !ok {
			// This output isn't locked
			continue
		} else if !out.IsLockedWith(removedLockState) {
			// This output doesn't have required lockState
			continue
		}

		innerOut, ok := out.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			continue
		}

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: h.ctx.AVAXAssetID},
			In: &locked.In{
				IDs: out.IDs,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt:   out.Amount(),
					Input: secp256k1fx.Input{},
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

func (h *handler) VerifyLock(
	tx txs.UnsignedTx,
	utxoDB state.UTXOGetter,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	burnedAmount uint64,
	assetID ids.ID,
	appliedLockState locked.State,
) error {
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

	return h.VerifyLockUTXOs(tx, utxos, ins, outs, creds, burnedAmount, assetID, appliedLockState)
}

func (h *handler) VerifyLockUTXOs(
	tx txs.UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	burnedAmount uint64,
	assetID ids.ID,
	appliedLockState locked.State,
) error {
	if appliedLockState != locked.StateBonded && appliedLockState != locked.StateDeposited {
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
			errInputsUTXOSMismatch,
		)
	}

	for _, cred := range creds {
		if err := cred.Verify(); err != nil {
			return errWrongCredentials
		}
	}

	// Track the amount of transfers and their owners
	// if appliedLockState == bond, then otherLockTxID is depositTxID and vice versa
	// ownerID -> otherLockTxID -> amount
	consumed := make(map[ids.ID]map[ids.ID]uint64)

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

		lockIDs := locked.IDs{}
		if lockedOut, ok := out.(*locked.Out); ok {
			// utxo is already locked with appliedLockState, so it can't be locked it again
			if lockedOut.IsLockedWith(appliedLockState) {
				return errLockingLockedUTXO
			}
			out = lockedOut.TransferableOut
			lockIDs = lockedOut.IDs
		}

		in := input.In
		if _, ok := in.(*stakeable.LockIn); ok {
			return errWrongInType
		}

		if lockedIn, ok := in.(*locked.In); ok {
			// This input is locked, but its LockIDs is wrong
			if lockIDs != lockedIn.IDs {
				return errLockIDsMismatch
			}
			in = lockedIn.TransferableIn
		} else if lockIDs.IsLocked() {
			// The UTXO says it's locked, but this input, which consumes it,
			// is not locked - this is invalid.
			return errLockedFundsNotMarkedAsLocked
		}

		// Verify that this tx's credentials allow [in] to be spent
		if err := h.fx.VerifyTransfer(tx, in, creds[index], out); err != nil {
			return fmt.Errorf("failed to verify transfer: %w", err)
		}

		otherLockTxID := &lockIDs.DepositTxID
		if appliedLockState == locked.StateDeposited {
			otherLockTxID = &lockIDs.BondTxID
		}

		ownerID := &ids.Empty
		if *otherLockTxID != ids.Empty {
			id, err := GetOwnerID(out)
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

	for _, output := range outs {
		out := output.Out
		if _, ok := out.(*stakeable.LockOut); ok {
			return errWrongOutType
		}

		lockIDs := locked.IDs{}
		if lockedOut, ok := out.(*locked.Out); ok {
			lockIDs = lockedOut.IDs
			out = lockedOut.TransferableOut
		}

		otherLockTxID := &lockIDs.DepositTxID
		if appliedLockState == locked.StateDeposited {
			otherLockTxID = &lockIDs.BondTxID
		}

		ownerID := &ids.Empty
		if *otherLockTxID != ids.Empty {
			id, err := GetOwnerID(out)
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
func (sort *innerSortUTXOs) Len() int      { return len(sort.utxos) }
func (sort *innerSortUTXOs) Swap(i, j int) { u := sort.utxos; u[j], u[i] = u[i], u[j] }

func sortUTXOs(utxos []*avax.UTXO, allowedAssetID ids.ID, lockState locked.State) {
	sort.Sort(&innerSortUTXOs{utxos: utxos, allowedAssetID: allowedAssetID, lockState: lockState})
}
