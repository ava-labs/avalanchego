// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestDeferredStakers(t *testing.T) {
	type stakerStatus uint
	const (
		pending stakerStatus = iota
		current
		deferred
		expired
	)

	type staker struct {
		nodeID                        ids.NodeID
		nodeKey                       *secp256k1.PrivateKey
		nodeOwnerAddr                 ids.ShortID
		nodeOwnerKey                  *secp256k1.PrivateKey
		startTime, endTime, deferTime time.Time
	}
	type test struct {
		stakers               []staker
		subnetStakers         []staker
		deferredStakers       []staker
		advanceTimeTo         []time.Time
		expectedStakers       map[ids.NodeID]stakerStatus
		expectedSubnetStakers map[ids.NodeID]stakerStatus
	}

	// Chronological order (not in scale):
	// Staker1:    			|----------------------------------------------------------|
	// Staker2:        			|------------------------|
	// Staker3:            			|------------------------|
	// Staker3sub:             			|----------------|
	// staker4:                                 		 |---|
	// staker5Deferred	        |---|--------------------|

	nodeIDs := make([]ids.NodeID, 6)
	nodeKeys := make([]*secp256k1.PrivateKey, 6)
	nodeOwnerAddresses := make([]ids.ShortID, 6)
	nodeOwnerKeys := make([]*secp256k1.PrivateKey, 6)
	for i := range [6]int{} {
		nodeKeys[i], nodeIDs[i] = nodeid.GenerateCaminoNodeKeyAndID()
		nodeOwnerKeys[i], nodeOwnerAddresses[i], _ = generateKeyAndOwner(t)
	}

	staker1 := staker{
		nodeID:        nodeIDs[0],
		nodeKey:       nodeKeys[0],
		nodeOwnerAddr: nodeOwnerAddresses[0],
		nodeOwnerKey:  nodeOwnerKeys[0],
		startTime:     defaultGenesisTime.Add(1 * time.Minute),
		endTime:       defaultGenesisTime.Add(10 * defaultMinStakingDuration).Add(1 * time.Minute),
	}
	staker2 := staker{
		nodeID:        nodeIDs[1],
		nodeKey:       nodeKeys[1],
		nodeOwnerAddr: nodeOwnerAddresses[1],
		nodeOwnerKey:  nodeOwnerKeys[1],
		startTime:     staker1.startTime.Add(1 * time.Minute),
		endTime:       staker1.startTime.Add(1 * time.Minute).Add(defaultMinStakingDuration),
	}
	staker3 := staker{
		nodeID:        nodeIDs[2],
		nodeKey:       nodeKeys[2],
		nodeOwnerAddr: nodeOwnerAddresses[2],
		nodeOwnerKey:  nodeOwnerKeys[2],
		startTime:     staker2.startTime.Add(1 * time.Minute),
		endTime:       staker2.endTime.Add(1 * time.Minute),
	}
	staker3Sub := staker{
		nodeID:        nodeIDs[2],
		nodeKey:       nodeKeys[2],
		nodeOwnerAddr: nodeOwnerAddresses[3],
		nodeOwnerKey:  nodeOwnerKeys[3],
		startTime:     staker3.startTime.Add(1 * time.Minute),
		endTime:       staker3.endTime.Add(-1 * time.Minute),
	}
	staker4 := staker{
		nodeID:        nodeIDs[4],
		nodeKey:       nodeKeys[4],
		nodeOwnerAddr: nodeOwnerAddresses[4],
		nodeOwnerKey:  nodeOwnerKeys[4],
		startTime:     staker2.endTime,
		endTime:       staker3.endTime,
	}
	staker5 := staker{
		nodeID:        nodeIDs[5],
		nodeKey:       nodeKeys[5],
		nodeOwnerAddr: nodeOwnerAddresses[5],
		nodeOwnerKey:  nodeOwnerKeys[5],
		startTime:     staker2.startTime,
		endTime:       staker2.endTime,
		deferTime:     staker3.startTime,
	}

	tests := map[string]test{
		"Staker 5 still in pending set": {
			stakers:       []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers: []staker{staker1, staker2, staker3, staker4, staker5},
			advanceTimeTo: []time.Time{staker1.startTime.Add(-1 * time.Second)},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: pending,
				staker2.nodeID: pending,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: pending,
			},
		},
		"Staker 5 in current set": {
			stakers:         []staker{staker1, staker2, staker3, staker4, staker5},
			deferredStakers: []staker{staker5},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: current,
			},
		},
		"Staker 5 deferred but still validating subnet": {
			stakers:         []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers:   []staker{staker1, staker2, staker3Sub, staker4, staker5},
			deferredStakers: []staker{staker5},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime, staker3.startTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: pending,
				staker5.nodeID: deferred,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: pending,
				staker4.nodeID: pending,
				staker5.nodeID: current,
			},
		},
		"Staker 5 expired": {
			stakers:         []staker{staker1, staker2, staker3, staker4, staker5},
			subnetStakers:   []staker{staker1, staker2, staker3Sub, staker4, staker5},
			deferredStakers: []staker{staker5},
			advanceTimeTo:   []time.Time{staker1.startTime, staker2.startTime, staker3.startTime, staker3Sub.startTime, staker5.endTime},
			expectedStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: current,
				staker3.nodeID: current,
				staker4.nodeID: current,
				staker5.nodeID: expired,
			},
			expectedSubnetStakers: map[ids.NodeID]stakerStatus{
				staker1.nodeID: current,
				staker2.nodeID: expired,
				staker3.nodeID: expired,
				staker4.nodeID: current,
				staker5.nodeID: expired,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(ts *testing.T) {
			require := require.New(ts)
			caminoGenesisConf := api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        caminoPreFundedKeys[0].Address(),
			}
			env := newCaminoEnvironment( /*postBanff*/ true, true, caminoGenesisConf)
			env.ctx.Lock.Lock()
			defer func() {
				if err := shutdownCaminoEnvironment(env); err != nil {
					t.Fatal(err)
				}
			}()

			dummyHeight := uint64(1)

			subnetID := testSubnet1.ID()
			env.config.TrackedSubnets.Add(subnetID)
			env.config.Validators.Add(subnetID, validators.NewSet())

			for _, staker := range test.stakers {
				_, err := addCaminoPendingValidator(
					env,
					staker.startTime,
					staker.endTime,
					staker.nodeID,
					staker.nodeOwnerAddr,
					[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], staker.nodeKey, staker.nodeOwnerKey},
				)
				require.NoError(err)
			}

			for _, staker := range test.subnetStakers {
				tx, err := env.txBuilder.NewAddSubnetValidatorTx(
					10, // Weight
					uint64(staker.startTime.Unix()),
					uint64(staker.endTime.Unix()),
					staker.nodeID, // validator ID
					subnetID,      // Subnet ID
					[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], caminoPreFundedKeys[1], staker.nodeKey},
					ids.ShortEmpty,
				)
				require.NoError(err)

				staker, err := state.NewPendingStaker(
					tx.ID(),
					tx.Unsigned.(*txs.AddSubnetValidatorTx),
				)
				require.NoError(err)

				env.state.PutPendingValidator(staker)
				env.state.AddTx(tx, status.Committed)
			}
			env.state.SetHeight(dummyHeight)
			require.NoError(env.state.Commit())

			for _, newTime := range test.advanceTimeTo {
				env.clk.Set(newTime)
				tx, err := env.txBuilder.NewAdvanceTimeTx(newTime)
				require.NoError(err)

				onCommitState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				onAbortState, err := state.NewDiff(lastAcceptedID, env)
				require.NoError(err)

				executor := ProposalTxExecutor{
					OnCommitState: onCommitState,
					OnAbortState:  onAbortState,
					Backend:       &env.backend,
					Tx:            tx,
				}
				require.NoError(tx.Unsigned.Visit(&executor))
				require.NoError(executor.OnCommitState.Apply(env.state))

				env.state.SetHeight(dummyHeight)
				require.NoError(env.state.Commit())

				for _, staker := range test.deferredStakers {
					if newTime == staker.deferTime {
						_, err = deferValidator(env, staker.nodeOwnerAddr, caminoPreFundedKeys[0])
						require.NoError(err)
					}
				}
			}

			for stakerNodeID, status := range test.expectedStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.False(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.NoError(err)
					require.True(validators.Contains(env.config.Validators, constants.PrimaryNetworkID, stakerNodeID))
				case expired:
					_, err := env.state.GetCurrentValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.Error(err)
					_, err = env.state.GetPendingValidator(constants.PrimaryNetworkID, stakerNodeID)
					require.Error(err)
				}
			}

			for stakerNodeID, status := range test.expectedSubnetStakers {
				switch status {
				case pending:
					_, err := env.state.GetPendingValidator(subnetID, stakerNodeID)
					require.NoError(err)
					require.False(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
				case current:
					_, err := env.state.GetCurrentValidator(subnetID, stakerNodeID)
					require.NoError(err)
					require.True(validators.Contains(env.config.Validators, subnetID, stakerNodeID))
				case expired:
					_, err := env.state.GetCurrentValidator(subnetID, stakerNodeID)
					require.Error(err)
					_, err = env.state.GetPendingValidator(subnetID, stakerNodeID)
					require.Error(err)
				}
			}
		})
	}
}

func addCaminoPendingValidator(
	env *caminoEnvironment,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	nodeOwnerAddr ids.ShortID,
	keys []*secp256k1.PrivateKey,
) (*txs.Tx, error) {
	env.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &nodeOwnerAddr)
	link := ids.ShortID(nodeID)
	env.state.SetShortIDLink(nodeOwnerAddr, state.ShortLinkKeyRegisterNode, &link)
	if err := env.state.Commit(); err != nil {
		return nil, err
	}

	tx, err := env.txBuilder.NewCaminoAddValidatorTx(
		env.config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		nodeOwnerAddr,
		ids.ShortID(nodeID),
		reward.PercentDenominator,
		keys,
		ids.ShortEmpty,
	)
	if err != nil {
		return nil, err
	}

	staker, err := state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.CaminoAddValidatorTx),
	)
	if err != nil {
		return nil, err
	}

	env.state.PutPendingValidator(staker)
	env.state.AddTx(tx, status.Committed)
	dummyHeight := uint64(1)
	env.state.SetHeight(dummyHeight)
	if err := env.state.Commit(); err != nil {
		return nil, err
	}
	return tx, nil
}

func deferValidator(env *caminoEnvironment, nodeOwnerAddress ids.ShortID, key *secp256k1.PrivateKey) (*txs.Tx, error) {
	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{key.Address()},
	}

	tx, err := env.txBuilder.NewAddressStateTx(
		nodeOwnerAddress,
		false,
		as.AddressStateBitNodeDeferred,
		key.Address(),
		[]*secp256k1.PrivateKey{key},
		outputOwners,
	)
	if err != nil {
		return nil, err
	}

	onAcceptState, err := state.NewDiff(lastAcceptedID, env)
	if err != nil {
		return nil, err
	}

	executor := CaminoStandardTxExecutor{
		StandardTxExecutor{
			Backend: &env.backend,
			State:   onAcceptState,
			Tx:      tx,
		},
	}
	err = tx.Unsigned.Visit(&executor)
	if err != nil {
		return nil, err
	}

	if err := executor.State.Apply(env.state); err != nil {
		return nil, err
	}

	if err := env.state.Commit(); err != nil {
		return nil, err
	}

	return tx, nil
}
