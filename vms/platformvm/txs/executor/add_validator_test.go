// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

func TestAddValidatorTxExecute(t *testing.T) {
	h := newTestHelpersCollection()
	h.ctx.Lock.Lock()
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()

	factory := crypto.FactorySECP256K1R{}
	key, err := factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	{
		// Case: Validator's start time too early
		tx, err := h.txBuilder.NewAddValidatorTx(
			h.cfg.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix())-1,
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've errored because start time too early")
		}
	}

	{
		// Case: Validator's start time too far in the future
		tx, err := h.txBuilder.NewAddValidatorTx(
			h.cfg.MinValidatorStake,
			uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Unix()+1),
			uint64(defaultValidateStartTime.Add(MaxFutureStartTime).Add(defaultMinStakingDuration).Unix()+1),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should've errored because start time too far in the future")
		}
	}

	{
		// Case: Validator already validating primary network
		tx, err := h.txBuilder.NewAddValidatorTx(
			h.cfg.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID), // node ID
			nodeID,             // reward address
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
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
		tx, err := h.txBuilder.NewAddValidatorTx(
			h.cfg.MinValidatorStake,                                 // stake amount
			uint64(startTime.Unix()),                                // start time
			uint64(startTime.Add(defaultMinStakingDuration).Unix()), // end time
			ids.NodeID(nodeID),                                      // node ID
			key2.PublicKey().Address(),                              // reward address
			reward.PercentDenominator,                               // shares
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr // key
		)
		if err != nil {
			t.Fatal(err)
		}

		h.tState.AddCurrentStaker(tx, 0)
		h.tState.AddTx(tx, status.Committed)
		dummyHeight := uint64(1)
		if err := h.tState.Write(dummyHeight); err != nil {
			t.Fatal(err)
		}
		if err := h.tState.Load(); err != nil {
			t.Fatal(err)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should have failed because validator in pending validator set")
		}
	}

	{
		// Case: Validator doesn't have enough tokens to cover stake amount
		tx, err := h.txBuilder.NewAddValidatorTx( // create the tx
			h.cfg.MinValidatorStake,
			uint64(defaultValidateStartTime.Unix()),
			uint64(defaultValidateEndTime.Unix()),
			ids.NodeID(nodeID),
			nodeID,
			reward.PercentDenominator,
			[]*crypto.PrivateKeySECP256K1R{preFundedKeys[0]},
			ids.ShortEmpty, // change addr
		)
		if err != nil {
			t.Fatal(err)
		}

		// Remove all UTXOs owned by preFundedKeys[0]
		utxoIDs, err := h.tState.UTXOIDs(preFundedKeys[0].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
		if err != nil {
			t.Fatal(err)
		}
		for _, utxoID := range utxoIDs {
			h.tState.DeleteUTXO(utxoID)
		}

		executor := ProposalTxExecutor{
			Backend:     &h.execBackend,
			ParentState: h.tState,
			Tx:          tx,
		}
		err = tx.Unsigned.Visit(&executor)
		if err == nil {
			t.Fatal("should have failed because tx fee paying key has no funds")
		}
	}
}
