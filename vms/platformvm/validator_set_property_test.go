// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	walletcommon "github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	startPrimaryWithBLS uint8 = iota
	startSubnetValidator

	failedValidatorSnapshotString = "could not take validators snapshot: "
	failedBuildingEventSeqString  = "failed building events sequence: "
)

var errEmptyEventsList = errors.New("empty events list")

// for a given (permissioned) subnet, the test stakes and restakes multiple
// times a node as a primary and subnet validator. The BLS key of the node is
// changed across staking periods, and it can even be nil. We test that
// GetValidatorSet returns the correct primary and subnet validators data, with
// the right BLS key version at all relevant heights.
func TestGetValidatorsSetProperty(t *testing.T) {
	properties := gopter.NewProperties(nil)

	// to reproduce a given scenario do something like this:
	// parameters := gopter.DefaultTestParametersWithSeed(1685887576153675816)
	// properties := gopter.NewProperties(parameters)

	properties.Property("check GetValidatorSet", prop.ForAll(
		func(events []uint8) string {
			vm, subnetID, err := buildVM(t)
			if err != nil {
				return "failed building vm: " + err.Error()
			}
			vm.ctx.Lock.Lock()
			defer func() {
				_ = vm.Shutdown(t.Context())
				vm.ctx.Lock.Unlock()
			}()
			nodeID := ids.GenerateTestNodeID()

			currentTime := genesistest.DefaultValidatorStartTime
			vm.clock.Set(currentTime)
			vm.state.SetTimestamp(currentTime)

			// build a valid sequence of validators start/end times, given the
			// random events sequence received as test input
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return "failed building events sequence: " + err.Error()
			}

			validatorSetByHeightAndSubnet := make(map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
			if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
				return failedValidatorSnapshotString + err.Error()
			}

			// insert validator sequence
			var (
				currentPrimaryValidator = (*state.Staker)(nil)
				currentSubnetValidator  = (*state.Staker)(nil)
			)
			for _, ev := range validatorsTimes {
				// at each step we remove at least a subnet validator
				if currentSubnetValidator != nil {
					err := terminateSubnetValidator(vm, currentSubnetValidator)
					if err != nil {
						return "could not terminate current subnet validator: " + err.Error()
					}
					currentSubnetValidator = nil

					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}
				}

				switch ev.eventType {
				case startSubnetValidator:
					currentSubnetValidator = addSubnetValidator(t, vm, ev, subnetID)
					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}

				case startPrimaryWithBLS:
					// when adding a primary validator, also remove the current
					// primary one
					if currentPrimaryValidator != nil {
						err := terminatePrimaryValidator(vm, currentPrimaryValidator)
						if err != nil {
							return "could not terminate current primary validator: " + err.Error()
						}
						// no need to nil current primary validator, we'll
						// reassign immediately

						if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
							return failedValidatorSnapshotString + err.Error()
						}
					}
					currentPrimaryValidator = addPrimaryValidatorWithBLSKey(t, vm, ev)
					if err := takeValidatorsSnapshotAtCurrentHeight(vm, validatorSetByHeightAndSubnet); err != nil {
						return failedValidatorSnapshotString + err.Error()
					}

				default:
					return fmt.Sprintf("unexpected staker type: %v", ev.eventType)
				}
			}

			// Checks: let's look back at validator sets at previous heights and
			// make sure they match the snapshots already taken
			snapshotHeights := maps.Keys(validatorSetByHeightAndSubnet)
			slices.Sort(snapshotHeights)
			for idx, snapShotHeight := range snapshotHeights {
				lastAcceptedHeight, err := vm.GetCurrentHeight(t.Context())
				if err != nil {
					return err.Error()
				}

				nextSnapShotHeight := lastAcceptedHeight + 1
				if idx != len(snapshotHeights)-1 {
					nextSnapShotHeight = snapshotHeights[idx+1]
				}

				// within [snapShotHeight] and [nextSnapShotHeight], the validator set
				// does not change and must be equal to snapshot at [snapShotHeight]
				for height := snapShotHeight; height < nextSnapShotHeight; height++ {
					for subnetID, validatorsSet := range validatorSetByHeightAndSubnet[snapShotHeight] {
						res, err := vm.GetValidatorSet(t.Context(), height, subnetID)
						if err != nil {
							return fmt.Sprintf("failed GetValidatorSet at height %v: %v", height, err)
						}
						if !reflect.DeepEqual(validatorsSet, res) {
							return "failed validators set comparison"
						}
					}
				}
			}

			return ""
		},
		gen.SliceOfN(
			10,
			gen.OneConstOf(
				startPrimaryWithBLS,
				startSubnetValidator,
			),
		).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.TestingRun(t)
}

func takeValidatorsSnapshotAtCurrentHeight(vm *VM, validatorsSetByHeightAndSubnet map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput) error {
	if validatorsSetByHeightAndSubnet == nil {
		validatorsSetByHeightAndSubnet = make(map[uint64]map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
	}

	lastBlkID := vm.state.GetLastAccepted()
	lastBlk, err := vm.state.GetStatelessBlock(lastBlkID)
	if err != nil {
		return err
	}
	height := lastBlk.Height()
	validatorsSetBySubnet, ok := validatorsSetByHeightAndSubnet[height]
	if !ok {
		validatorsSetByHeightAndSubnet[height] = make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
		validatorsSetBySubnet = validatorsSetByHeightAndSubnet[height]
	}

	stakerIt, err := vm.state.GetCurrentStakerIterator()
	if err != nil {
		return err
	}
	defer stakerIt.Release()
	for stakerIt.Next() {
		v := stakerIt.Value()
		validatorsSet, ok := validatorsSetBySubnet[v.SubnetID]
		if !ok {
			validatorsSetBySubnet[v.SubnetID] = make(map[ids.NodeID]*validators.GetValidatorOutput)
			validatorsSet = validatorsSetBySubnet[v.SubnetID]
		}

		blsKey := v.PublicKey
		if v.SubnetID != constants.PrimaryNetworkID {
			// pick bls key from primary validator
			s, err := vm.state.GetCurrentValidator(constants.PlatformChainID, v.NodeID)
			if err != nil {
				return err
			}
			blsKey = s.PublicKey
		}

		var blsKeyBytes []byte
		if blsKey != nil {
			blsKeyBytes = bls.PublicKeyToUncompressedBytes(blsKey)
		}

		validatorsSet[v.NodeID] = &validators.GetValidatorOutput{
			NodeID:         v.NodeID,
			PublicKey:      blsKey,
			PublicKeyBytes: blsKeyBytes,
			Weight:         v.Weight,
		}
	}
	return nil
}

func addSubnetValidator(t testing.TB, vm *VM, data *validatorInputData, subnetID ids.ID) *state.Staker {
	require := require.New(t)

	wallet := newWallet(t, vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})
	tx, err := wallet.IssueAddSubnetValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: data.nodeID,
				Start:  uint64(data.startTime.Unix()),
				End:    uint64(data.endTime.Unix()),
				Wght:   vm.Internal.MinValidatorStake,
			},
			Subnet: subnetID,
		},
	)
	require.NoError(err)

	staker, err := internalAddValidator(vm, tx)
	require.NoError(err)
	return staker
}

func addPrimaryValidatorWithBLSKey(t testing.TB, vm *VM, data *validatorInputData) *state.Staker {
	require := require.New(t)

	wallet := newWallet(t, vm, walletConfig{})

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: data.nodeID,
				Start:  uint64(data.startTime.Unix()),
				End:    uint64(data.endTime.Unix()),
				Wght:   vm.Internal.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		reward.PercentDenominator,
	)
	require.NoError(err)

	staker, err := internalAddValidator(vm, tx)
	require.NoError(err)
	return staker
}

func internalAddValidator(vm *VM, signedTx *txs.Tx) (*state.Staker, error) {
	vm.ctx.Lock.Unlock()
	err := vm.issueTxFromRPC(signedTx)
	vm.ctx.Lock.Lock()

	if err != nil {
		return nil, fmt.Errorf("could not add tx to mempool: %w", err)
	}

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return nil, fmt.Errorf("failed verifying block: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return nil, fmt.Errorf("failed accepting block: %w", err)
	}
	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return nil, fmt.Errorf("failed setting preference: %w", err)
	}

	stakerTx := signedTx.Unsigned.(txs.Staker)
	return vm.state.GetCurrentValidator(stakerTx.SubnetID(), stakerTx.NodeID())
}

func terminateSubnetValidator(vm *VM, validator *state.Staker) error {
	currentTime := validator.EndTime
	vm.clock.Set(currentTime)
	vm.state.SetTimestamp(currentTime)

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying block: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting block: %w", err)
	}
	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return fmt.Errorf("failed setting preference: %w", err)
	}

	return nil
}

func terminatePrimaryValidator(vm *VM, validator *state.Staker) error {
	currentTime := validator.EndTime
	vm.clock.Set(currentTime)
	vm.state.SetTimestamp(currentTime)

	blk, err := vm.Builder.BuildBlock(context.Background())
	if err != nil {
		return fmt.Errorf("failed building block: %w", err)
	}
	if err := blk.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying block: %w", err)
	}

	proposalBlk := blk.(snowman.OracleBlock)
	options, err := proposalBlk.Options(context.Background())
	if err != nil {
		return fmt.Errorf("failed retrieving options: %w", err)
	}

	commit := options[0].(*blockexecutor.Block)
	_, ok := commit.Block.(*block.BanffCommitBlock)
	if !ok {
		return fmt.Errorf("failed retrieving commit option: %w", err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting block: %w", err)
	}

	if err := commit.Verify(context.Background()); err != nil {
		return fmt.Errorf("failed verifying commit block: %w", err)
	}
	if err := commit.Accept(context.Background()); err != nil {
		return fmt.Errorf("failed accepting commit block: %w", err)
	}

	if err := vm.SetPreference(context.Background(), vm.manager.LastAccepted()); err != nil {
		return fmt.Errorf("failed setting preference: %w", err)
	}

	return nil
}

type validatorInputData struct {
	eventType uint8
	startTime time.Time
	endTime   time.Time
	nodeID    ids.NodeID
}

// buildTimestampsList creates validators start and end time, given the event list.
// output is returned as a list of validatorInputData
func buildTimestampsList(events []uint8, currentTime time.Time, nodeID ids.NodeID) ([]*validatorInputData, error) {
	res := make([]*validatorInputData, 0, len(events))

	currentTime = currentTime.Add(txexecutor.SyncBound)
	switch endTime := currentTime.Add(defaultMinStakingDuration); events[0] {
	case startPrimaryWithBLS:
		res = append(res, &validatorInputData{
			eventType: startPrimaryWithBLS,
			startTime: currentTime,
			endTime:   endTime,
			nodeID:    nodeID,
		})
	default:
		return nil, fmt.Errorf("unexpected initial event %d", events[0])
	}

	// track current primary validator to make sure its staking period
	// covers all of its subnet validators
	currentPrimaryVal := res[0]
	for i := 1; i < len(events); i++ {
		currentTime = currentTime.Add(txexecutor.SyncBound)

		switch currentEvent := events[i]; currentEvent {
		case startSubnetValidator:
			endTime := currentTime.Add(defaultMinStakingDuration)
			res = append(res, &validatorInputData{
				eventType: startSubnetValidator,
				startTime: currentTime,
				endTime:   endTime,
				nodeID:    nodeID,
			})

			currentPrimaryVal.endTime = endTime.Add(time.Second)
			currentTime = endTime.Add(time.Second)

		case startPrimaryWithBLS:
			currentTime = currentPrimaryVal.endTime.Add(txexecutor.SyncBound)
			endTime := currentTime.Add(defaultMinStakingDuration)
			val := &validatorInputData{
				eventType: startPrimaryWithBLS,
				startTime: currentTime,
				endTime:   endTime,
				nodeID:    nodeID,
			}
			res = append(res, val)
			currentPrimaryVal = val
		}
	}
	return res, nil
}

func TestTimestampListGenerator(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("primary validators are returned in sequence", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			// nil out non subnet validators
			subnetIndexes := make([]int, 0, len(validatorsTimes))
			for idx, ev := range validatorsTimes {
				if ev.eventType == startSubnetValidator {
					subnetIndexes = append(subnetIndexes, idx)
				}
			}
			for _, idx := range subnetIndexes {
				validatorsTimes[idx] = nil
			}

			currentEventTime := currentTime
			for i, ev := range validatorsTimes {
				if ev == nil {
					continue // a subnet validator
				}
				if currentEventTime.After(ev.startTime) {
					return fmt.Sprintf("validator %d start time larger than current event time", i)
				}

				if ev.startTime.After(ev.endTime) {
					return fmt.Sprintf("validator %d start time larger than its end time", i)
				}

				currentEventTime = ev.endTime
			}

			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startSubnetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.Property("subnet validators are returned in sequence", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			// nil out non subnet validators
			nonSubnetIndexes := make([]int, 0, len(validatorsTimes))
			for idx, ev := range validatorsTimes {
				if ev.eventType != startSubnetValidator {
					nonSubnetIndexes = append(nonSubnetIndexes, idx)
				}
			}
			for _, idx := range nonSubnetIndexes {
				validatorsTimes[idx] = nil
			}

			currentEventTime := currentTime
			for i, ev := range validatorsTimes {
				if ev == nil {
					continue // a non-subnet validator
				}
				if currentEventTime.After(ev.startTime) {
					return fmt.Sprintf("validator %d start time larger than current event time", i)
				}

				if ev.startTime.After(ev.endTime) {
					return fmt.Sprintf("validator %d start time larger than its end time", i)
				}

				currentEventTime = ev.endTime
			}

			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startSubnetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.Property("subnet validators' times are bound by a primary validator's times", prop.ForAll(
		func(events []uint8) string {
			currentTime := time.Now()
			nodeID := ids.GenerateTestNodeID()
			validatorsTimes, err := buildTimestampsList(events, currentTime, nodeID)
			if err != nil {
				return failedBuildingEventSeqString + err.Error()
			}

			if len(validatorsTimes) == 0 {
				return errEmptyEventsList.Error()
			}

			currentPrimaryValidator := validatorsTimes[0]
			for i := 1; i < len(validatorsTimes); i++ {
				if validatorsTimes[i].eventType != startSubnetValidator {
					currentPrimaryValidator = validatorsTimes[i]
					continue
				}

				subnetVal := validatorsTimes[i]
				if currentPrimaryValidator.startTime.After(subnetVal.startTime) ||
					subnetVal.endTime.After(currentPrimaryValidator.endTime) {
					return "subnet validator not bounded by primary network ones"
				}
			}
			return ""
		},
		gen.SliceOf(gen.OneConstOf(
			startPrimaryWithBLS,
			startSubnetValidator,
		)).SuchThat(func(v interface{}) bool {
			list := v.([]uint8)
			return len(list) > 0 && list[0] == startPrimaryWithBLS
		}),
	))

	properties.TestingRun(t)
}

// add a single validator at the end of times,
// to make sure it won't pollute our tests
func buildVM(t *testing.T) (*VM, ids.ID, error) {
	vm := &VM{Internal: config.Internal{
		Chains:                 chains.TestManager,
		UptimeLockedCalculator: uptime.NewLockedCalculator(),
		SybilProtectionEnabled: true,
		Validators:             validators.NewManager(),
		DynamicFeeConfig:       defaultDynamicFeeConfig,
		MinValidatorStake:      defaultMinValidatorStake,
		MaxValidatorStake:      defaultMaxValidatorStake,
		MinDelegatorStake:      defaultMinDelegatorStake,
		MinStakeDuration:       defaultMinStakingDuration,
		MaxStakeDuration:       defaultMaxStakingDuration,
		RewardConfig:           defaultRewardConfig,
		UpgradeConfig:          upgradetest.GetConfigWithUpgradeTime(upgradetest.Durango, genesistest.DefaultValidatorStartTime),
	}}
	vm.clock.Set(genesistest.DefaultValidatorStartTime.Add(time.Second))

	baseDB := memdb.New()
	chainDB := prefixdb.New([]byte{0}, baseDB)
	atomicDB := prefixdb.New([]byte{1}, baseDB)

	ctx := snowtest.Context(t, snowtest.PChainID)

	m := atomic.NewMemory(atomicDB)
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	ctx.Lock.Lock()
	defer ctx.Lock.Unlock()
	appSender := &enginetest.Sender{}
	appSender.CantSendAppGossip = true
	appSender.SendAppGossipF = func(context.Context, common.SendConfig, []byte) error {
		return nil
	}

	err := vm.Initialize(
		t.Context(),
		ctx,
		chainDB,
		genesistest.NewBytes(t, genesistest.Config{
			NodeIDs: []ids.NodeID{
				genesistest.DefaultNodeIDs[len(genesistest.DefaultNodeIDs)-1],
			},
			ValidatorEndTime: mockable.MaxTime,
		}),
		nil,
		nil,
		nil,
		appSender,
	)
	if err != nil {
		return nil, ids.Empty, err
	}

	err = vm.SetState(t.Context(), snow.NormalOp)
	if err != nil {
		return nil, ids.Empty, err
	}

	// Create a subnet and store it in testSubnet1
	// Note: following Banff activation, block acceptance will move
	// chain time ahead
	wallet := newWallet(t, vm, walletConfig{})
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	}
	testSubnet1, err = wallet.IssueCreateSubnetTx(
		owner,
		walletcommon.WithChangeOwner(owner),
	)
	if err != nil {
		return nil, ids.Empty, err
	}

	vm.ctx.Lock.Unlock()
	err = vm.issueTxFromRPC(testSubnet1)
	vm.ctx.Lock.Lock()
	if err != nil {
		return nil, ids.Empty, err
	}

	blk, err := vm.Builder.BuildBlock(t.Context())
	if err != nil {
		return nil, ids.Empty, err
	}
	if err := blk.Verify(t.Context()); err != nil {
		return nil, ids.Empty, err
	}
	if err := blk.Accept(t.Context()); err != nil {
		return nil, ids.Empty, err
	}
	if err := vm.SetPreference(t.Context(), vm.manager.LastAccepted()); err != nil {
		return nil, ids.Empty, err
	}

	return vm, testSubnet1.ID(), nil
}
