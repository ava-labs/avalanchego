// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"reflect"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	root_genesis "github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	pvm_genesis "github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
	tests := map[string]struct {
		lockModeDepositBond bool
		withDeposit         bool
		withCaminoValidator bool
	}{
		"LockModeDepositBond: true with Validator and Deposit": {
			lockModeDepositBond: true, withDeposit: true, withCaminoValidator: true,
		},
		"LockModeDepositBond: true with Validator": {
			lockModeDepositBond: true, withDeposit: false, withCaminoValidator: true,
		},
		"LockModeDepositBond: true with Deposit": {
			lockModeDepositBond: true, withDeposit: true, withCaminoValidator: false,
		},
		"LockModeDepositBond: true": {
			lockModeDepositBond: true, withDeposit: false, withCaminoValidator: false,
		},
		"LockModeDepositBond: false with Validator and Deposit": {
			lockModeDepositBond: false, withDeposit: true, withCaminoValidator: true,
		},
		"LockModeDepositBond: false with Validator": {
			lockModeDepositBond: false, withDeposit: false, withCaminoValidator: true,
		},
		"LockModeDepositBond: false with Deposit": {
			lockModeDepositBond: false, withDeposit: true, withCaminoValidator: false,
		},
		"LockModeDepositBond: false": {
			lockModeDepositBond: false, withDeposit: false, withCaminoValidator: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			genesisConfig := testGenesisConfig(
				tt.lockModeDepositBond,
				tt.withCaminoValidator,
				tt.withDeposit,
			)

			genBytes, _, err := root_genesis.FromConfig(genesisConfig)
			require.NoError(err)

			state := newEmptyState(t)
			require.NoError(state.sync(genBytes))

			expectedSupply := getExpectedSupply(t, genesisConfig, tt.lockModeDepositBond, state.rewards)

			require.EqualValues(expectedSupply, state.currentSupply)
		})
	}
}

func getExpectedSupply(
	t *testing.T,
	config *root_genesis.Config,
	lockModeDepositBond bool,
	avaxRewardCalc reward.Calculator,
) uint64 {
	require := require.New(t)

	initialSupply := uint64(0)
	rewards := uint64(0)

	if lockModeDepositBond {
		for _, alloc := range config.Camino.Allocations {
			initialSupply += alloc.XAmount
			for _, palloc := range alloc.PlatformAllocations {
				initialSupply += palloc.Amount
			}
		}
		offers := make(map[string]root_genesis.DepositOffer, len(config.Camino.DepositOffers))
		for _, offer := range config.Camino.DepositOffers {
			offers[offer.Memo] = offer
		}

		for _, alloc := range config.Camino.Allocations {
			for _, palloc := range alloc.PlatformAllocations {
				if palloc.DepositOfferMemo != "" {
					configOffer, ok := offers[palloc.DepositOfferMemo]
					require.True(ok)

					offer, err := root_genesis.DepositOfferFromConfig(configOffer)
					require.NoError(err)

					dep := deposit.Deposit{
						Amount:   palloc.Amount,
						Duration: uint32(palloc.DepositDuration),
					}

					rewards += dep.TotalReward(offer)
				}
			}
		}
	} else {
		for _, alloc := range config.Allocations {
			initialSupply += alloc.InitialAmount
			for _, unlockSchedule := range alloc.UnlockSchedule {
				initialSupply += unlockSchedule.Amount
			}
		}

		initialStakedFunds := uint64(0)
		for _, allocAVAXAddr := range config.InitialStakedFunds {
			for _, alloc := range config.Allocations {
				if alloc.AVAXAddr != allocAVAXAddr {
					continue
				}
				for _, palloc := range alloc.UnlockSchedule {
					initialStakedFunds += palloc.Amount
				}
			}
		}
		stake := initialStakedFunds / uint64(len(config.InitialStakers))

		offset := config.InitialStakeDurationOffset
		for i := 0; i < len(config.InitialStakers); i++ {
			reward := avaxRewardCalc.Calculate(
				time.Duration(config.InitialStakeDuration-offset*uint64(i))*time.Second,
				stake,
				initialSupply,
			)
			rewards += reward
		}
	}

	return initialSupply + rewards
}

func TestSyncGenesis(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s, _ := newInitializedState(require)
	baseDBManager := db_manager.NewMemDB(version.Semantic1_0_0)
	baseDB := versiondb.New(baseDBManager.Current().Database)
	validatorsDB := prefixdb.New(validatorsPrefix, baseDB)

	var (
		id           = ids.GenerateTestID()
		shortID      = ids.GenerateTestShortID()
		shortID2     = ids.GenerateTestShortID()
		initialAdmin = ids.GenerateTestShortID()
	)

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
			Memo:                    []byte("some memo"),
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
	for _, offer := range depositOffers {
		require.NoError(pvm_genesis.SetDepositOfferID(offer))
	}

	depositTx1, err := txs.NewSigned(&txs.DepositTx{
		BaseTx:          *generateBaseTx(id, 1, outputOwners, ids.Empty, ids.Empty),
		DepositOfferID:  depositOffers[0].ID,
		DepositDuration: 1,
		RewardsOwner:    &outputOwners,
	}, pvm_genesis.Codec, nil)
	require.NoError(err)

	depositTx2, err := txs.NewSigned(&txs.DepositTx{
		BaseTx:          *generateBaseTx(id, 2, outputOwners2, ids.Empty, ids.Empty),
		DepositOfferID:  depositOffers[1].ID,
		DepositDuration: 2,
		RewardsOwner:    &outputOwners2,
	}, pvm_genesis.Codec, nil)
	require.NoError(err)

	depositTxs := []*txs.Tx{depositTx1, depositTx2}

	type args struct {
		s *state
		g *pvm_genesis.State
	}
	tests := map[string]struct {
		cs   caminoState
		args args
		want caminoDiff
		err  error
	}{
		"successful addition of address states, deposits and deposit offers": {
			args: args{
				s: s.(*state),
				g: defaultGenesisState([]pvm_genesis.AddressState{
					{
						Address: initialAdmin,
						State:   txs.AddressStateRoleAdmin,
					},
					{
						Address: shortID,
						State:   txs.AddressStateRoleKYC,
					},
				}, depositTxs, initialAdmin),
			},
			cs: *wrappers.IgnoreError(newCaminoState(baseDB, validatorsDB, prometheus.NewRegistry())).(*caminoState),
			want: caminoDiff{
				modifiedAddressStates: map[ids.ShortID]txs.AddressState{initialAdmin: txs.AddressStateRoleAdmin, shortID: txs.AddressStateRoleKYC},
				modifiedDepositOffers: map[ids.ID]*deposit.Offer{
					depositOffers[0].ID: depositOffers[0],
					depositOffers[1].ID: depositOffers[1],
				},
				modifiedDeposits: map[ids.ID]*depositDiff{
					depositTxs[0].ID(): {
						Deposit: &deposit.Deposit{
							DepositOfferID: depositTxs[0].Unsigned.(*txs.DepositTx).DepositOfferID,
							Amount:         1,
						},
						added: true,
					},
					depositTxs[1].ID(): {
						Deposit: &deposit.Deposit{
							DepositOfferID: depositTxs[1].Unsigned.(*txs.DepositTx).DepositOfferID,
							Amount:         2,
						},
						added: true,
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.NoError(tt.args.g.Camino.Init())
			err := tt.cs.SyncGenesis(tt.args.s, tt.args.g)
			require.ErrorIs(tt.err, err)

			require.Len(tt.cs.modifiedDeposits, len(tt.want.modifiedDeposits))
			for expectedAddedDepositID, expectedAddedDeposit := range tt.want.modifiedDeposits {
				actualAddedDepositDiff, ok := tt.cs.modifiedDeposits[expectedAddedDepositID]
				require.True(ok)
				require.Equal(expectedAddedDeposit.DepositOfferID, actualAddedDepositDiff.DepositOfferID)
				require.Equal(expectedAddedDeposit.Amount, actualAddedDepositDiff.Amount)
			}
			for expectedModifiedDepositOfferID, expectedModifiedDepositOffer := range tt.want.modifiedDepositOffers {
				actualModifiedDepositOffer, ok := tt.cs.modifiedDepositOffers[expectedModifiedDepositOfferID]
				require.True(ok)
				require.Equal(expectedModifiedDepositOffer, actualModifiedDepositOffer)
			}
			require.Truef(reflect.DeepEqual(tt.want.modifiedAddressStates, tt.cs.caminoDiff.modifiedAddressStates), "\ngot: %v\nwant: %v", tt.want.modifiedAddressStates, tt.cs.caminoDiff.modifiedAddressStates)
		})
	}
}

func testGenesisConfig(lockModeBondDeposit bool, validator, deposit bool) *root_genesis.Config {
	var (
		defaultMinValidatorStake = 5 * units.MilliAvax
		minStake                 = root_genesis.GetStakingConfig(testNetworkID).MinValidatorStake
		minStakeDuration         = uint64(1000)
		initialAdmin             = ids.GenerateTestShortID()
	)

	depositOffer := root_genesis.DepositOffer{
		InterestRateNominator:   1,
		Start:                   2,
		End:                     3,
		MinAmount:               4,
		MinDuration:             5,
		MaxDuration:             6,
		UnlockPeriodDuration:    7,
		NoRewardsPeriodDuration: 2,
		Flags:                   9,
		Memo:                    "some memo",
	}

	depositOfferMemo := ""
	if deposit {
		depositOfferMemo = depositOffer.Memo
	}

	nodeID := ids.EmptyNodeID
	if validator {
		nodeID = ids.GenerateTestNodeID()
	}

	var caminoAllocations []root_genesis.CaminoAllocation
	var avaxAllocations []root_genesis.Allocation
	var avaxInitialStakers []root_genesis.Staker
	var avaxInitialStakerFunds []ids.ShortID

	if lockModeBondDeposit {
		caminoAllocations = []root_genesis.CaminoAllocation{{
			ETHAddr:  initialAdmin,
			AVAXAddr: initialAdmin,
			XAmount:  10 * defaultMinValidatorStake,
			AddressStates: root_genesis.AddressStates{
				ConsortiumMember: true,
				KYCVerified:      true,
			},
			PlatformAllocations: []root_genesis.PlatformAllocation{{
				Amount:            minStake,
				NodeID:            nodeID,
				ValidatorDuration: defaultMinValidatorStake,
				DepositDuration:   5,
				TimestampOffset:   10,
				DepositOfferMemo:  depositOfferMemo,
			}},
		}}
	} else {
		avaxInitialStakerFunds = []ids.ShortID{initialAdmin}
		avaxInitialStakers = []root_genesis.Staker{{
			NodeID:        nodeID,
			RewardAddress: initialAdmin,
		}}
		avaxAllocations = []root_genesis.Allocation{{
			ETHAddr:       initialAdmin,
			AVAXAddr:      initialAdmin,
			InitialAmount: minStake,
			UnlockSchedule: []root_genesis.LockedAmount{{
				Amount:   minStake,
				Locktime: 0,
			}},
		}}
	}

	return &root_genesis.Config{
		Allocations:                avaxAllocations,
		StartTime:                  10,
		InitialStakeDuration:       minStakeDuration,
		InitialStakeDurationOffset: 10,
		InitialStakedFunds:         avaxInitialStakerFunds,
		InitialStakers:             avaxInitialStakers,
		Camino: root_genesis.Camino{
			VerifyNodeSignature: true,
			LockModeBondDeposit: lockModeBondDeposit,
			InitialAdmin:        initialAdmin,
			DepositOffers:       []root_genesis.DepositOffer{depositOffer},
			Allocations:         caminoAllocations,
		},
	}
}

func defaultGenesisState(addresses []pvm_genesis.AddressState, deposits []*txs.Tx, initialAdmin ids.ShortID) *pvm_genesis.State {
	return &pvm_genesis.State{
		UTXOs: []*avax.UTXO{{
			UTXOID: avax.UTXOID{
				TxID:        initialTxID,
				OutputIndex: 0,
			},
			Asset: avax.Asset{ID: initialTxID},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Schmeckle,
			},
		}},
		Chains:        []*txs.Tx{{}},
		Timestamp:     uint64(initialTime.Unix()),
		InitialSupply: units.Schmeckle + units.Avax,
		Camino: pvm_genesis.Camino{
			AddressStates: addresses,
			InitialAdmin:  initialAdmin,
			Blocks: []*pvm_genesis.Block{{
				Validators: []*txs.Tx{{Unsigned: &txs.CaminoAddValidatorTx{
					AddValidatorTx: txs.AddValidatorTx{
						BaseTx:       txs.BaseTx{},
						Validator:    txs.Validator{},
						RewardsOwner: &secp256k1fx.OutputOwners{},
					},
					NodeOwnerAuth: &secp256k1fx.Input{},
				}}},
				Deposits: deposits,
			}},
			DepositOffers: []*deposit.Offer{
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
					Memo:                    []byte("some memo"),
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

func TestGetBaseFee(t *testing.T) {
	tests := map[string]struct {
		caminoState         *caminoState
		expectedCaminoState *caminoState
		expectedBaseFee     uint64
		expectedErr         error
	}{
		"OK": {
			caminoState:         &caminoState{baseFee: 123},
			expectedCaminoState: &caminoState{baseFee: 123},
			expectedBaseFee:     123,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			baseFee, err := tt.caminoState.GetBaseFee()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedBaseFee, baseFee)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}

func TestSetBaseFee(t *testing.T) {
	tests := map[string]struct {
		baseFee             uint64
		caminoState         *caminoState
		expectedCaminoState *caminoState
	}{
		"OK": {
			baseFee:             123,
			caminoState:         &caminoState{baseFee: 111},
			expectedCaminoState: &caminoState{baseFee: 123},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.SetBaseFee(tt.baseFee)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}
