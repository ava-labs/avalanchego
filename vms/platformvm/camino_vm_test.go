// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestRewardSuspendedValidator(t *testing.T) {
	require := require.New(t)
	addr := caminoPreFundedKeys[0].Address()
	hrp := constants.NetworkIDToHRP[testNetworkID]
	bech32Addr, err := address.FormatBech32(hrp, addr.Bytes())
	require.NoError(err)

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)
	consortiumMemberKey, ok := key.(*crypto.PrivateKeySECP256K1R)
	require.True(ok)

	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        addr,
	}
	genesisUTXOs := []api.UTXO{
		{
			Amount:  json.Uint64(defaultCaminoValidatorWeight),
			Address: bech32Addr,
		},
	}

	vm := newCaminoVM(caminoGenesisConf, genesisUTXOs)
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	utxo := generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultBalance, *outputOwners, ids.Empty, ids.Empty)
	vm.state.AddUTXO(utxo)
	err = vm.state.Commit()
	require.NoError(err)

	// Set consortium member
	tx, err := vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateConsortium,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Register node
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Add the validator
	vm.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr)
	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := defaultValidateEndTime.Add(-1 * time.Hour)
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.Config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], consortiumMemberKey},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		addValidatorTx.ID(),
		addValidatorTx.Unsigned.(*txs.CaminoAddValidatorTx),
		0,
	)
	require.NoError(err)
	vm.state.PutCurrentValidator(staker)
	vm.state.AddTx(addValidatorTx, status.Committed)
	err = vm.state.Commit()
	require.NoError(err)

	utxo = generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultBalance, *outputOwners, ids.Empty, ids.Empty)
	vm.state.AddUTXO(utxo)
	err = vm.state.Commit()
	require.NoError(err)

	// Suspend the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateNodeDeferred,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Verify that the validator is suspended (moved from current to pending stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Fast-forward clock to time for validator to be rewarded
	vm.clock.Set(endTime)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[1].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[0].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that the validator is rewarded
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	timestamp := vm.state.GetTimestamp()
	require.Equal(endTime.Unix(), timestamp.Unix())
}

func TestRewardReactivatedValidator(t *testing.T) {
	require := require.New(t)
	addr := caminoPreFundedKeys[0].Address()
	hrp := constants.NetworkIDToHRP[testNetworkID]
	bech32Addr, err := address.FormatBech32(hrp, addr.Bytes())
	require.NoError(err)

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	key, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)
	consortiumMemberKey, ok := key.(*crypto.PrivateKeySECP256K1R)
	require.True(ok)

	outputOwners := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        addr,
	}
	genesisUTXOs := []api.UTXO{
		{
			Amount:  json.Uint64(defaultCaminoValidatorWeight),
			Address: bech32Addr,
		},
	}

	vm := newCaminoVM(caminoGenesisConf, genesisUTXOs)
	vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
		vm.ctx.Lock.Unlock()
	}()

	utxo := generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultBalance, *outputOwners, ids.Empty, ids.Empty)
	vm.state.AddUTXO(utxo)
	err = vm.state.Commit()
	require.NoError(err)

	// Set consortium member
	tx, err := vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateConsortium,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Register node
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Add the validator
	vm.state.SetShortIDLink(ids.ShortID(nodeID), state.ShortLinkKeyRegisterNode, &addr)
	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := defaultValidateEndTime.Add(-1 * time.Hour)
	addValidatorTx, err := vm.txBuilder.NewAddValidatorTx(
		vm.Config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0], nodeKey},
		ids.ShortEmpty,
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		addValidatorTx.ID(),
		addValidatorTx.Unsigned.(*txs.CaminoAddValidatorTx),
		0,
	)
	require.NoError(err)
	vm.state.PutCurrentValidator(staker)
	vm.state.AddTx(addValidatorTx, status.Committed)
	err = vm.state.Commit()
	require.NoError(err)

	utxo = generateTestUTXO(ids.GenerateTestID(), avaxAssetID, defaultBalance, *outputOwners, ids.Empty, ids.Empty)
	vm.state.AddUTXO(utxo)
	err = vm.state.Commit()
	require.NoError(err)

	// Suspend the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateNodeDeferred,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Verify that the validator is suspended (moved from current to pending stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Reactivate the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		true,
		txs.AddressStateNodeDeferred,
		[]*crypto.PrivateKeySECP256K1R{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	err = vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)
	err = blk.Accept(context.Background())
	require.NoError(err)
	err = vm.SetPreference(context.Background(), vm.manager.LastAccepted())
	require.NoError(err)

	// Verify that the validator is activated again (moved from pending to current stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Fast-forward clock to time for validator to be rewarded
	vm.clock.Set(endTime)
	blk, err = vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert preferences are correct
	block := blk.(smcon.OracleBlock)
	options, err := block.Options(context.Background())
	require.NoError(err)

	commit := options[1].(*blockexecutor.Block)
	_, ok = commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[0].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(block.Accept(context.Background()))
	require.NoError(commit.Verify(context.Background()))
	require.NoError(abort.Verify(context.Background()))

	txID := blk.(blocks.Block).Txs()[0].ID()
	{
		onAccept, ok := vm.manager.GetState(abort.ID())
		require.True(ok)

		_, txStatus, err := onAccept.GetTx(txID)
		require.NoError(err)
		require.Equal(status.Aborted, txStatus)
	}

	require.NoError(commit.Accept(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	_, txStatus, err := vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that the validator is rewarded
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	timestamp := vm.state.GetTimestamp()
	require.Equal(endTime.Unix(), timestamp.Unix())
}
