// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

func TestUptimeDisallowedWithRestart(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.DefaultVersion1_0_0)

	firstDB := db.NewPrefixDBManager([]byte{})
	firstVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	firstCtx := defaultContext()
	firstCtx.Lock.Lock()

	firstMsgChan := make(chan common.Message, 1)
	if err := firstVM.Initialize(firstCtx, firstDB, genesisBytes, nil, nil, firstMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	firstVM.clock.Set(defaultGenesisTime)
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultGenesisTime)

	if err := firstVM.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := firstVM.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	// Fast forward clock to time for genesis validators to leave
	firstVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	if err := firstVM.Shutdown(); err != nil {
		t.Fatal(err)
	}
	firstCtx.Lock.Unlock()

	secondDB := db.NewPrefixDBManager([]byte{})
	secondVM := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .21,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	secondCtx := defaultContext()
	secondCtx.Lock.Lock()
	defer func() {
		if err := secondVM.Shutdown(); err != nil {
			t.Fatal(err)
		}
		secondCtx.Lock.Unlock()
	}()

	secondMsgChan := make(chan common.Message, 1)
	if err := secondVM.Initialize(secondCtx, secondDB, genesisBytes, nil, nil, secondMsgChan, nil, nil); err != nil {
		t.Fatal(err)
	}

	secondVM.clock.Set(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateStartTime.Add(2 * defaultMinStakingDuration))

	if err := secondVM.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := secondVM.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	secondVM.clock.Set(defaultValidateEndTime)
	secondVM.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := secondVM.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// Assert preferences are correct
	block := blk.(*stateful.ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}

	commit, ok := options[0].(*stateful.CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}

	abort, ok := options[1].(*stateful.AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}

	if err := block.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	onAbortState := abort.OnAccept()
	_, txStatus, err := onAbortState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	if err := commit.Accept(); err != nil { // advance the timestamp
		t.Fatal(err)
	}

	_, txStatus, err = secondVM.internalState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	// Verify that chain's timestamp has advanced
	timestamp := secondVM.internalState.GetTimestamp()
	if !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	blk, err = secondVM.BuildBlock() // should contain proposal to reward genesis validator
	if err != nil {
		t.Fatal(err)
	}
	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	block = blk.(*stateful.ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}

	commit, ok = options[1].(*stateful.CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	abort, ok = options[0].(*stateful.AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}

	onCommitState := commit.OnAccept()
	_, txStatus, err = onCommitState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if txStatus != status.Committed {
		t.Fatalf("status should be Committed but is %s", txStatus)
	}

	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Accept(); err != nil { // do not reward the genesis validator
		t.Fatal(err)
	}

	_, txStatus, err = secondVM.internalState.GetTx(block.Tx.ID())
	if err != nil {
		t.Fatal(err)
	}
	if txStatus != status.Aborted {
		t.Fatalf("status should be Aborted but is %s", txStatus)
	}

	currentStakers := secondVM.internalState.CurrentStakerChainState()
	_, err = currentStakers.GetValidator(ids.NodeID(keys[1].PublicKey().Address()))
	if err == nil {
		t.Fatal("should have removed a genesis validator")
	}
}

func TestUptimeDisallowedAfterNeverConnecting(t *testing.T) {
	_, genesisBytes := defaultGenesis()
	db := manager.NewMemDB(version.DefaultVersion1_0_0)

	vm := &VM{Factory: Factory{
		Config: config.Config{
			Chains:                 chains.MockManager{},
			UptimePercentage:       .2,
			RewardConfig:           defaultRewardConfig,
			Validators:             validators.NewManager(),
			UptimeLockedCalculator: uptime.NewLockedCalculator(),
		},
	}}

	ctx := defaultContext()
	ctx.Lock.Lock()

	msgChan := make(chan common.Message, 1)
	appSender := &common.SenderTest{T: t}
	if err := vm.Initialize(ctx, db, genesisBytes, nil, nil, msgChan, nil, appSender); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		ctx.Lock.Unlock()
	}()

	vm.clock.Set(defaultGenesisTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultGenesisTime)

	if err := vm.SetState(snow.Bootstrapping); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetState(snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	// Fast forward clock to time for genesis validators to leave
	vm.clock.Set(defaultValidateEndTime)
	vm.uptimeManager.(uptime.TestManager).SetTime(defaultValidateEndTime)

	blk, err := vm.BuildBlock() // should contain proposal to advance time
	if err != nil {
		t.Fatal(err)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	// first the time will be advanced.
	block := blk.(*stateful.ProposalBlock)
	options, err := block.Options()
	if err != nil {
		t.Fatal(err)
	}

	commit, ok := options[0].(*stateful.CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}
	abort, ok := options[1].(*stateful.AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefCommit)
	}

	if err := block.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	// advance the timestamp
	if err := commit.Accept(); err != nil {
		t.Fatal(err)
	}

	// Verify that chain's timestamp has advanced
	timestamp := vm.internalState.GetTimestamp()
	if !timestamp.Equal(defaultValidateEndTime) {
		t.Fatal("expected timestamp to have advanced")
	}

	// should contain proposal to reward genesis validator
	blk, err = vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	block = blk.(*stateful.ProposalBlock)
	options, err = block.Options()
	if err != nil {
		t.Fatal(err)
	}

	abort, ok = options[0].(*stateful.AbortBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}
	commit, ok = options[1].(*stateful.CommitBlock)
	if !ok {
		t.Fatal(errShouldPrefAbort)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Verify(); err != nil {
		t.Fatal(err)
	}
	if err := abort.Verify(); err != nil {
		t.Fatal(err)
	}

	// do not reward the genesis validator
	if err := abort.Accept(); err != nil {
		t.Fatal(err)
	}

	currentStakers := vm.internalState.CurrentStakerChainState()
	_, err = currentStakers.GetValidator(ids.NodeID(keys[1].PublicKey().Address()))
	if err == nil {
		t.Fatal("should have removed a genesis validator")
	}
}
