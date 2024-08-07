// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package expect

import (
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func VerifyMultisigPermission(t *testing.T, s *state.MockDiff, addrs []ids.ShortID, aliases []*multisig.AliasWithNonce) {
	t.Helper()
	GetMultisigAliases(t, s, addrs, aliases)
}

func GetMultisigAliases(t *testing.T, s *state.MockDiff, addrs []ids.ShortID, aliases []*multisig.AliasWithNonce) {
	t.Helper()
	for i := range addrs {
		var alias *multisig.AliasWithNonce
		if i < len(aliases) {
			alias = aliases[i]
		}
		if alias == nil {
			s.EXPECT().GetMultisigAlias(addrs[i]).Return(nil, database.ErrNotFound)
		} else {
			s.EXPECT().GetMultisigAlias(addrs[i]).Return(alias, nil)
		}
	}
}

func VerifyLock(
	t *testing.T,
	s *state.MockDiff,
	ins []*avax.TransferableInput,
	utxos []*avax.UTXO,
	addrs []ids.ShortID,
	aliases []*multisig.AliasWithNonce,
) {
	t.Helper()
	GetUTXOsFromInputs(t, s, ins, utxos)
	GetMultisigAliases(t, s, addrs, aliases)
}

func VerifyUnlockDeposit(
	t *testing.T,
	s *state.MockDiff,
	ins []*avax.TransferableInput,
	utxos []*avax.UTXO,
	addrs []ids.ShortID,
	aliases []*multisig.AliasWithNonce,
) {
	t.Helper()
	GetUTXOsFromInputs(t, s, ins, utxos)
	GetMultisigAliases(t, s, addrs, aliases)
}

// TODO @evlekht seems, that [addrs] actually not affecting anything and could be omitted
func Unlock(
	t *testing.T,
	s *state.MockDiff,
	lockTxIDs []ids.ID,
	addrs []ids.ShortID,
	utxos []*avax.UTXO,
	removedLockState locked.State,
) {
	t.Helper()
	lockTxIDsSet := set.NewSet[ids.ID](len(lockTxIDs))
	addrsSet := set.NewSet[ids.ShortID](len(addrs))
	lockTxIDsSet.Add(lockTxIDs...)
	addrsSet.Add(addrs...)
	for _, txID := range lockTxIDs {
		s.EXPECT().GetTx(txID).Return(&txs.Tx{
			Unsigned: &txs.BaseTx{BaseTx: avax.BaseTx{
				Outs: []*avax.TransferableOutput{{
					Out: &locked.Out{
						IDs: locked.IDsEmpty.Lock(removedLockState),
						TransferableOut: &secp256k1fx.TransferOutput{
							OutputOwners: secp256k1fx.OutputOwners{Addrs: addrs},
						},
					},
				}},
			}},
		}, status.Committed, nil)
	}
	s.EXPECT().LockedUTXOs(lockTxIDsSet, addrsSet, removedLockState).Return(utxos, nil)
}

// TODO @evlekht seems, that [addrs] actually not affecting anything and could be omitted
func UnlockDeposit(
	t *testing.T,
	s *state.MockState,
	deposits map[ids.ID]*deposit.Deposit,
	depositOffers []*deposit.Offer,
	utxoOwners []ids.ShortID,
	utxos []*avax.UTXO,
	removedLockState locked.State,
) {
	t.Helper()
	lockTxIDsSet := set.NewSet[ids.ID](len(deposits))
	addrsSet := set.NewSet[ids.ShortID](len(utxoOwners))
	addrsSet.Add(utxoOwners...)
	for depositTxID, deposit := range deposits {
		s.EXPECT().GetDeposit(depositTxID).Return(deposit, nil)
		lockTxIDsSet.Add(depositTxID)
	}
	for _, offer := range depositOffers {
		s.EXPECT().GetDepositOffer(offer.ID).Return(offer, nil)
	}
	s.EXPECT().LockedUTXOs(lockTxIDsSet, addrsSet, removedLockState).Return(utxos, nil)
	for _, addr := range utxoOwners {
		fmt.Printf("UnlockDeposit expect GetMultisigAlias %s", addr)
		s.EXPECT().GetMultisigAlias(addr).Return(nil, database.ErrNotFound)
	}
}

func GetUTXOsFromInputs(t *testing.T, s *state.MockDiff, ins []*avax.TransferableInput, utxos []*avax.UTXO) {
	t.Helper()
	for i := range ins {
		if utxos[i] == nil {
			s.EXPECT().GetUTXO(ins[i].InputID()).Return(nil, database.ErrNotFound)
		} else {
			s.EXPECT().GetUTXO(ins[i].InputID()).Return(utxos[i], nil)
		}
	}
}

func ConsumeUTXOs(t *testing.T, s *state.MockDiff, ins []*avax.TransferableInput) {
	t.Helper()
	for _, in := range ins {
		s.EXPECT().DeleteUTXO(in.InputID())
	}
}

func ProduceUTXOs(t *testing.T, s *state.MockDiff, outs []*avax.TransferableOutput, txID ids.ID, baseOutIndex int) {
	t.Helper()
	for i := range outs {
		s.EXPECT().AddUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(baseOutIndex + i),
			},
			Asset: outs[i].Asset,
			Out:   outs[i].Out,
		})
	}
}

func ProduceNewlyLockedUTXOs(t *testing.T, s *state.MockDiff, outs []*avax.TransferableOutput, txID ids.ID, baseOutIndex int, lockState locked.State) {
	t.Helper()
	for i := range outs {
		out := outs[i].Out
		if lockedOut, ok := out.(*locked.Out); ok {
			utxoLockedOut := *lockedOut
			utxoLockedOut.FixLockID(txID, lockState)
			out = &utxoLockedOut
		}
		s.EXPECT().AddUTXO(&avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(baseOutIndex + i),
			},
			Asset: outs[i].Asset,
			Out:   out,
		})
	}
}

// also fine for Spend
func Lock(t *testing.T, s *state.MockState, utxosMap map[ids.ShortID][]*avax.UTXO) {
	t.Helper()
	for addr, utxos := range utxosMap {
		utxoids := make([]ids.ID, len(utxos))
		for i := range utxos {
			utxoids[i] = utxos[i].InputID()
		}
		s.EXPECT().UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt).Return(utxoids, nil)
		for _, utxo := range utxos {
			s.EXPECT().GetUTXO(utxo.InputID()).Return(utxo, nil)
		}
	}
	for addr := range utxosMap {
		s.EXPECT().GetMultisigAlias(addr).Return(nil, database.ErrNotFound).AnyTimes()
	}
}
