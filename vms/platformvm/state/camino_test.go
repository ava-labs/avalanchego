// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database/versiondb"
	root_genesis "github.com/ava-labs/avalanchego/genesis"
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

const (
	testNetworkID = 10 // To be used in tests
)

func TestSupplyInState(t *testing.T) {
	require := require.New(t)

	tests := map[string]struct {
		s             *state
		genesisConfig root_genesis.Config
	}{
		"LockModeDepositBond: true with NodeID and Deposit": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(true, initialNodeID, true),
		},
		"LockModeDepositBond: true with NodeID": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(true, initialNodeID, false),
		},
		"LockModeDepositBond: true": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(true, ids.EmptyNodeID, false),
		},
		"LockModeDepositBond: false with NodeID and Deposit": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(false, initialNodeID, true),
		},
		"LockModeDepositBond: false with NodeID": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(false, initialNodeID, false),
		},
		"LockModeDepositBond: false": {
			s:             newEmptyState(t),
			genesisConfig: defaultConfig(false, ids.EmptyNodeID, false),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			genBytes, _, err := root_genesis.FromConfig(&tt.genesisConfig)
			require.NoError(err)

			err = tt.s.sync(genBytes)
			require.NoError(err)

			expectedSupply := getExpectedSupply(tt.genesisConfig, tt.s, genBytes, require)

			require.EqualValues(expectedSupply, tt.s.currentSupply)
		})
	}
}

func getExpectedSupply(config root_genesis.Config, s *state, genBytes []byte, require *require.Assertions) uint64 {
	var totalXAmount, totalPAmount, totalIAmount uint64

	for _, alloc := range config.Allocations {
		totalIAmount += alloc.InitialAmount

		for _, unlockSchedule := range alloc.UnlockSchedule {
			totalIAmount += unlockSchedule.Amount
		}
	}
	for _, alloc := range config.Camino.Allocations {
		totalXAmount += alloc.XAmount
		for _, palloc := range alloc.PlatformAllocations {
			totalPAmount += palloc.Amount
		}
	}

	allocationsSum := totalXAmount + totalPAmount + totalIAmount

	offers := make(map[ids.ID]genesis.DepositOffer, len(config.Camino.DepositOffers))
	for _, offer := range config.Camino.DepositOffers {
		offerID, _ := offer.ID()
		offers[offerID] = offer
	}

	totalRewardAmount := uint64(0)
	for _, alloc := range config.Camino.Allocations {
		for _, palloc := range alloc.PlatformAllocations {
			if palloc.DepositOfferID != ids.Empty {
				depositOffer, err := s.GetDepositOffer(palloc.DepositOfferID)
				require.NoError(err)

				dep := deposit.Deposit{
					Amount:   palloc.Amount,
					Duration: uint32(palloc.DepositDuration),
				}

				totalRewardAmount += dep.TotalReward(depositOffer)
			}
		}
	}

	if totalRewardAmount == 0 {
		genesisState, _ := genesis.ParseState(genBytes)

		for _, vdrTx := range genesisState.Validators {
			tx, _ := vdrTx.Unsigned.(txs.ValidatorTx)
			stakeAmount := tx.Weight()
			stakeDuration := tx.EndTime().Sub(tx.StartTime())
			totalRewardAmount += s.rewards.Calculate(
				stakeDuration,
				stakeAmount,
				s.currentSupply,
			)
		}
	}

	return allocationsSum + totalRewardAmount
}

func TestSyncGenesis(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s, _ := newInitializedState(require)

	var (
		id           = ids.GenerateTestID()
		shortID      = ids.GenerateTestShortID()
		shortID2     = ids.GenerateTestShortID()
		initialAdmin = ids.GenerateTestShortID()
	)

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
				}, depositTxs, initialAdmin),
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

func defaultConfig(lockModeBondDeposit bool, nodeID ids.NodeID, deposit bool) root_genesis.Config {
	var (
		defaultMinValidatorStake = 5 * units.MilliAvax
		minStake                 = root_genesis.GetStakingConfig(testNetworkID).MinValidatorStake
		minStakeDuration         = uint64(1000)
		depositOfferID           = ids.Empty
		initialAdmin             = ids.GenerateTestShortID()
	)

	depositOffers := []genesis.DepositOffer{
		{
			InterestRateNominator:   1,
			Start:                   2,
			End:                     3,
			MinAmount:               4,
			MinDuration:             5,
			MaxDuration:             6,
			UnlockPeriodDuration:    7,
			NoRewardsPeriodDuration: 2,
			Flags:                   9,
		},
	}

	if deposit {
		depositOfferID, _ = depositOffers[0].ID()
		depositOffers[0].OfferID = depositOfferID
	}

	var caminoAllocations []root_genesis.CaminoAllocation
	var allocations []root_genesis.Allocation

	platformAllocations := []root_genesis.PlatformAllocation{
		{
			Amount:            minStake,
			NodeID:            nodeID,
			ValidatorDuration: defaultMinValidatorStake,
			DepositDuration:   5,
			TimestampOffset:   10,
			DepositOfferID:    depositOfferID,
			Memo:              "",
		},
	}

	if lockModeBondDeposit {
		caminoAllocations = []root_genesis.CaminoAllocation{
			{
				ETHAddr:  initialAdmin,
				AVAXAddr: initialAdmin,
				XAmount:  10 * defaultMinValidatorStake,
				AddressStates: root_genesis.AddressStates{
					ConsortiumMember: true,
					KYCVerified:      true,
				},
				PlatformAllocations: platformAllocations,
			},
		}
	} else {
		allocations = []root_genesis.Allocation{
			{
				ETHAddr:       initialAdmin,
				AVAXAddr:      initialAdmin,
				InitialAmount: minStake,
				UnlockSchedule: []root_genesis.LockedAmount{
					{
						Amount:   1,
						Locktime: 0,
					},
				},
			},
		}
	}

	config := root_genesis.Config{
		NetworkID:                  0,
		Allocations:                allocations,
		StartTime:                  10,
		InitialStakeDuration:       minStakeDuration,
		InitialStakeDurationOffset: 10,
		InitialStakedFunds:         []ids.ShortID{initialAdmin},
		InitialStakers: []root_genesis.Staker{
			{
				NodeID:        nodeID,
				RewardAddress: initialAdmin,
			},
		},
		Camino: root_genesis.Camino{
			VerifyNodeSignature:      true,
			LockModeBondDeposit:      lockModeBondDeposit,
			InitialAdmin:             initialAdmin,
			DepositOffers:            depositOffers,
			Allocations:              caminoAllocations,
			InitialMultisigAddresses: nil,
		},
		CChainGenesis: "",
		Message:       "",
	}

	return config
}

func defaultGenesisState(addresses []genesis.AddressState, deposits []*txs.Tx, initialAdmin ids.ShortID) *genesis.State {
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
				Deposits: deposits,
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
