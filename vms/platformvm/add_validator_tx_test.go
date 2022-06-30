// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestAddValidatorTxSyntacticVerify(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	key, err := testKeyfactory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// Case: tx is nil
	var unsignedTx *txs.AddValidatorTx
	stx := txs.Tx{
		Unsigned: unsignedTx,
	}
	if err := stx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 3: Wrong Network ID
	tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		ids.NodeID(nodeID),
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr

	)
	if err != nil {
		t.Fatal(err)
	}

	addValidatorTx := tx.Unsigned.(*txs.AddValidatorTx)
	addValidatorTx.NetworkID++
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addValidatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have erred because the wrong network ID was used")
	}

	// Case: Stake owner has no addresses
	tx, err = vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		ids.NodeID(nodeID),
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr

	)
	if err != nil {
		t.Fatal(err)
	}

	addValidatorTx = tx.Unsigned.(*txs.AddValidatorTx)
	addValidatorTx.Stake = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: vm.MinValidatorStake,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     nil,
			},
		},
	}}
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addValidatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have erred because stake owner has no addresses")
	}

	// Case: Rewards owner has no addresses
	tx, err = vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		ids.NodeID(nodeID),
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr

	)
	if err != nil {
		t.Fatal(err)
	}

	addValidatorTx = tx.Unsigned.(*txs.AddValidatorTx)
	addValidatorTx.RewardsOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     nil,
	}
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addValidatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have erred because rewards owner has no addresses")
	}

	// Case: Too many shares
	tx, err = vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		ids.NodeID(nodeID),
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr

	)
	if err != nil {
		t.Fatal(err)
	}

	addValidatorTx = tx.Unsigned.(*txs.AddValidatorTx)
	addValidatorTx.Shares++ // 1 more than max amount
	// This tx was syntactically verified when it was created... pretend it
	// wasn't so we don't use cache
	addValidatorTx.SyntacticallyVerified = false
	if err := tx.SyntacticVerify(vm.ctx); err == nil {
		t.Fatal("should have erred because of too many shares")
	}

	// Case: Valid
	if tx, err := vm.txBuilder.NewAddValidatorTx(
		vm.MinValidatorStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		ids.NodeID(nodeID),
		nodeID,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		ids.ShortEmpty, // change addr

	); err != nil {
		t.Fatal(err)
	} else if err := tx.SyntacticVerify(vm.ctx); err != nil {
		t.Fatal(err)
	}
}

// Test AddValidatorTx.Execute
func TestAddValidatorTxExecute(t *testing.T) {
	vm, _, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		vm.ctx.Lock.Unlock()
	}()

	key, err := testKeyfactory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	{
		// Case: Validator's start time too early
		tx, err := vm.txBuilder.NewAddValidatorTx(
			vm.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix())-1,
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{keys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've errored because start time too early")
		}
	}

	{
		// Case: Validator's start time too far in the future
		tx, err := vm.txBuilder.NewAddValidatorTx(
			vm.MinValidatorStake,
			uint64(defaultValidateStartTime.Add(maxFutureStartTime).Unix()+1),
			uint64(defaultValidateStartTime.Add(maxFutureStartTime).Add(defaultMinStakingDuration).Unix()+1),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{keys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've errored because start time too far in the future")
		}
	}

	{
		// Case: Validator already validating primary network
		tx, err := vm.txBuilder.NewAddValidatorTx(
			vm.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID), // node ID
			nodeID,             // reward address
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{keys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've errored because validator already validating")
		}
	}

	{
		// Case: Validator in pending validator set of primary network
		key2, err := testKeyfactory.NewPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		startTime := defaultGenesisTime.Add(1 * time.Second)
		tx, err := vm.txBuilder.NewAddValidatorTx(
			vm.MinValidatorStake,     // stake amount
			uint64(startTime.Unix()), // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			ids.NodeID(nodeID),         // node ID
			key2.PublicKey().Address(), // reward address
			reward.PercentDenominator,  // shares
			[]*crypto.PrivateKeySECP256K1R{keys[0]},
			ids.ShortEmpty, // change addr // key
		)
		if err != nil {
			t.Fatal(err)
		}

		vm.internalState.AddCurrentStaker(tx, 0)
		vm.internalState.AddTx(tx, status.Committed)
		if err := vm.internalState.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := vm.internalState.Load(); err != nil {
			t.Fatal(err)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should have failed because validator in pending validator set")
		}
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		tx, err := vm.txBuilder.NewAddValidatorTx( // create the tx
			vm.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{keys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		// Remove all UTXOs owned by keys[0]
		utxoIDs, err := vm.internalState.UTXOIDs(keys[0].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
		if err != nil {
			t.Fatal(err)
		}
		for _, utxoID := range utxoIDs {
			vm.internalState.DeleteUTXO(utxoID)
		}

		executor := proposalTxExecutor{
			vm:          vm,
			parentState: vm.internalState,
			tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should have failed because tx fee paying key has no funds")
		}
	}
}
