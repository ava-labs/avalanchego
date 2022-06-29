// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestAddDelegatorTxSyntacticVerify(t *testing.T) {
	h := newTestHelpersCollection()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	rewardAddress := prefundedKeys[0].PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	// Case : tx is nil
	var unsignedTx *txs.AddDelegatorTx
	stx := txs.Tx{
		Unsigned: unsignedTx,
	}
	if err := stx.SyntacticVerify(h.ctx); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case: Wrong network ID
	tx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}

	addDelegatorTx := tx.Unsigned.(*txs.AddDelegatorTx)
	addDelegatorTx.NetworkID++
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addDelegatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(h.ctx); err == nil {
		t.Fatal("should have erred because the wrong network ID was used")
	}
	// Case: Valid
	if tx, err := h.txBuilder.NewAddDelegatorTx(
		h.cfg.MinDelegatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		rewardAddress,
		[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
		ids.ShortEmpty,
	); err != nil {
		t.Fatal(err)
	} else if err := tx.SyntacticVerify(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAddDelegatorTxExecute(t *testing.T) {
	rewardAddress := prefundedKeys[0].PublicKey().Address()
	nodeID := ids.NodeID(rewardAddress)

	factory := crypto.FactorySECP256K1R{}
	keyIntf, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	newValidatorKey := keyIntf.(*crypto.PrivateKeySECP256K1R)
	newValidatorID := ids.NodeID(newValidatorKey.PublicKey().Address())
	newValidatorStartTime := uint64(defaultValidateStartTime.Add(5 * time.Second).Unix())
	newValidatorEndTime := uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix())

	// [addMinStakeValidator] adds a new validator to the primary network's
	// pending validator set with the minimum staking amount
	addMinStakeValidator := func(target *testHelpersCollection) {
		tx, err := target.txBuilder.NewAddValidatorTx(
			target.cfg.MinValidatorStake, // stake amount
			newValidatorStartTime,        // start time
			newValidatorEndTime,          // end time
			newValidatorID,               // node ID
			rewardAddress,                // Reward Address
			reward.PercentDenominator,    // Shares
			[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			ids.ShortEmpty,
		)
		if err != nil {
			t.Fatal(err)
		}

		target.tState.AddCurrentStaker(tx, 0)
		target.tState.AddTx(tx, status.Committed)
		if err := target.tState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := target.tState.Load(); err != nil {
			t.Fatal(err)
		}
	}

	// [addMaxStakeValidator] adds a new validator to the primary network's
	// pending validator set with the maximum staking amount
	addMaxStakeValidator := func(target *testHelpersCollection) {
		tx, err := target.txBuilder.NewAddValidatorTx(
			target.cfg.MaxValidatorStake, // stake amount
			newValidatorStartTime,        // start time
			newValidatorEndTime,          // end time
			newValidatorID,               // node ID
			rewardAddress,                // Reward Address
			reward.PercentDenominator,    // Shared
			[]*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			ids.ShortEmpty,
		)
		if err != nil {
			t.Fatal(err)
		}

		target.tState.AddCurrentStaker(tx, 0)
		target.tState.AddTx(tx, status.Committed)
		if err := target.tState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := target.tState.Load(); err != nil {
			t.Fatal(err)
		}
	}

	dummyH := newTestHelpersCollection()
	currentTimestamp := dummyH.tState.GetTimestamp()

	type test struct {
		stakeAmount   uint64
		startTime     uint64
		endTime       uint64
		nodeID        ids.NodeID
		rewardAddress ids.ShortID
		feeKeys       []*crypto.PrivateKeySECP256K1R
		setup         func(*testHelpersCollection)
		AP3Time       time.Time
		shouldErr     bool
		description   string
	}

	tests := []test{
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Unix()),
			endTime:       uint64(defaultValidateEndTime.Unix()) + 1,
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator stops validating primary network earlier than subnet",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     uint64(currentTimestamp.Add(MaxFutureStartTime + time.Second).Unix()),
			endTime:       uint64(currentTimestamp.Add(MaxFutureStartTime * 2).Unix()),
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   fmt.Sprintf("validator should not be added more than (%s) in the future", MaxFutureStartTime),
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Unix()),
			endTime:       uint64(defaultValidateEndTime.Unix()) + 1,
			nodeID:        nodeID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "end time is after the primary network end time",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     uint64(defaultValidateStartTime.Add(5 * time.Second).Unix()),
			endTime:       uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix()),
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator not in the current or pending validator sets of the subnet",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     newValidatorStartTime - 1, // start validating subnet before primary network
			endTime:       newValidatorEndTime,
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator starts validating subnet before primary network",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     newValidatorStartTime,
			endTime:       newValidatorEndTime + 1, // stop validating subnet after stopping validating primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "validator stops validating primary network before subnet",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         addMinStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     false,
			description:   "valid",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,                     // weight
			startTime:     uint64(currentTimestamp.Unix()),                  // start time
			endTime:       uint64(defaultValidateEndTime.Unix()),            // end time
			nodeID:        nodeID,                                           // node ID
			rewardAddress: rewardAddress,                                    // Reward Address
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]}, // tx fee payer
			setup:         nil,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "starts validating at current timestamp",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,                     // weight
			startTime:     uint64(defaultValidateStartTime.Unix()),          // start time
			endTime:       uint64(defaultValidateEndTime.Unix()),            // end time
			nodeID:        nodeID,                                           // node ID
			rewardAddress: rewardAddress,                                    // Reward Address
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[1]}, // tx fee payer
			setup: func(target *testHelpersCollection) { // Remove all UTXOs owned by keys[1]
				utxoIDs, err := target.tState.UTXOIDs(
					prefundedKeys[1].PublicKey().Address().Bytes(),
					ids.Empty,
					math.MaxInt32)
				if err != nil {
					t.Fatal(err)
				}
				for _, utxoID := range utxoIDs {
					target.tState.DeleteUTXO(utxoID)
				}
				if err := target.tState.Commit(); err != nil {
					t.Fatal(err)
				}
			},
			AP3Time:     defaultGenesisTime,
			shouldErr:   true,
			description: "tx fee paying key has no funds",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         addMaxStakeValidator,
			AP3Time:       defaultValidateEndTime,
			shouldErr:     false,
			description:   "over delegation before AP3",
		},
		{
			stakeAmount:   dummyH.cfg.MinDelegatorStake,
			startTime:     newValidatorStartTime, // same start time as for primary network
			endTime:       newValidatorEndTime,   // same end time as for primary network
			nodeID:        newValidatorID,
			rewardAddress: rewardAddress,
			feeKeys:       []*crypto.PrivateKeySECP256K1R{prefundedKeys[0]},
			setup:         addMaxStakeValidator,
			AP3Time:       defaultGenesisTime,
			shouldErr:     true,
			description:   "over delegation after AP3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			freshTH := newTestHelpersCollection()
			freshTH.cfg.ApricotPhase3Time = tt.AP3Time
			defer func() {
				if err := internalStateShutdown(freshTH); err != nil {
					t.Fatal(err)
				}
			}()

			tx, err := freshTH.txBuilder.NewAddDelegatorTx(
				tt.stakeAmount,
				tt.startTime,
				tt.endTime,
				tt.nodeID,
				tt.rewardAddress,
				tt.feeKeys,
				ids.ShortEmpty,
			)
			if err != nil {
				t.Fatalf("couldn't build tx: %s", err)
			}
			if tt.setup != nil {
				tt.setup(freshTH)
			}

			executor := ProposalTxExecutor{
				Backend:     &freshTH.execBackend,
				ParentState: freshTH.tState,
				Tx:          tx,
			}
			err = tx.Unsigned.Visit(&executor)
			if err != nil && !tt.shouldErr {
				t.Fatalf("shouldn't have errored but got %s", err)
			} else if err == nil && tt.shouldErr {
				t.Fatalf("expected test to error but got none")
			}
		})
	}
}
