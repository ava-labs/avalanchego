// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	db_manager "github.com/ava-labs/avalanchego/database/manager"
)

var (
	id           = ids.GenerateTestID()
	shortID      = ids.GenerateTestShortID()
	shortID2     = ids.GenerateTestShortID()
	initialAdmin = ids.GenerateTestShortID()
)

func TestSyncGenesis(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s, _ := newInitializedState(require)

	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)

	outputOwners := secp256k1fx.OutputOwners{
		Addrs: []ids.ShortID{shortID},
	}
	outputOwners2 := secp256k1fx.OutputOwners{
		Addrs: []ids.ShortID{shortID2},
	}

	depositOffers := []*deposit.Offer{
		{
			InterestRateNominator:   1,
			Start:                   2,
			End:                     3,
			MinAmount:               4,
			MinDuration:             5,
			MaxDuration:             6,
			UnlockPeriodDuration:    7,
			NoRewardsPeriodDuration: 8,
			Flags:                   9,
		}, {
			InterestRateNominator:   2,
			Start:                   3,
			End:                     4,
			MinAmount:               5,
			MinDuration:             6,
			MaxDuration:             7,
			UnlockPeriodDuration:    8,
			NoRewardsPeriodDuration: 9,
			Flags:                   10,
		},
	}
	require.NoError(depositOffers[0].SetID())
	require.NoError(depositOffers[1].SetID())

	depositTxs := []*txs.Tx{
		{
			Unsigned: &txs.DepositTx{
				BaseTx:          *generateBaseTx(id, 1, outputOwners, ids.Empty, ids.Empty),
				DepositOfferID:  depositOffers[0].ID,
				DepositDuration: 1,
				RewardsOwner:    &outputOwners,
			},
			Creds: nil,
		},
		{
			Unsigned: &txs.DepositTx{
				BaseTx:          *generateBaseTx(id, 2, outputOwners2, ids.Empty, ids.Empty),
				DepositOfferID:  depositOffers[1].ID,
				DepositDuration: 2,
				RewardsOwner:    &outputOwners2,
			},
			Creds: nil,
		},
	}
	depositTxs[0].Initialize(utils.RandomBytes(16), utils.RandomBytes(16))
	depositTxs[1].Initialize(utils.RandomBytes(16), utils.RandomBytes(16))

	type args struct {
		s *state
		g *genesis.State
	}
	tests := map[string]struct {
		cs      caminoState
		args    args
		want    caminoDiff
		prepare func(cd caminoDiff)
		err     error
	}{
		"successful addition of address states, deposits and deposit offers": {
			args: args{
				s: s.(*state),
				g: defaultGenesisState([]genesis.AddressState{
					{
						Address: initialAdmin,
						State:   txs.AddressStateRoleAdminBit,
					},
					{
						Address: shortID,
						State:   txs.AddressStateRoleValidatorBit,
					},
				}, depositTxs),
			},
			cs: *wrappers.IgnoreError(newCaminoState(versiondb.New(baseDBManager.Current().Database), prometheus.NewRegistry())).(*caminoState),
			want: caminoDiff{
				modifiedAddressStates: map[ids.ShortID]uint64{initialAdmin: txs.AddressStateRoleAdminBit, shortID: txs.AddressStateRoleValidatorBit},
				modifiedDepositOffers: map[ids.ID]*deposit.Offer{},
				modifiedDeposits: map[ids.ID]*deposit.Deposit{
					depositTxs[0].ID(): {
						DepositOfferID: depositTxs[0].Unsigned.(*txs.DepositTx).DepositOfferID,
						Amount:         1,
					},
					depositTxs[1].ID(): {
						DepositOfferID: depositTxs[1].Unsigned.(*txs.DepositTx).DepositOfferID,
						Amount:         2,
					},
				},
			},
			prepare: func(cd caminoDiff) {
				for _, v := range depositOffers {
					cd.modifiedDepositOffers[v.ID] = v //nolint:nolintlint
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.prepare(tt.want)
			err := tt.cs.SyncGenesis(tt.args.s, tt.args.g)
			require.ErrorIs(tt.err, err)

			for k, d := range tt.want.modifiedDeposits {
				require.Equal(d.DepositOfferID, tt.cs.modifiedDeposits[k].DepositOfferID)
				require.Equal(d.Amount, tt.cs.modifiedDeposits[k].Amount)
			}
			for i, o := range tt.want.modifiedDepositOffers {
				require.Equal(o, tt.cs.modifiedDepositOffers[i])
			}
			require.Truef(reflect.DeepEqual(tt.want.modifiedAddressStates, tt.cs.caminoDiff.modifiedAddressStates), "\ngot: %v\nwant: %v", tt.want.modifiedAddressStates, tt.cs.caminoDiff.modifiedAddressStates)
		})
	}
}

func defaultGenesisState(addresses []genesis.AddressState, deposits []*txs.Tx) *genesis.State {
	return &genesis.State{
		UTXOs: []*avax.UTXO{
			{
				UTXOID: avax.UTXOID{
					TxID:        initialTxID,
					OutputIndex: 0,
				},
				Asset: avax.Asset{ID: initialTxID},
				Out: &secp256k1fx.TransferOutput{
					Amt: units.Schmeckle,
				},
			},
		},
		Chains:        []*txs.Tx{{}},
		Timestamp:     uint64(initialTime.Unix()),
		InitialSupply: units.Schmeckle + units.Avax,
		Camino: genesis.Camino{
			AddressStates: addresses,
			InitialAdmin:  initialAdmin,
			Blocks: []*genesis.Block{{
				Validators: []*txs.Tx{{Unsigned: &txs.CaminoAddValidatorTx{
					AddValidatorTx: txs.AddValidatorTx{
						BaseTx:       txs.BaseTx{},
						Validator:    validator.Validator{},
						RewardsOwner: &secp256k1fx.OutputOwners{},
					},
				}}},
				Deposits:        deposits,
				UnlockedUTXOsTx: &txs.Tx{Unsigned: &txs.BaseTx{}},
			}},
			DepositOffers: []genesis.DepositOffer{
				{
					InterestRateNominator:   1,
					Start:                   2,
					End:                     3,
					MinAmount:               4,
					MinDuration:             5,
					MaxDuration:             6,
					UnlockPeriodDuration:    7,
					NoRewardsPeriodDuration: 8,
					Flags:                   9,
				},
				{
					InterestRateNominator:   2,
					Start:                   3,
					End:                     4,
					MinAmount:               5,
					MinDuration:             6,
					MaxDuration:             7,
					UnlockPeriodDuration:    8,
					NoRewardsPeriodDuration: 9,
					Flags:                   10,
				},
			},
		},
	}
}
