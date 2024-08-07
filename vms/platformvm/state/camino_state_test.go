// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

func TestLockedUTXOs(t *testing.T) {
	bondTxID1 := ids.ID{0, 1}
	bondTxID2 := ids.ID{0, 2}
	depositTxID1 := ids.ID{0, 3}
	depositTxID2 := ids.ID{0, 4}
	owner1 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{1}}}
	owner2 := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{2}}}
	assetID := ids.ID{}

	// addr1, bond1
	utxoAddr1Bond1_1 := generate.UTXO(
		ids.ID{1}, assetID, 1, owner1, ids.Empty, bondTxID1, true)
	utxoAddr1Bond1_2 := generate.UTXO(
		ids.ID{2}, assetID, 1, owner1, ids.Empty, bondTxID1, true)
	// addr2, bond1
	utxoAddr2Bond1_1 := generate.UTXO(
		ids.ID{3}, assetID, 1, owner2, ids.Empty, bondTxID1, true)
	utxoAddr2Bond1_2 := generate.UTXO(
		ids.ID{4}, assetID, 1, owner2, ids.Empty, bondTxID1, true)
	// addr1, bond2
	utxoAddr1Bond2_1 := generate.UTXO(
		ids.ID{5}, assetID, 1, owner1, ids.Empty, bondTxID2, true)
	utxoAddr1Bond2_2 := generate.UTXO(
		ids.ID{6}, assetID, 1, owner1, ids.Empty, bondTxID2, true)
	// addr2, bond2
	utxoAddr2Bond2_1 := generate.UTXO(
		ids.ID{7}, assetID, 1, owner2, ids.Empty, bondTxID2, true)
	utxoAddr2Bond2_2 := generate.UTXO(
		ids.ID{8}, assetID, 1, owner2, ids.Empty, bondTxID2, true)
	// addr1, deposit1
	utxoAddr1Deposit1_1 := generate.UTXO(
		ids.ID{9}, assetID, 1, owner1, depositTxID1, ids.Empty, true)
	utxoAddr1Deposit1_2 := generate.UTXO(
		ids.ID{10}, assetID, 1, owner1, depositTxID1, ids.Empty, true)
	// addr2, deposit1
	utxoAddr2Deposit1_1 := generate.UTXO(
		ids.ID{11}, assetID, 1, owner2, depositTxID1, ids.Empty, true)
	utxoAddr2Deposit1_2 := generate.UTXO(
		ids.ID{12}, assetID, 1, owner2, depositTxID1, ids.Empty, true)
	// addr1, deposit2
	utxoAddr1Deposit2_1 := generate.UTXO(
		ids.ID{13}, assetID, 1, owner1, depositTxID2, ids.Empty, true)
	utxoAddr1Deposit2_2 := generate.UTXO(
		ids.ID{14}, assetID, 1, owner1, depositTxID2, ids.Empty, true)
	// addr2, deposit2
	utxoAddr2Deposit2_1 := generate.UTXO(
		ids.ID{15}, assetID, 1, owner2, depositTxID2, ids.Empty, true)
	utxoAddr2Deposit2_2 := generate.UTXO(
		ids.ID{16}, assetID, 1, owner2, depositTxID2, ids.Empty, true)
	// addr1, deposit1, bond1
	utxoAddr1Deposit1Bond1_1 := generate.UTXO(
		ids.ID{17}, assetID, 1, owner1, depositTxID1, bondTxID1, true)
	utxoAddr1Deposit1Bond1_2 := generate.UTXO(
		ids.ID{18}, assetID, 1, owner1, depositTxID1, bondTxID1, true)
	// addr2, deposit1, bond1
	utxoAddr2Deposit1Bond1_1 := generate.UTXO(
		ids.ID{19}, assetID, 1, owner2, depositTxID1, bondTxID1, true)
	utxoAddr2Deposit1Bond1_2 := generate.UTXO(
		ids.ID{20}, assetID, 1, owner2, depositTxID1, bondTxID1, true)
	// addr1, deposit2, bond1
	utxoAddr1Deposit2Bond1_1 := generate.UTXO(
		ids.ID{21}, assetID, 1, owner1, depositTxID2, bondTxID1, true)
	utxoAddr1Deposit2Bond1_2 := generate.UTXO(
		ids.ID{22}, assetID, 1, owner1, depositTxID2, bondTxID1, true)
	// addr2, deposit2, bond1
	utxoAddr2Deposit2Bond1_1 := generate.UTXO(
		ids.ID{23}, assetID, 1, owner2, depositTxID2, bondTxID1, true)
	utxoAddr2Deposit2Bond1_2 := generate.UTXO(
		ids.ID{24}, assetID, 1, owner2, depositTxID2, bondTxID1, true)

	allUTXOs := []*avax.UTXO{
		utxoAddr1Bond1_1, utxoAddr1Bond1_2,
		utxoAddr2Bond1_1, utxoAddr2Bond1_2,
		utxoAddr1Bond2_1, utxoAddr1Bond2_2,
		utxoAddr2Bond2_1, utxoAddr2Bond2_2,
		utxoAddr1Deposit1_1, utxoAddr1Deposit1_2,
		utxoAddr2Deposit1_1, utxoAddr2Deposit1_2,
		utxoAddr1Deposit2_1, utxoAddr1Deposit2_2,
		utxoAddr2Deposit2_1, utxoAddr2Deposit2_2,
		utxoAddr1Deposit1Bond1_1, utxoAddr1Deposit1Bond1_2,
		utxoAddr2Deposit1Bond1_1, utxoAddr2Deposit1Bond1_2,
		utxoAddr1Deposit2Bond1_1, utxoAddr1Deposit2Bond1_2,
		utxoAddr2Deposit2Bond1_1, utxoAddr2Deposit2Bond1_2,
	}

	tests := map[string]struct {
		state         func(*testing.T) *state
		lockTxIDs     []ids.ID
		addresses     []ids.ShortID
		lockState     locked.State
		expectedUTXOs []*avax.UTXO
		expectedErr   error
	}{
		"OK: addr1, bond1": {
			state: func(t *testing.T) *state {
				state := newEmptyState(t)
				for _, utxo := range allUTXOs {
					state.AddUTXO(utxo)
				}
				require.NoError(t, state.Commit())
				return state
			},
			lockTxIDs: []ids.ID{bondTxID1},
			addresses: []ids.ShortID{owner1.Addrs[0]},
			lockState: locked.StateBonded,
			expectedUTXOs: []*avax.UTXO{
				utxoAddr1Bond1_1, utxoAddr1Bond1_2,
				utxoAddr1Deposit1Bond1_1, utxoAddr1Deposit1Bond1_2,
				utxoAddr1Deposit2Bond1_1, utxoAddr1Deposit2Bond1_2,
			},
		},
		"OK: addr2, bond1, bond2": {
			state: func(t *testing.T) *state {
				state := newEmptyState(t)
				for _, utxo := range allUTXOs {
					state.AddUTXO(utxo)
				}
				require.NoError(t, state.Commit())
				return state
			},
			lockTxIDs: []ids.ID{bondTxID1, bondTxID2},
			addresses: []ids.ShortID{owner2.Addrs[0]},
			lockState: locked.StateBonded,
			expectedUTXOs: []*avax.UTXO{
				utxoAddr2Bond1_1, utxoAddr2Bond1_2,
				utxoAddr2Bond2_1, utxoAddr2Bond2_2,
				utxoAddr2Deposit1Bond1_1, utxoAddr2Deposit1Bond1_2,
				utxoAddr2Deposit2Bond1_1, utxoAddr2Deposit2Bond1_2,
			},
		},
		"OK: addr1, deposit1": {
			state: func(t *testing.T) *state {
				state := newEmptyState(t)
				for _, utxo := range allUTXOs {
					state.AddUTXO(utxo)
				}
				require.NoError(t, state.Commit())
				return state
			},
			lockTxIDs: []ids.ID{depositTxID1},
			addresses: []ids.ShortID{owner1.Addrs[0]},
			lockState: locked.StateDeposited,
			expectedUTXOs: []*avax.UTXO{
				utxoAddr1Deposit1_1, utxoAddr1Deposit1_2,
				utxoAddr1Deposit1Bond1_1, utxoAddr1Deposit1Bond1_2,
			},
		},
		"OK: addr1, deposit1, deposit2": {
			state: func(t *testing.T) *state {
				state := newEmptyState(t)
				for _, utxo := range allUTXOs {
					state.AddUTXO(utxo)
				}
				require.NoError(t, state.Commit())
				return state
			},
			lockTxIDs: []ids.ID{depositTxID1, depositTxID2},
			addresses: []ids.ShortID{owner1.Addrs[0]},
			lockState: locked.StateDeposited,
			expectedUTXOs: []*avax.UTXO{
				utxoAddr1Deposit1_1, utxoAddr1Deposit1_2,
				utxoAddr1Deposit2_1, utxoAddr1Deposit2_2,
				utxoAddr1Deposit1Bond1_1, utxoAddr1Deposit1Bond1_2,
				utxoAddr1Deposit2Bond1_1, utxoAddr1Deposit2Bond1_2,
			},
		},
		"OK: addr1, addr2, deposit1": {
			state: func(t *testing.T) *state {
				state := newEmptyState(t)
				for _, utxo := range allUTXOs {
					state.AddUTXO(utxo)
				}
				require.NoError(t, state.Commit())
				return state
			},
			lockTxIDs: []ids.ID{depositTxID1},
			addresses: []ids.ShortID{owner1.Addrs[0], owner2.Addrs[0]},
			lockState: locked.StateDeposited,
			expectedUTXOs: []*avax.UTXO{
				utxoAddr1Deposit1_1, utxoAddr1Deposit1_2,
				utxoAddr2Deposit1_1, utxoAddr2Deposit1_2,
				utxoAddr1Deposit1Bond1_1, utxoAddr1Deposit1Bond1_2,
				utxoAddr2Deposit1Bond1_1, utxoAddr2Deposit1Bond1_2,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			lockTxIDs := set.Set[ids.ID]{}
			addresses := set.Set[ids.ShortID]{}
			for _, lockTxID := range tt.lockTxIDs {
				lockTxIDs.Add(lockTxID)
			}
			for _, addr := range tt.addresses {
				addresses.Add(addr)
			}

			utxos, err := tt.state(t).LockedUTXOs(lockTxIDs, addresses, tt.lockState)
			require.ErrorIs(t, err, tt.expectedErr)
			require.ElementsMatch(t, tt.expectedUTXOs, utxos)
		})
	}
}
