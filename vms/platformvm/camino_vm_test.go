// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/nodeid"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	smcon "github.com/ava-labs/avalanchego/snow/consensus/snowman"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestRemoveDeferredValidator(t *testing.T) {
	require := require.New(t)
	addr := caminoPreFundedKeys[0].Address()
	hrp := constants.NetworkIDToHRP[testNetworkID]
	bech32Addr, err := address.FormatBech32(hrp, addr.Bytes())
	require.NoError(err)

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	consortiumMemberKey, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

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

	vm := newCaminoVM(caminoGenesisConf, genesisUTXOs, nil)
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
		txs.AddressStateBitConsortium,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
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
	startTime := vm.clock.Time().Add(txexecutor.SyncBound).Add(1 * time.Second)
	endTime := defaultValidateEndTime.Add(-1 * time.Hour)
	addValidatorTx, err := vm.txBuilder.NewCaminoAddValidatorTx(
		vm.Config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		consortiumMemberKey.Address(),
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], consortiumMemberKey},
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

	// Defer the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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

	// Verify that the validator is deferred (moved from current to deferred stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Verify that the validator's owner's deferred state and consortium member is true
	ownerState, _ := vm.state.GetAddressStates(consortiumMemberKey.Address())
	require.Equal(ownerState, txs.AddressStateNodeDeferred|txs.AddressStateConsortiumMember)

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
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
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
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Verify that the validator's owner's deferred state is false
	ownerState, _ = vm.state.GetAddressStates(consortiumMemberKey.Address())
	require.Equal(ownerState, txs.AddressStateConsortiumMember)

	timestamp := vm.state.GetTimestamp()
	require.Equal(endTime.Unix(), timestamp.Unix())
}

func TestRemoveReactivatedValidator(t *testing.T) {
	require := require.New(t)
	addr := caminoPreFundedKeys[0].Address()
	hrp := constants.NetworkIDToHRP[testNetworkID]
	bech32Addr, err := address.FormatBech32(hrp, addr.Bytes())
	require.NoError(err)

	nodeKey, nodeID := nodeid.GenerateCaminoNodeKeyAndID()

	consortiumMemberKey, err := testKeyFactory.NewPrivateKey()
	require.NoError(err)

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

	vm := newCaminoVM(caminoGenesisConf, genesisUTXOs, nil)
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
		txs.AddressStateBitConsortium,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
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
	addValidatorTx, err := vm.txBuilder.NewCaminoAddValidatorTx(
		vm.Config.MinValidatorStake,
		uint64(startTime.Unix()),
		uint64(endTime.Unix()),
		nodeID,
		consortiumMemberKey.Address(),
		ids.ShortEmpty,
		reward.PercentDenominator,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
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

	// Defer the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		txs.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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

	// Verify that the validator is deferred (moved from current to deferred stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Reactivate the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		true,
		txs.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
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

	// Verify that the validator is activated again (moved from deferred to current stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
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
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
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
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	timestamp := vm.state.GetTimestamp()
	require.Equal(endTime.Unix(), timestamp.Unix())
}

func TestDepositsAutoUnlock(t *testing.T) {
	require := require.New(t)

	depositOwnerKey, depositOwnerAddr, depositOwner := generateKeyAndOwner(t)
	ownerID, err := txs.GetOwnerID(depositOwner)
	require.NoError(err)
	depositOwnerAddrBech32, err := address.FormatBech32(constants.NetworkIDToHRP[testNetworkID], depositOwnerAddr.Bytes())
	require.NoError(err)

	depositOffer := &deposit.Offer{
		End:                   uint64(defaultGenesisTime.Unix() + 365*24*60*60 + 1),
		MinAmount:             10000,
		MaxDuration:           100,
		InterestRateNominator: 1_000_000 * 365 * 24 * 60 * 60, // 100% per year
	}
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		DepositOffers:       []*deposit.Offer{depositOffer},
	}
	require.NoError(genesis.SetDepositOfferID(caminoGenesisConf.DepositOffers[0]))

	vm := newCaminoVM(caminoGenesisConf, []api.UTXO{{
		Amount:  json.Uint64(depositOffer.MinAmount + defaultTxFee),
		Address: depositOwnerAddrBech32,
	}}, nil)
	vm.ctx.Lock.Lock()
	defer func() { require.NoError(vm.Shutdown(context.Background())) }() //nolint:lint

	// Add deposit
	depositTx, err := vm.txBuilder.NewDepositTx(
		depositOffer.MinAmount,
		depositOffer.MaxDuration,
		depositOffer.ID,
		depositOwnerAddr,
		[]*secp256k1.PrivateKey{depositOwnerKey},
		&depositOwner,
	)
	require.NoError(err)
	buildAndAcceptBlock(t, vm, depositTx)
	deposit, err := vm.state.GetDeposit(depositTx.ID())
	require.NoError(err)
	require.Zero(getUnlockedBalance(t, vm.state, treasury.Addr))
	require.Zero(getUnlockedBalance(t, vm.state, depositOwnerAddr))

	// Fast-forward clock to time a bit forward, but still before deposit will be unlocked
	vm.clock.Set(vm.Clock().Time().Add(time.Duration(deposit.Duration) * time.Second / 2))
	_, err = vm.Builder.BuildBlock(context.Background())
	require.Error(err)

	// Fast-forward clock to time for deposit to be unlocked
	vm.clock.Set(deposit.EndTime())
	blk := buildAndAcceptBlock(t, vm, nil)
	txID := blk.Txs()[0].ID()
	onAccept, ok := vm.manager.GetState(blk.ID())
	require.True(ok)
	_, txStatus, err := onAccept.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(err)
	require.Equal(status.Committed, txStatus)

	// Verify that the deposit is unlocked and reward is transferred to treasury
	_, err = vm.state.GetDeposit(depositTx.ID())
	require.ErrorIs(err, database.ErrNotFound)
	claimable, err := vm.state.GetClaimable(ownerID)
	require.NoError(err)
	require.Equal(&state.Claimable{
		Owner:                &depositOwner,
		ExpiredDepositReward: deposit.TotalReward(depositOffer),
	}, claimable)
	require.Equal(getUnlockedBalance(t, vm.state, depositOwnerAddr), depositOffer.MinAmount)
	require.Equal(deposit.EndTime(), vm.state.GetTimestamp())
	_, err = vm.state.GetNextToUnlockDepositTime(nil)
	require.ErrorIs(err, database.ErrNotFound)
}

func buildAndAcceptBlock(t *testing.T, vm *VM, tx *txs.Tx) blocks.Block {
	t.Helper()
	if tx != nil {
		require.NoError(t, vm.Builder.AddUnverifiedTx(tx))
	}
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(t, err)
	block, ok := blk.(blocks.Block)
	require.True(t, ok)
	require.NoError(t, blk.Verify(context.Background()))
	require.NoError(t, blk.Accept(context.Background()))
	require.NoError(t, vm.SetPreference(context.Background(), vm.manager.LastAccepted()))

	return block
}

func getUnlockedBalance(t *testing.T, db avax.UTXOReader, addr ids.ShortID) uint64 {
	t.Helper()
	utxos, err := avax.GetAllUTXOs(db, set.Set[ids.ShortID]{addr: struct{}{}})
	require.NoError(t, err)
	balance := uint64(0)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
			balance += out.Amount()
		}
	}
	return balance
}
