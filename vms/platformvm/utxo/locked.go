package utxo

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/lock"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"go.uber.org/zap"
)

var errInvalidTargetLockState = errors.New("invalid target lock state")

// Adds the UTXOs created by [outs] to the UTXO set.
// [txID] is the ID of the tx that created [outs].
// TODO@ comment
func ProduceLocked(
	utxoDB state.UTXOAdder,
	txID ids.ID,
	outs []*avax.TransferableOutput,
) {
	for index, output := range outs {
		out := output.Out
		if lockedOut, ok := out.(*lock.LockedOut); ok {
			utxoLockedOut := *lockedOut
			utxoLockedOut.LockIDs.FixLockID(txID)
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
}

func (h *handler) Lock(
	keys []*crypto.PrivateKeySECP256K1R,
	amount uint64,
	fee uint64,
	changeAddr ids.ShortID,
	appliedLockState lock.LockState,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	[][]*crypto.PrivateKeySECP256K1R, // signers
	error,
) {
	if appliedLockState != lock.LockStateBonded && appliedLockState != lock.LockStateDeposited {
		return nil, nil, nil, errInvalidTargetLockState
	}

	addrs := ids.NewShortSet(len(keys)) // The addresses controlled by [keys]
	for _, key := range keys {
		addrs.Add(key.PublicKey().Address())
	}
	utxos, err := avax.GetAllUTXOs(h.utxosReader, addrs) // The UTXOs controlled by [keys]
	if err != nil {
		return nil, nil, nil, fmt.Errorf("couldn't get UTXOs: %w", err)
	}

	kc := secp256k1fx.NewKeychain(keys...) // Keychain consumes UTXOs and creates new ones

	// Minimum time this transaction will be issued at
	now := uint64(h.clk.Time().Unix())

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}
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

		if assetID := utxo.AssetID(); assetID != h.ctx.AVAXAssetID {
			continue // We only care about staking AVAX, so ignore other assets
		}

		lockedOut, ok := utxo.Out.(*lock.LockedOut)
		if !ok {
			// This output isn't locked, so it will be handled during the next
			// iteration of the UTXO set
			continue
		} else if lockedOut.LockState().IsLockedWith(appliedLockState) {
			// This output can't be locked with target lockState
			continue
		}

		innerOut, ok := lockedOut.TransferableOut.(*secp256k1fx.TransferOutput)
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

		// The remaining value is initially the full value of the input
		remainingValue := in.Amount()

		// Stake any value that should be staked
		amountToStake := math.Min(
			amount-amountStaked, // Amount we still need to stake
			remainingValue,      // Amount available to stake
		)
		amountStaked += amountToStake
		remainingValue -= amountToStake

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: h.ctx.AVAXAssetID},
			In: &lock.LockedIn{
				LockIDs:        lockedOut.LockIDs,
				TransferableIn: in,
			},
		})

		// Add the output to the staked outputs
		outs = append(outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
			Out: &lock.LockedOut{
				LockIDs: lockedOut.LockIDs.Lock(appliedLockState),
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          amountToStake,
					OutputOwners: innerOut.OutputOwners,
				},
			},
		})

		if remainingValue > 0 {
			// This input provided more value than was needed to be locked.
			// Some of it must be returned
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &lock.LockedOut{
					LockIDs: lockedOut.LockIDs,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          remainingValue,
						OutputOwners: innerOut.OutputOwners,
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
		// If we have consumed more AVAX than we are trying to stake,
		// and we have burned more AVAX than we need to,
		// then we have no need to consume more AVAX
		if amountBurned >= fee && amountStaked >= amount {
			break
		}

		if assetID := utxo.AssetID(); assetID != h.ctx.AVAXAssetID {
			continue // We only care about burning AVAX, so ignore other assets
		}

		if _, ok := utxo.Out.(*lock.LockedOut); ok {
			// This output is currently locked, so this output can't be
			// burned. Additionally, it may have already been consumed
			// above. Regardless, we skip to the next UTXO
			continue
		}

		innerOut, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only know how to clone secp256k1 outputs for now
			continue
		}

		inIntf, inSigners, err := kc.Spend(innerOut, now)
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
		amountToBurn := math.Min(
			fee-amountBurned, // Amount we still need to burn
			remainingValue,   // Amount available to burn
		)
		amountBurned += amountToBurn
		remainingValue -= amountToBurn

		// Stake any value that should be staked
		amountToStake := math.Min(
			amount-amountStaked, // Amount we still need to stake
			remainingValue,      // Amount available to stake
		)
		amountStaked += amountToStake
		remainingValue -= amountToStake

		// Add the input to the consumed inputs
		ins = append(ins, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  avax.Asset{ID: h.ctx.AVAXAssetID},
			In:     in,
		})

		if amountToStake > 0 {
			// Some of this input was put for staking
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &lock.LockedOut{
					LockIDs: lock.LockIDs{}.Lock(appliedLockState),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: amountToStake,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  0,
							Threshold: 1,
							Addrs:     []ids.ShortID{changeAddr},
						},
					},
				},
			})
		}

		if remainingValue > 0 {
			// This input had extra value, so some of it must be returned
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
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
		return nil, nil, nil, fmt.Errorf(
			"provided keys have balance (unlocked, locked) (%d, %d) but need (%d, %d)",
			amountBurned, amountStaked, fee, amount)
	}

	avax.SortTransferableInputsWithSigners(ins, signers) // sort inputs and keys
	avax.SortTransferableOutputs(outs, txs.Codec)        // sort outputs

	return ins, outs, signers, nil
}

func (h *handler) Unlock(
	utxoDB avax.UTXOReader,
	lockTxIDs []ids.ID,
	removedLockState lock.LockState,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	error,
) {
	if removedLockState != lock.LockStateBonded && removedLockState != lock.LockStateDeposited {
		return nil, nil, errInvalidTargetLockState
	}

	// TODO@ get utxos locked by lockTxIDs
	var utxos []*avax.UTXO

	return h.unlockUTXOs(utxos, removedLockState)
}

func (h *handler) unlockUTXOs(
	utxos []*avax.UTXO,
	removedLockState lock.LockState,
) (
	[]*avax.TransferableInput, // inputs
	[]*avax.TransferableOutput, // outputs
	error,
) {
	if removedLockState != lock.LockStateBonded && removedLockState != lock.LockStateDeposited {
		return nil, nil, errInvalidTargetLockState
	}

	ins := []*avax.TransferableInput{}
	outs := []*avax.TransferableOutput{}

	for _, utxo := range utxos {
		out, ok := utxo.Out.(*lock.LockedOut)
		if !ok {
			// This output isn't locked
			continue
		} else if !out.LockState().IsLockedWith(removedLockState) {
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
			In: &lock.LockedIn{
				LockIDs: out.LockIDs,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt:   out.Amount(),
					Input: secp256k1fx.Input{},
				},
			},
		})

		if newLockIDs := out.LockIDs.Unlock(removedLockState); newLockIDs.LockState().IsLocked() {
			outs = append(outs, &avax.TransferableOutput{
				Asset: avax.Asset{ID: h.ctx.AVAXAssetID},
				Out: &lock.LockedOut{
					LockIDs: newLockIDs,
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
