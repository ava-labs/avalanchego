// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx/fxmock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var defaultValidatorNodeID = ids.GenerateTestNodeID()

func newTestState(t testing.TB, db database.Database) *state {
	s, err := New(
		db,
		genesistest.NewBytes(t, genesistest.Config{
			NodeIDs: []ids.NodeID{defaultValidatorNodeID},
		}),
		prometheus.NewRegistry(),
		validators.NewManager(),
		upgradetest.GetConfig(upgradetest.Latest),
		&config.DefaultExecutionConfig,
		&snow.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    ids.GenerateTestNodeID(),
			Log:       logging.NoLog{},
		},
		metrics.Noop,
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .1 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaAvax,
		}),
	)
	require.NoError(t, err)
	require.IsType(t, (*state)(nil), s)
	return s.(*state)
}

func TestStateSyncGenesis(t *testing.T) {
	require := require.New(t)
	state := newTestState(t, memdb.New())

	staker, err := state.GetCurrentValidator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.NotNil(staker)
	require.Equal(defaultValidatorNodeID, staker.NodeID)

	delegatorIterator, err := state.GetCurrentDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)

	stakerIterator, err := state.GetCurrentStakerIterator()
	require.NoError(err)
	require.Equal(
		[]*Staker{staker},
		iterator.ToSlice(stakerIterator),
	)

	_, err = state.GetPendingValidator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.ErrorIs(err, database.ErrNotFound)

	delegatorIterator, err = state.GetPendingDelegatorIterator(constants.PrimaryNetworkID, defaultValidatorNodeID)
	require.NoError(err)
	require.Empty(
		iterator.ToSlice(delegatorIterator),
	)
}

// Whenever we store a staker, a whole bunch a data structures are updated
// This test is meant to capture which updates are carried out
func TestPersistStakers(t *testing.T) {
	tests := map[string]struct {
		// Insert or delete a staker to state and store it
		storeStaker func(*require.Assertions, ids.ID /*=subnetID*/, *state) *Staker

		// Check that the staker is duly stored/removed in P-chain state
		checkStakerInState func(*require.Assertions, *state, *Staker)

		// Check whether validators are duly reported in the validator set,
		// with the right weight and showing the BLS key
		checkValidatorsSet func(*require.Assertions, *state, *Staker)

		// Check that node duly track stakers uptimes
		checkValidatorUptimes func(*require.Assertions, *state, *Staker)

		// Check whether weight/bls keys diffs are duly stored
		checkDiffs func(*require.Assertions, *state, *Staker, uint64)
	}{
		"add current validator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				var (
					startTime = time.Now().Unix()
					endTime   = time.Now().Add(14 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(endTime),
						Wght:   1234,
					}
					validatorReward uint64 = 5678
				)

				utx := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utx}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				staker, err := NewCurrentStaker(
					addPermValTx.ID(),
					utx,
					time.Unix(startTime, 0),
					validatorReward,
				)
				r.NoError(err)

				r.NoError(s.PutCurrentValidator(staker))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())
				return staker
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				retrievedStaker, err := s.GetCurrentValidator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.Equal(staker, retrievedStaker)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.Contains(valsMap, staker.NodeID)
				r.Equal(
					&validators.GetValidatorOutput{
						NodeID:    staker.NodeID,
						PublicKey: staker.PublicKey,
						Weight:    staker.Weight,
					},
					valsMap[staker.NodeID],
				)
			},
			checkValidatorUptimes: func(r *require.Assertions, s *state, staker *Staker) {
				upDuration, lastUpdated, err := s.GetUptime(staker.NodeID)
				if staker.SubnetID != constants.PrimaryNetworkID {
					// only primary network validators have uptimes
					r.ErrorIs(err, database.ErrNotFound)
				} else {
					r.NoError(err)
					r.Equal(upDuration, time.Duration(0))
					r.Equal(lastUpdated, staker.StartTime)
				}
			},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				weightDiffBytes, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.NoError(err)
				weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
				r.NoError(err)
				r.Equal(&ValidatorWeightDiff{
					Decrease: false,
					Amount:   staker.Weight,
				}, weightDiff)

				blsDiffBytes, err := s.validatorPublicKeyDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				if staker.SubnetID == constants.PrimaryNetworkID {
					r.NoError(err)
					r.Nil(blsDiffBytes)
				} else {
					r.ErrorIs(err, database.ErrNotFound)
				}
			},
		},
		"add current delegator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				// insert the delegator and its validator
				var (
					valStartTime = time.Now().Truncate(time.Second).Unix()
					delStartTime = time.Unix(valStartTime, 0).Add(time.Hour).Unix()
					delEndTime   = time.Unix(delStartTime, 0).Add(30 * 24 * time.Hour).Unix()
					valEndTime   = time.Unix(valStartTime, 0).Add(365 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(valEndTime),
						Wght:   1234,
					}
					validatorReward uint64 = 5678

					delegatorData = txs.Validator{
						NodeID: validatorsData.NodeID,
						End:    uint64(delEndTime),
						Wght:   validatorsData.Wght / 2,
					}
					delegatorReward uint64 = 5432
				)

				utxVal := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utxVal}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				val, err := NewCurrentStaker(
					addPermValTx.ID(),
					utxVal,
					time.Unix(valStartTime, 0),
					validatorReward,
				)
				r.NoError(err)

				utxDel := createPermissionlessDelegatorTx(subnetID, delegatorData)
				addPermDelTx := &txs.Tx{Unsigned: utxDel}
				r.NoError(addPermDelTx.Initialize(txs.Codec))

				del, err := NewCurrentStaker(
					addPermDelTx.ID(),
					utxDel,
					time.Unix(delStartTime, 0),
					delegatorReward,
				)
				r.NoError(err)

				r.NoError(s.PutCurrentValidator(val))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.PutCurrentDelegator(del)
				s.AddTx(addPermDelTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())
				return del
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				delIt, err := s.GetCurrentDelegatorIterator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.True(delIt.Next())
				retrievedDelegator := delIt.Value()
				r.False(delIt.Next())
				delIt.Release()
				r.Equal(staker, retrievedDelegator)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				val, err := s.GetCurrentValidator(staker.SubnetID, staker.NodeID)
				r.NoError(err)

				valsMap := s.validators.GetMap(staker.SubnetID)
				r.Contains(valsMap, staker.NodeID)
				valOut := valsMap[staker.NodeID]
				r.Equal(valOut.NodeID, staker.NodeID)
				r.Equal(valOut.Weight, val.Weight+staker.Weight)
			},
			checkValidatorUptimes: func(*require.Assertions, *state, *Staker) {},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				// validator's weight must increase of delegator's weight amount
				weightDiffBytes, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.NoError(err)
				weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
				r.NoError(err)
				r.Equal(&ValidatorWeightDiff{
					Decrease: false,
					Amount:   staker.Weight,
				}, weightDiff)
			},
		},
		"add pending validator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				var (
					startTime = time.Now().Unix()
					endTime   = time.Now().Add(14 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(startTime),
						End:    uint64(endTime),
						Wght:   1234,
					}
				)

				utx := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utx}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				staker, err := NewPendingStaker(
					addPermValTx.ID(),
					utx,
				)
				r.NoError(err)

				r.NoError(s.PutPendingValidator(staker))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())
				return staker
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				retrievedStaker, err := s.GetPendingValidator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.Equal(staker, retrievedStaker)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				// pending validators are not showed in validators set
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.NotContains(valsMap, staker.NodeID)
			},
			checkValidatorUptimes: func(r *require.Assertions, s *state, staker *Staker) {
				// pending validators uptime is not tracked
				_, _, err := s.GetUptime(staker.NodeID)
				r.ErrorIs(err, database.ErrNotFound)
			},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				// pending validators weight diff and bls diffs are not stored
				_, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.ErrorIs(err, database.ErrNotFound)

				_, err = s.validatorPublicKeyDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.ErrorIs(err, database.ErrNotFound)
			},
		},
		"add pending delegator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				// insert the delegator and its validator
				var (
					valStartTime = time.Now().Truncate(time.Second).Unix()
					delStartTime = time.Unix(valStartTime, 0).Add(time.Hour).Unix()
					delEndTime   = time.Unix(delStartTime, 0).Add(30 * 24 * time.Hour).Unix()
					valEndTime   = time.Unix(valStartTime, 0).Add(365 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(valStartTime),
						End:    uint64(valEndTime),
						Wght:   1234,
					}

					delegatorData = txs.Validator{
						NodeID: validatorsData.NodeID,
						Start:  uint64(delStartTime),
						End:    uint64(delEndTime),
						Wght:   validatorsData.Wght / 2,
					}
				)

				utxVal := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utxVal}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				val, err := NewPendingStaker(addPermValTx.ID(), utxVal)
				r.NoError(err)

				utxDel := createPermissionlessDelegatorTx(subnetID, delegatorData)
				addPermDelTx := &txs.Tx{Unsigned: utxDel}
				r.NoError(addPermDelTx.Initialize(txs.Codec))

				del, err := NewPendingStaker(addPermDelTx.ID(), utxDel)
				r.NoError(err)

				r.NoError(s.PutPendingValidator(val))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.PutPendingDelegator(del)
				s.AddTx(addPermDelTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				return del
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				delIt, err := s.GetPendingDelegatorIterator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.True(delIt.Next())
				retrievedDelegator := delIt.Value()
				r.False(delIt.Next())
				delIt.Release()
				r.Equal(staker, retrievedDelegator)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.NotContains(valsMap, staker.NodeID)
			},
			checkValidatorUptimes: func(*require.Assertions, *state, *Staker) {},
			checkDiffs:            func(*require.Assertions, *state, *Staker, uint64) {},
		},
		"delete current validator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				// add them remove the validator
				var (
					startTime = time.Now().Unix()
					endTime   = time.Now().Add(14 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(endTime),
						Wght:   1234,
					}
					validatorReward uint64 = 5678
				)

				utx := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utx}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				staker, err := NewCurrentStaker(
					addPermValTx.ID(),
					utx,
					time.Unix(startTime, 0),
					validatorReward,
				)
				r.NoError(err)

				r.NoError(s.PutCurrentValidator(staker))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.DeleteCurrentValidator(staker)
				r.NoError(s.Commit())
				return staker
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				_, err := s.GetCurrentValidator(staker.SubnetID, staker.NodeID)
				r.ErrorIs(err, database.ErrNotFound)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				// deleted validators are not showed in the validators set anymore
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.NotContains(valsMap, staker.NodeID)
			},
			checkValidatorUptimes: func(r *require.Assertions, s *state, staker *Staker) {
				// uptimes of delete validators are dropped
				_, _, err := s.GetUptime(staker.NodeID)
				r.ErrorIs(err, database.ErrNotFound)
			},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				weightDiffBytes, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.NoError(err)
				weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
				r.NoError(err)
				r.Equal(&ValidatorWeightDiff{
					Decrease: true,
					Amount:   staker.Weight,
				}, weightDiff)

				blsDiffBytes, err := s.validatorPublicKeyDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				if staker.SubnetID == constants.PrimaryNetworkID {
					r.NoError(err)
					r.Equal(bls.PublicKeyFromValidUncompressedBytes(blsDiffBytes), staker.PublicKey)
				} else {
					r.ErrorIs(err, database.ErrNotFound)
				}
			},
		},
		"delete current delegator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				// insert validator and delegator, then remove the delegator
				var (
					valStartTime = time.Now().Truncate(time.Second).Unix()
					delStartTime = time.Unix(valStartTime, 0).Add(time.Hour).Unix()
					delEndTime   = time.Unix(delStartTime, 0).Add(30 * 24 * time.Hour).Unix()
					valEndTime   = time.Unix(valStartTime, 0).Add(365 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						End:    uint64(valEndTime),
						Wght:   1234,
					}
					validatorReward uint64 = 5678

					delegatorData = txs.Validator{
						NodeID: validatorsData.NodeID,
						End:    uint64(delEndTime),
						Wght:   validatorsData.Wght / 2,
					}
					delegatorReward uint64 = 5432
				)

				utxVal := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utxVal}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				val, err := NewCurrentStaker(
					addPermValTx.ID(),
					utxVal,
					time.Unix(valStartTime, 0),
					validatorReward,
				)
				r.NoError(err)

				utxDel := createPermissionlessDelegatorTx(subnetID, delegatorData)
				addPermDelTx := &txs.Tx{Unsigned: utxDel}
				r.NoError(addPermDelTx.Initialize(txs.Codec))

				del, err := NewCurrentStaker(
					addPermDelTx.ID(),
					utxDel,
					time.Unix(delStartTime, 0),
					delegatorReward,
				)
				r.NoError(err)

				r.NoError(s.PutCurrentValidator(val))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker

				s.PutCurrentDelegator(del)
				s.AddTx(addPermDelTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.DeleteCurrentDelegator(del)
				r.NoError(s.Commit())

				return del
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				delIt, err := s.GetCurrentDelegatorIterator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.False(delIt.Next())
				delIt.Release()
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				val, err := s.GetCurrentValidator(staker.SubnetID, staker.NodeID)
				r.NoError(err)

				valsMap := s.validators.GetMap(staker.SubnetID)
				r.Contains(valsMap, staker.NodeID)
				valOut := valsMap[staker.NodeID]
				r.Equal(valOut.NodeID, staker.NodeID)
				r.Equal(valOut.Weight, val.Weight)
			},
			checkValidatorUptimes: func(*require.Assertions, *state, *Staker) {},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				// validator's weight must decrease of delegator's weight amount
				weightDiffBytes, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.NoError(err)
				weightDiff, err := unmarshalWeightDiff(weightDiffBytes)
				r.NoError(err)
				r.Equal(&ValidatorWeightDiff{
					Decrease: true,
					Amount:   staker.Weight,
				}, weightDiff)
			},
		},
		"delete pending validator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				var (
					startTime = time.Now().Unix()
					endTime   = time.Now().Add(14 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(startTime),
						End:    uint64(endTime),
						Wght:   1234,
					}
				)

				utx := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utx}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				staker, err := NewPendingStaker(
					addPermValTx.ID(),
					utx,
				)
				r.NoError(err)

				r.NoError(s.PutPendingValidator(staker))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.DeletePendingValidator(staker)
				r.NoError(s.Commit())

				return staker
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				_, err := s.GetPendingValidator(staker.SubnetID, staker.NodeID)
				r.ErrorIs(err, database.ErrNotFound)
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.NotContains(valsMap, staker.NodeID)
			},
			checkValidatorUptimes: func(r *require.Assertions, s *state, staker *Staker) {
				_, _, err := s.GetUptime(staker.NodeID)
				r.ErrorIs(err, database.ErrNotFound)
			},
			checkDiffs: func(r *require.Assertions, s *state, staker *Staker, height uint64) {
				_, err := s.validatorWeightDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.ErrorIs(err, database.ErrNotFound)

				_, err = s.validatorPublicKeyDiffsDB.Get(marshalDiffKey(staker.SubnetID, height, staker.NodeID))
				r.ErrorIs(err, database.ErrNotFound)
			},
		},
		"delete pending delegator": {
			storeStaker: func(r *require.Assertions, subnetID ids.ID, s *state) *Staker {
				// insert validator and delegator the remove the validator
				var (
					valStartTime = time.Now().Truncate(time.Second).Unix()
					delStartTime = time.Unix(valStartTime, 0).Add(time.Hour).Unix()
					delEndTime   = time.Unix(delStartTime, 0).Add(30 * 24 * time.Hour).Unix()
					valEndTime   = time.Unix(valStartTime, 0).Add(365 * 24 * time.Hour).Unix()

					validatorsData = txs.Validator{
						NodeID: ids.GenerateTestNodeID(),
						Start:  uint64(valStartTime),
						End:    uint64(valEndTime),
						Wght:   1234,
					}

					delegatorData = txs.Validator{
						NodeID: validatorsData.NodeID,
						Start:  uint64(delStartTime),
						End:    uint64(delEndTime),
						Wght:   validatorsData.Wght / 2,
					}
				)

				utxVal := createPermissionlessValidatorTx(r, subnetID, validatorsData)
				addPermValTx := &txs.Tx{Unsigned: utxVal}
				r.NoError(addPermValTx.Initialize(txs.Codec))

				val, err := NewPendingStaker(addPermValTx.ID(), utxVal)
				r.NoError(err)

				utxDel := createPermissionlessDelegatorTx(subnetID, delegatorData)
				addPermDelTx := &txs.Tx{Unsigned: utxDel}
				r.NoError(addPermDelTx.Initialize(txs.Codec))

				del, err := NewPendingStaker(addPermDelTx.ID(), utxDel)
				r.NoError(err)

				r.NoError(s.PutPendingValidator(val))
				s.AddTx(addPermValTx, status.Committed) // this is currently needed to reload the staker

				s.PutPendingDelegator(del)
				s.AddTx(addPermDelTx, status.Committed) // this is currently needed to reload the staker
				r.NoError(s.Commit())

				s.DeletePendingDelegator(del)
				r.NoError(s.Commit())
				return del
			},
			checkStakerInState: func(r *require.Assertions, s *state, staker *Staker) {
				delIt, err := s.GetPendingDelegatorIterator(staker.SubnetID, staker.NodeID)
				r.NoError(err)
				r.False(delIt.Next())
				delIt.Release()
			},
			checkValidatorsSet: func(r *require.Assertions, s *state, staker *Staker) {
				valsMap := s.validators.GetMap(staker.SubnetID)
				r.NotContains(valsMap, staker.NodeID)
			},
			checkValidatorUptimes: func(*require.Assertions, *state, *Staker) {},
			checkDiffs:            func(*require.Assertions, *state, *Staker, uint64) {},
		},
	}

	subnetIDs := []ids.ID{constants.PrimaryNetworkID, ids.GenerateTestID()}
	for _, subnetID := range subnetIDs {
		for name, test := range tests {
			t.Run(fmt.Sprintf("%s - subnetID %s", name, subnetID), func(t *testing.T) {
				require := require.New(t)

				db := memdb.New()
				state := newTestState(t, db)

				// create and store the staker
				staker := test.storeStaker(require, subnetID, state)

				// check all relevant data are stored
				test.checkStakerInState(require, state, staker)
				test.checkValidatorsSet(require, state, staker)
				test.checkValidatorUptimes(require, state, staker)
				test.checkDiffs(require, state, staker, 0 /*height*/)

				// rebuild the state
				rebuiltState := newTestState(t, db)

				// check again that all relevant data are still available in rebuilt state
				test.checkStakerInState(require, rebuiltState, staker)
				test.checkValidatorsSet(require, rebuiltState, staker)
				test.checkValidatorUptimes(require, rebuiltState, staker)
				test.checkDiffs(require, rebuiltState, staker, 0 /*height*/)
			})
		}
	}
}

func createPermissionlessValidatorTx(r *require.Assertions, subnetID ids.ID, validatorsData txs.Validator) *txs.AddPermissionlessValidatorTx {
	var sig signer.Signer = &signer.Empty{}
	if subnetID == constants.PrimaryNetworkID {
		sk, err := bls.NewSecretKey()
		r.NoError(err)
		sig = signer.NewProofOfPossession(sk)
	}

	return &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        ids.GenerateTestID(),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Validator: validatorsData,
		Subnet:    subnetID,
		Signer:    sig,

		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
		ValidatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		DelegatorRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
		DelegationShares: reward.PercentDenominator,
	}
}

func createPermissionlessDelegatorTx(subnetID ids.ID, delegatorData txs.Validator) *txs.AddPermissionlessDelegatorTx {
	return &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{
			BaseTx: avax.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: constants.PlatformChainID,
				Outs:         []*avax.TransferableOutput{},
				Ins: []*avax.TransferableInput{
					{
						UTXOID: avax.UTXOID{
							TxID:        ids.GenerateTestID(),
							OutputIndex: 1,
						},
						Asset: avax.Asset{
							ID: ids.GenerateTestID(),
						},
						In: &secp256k1fx.TransferInput{
							Amt: 2 * units.KiloAvax,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{1},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Validator: delegatorData,
		Subnet:    subnetID,

		StakeOuts: []*avax.TransferableOutput{
			{
				Asset: avax.Asset{
					ID: ids.GenerateTestID(),
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: 2 * units.KiloAvax,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  0,
						Threshold: 1,
						Addrs: []ids.ShortID{
							ids.GenerateTestShortID(),
						},
					},
				},
			},
		},
		DelegationRewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs: []ids.ShortID{
				ids.GenerateTestShortID(),
			},
		},
	}
}

func TestValidatorWeightDiff(t *testing.T) {
	type test struct {
		name        string
		ops         []func(*ValidatorWeightDiff) error
		expected    *ValidatorWeightDiff
		expectedErr error
	}

	tests := []test{
		{
			name:        "no ops",
			ops:         []func(*ValidatorWeightDiff) error{},
			expected:    &ValidatorWeightDiff{},
			expectedErr: nil,
		},
		{
			name: "simple decrease",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
			expectedErr: nil,
		},
		{
			name: "decrease overflow",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, math.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "simple increase",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: false,
				Amount:   2,
			},
			expectedErr: nil,
		},
		{
			name: "increase overflow",
			ops: []func(*ValidatorWeightDiff) error{
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, math.MaxUint64)
				},
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1)
				},
			},
			expected:    &ValidatorWeightDiff{},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "varied use",
			ops: []func(*ValidatorWeightDiff) error{
				// Add to 0
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 2) // Value 2
				},
				// Subtract from positive number
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 1) // Value 1
				},
				// Subtract from positive number
				// to make it negative
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 3) // Value -2
				},
				// Subtract from a negative number
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 3) // Value -5
				},
				// Add to a negative number
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1) // Value -4
				},
				// Add to a negative number
				// to make it positive
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 5) // Value 1
				},
				// Add to a positive number
				func(d *ValidatorWeightDiff) error {
					return d.Add(false, 1) // Value 2
				},
				// Get to zero
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 2) // Value 0
				},
				// Subtract from zero
				func(d *ValidatorWeightDiff) error {
					return d.Add(true, 2) // Value -2
				},
			},
			expected: &ValidatorWeightDiff{
				Decrease: true,
				Amount:   2,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			diff := &ValidatorWeightDiff{}
			errs := wrappers.Errs{}
			for _, op := range tt.ops {
				errs.Add(op(diff))
			}
			require.ErrorIs(errs.Err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, diff)
		})
	}
}

// Tests PutCurrentValidator, DeleteCurrentValidator, GetCurrentValidator,
// ApplyValidatorWeightDiffs, ApplyValidatorPublicKeyDiffs
func TestStateAddRemoveValidator(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())

	var (
		numNodes  = 3
		subnetID  = ids.GenerateTestID()
		startTime = time.Now()
		endTime   = startTime.Add(24 * time.Hour)
		stakers   = make([]Staker, numNodes)
	)
	for i := 0; i < numNodes; i++ {
		stakers[i] = Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			Weight:          uint64(i + 1),
			StartTime:       startTime.Add(time.Duration(i) * time.Second),
			EndTime:         endTime.Add(time.Duration(i) * time.Second),
			PotentialReward: uint64(i + 1),
		}
		if i%2 == 0 {
			stakers[i].SubnetID = subnetID
		} else {
			sk, err := bls.NewSecretKey()
			require.NoError(err)
			stakers[i].PublicKey = bls.PublicFromSecretKey(sk)
			stakers[i].SubnetID = constants.PrimaryNetworkID
		}
	}

	type diff struct {
		addedValidators   []Staker
		addedDelegators   []Staker
		removedDelegators []Staker
		removedValidators []Staker

		expectedPrimaryValidatorSet map[ids.NodeID]*validators.GetValidatorOutput
		expectedSubnetValidatorSet  map[ids.NodeID]*validators.GetValidatorOutput
	}
	diffs := []diff{
		{
			// Do nothing
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add a subnet validator
			addedValidators:             []Staker{stakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[0].NodeID: {
					NodeID: stakers[0].NodeID,
					Weight: stakers[0].Weight,
				},
			},
		},
		{
			// Remove a subnet validator
			removedValidators:           []Staker{stakers[0]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{ // Add a primary network validator
			addedValidators: []Staker{stakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Do nothing
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{ // Remove a primary network validator
			removedValidators:           []Staker{stakers[1]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
		{
			// Add 2 subnet validators and a primary network validator
			addedValidators: []Staker{stakers[0], stakers[1], stakers[2]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[1].NodeID: {
					NodeID:    stakers[1].NodeID,
					PublicKey: stakers[1].PublicKey,
					Weight:    stakers[1].Weight,
				},
			},
			expectedSubnetValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{
				stakers[0].NodeID: {
					NodeID: stakers[0].NodeID,
					Weight: stakers[0].Weight,
				},
				stakers[2].NodeID: {
					NodeID: stakers[2].NodeID,
					Weight: stakers[2].Weight,
				},
			},
		},
		{
			// Remove 2 subnet validators and a primary network validator.
			removedValidators:           []Staker{stakers[0], stakers[1], stakers[2]},
			expectedPrimaryValidatorSet: map[ids.NodeID]*validators.GetValidatorOutput{},
			expectedSubnetValidatorSet:  map[ids.NodeID]*validators.GetValidatorOutput{},
		},
	}
	for currentIndex, diff := range diffs {
		for _, added := range diff.addedValidators {
			added := added
			require.NoError(state.PutCurrentValidator(&added))
		}
		for _, added := range diff.addedDelegators {
			added := added
			state.PutCurrentDelegator(&added)
		}
		for _, removed := range diff.removedDelegators {
			removed := removed
			state.DeleteCurrentDelegator(&removed)
		}
		for _, removed := range diff.removedValidators {
			removed := removed
			state.DeleteCurrentValidator(&removed)
		}

		currentHeight := uint64(currentIndex + 1)
		state.SetHeight(currentHeight)

		require.NoError(state.Commit())

		for _, added := range diff.addedValidators {
			gotValidator, err := state.GetCurrentValidator(added.SubnetID, added.NodeID)
			require.NoError(err)
			require.Equal(added, *gotValidator)
		}

		for _, removed := range diff.removedValidators {
			_, err := state.GetCurrentValidator(removed.SubnetID, removed.NodeID)
			require.ErrorIs(err, database.ErrNotFound)
		}

		for i := 0; i < currentIndex; i++ {
			prevDiff := diffs[i]
			prevHeight := uint64(i + 1)

			primaryValidatorSet := copyValidatorSet(diff.expectedPrimaryValidatorSet)
			require.NoError(state.ApplyValidatorWeightDiffs(
				context.Background(),
				primaryValidatorSet,
				currentHeight,
				prevHeight+1,
				constants.PrimaryNetworkID,
			))
			requireEqualWeightsValidatorSet(require, prevDiff.expectedPrimaryValidatorSet, primaryValidatorSet)

			require.NoError(state.ApplyValidatorPublicKeyDiffs(
				context.Background(),
				primaryValidatorSet,
				currentHeight,
				prevHeight+1,
			))
			requireEqualPublicKeysValidatorSet(require, prevDiff.expectedPrimaryValidatorSet, primaryValidatorSet)

			subnetValidatorSet := copyValidatorSet(diff.expectedSubnetValidatorSet)
			require.NoError(state.ApplyValidatorWeightDiffs(
				context.Background(),
				subnetValidatorSet,
				currentHeight,
				prevHeight+1,
				subnetID,
			))
			requireEqualWeightsValidatorSet(require, prevDiff.expectedSubnetValidatorSet, subnetValidatorSet)
		}
	}
}

func copyValidatorSet(
	input map[ids.NodeID]*validators.GetValidatorOutput,
) map[ids.NodeID]*validators.GetValidatorOutput {
	result := make(map[ids.NodeID]*validators.GetValidatorOutput, len(input))
	for nodeID, vdr := range input {
		vdrCopy := *vdr
		result[nodeID] = &vdrCopy
	}
	return result
}

func requireEqualWeightsValidatorSet(
	require *require.Assertions,
	expected map[ids.NodeID]*validators.GetValidatorOutput,
	actual map[ids.NodeID]*validators.GetValidatorOutput,
) {
	require.Len(actual, len(expected))
	for nodeID, expectedVdr := range expected {
		require.Contains(actual, nodeID)

		actualVdr := actual[nodeID]
		require.Equal(expectedVdr.NodeID, actualVdr.NodeID)
		require.Equal(expectedVdr.Weight, actualVdr.Weight)
	}
}

func requireEqualPublicKeysValidatorSet(
	require *require.Assertions,
	expected map[ids.NodeID]*validators.GetValidatorOutput,
	actual map[ids.NodeID]*validators.GetValidatorOutput,
) {
	require.Len(actual, len(expected))
	for nodeID, expectedVdr := range expected {
		require.Contains(actual, nodeID)

		actualVdr := actual[nodeID]
		require.Equal(expectedVdr.NodeID, actualVdr.NodeID)
		require.Equal(expectedVdr.PublicKey, actualVdr.PublicKey)
	}
}

func TestParsedStateBlock(t *testing.T) {
	var (
		require = require.New(t)
		blks    = makeBlocks(require)
	)

	for _, blk := range blks {
		stBlk := stateBlk{
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}

		stBlkBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &stBlk)
		require.NoError(err)

		gotBlk, isStateBlk, err := parseStoredBlock(stBlkBytes)
		require.NoError(err)
		require.True(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())

		gotBlk, isStateBlk, err = parseStoredBlock(blk.Bytes())
		require.NoError(err)
		require.False(isStateBlk)
		require.Equal(blk.ID(), gotBlk.ID())
	}
}

func TestReindexBlocks(t *testing.T) {
	var (
		require = require.New(t)
		s       = newTestState(t, memdb.New())
		blks    = makeBlocks(require)
	)

	// Populate the blocks using the legacy format.
	for _, blk := range blks {
		stBlk := stateBlk{
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}
		stBlkBytes, err := block.GenesisCodec.Marshal(block.CodecVersion, &stBlk)
		require.NoError(err)

		blkID := blk.ID()
		require.NoError(s.blockDB.Put(blkID[:], stBlkBytes))
	}

	// Convert the indices to the new format.
	require.NoError(s.ReindexBlocks(&sync.Mutex{}, logging.NoLog{}))

	// Verify that the blocks are stored in the new format.
	for _, blk := range blks {
		blkID := blk.ID()
		blkBytes, err := s.blockDB.Get(blkID[:])
		require.NoError(err)

		parsedBlk, err := block.Parse(block.GenesisCodec, blkBytes)
		require.NoError(err)
		require.Equal(blkID, parsedBlk.ID())
	}

	// Verify that the flag has been written to disk to allow skipping future
	// reindexings.
	reindexed, err := s.singletonDB.Has(BlocksReindexedKey)
	require.NoError(err)
	require.True(reindexed)
}

func TestStateSubnetOwner(t *testing.T) {
	require := require.New(t)

	state := newTestState(t, memdb.New())
	ctrl := gomock.NewController(t)

	var (
		owner1 = fxmock.NewOwner(ctrl)
		owner2 = fxmock.NewOwner(ctrl)

		createSubnetTx = &txs.Tx{
			Unsigned: &txs.CreateSubnetTx{
				BaseTx: txs.BaseTx{},
				Owner:  owner1,
			},
		}

		subnetID = createSubnetTx.ID()
	)

	owner, err := state.GetSubnetOwner(subnetID)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(owner)

	state.AddSubnet(subnetID)
	state.SetSubnetOwner(subnetID, owner1)

	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner1, owner)

	state.SetSubnetOwner(subnetID, owner2)
	owner, err = state.GetSubnetOwner(subnetID)
	require.NoError(err)
	require.Equal(owner2, owner)
}

func TestStateSubnetManager(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s State, subnetID ids.ID, chainID ids.ID, addr []byte)
	}{
		{
			name: "in-memory",
			setup: func(_ *testing.T, s State, subnetID ids.ID, chainID ids.ID, addr []byte) {
				s.SetSubnetManager(subnetID, chainID, addr)
			},
		},
		{
			name: "cache",
			setup: func(t *testing.T, s State, subnetID ids.ID, chainID ids.ID, addr []byte) {
				subnetManagerCache := s.(*state).subnetManagerCache

				require.Zero(t, subnetManagerCache.Len())
				subnetManagerCache.Put(subnetID, chainIDAndAddr{
					ChainID: chainID,
					Addr:    addr,
				})
				require.Equal(t, 1, subnetManagerCache.Len())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			initializedState := newTestState(t, memdb.New())

			subnetID := ids.GenerateTestID()
			chainID, addr, err := initializedState.GetSubnetManager(subnetID)
			require.ErrorIs(err, database.ErrNotFound)
			require.Equal(ids.Empty, chainID)
			require.Nil(addr)

			expectedChainID := ids.GenerateTestID()
			expectedAddr := []byte{'a', 'd', 'd', 'r'}

			test.setup(t, initializedState, subnetID, expectedChainID, expectedAddr)

			chainID, addr, err = initializedState.GetSubnetManager(subnetID)
			require.NoError(err)
			require.Equal(expectedChainID, chainID)
			require.Equal(expectedAddr, addr)
		})
	}
}

func makeBlocks(require *require.Assertions) []block.Block {
	var blks []block.Block
	{
		blk, err := block.NewApricotAbortBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewApricotAtomicBlock(ids.GenerateTestID(), 1000, &txs.Tx{
			Unsigned: &txs.AdvanceTimeTx{
				Time: 1000,
			},
		})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewApricotCommitBlock(ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))
		blk, err := block.NewApricotProposalBlock(ids.GenerateTestID(), 1000, tx)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))
		blk, err := block.NewApricotStandardBlock(ids.GenerateTestID(), 1000, []*txs.Tx{tx})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewBanffAbortBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		blk, err := block.NewBanffCommitBlock(time.Now(), ids.GenerateTestID(), 1000)
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))

		blk, err := block.NewBanffProposalBlock(time.Now(), ids.GenerateTestID(), 1000, tx, []*txs.Tx{})
		require.NoError(err)
		blks = append(blks, blk)
	}

	{
		tx := &txs.Tx{
			Unsigned: &txs.RewardValidatorTx{
				TxID: ids.GenerateTestID(),
			},
		}
		require.NoError(tx.Initialize(txs.Codec))

		blk, err := block.NewBanffStandardBlock(time.Now(), ids.GenerateTestID(), 1000, []*txs.Tx{tx})
		require.NoError(err)
		blks = append(blks, blk)
	}
	return blks
}

// Verify that committing the state writes the fee state to the database and
// that loading the state fetches the fee state from the database.
func TestStateFeeStateCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	expectedFeeState := gas.State{
		Capacity: 1,
		Excess:   2,
	}
	s.SetFeeState(expectedFeeState)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedFeeState, s.GetFeeState())
}

// Verify that committing the state writes the accrued fees to the database and
// that loading the state fetches the accrued fees from the database.
func TestStateAccruedFeesCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	expectedAccruedFees := uint64(1)
	s.SetAccruedFees(expectedAccruedFees)
	require.NoError(s.Commit())

	s = newTestState(t, db)
	require.Equal(expectedAccruedFees, s.GetAccruedFees())
}

func TestMarkAndIsInitialized(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	defaultIsInitialized, err := isInitialized(db)
	require.NoError(err)
	require.False(defaultIsInitialized)

	require.NoError(markInitialized(db))

	isInitializedAfterMarking, err := isInitialized(db)
	require.NoError(err)
	require.True(isInitializedAfterMarking)
}

// Verify that reading from the database returns the same value that was written
// to it.
func TestPutAndGetFeeState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	defaultFeeState, err := getFeeState(db)
	require.NoError(err)
	require.Equal(gas.State{}, defaultFeeState)

	//nolint:gosec // This does not require a secure random number generator
	expectedFeeState := gas.State{
		Capacity: gas.Gas(rand.Uint64()),
		Excess:   gas.Gas(rand.Uint64()),
	}
	require.NoError(putFeeState(db, expectedFeeState))

	actualFeeState, err := getFeeState(db)
	require.NoError(err)
	require.Equal(expectedFeeState, actualFeeState)
}

func TestGetFeeStateErrors(t *testing.T) {
	tests := []struct {
		value       []byte
		expectedErr error
	}{
		{
			value: []byte{
				// truncated codec version
				0x00,
			},
			expectedErr: codec.ErrCantUnpackVersion,
		},
		{
			value: []byte{
				// codec version
				0x00, 0x00,
				// truncated capacity
				0x12, 0x34, 0x56, 0x78,
			},
			expectedErr: wrappers.ErrInsufficientLength,
		},
		{
			value: []byte{
				// codec version
				0x00, 0x00,
				// capacity
				0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78,
				// excess
				0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78,
				// extra bytes
				0x00,
			},
			expectedErr: codec.ErrExtraSpace,
		},
	}
	for _, test := range tests {
		t.Run(test.expectedErr.Error(), func(t *testing.T) {
			var (
				require = require.New(t)
				db      = memdb.New()
			)
			require.NoError(db.Put(FeeStateKey, test.value))

			actualState, err := getFeeState(db)
			require.Equal(gas.State{}, actualState)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}

// Verify that committing the state writes the expiry changes to the database
// and that loading the state fetches the expiry from the database.
func TestStateExpiryCommitAndLoad(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	s := newTestState(t, db)

	// Populate an entry.
	expiry := ExpiryEntry{
		Timestamp: 1,
	}
	s.PutExpiry(expiry)
	require.NoError(s.Commit())

	// Verify that the entry was written and loaded correctly.
	s = newTestState(t, db)
	has, err := s.HasExpiry(expiry)
	require.NoError(err)
	require.True(has)

	// Delete an entry.
	s.DeleteExpiry(expiry)
	require.NoError(s.Commit())

	// Verify that the entry was deleted correctly.
	s = newTestState(t, db)
	has, err = s.HasExpiry(expiry)
	require.NoError(err)
	require.False(has)
}
