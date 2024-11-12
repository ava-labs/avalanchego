// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxo

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Verifier = (*verifier)(nil)

	ErrInsufficientFunds            = errors.New("insufficient funds")
	ErrInsufficientUnlockedFunds    = errors.New("insufficient unlocked funds")
	ErrInsufficientLockedFunds      = errors.New("insufficient locked funds")
	errWrongNumberCredentials       = errors.New("wrong number of credentials")
	errWrongNumberUTXOs             = errors.New("wrong number of UTXOs")
	errAssetIDMismatch              = errors.New("input asset ID does not match UTXO asset ID")
	errLocktimeMismatch             = errors.New("input locktime does not match UTXO locktime")
	errLockedFundsNotMarkedAsLocked = errors.New("locked funds not marked as locked")
)

type Verifier interface {
	// Verify that [tx] is semantically valid.
	// [ins] and [outs] are the inputs and outputs of [tx].
	// [creds] are the credentials of [tx], which allow [ins] to be spent.
	// [unlockedProduced] is the map of assets that were produced and their
	// amounts.
	// The [ins] must have at least [unlockedProduced] than the [outs].
	//
	// Precondition: [tx] has already been syntactically verified.
	//
	// Note: [unlockedProduced] is modified by this method.
	VerifySpend(
		tx txs.UnsignedTx,
		utxoDB avax.UTXOGetter,
		ins []*avax.TransferableInput,
		outs []*avax.TransferableOutput,
		creds []verify.Verifiable,
		unlockedProduced map[ids.ID]uint64,
	) error

	// Verify that [tx] is semantically valid.
	// [utxos[i]] is the UTXO being consumed by [ins[i]].
	// [ins] and [outs] are the inputs and outputs of [tx].
	// [creds] are the credentials of [tx], which allow [ins] to be spent.
	// [unlockedProduced] is the map of assets that were produced and their
	// amounts.
	// The [ins] must have at least [unlockedProduced] more than the [outs].
	//
	// Precondition: [tx] has already been syntactically verified.
	//
	// Note: [unlockedProduced] is modified by this method.
	VerifySpendUTXOs(
		tx txs.UnsignedTx,
		utxos []*avax.UTXO,
		ins []*avax.TransferableInput,
		outs []*avax.TransferableOutput,
		creds []verify.Verifiable,
		unlockedProduced map[ids.ID]uint64,
	) error
}

func NewVerifier(
	ctx *snow.Context,
	clk *mockable.Clock,
	fx fx.Fx,
) Verifier {
	return &verifier{
		ctx: ctx,
		clk: clk,
		fx:  fx,
	}
}

type verifier struct {
	ctx *snow.Context
	clk *mockable.Clock
	fx  fx.Fx
}

func (h *verifier) VerifySpend(
	tx txs.UnsignedTx,
	utxoDB avax.UTXOGetter,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	unlockedProduced map[ids.ID]uint64,
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

	return h.VerifySpendUTXOs(tx, utxos, ins, outs, creds, unlockedProduced)
}

func (h *verifier) VerifySpendUTXOs(
	tx txs.UnsignedTx,
	utxos []*avax.UTXO,
	ins []*avax.TransferableInput,
	outs []*avax.TransferableOutput,
	creds []verify.Verifiable,
	unlockedProduced map[ids.ID]uint64,
) error {
	if len(ins) != len(creds) {
		return fmt.Errorf(
			"%w: %d inputs != %d credentials",
			errWrongNumberCredentials,
			len(ins),
			len(creds),
		)
	}
	if len(ins) != len(utxos) {
		return fmt.Errorf(
			"%w: %d inputs != %d utxos",
			errWrongNumberUTXOs,
			len(ins),
			len(utxos),
		)
	}
	for _, cred := range creds { // Verify credentials are well-formed.
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	// Time this transaction is being verified
	now := uint64(h.clk.Time().Unix())

	// Track the amount of unlocked transfers
	// assetID -> amount
	unlockedConsumed := make(map[ids.ID]uint64)

	// Track the amount of locked transfers and their owners
	// assetID -> locktime -> ownerID -> amount
	lockedProduced := make(map[ids.ID]map[uint64]map[ids.ID]uint64)
	lockedConsumed := make(map[ids.ID]map[uint64]map[ids.ID]uint64)

	for index, input := range ins {
		utxo := utxos[index] // The UTXO consumed by [input]

		realAssetID := utxo.AssetID()
		claimedAssetID := input.AssetID()
		if realAssetID != claimedAssetID {
			return fmt.Errorf(
				"%w: %s != %s",
				errAssetIDMismatch,
				claimedAssetID,
				realAssetID,
			)
		}

		out := utxo.Out
		locktime := uint64(0)
		// Set [locktime] to this UTXO's locktime, if applicable
		if inner, ok := out.(*stakeable.LockOut); ok {
			out = inner.TransferableOut
			locktime = inner.Locktime
		}

		in := input.In
		// The UTXO says it's locked until [locktime], but this input, which
		// consumes it, is not locked even though [locktime] hasn't passed. This
		// is invalid.
		if inner, ok := in.(*stakeable.LockIn); now < locktime && !ok {
			return errLockedFundsNotMarkedAsLocked
		} else if ok {
			if inner.Locktime != locktime {
				// This input is locked, but its locktime is wrong
				return fmt.Errorf(
					"%w: %d != %d",
					errLocktimeMismatch,
					inner.Locktime,
					locktime,
				)
			}
			in = inner.TransferableIn
		}

		// Verify that this tx's credentials allow [in] to be spent
		if err := h.fx.VerifyTransfer(tx, in, creds[index], out); err != nil {
			return fmt.Errorf("failed to verify transfer: %w", err)
		}

		amount := in.Amount()

		if now >= locktime {
			newUnlockedConsumed, err := math.Add(unlockedConsumed[realAssetID], amount)
			if err != nil {
				return err
			}
			unlockedConsumed[realAssetID] = newUnlockedConsumed
			continue
		}

		owned, ok := out.(fx.Owned)
		if !ok {
			return fmt.Errorf("expected fx.Owned but got %T", out)
		}
		owner := owned.Owners()
		ownerBytes, err := txs.Codec.Marshal(txs.CodecVersion, owner)
		if err != nil {
			return fmt.Errorf("couldn't marshal owner: %w", err)
		}
		lockedConsumedAsset, ok := lockedConsumed[realAssetID]
		if !ok {
			lockedConsumedAsset = make(map[uint64]map[ids.ID]uint64)
			lockedConsumed[realAssetID] = lockedConsumedAsset
		}
		ownerID := hashing.ComputeHash256Array(ownerBytes)
		owners, ok := lockedConsumedAsset[locktime]
		if !ok {
			owners = make(map[ids.ID]uint64)
			lockedConsumedAsset[locktime] = owners
		}
		newAmount, err := math.Add(owners[ownerID], amount)
		if err != nil {
			return err
		}
		owners[ownerID] = newAmount
	}

	for _, out := range outs {
		assetID := out.AssetID()

		output := out.Output()
		locktime := uint64(0)
		// Set [locktime] to this output's locktime, if applicable
		if inner, ok := output.(*stakeable.LockOut); ok {
			output = inner.TransferableOut
			locktime = inner.Locktime
		}

		amount := output.Amount()

		if locktime == 0 {
			newUnlockedProduced, err := math.Add(unlockedProduced[assetID], amount)
			if err != nil {
				return err
			}
			unlockedProduced[assetID] = newUnlockedProduced
			continue
		}

		owned, ok := output.(fx.Owned)
		if !ok {
			return fmt.Errorf("expected fx.Owned but got %T", out)
		}
		owner := owned.Owners()
		ownerBytes, err := txs.Codec.Marshal(txs.CodecVersion, owner)
		if err != nil {
			return fmt.Errorf("couldn't marshal owner: %w", err)
		}
		lockedProducedAsset, ok := lockedProduced[assetID]
		if !ok {
			lockedProducedAsset = make(map[uint64]map[ids.ID]uint64)
			lockedProduced[assetID] = lockedProducedAsset
		}
		ownerID := hashing.ComputeHash256Array(ownerBytes)
		owners, ok := lockedProducedAsset[locktime]
		if !ok {
			owners = make(map[ids.ID]uint64)
			lockedProducedAsset[locktime] = owners
		}
		newAmount, err := math.Add(owners[ownerID], amount)
		if err != nil {
			return err
		}
		owners[ownerID] = newAmount
	}

	// Make sure that for each assetID and locktime, tokens produced <= tokens consumed
	for assetID, producedAssetAmounts := range lockedProduced {
		lockedConsumedAsset := lockedConsumed[assetID]
		for locktime, producedAmounts := range producedAssetAmounts {
			consumedAmounts := lockedConsumedAsset[locktime]
			for ownerID, producedAmount := range producedAmounts {
				consumedAmount := consumedAmounts[ownerID]

				if producedAmount > consumedAmount {
					increase := producedAmount - consumedAmount
					unlockedConsumedAsset := unlockedConsumed[assetID]
					if increase > unlockedConsumedAsset {
						return fmt.Errorf(
							"%w: %s needs %d more %s for locktime %d",
							ErrInsufficientLockedFunds,
							ownerID,
							increase-unlockedConsumedAsset,
							assetID,
							locktime,
						)
					}
					unlockedConsumed[assetID] = unlockedConsumedAsset - increase
				}
			}
		}
	}

	for assetID, unlockedProducedAsset := range unlockedProduced {
		unlockedConsumedAsset := unlockedConsumed[assetID]
		// More unlocked tokens produced than consumed. Invalid.
		if unlockedProducedAsset > unlockedConsumedAsset {
			return fmt.Errorf(
				"%w: needs %d more %s",
				ErrInsufficientUnlockedFunds,
				unlockedProducedAsset-unlockedConsumedAsset,
				assetID,
			)
		}
	}
	return nil
}
