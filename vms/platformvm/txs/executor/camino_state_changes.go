// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
)

var (
	errEndOfTime = errors.New("program time is suspiciously far in the future")
)

type caminoStateChanges struct {
	updatedClaimables map[ids.ID]*state.Claimable
	removedDeposits   []ids.ID
	consumedUTXOs     []*avax.TransferableInput
	addedUTXOs        []*avax.UTXO
	addedRewardUTXOs  []*avax.UTXO // TODO@ do we need reward utxos at all? learn and make consistent !!
}

func (cs *caminoStateChanges) Apply(stateDiff state.Diff) {
	for claimableOwnerID, claimable := range cs.updatedClaimables {
		stateDiff.SetClaimable(claimableOwnerID, claimable)
	}
	for _, depositTxID := range cs.removedDeposits {
		stateDiff.SetDeposit(depositTxID, nil)
	}
	utxo.Consume(stateDiff, cs.consumedUTXOs)
	for _, utxo := range cs.addedUTXOs {
		stateDiff.AddUTXO(utxo)
	}
	for _, utxo := range cs.addedRewardUTXOs {
		stateDiff.AddRewardUTXO(utxo.TxID, utxo)
	}

	// var tx *txs.Tx
	// err := tx.Unsigned.Visit(&CaminoStandardTxExecutor{
	// 	StandardTxExecutor{
	// 		Backend: &env.backend,
	// 		State:   stateDiff,
	// 		Tx:      tx,
	// 	},
	// })
	// stateDiff.AddTx(tx, status.Committed)
}

func (cs *caminoStateChanges) Len() int {
	return len(cs.updatedClaimables) + len(cs.removedDeposits) +
		len(cs.consumedUTXOs) + len(cs.addedUTXOs) + len(cs.addedRewardUTXOs)
}

func caminoAdvanceTimeTo(
	backend *Backend,
	parentState state.Chain,
	newChainTime time.Time,
	changes *stateChanges,
) error {
	depositTxIDs, shouldUnlock, err := getNextDepositsToUnlock(parentState, newChainTime)
	if err != nil {
		return fmt.Errorf("could not find next staker to reward: %w", err)
	}
	if shouldUnlock {
		// ins, outs, err := backend.SystemSpender.Unlock(
		// 	parentState,
		// 	depositTxIDs,
		// 	locked.StateDeposited,
		// )
		// if err != nil {
		// 	return fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		// }

		// utx := &txs.UnlockDepositTx{
		// 	BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
		// 		NetworkID:    backend.Ctx.NetworkID,
		// 		BlockchainID: backend.Ctx.ChainID,
		// 		Ins:          ins,
		// 		Outs:         outs,
		// 	}},
		// }

		// tx, err := txs.NewSigned(utx, txs.Codec, nil)
		// if err != nil {
		// 	return err
		// }
		// if err := tx.SyntacticVerify(backend.Ctx); err != nil {
		// 	return err
		// }

		ins, outs, err := backend.SystemSpender.Unlock(
			parentState,
			depositTxIDs,
			locked.StateDeposited,
		)
		if err != nil {
			return fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}

		insBytes, err := txs.Codec.Marshal(txs.Version, ins)
		if err != nil {
			return err
		}

		// * @evlekht we can make real tx out of this here and just add it - its safe and sync-ok
		pseudoTxID := ids.ID(hashing.ComputeHash256Array(insBytes))

		changes.addedRewardUTXOs, changes.updatedClaimables, err = unlockDepositsFull(parentState, backend, pseudoTxID, depositTxIDs, len(outs))
		if err != nil {
			return err
		}
		changes.addedUTXOs = append(changes.addedUTXOs, changes.addedRewardUTXOs...)
		changes.removedDeposits = depositTxIDs
		// changes.removedDeposits = make(map[ids.ID]*deposit.Deposit, len(depositTxIDs))

		// for i, depositTxID := range depositTxIDs {
		// 	deposit, err := parentState.GetDeposit(depositTxID)
		// 	if err != nil {
		// 		return err
		// 	}

		// 	offer, err := parentState.GetDepositOffer(deposit.DepositOfferID)
		// 	if err != nil {
		// 		return err
		// 	}

		// 	if remainingReward := deposit.TotalReward(offer) - deposit.ClaimedRewardAmount; remainingReward > 0 {
		// 		tx, _, err := parentState.GetTx(depositTxID)
		// 		if err != nil {
		// 			return err
		// 		}
		// 		depositTx, ok := tx.Unsigned.(*txs.DepositTx)
		// 		if !ok {
		// 			return errWrongTxType
		// 		}

		// 		ownerID, err := txs.GetOwnerID(depositTx.RewardsOwner)
		// 		if err != nil {
		// 			return err
		// 		}

		// 		outIntf, err := backend.Fx.CreateOutput(remainingReward, depositTx.RewardsOwner)
		// 		if err != nil {
		// 			return fmt.Errorf("failed to create output: %w", err)
		// 		}
		// 		out, ok := outIntf.(verify.State)
		// 		if !ok {
		// 			return errInvalidState
		// 		}

		// 		claimable, err := parentState.GetClaimable(ownerID)
		// 		if err != nil {
		// 			return err
		// 		}

		// 		newClaimable := &state.Claimable{
		// 			Owner:           claimable.Owner,
		// 			ValidatorReward: claimable.ValidatorReward,
		// 		}

		// 		newClaimable.DepositReward, err = math.Add64(claimable.DepositReward, remainingReward)
		// 		if err != nil {
		// 			return err
		// 		}

		// 		utxo := &avax.UTXO{
		// 			UTXOID: avax.UTXOID{
		// 				TxID:        pseudoTxID,
		// 				OutputIndex: uint32(len(outs) + i),
		// 			},
		// 			Asset: avax.Asset{ID: backend.Ctx.AVAXAssetID},
		// 			Out:   out,
		// 		}

		// 		changes.addedUTXOs = append(changes.addedUTXOs, utxo)
		// 		changes.addedRewardUTXOs = append(changes.addedRewardUTXOs, utxo)

		// 		changes.updatedClaimables[ownerID] = newClaimable
		// 		changes.removedDeposits[depositTxID] = deposit
		// 	}
		// }

		for i, out := range outs {
			changes.addedUTXOs = append(changes.addedUTXOs, &avax.UTXO{
				UTXOID: avax.UTXOID{
					TxID:        pseudoTxID,
					OutputIndex: uint32(i),
				},
				Asset: out.Asset,
				Out:   out.Output(),
			})
		}

		changes.consumedUTXOs = ins

	}

	return nil
}

func getNextDepositsToUnlock(
	preferredState state.Chain,
	chainTime time.Time,
) ([]ids.ID, bool, error) {
	if !chainTime.Before(mockable.MaxTime) {
		return nil, false, errEndOfTime
	}

	nextDeposits, nextDepositsEndtime, err := preferredState.GetNextToUnlockDepositIDsAndTime()
	if err == database.ErrNotFound {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	return nextDeposits, nextDepositsEndtime.Equal(chainTime), nil
}
