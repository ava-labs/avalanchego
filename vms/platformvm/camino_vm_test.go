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
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
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

	rootAdminKey := caminoPreFundedKeys[0]
	adminProposerKey := caminoPreFundedKeys[0]
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
		adminProposerKey.Address(),
		false,
		as.AddressStateBitRoleConsortiumAdminProposer,
		[]*secp256k1.PrivateKey{rootAdminKey},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	proposalTx := buildAddMemberProposalTx(
		t,
		vm,
		caminoPreFundedKeys[0],
		vm.Config.CaminoConfig.DACProposalBondAmount,
		defaultTxFee,
		adminProposerKey, // AdminProposer
		consortiumMemberKey.Address(),
		vm.clock.Time(),
		true,
	)
	_, _, _, _ = makeProposalWithTx(t, vm, proposalTx) // add admin proposal
	_ = buildAndAcceptBlock(t, vm, nil)                // execute admin proposal

	// Register node
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

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
		as.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

	// Verify that the validator is deferred (moved from current to deferred stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Verify that the validator's owner's deferred state and consortium member is true
	ownerState, _ := vm.state.GetAddressStates(consortiumMemberKey.Address())
	require.Equal(ownerState, as.AddressStateNodeDeferred|as.AddressStateConsortiumMember)

	// Fast-forward clock to time for validator to be rewarded
	vm.clock.Set(endTime)
	blk, err := vm.Builder.BuildBlock(context.Background())
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
	require.Equal(ownerState, as.AddressStateConsortiumMember)

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

	rootAdminKey := caminoPreFundedKeys[0]
	adminProposerKey := caminoPreFundedKeys[0]
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
		adminProposerKey.Address(),
		false,
		as.AddressStateBitRoleConsortiumAdminProposer,
		[]*secp256k1.PrivateKey{rootAdminKey},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	proposalTx := buildAddMemberProposalTx(
		t,
		vm,
		caminoPreFundedKeys[0],
		vm.Config.CaminoConfig.DACProposalBondAmount,
		defaultTxFee,
		adminProposerKey, // AdminProposer
		consortiumMemberKey.Address(),
		vm.clock.Time(),
		true,
	)
	_, _, _, _ = makeProposalWithTx(t, vm, proposalTx) // add admin proposal
	_ = buildAndAcceptBlock(t, vm, nil)                // execute admin proposal

	// Register node
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0], nodeKey, consortiumMemberKey},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

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
		as.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

	// Verify that the validator is deferred (moved from current to deferred stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)

	// Reactivate the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		true,
		as.AddressStateBitNodeDeferred,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		outputOwners,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

	// Verify that the validator is activated again (moved from deferred to current stakers set)
	_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	require.NoError(err)
	_, err = vm.state.GetDeferredValidator(constants.PrimaryNetworkID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Fast-forward clock to time for validator to be rewarded
	vm.clock.Set(endTime)
	blk, err := vm.Builder.BuildBlock(context.Background())
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

func TestProposals(t *testing.T) {
	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)
	proposerAddrStr, err := address.FormatBech32(constants.NetworkIDToHRP[testNetworkID], proposerAddr.Bytes())
	require.NoError(t, err)
	caminoPreFundedKey0AddrStr, err := address.FormatBech32(constants.NetworkIDToHRP[testNetworkID], caminoPreFundedKeys[0].Address().Bytes())
	require.NoError(t, err)

	defaultConfig := defaultCaminoConfig(true)
	proposalBondAmount := defaultConfig.CaminoConfig.DACProposalBondAmount
	newFee := (defaultTxFee + 7) * 10

	type vote struct {
		option  uint32
		success bool // proposal is successful after this vote
	}

	tests := map[string]struct {
		feeOptions    []uint64
		winningOption uint32
		earlyFinish   bool
		votes         []vote // no more than 5 votes, cause we have only 5 validators
	}{
		"Early success: 1|3 votes": {
			feeOptions:    []uint64{1, newFee},
			winningOption: 1,
			earlyFinish:   true,
			votes: []vote{
				{option: 1},
				{option: 1},
				{option: 0, success: true},
				{option: 1, success: true},
			},
		},
		"Early fail: 2|2|1 votes, not reaching mostVoted threshold and being ambiguous": {
			feeOptions:  []uint64{1, 2, 3},
			earlyFinish: true,
			votes: []vote{
				{option: 0},
				{option: 0},
				{option: 1, success: true},
				{option: 1},
				{option: 2},
			},
		},
		"Success: 0|2|1 votes": {
			feeOptions:    []uint64{1, newFee, 17},
			winningOption: 1,
			votes: []vote{
				{option: 1},
				{option: 1},
				{option: 2, success: true},
			},
		},
		"Fail: 0 votes": {
			feeOptions: []uint64{1},
			votes:      []vote{},
		},
		"Fail: 2|1|1 votes, not reaching mostVoted threshold": {
			feeOptions: []uint64{1, 2, 3},
			votes: []vote{
				{option: 0},
				{option: 0},
				{option: 1, success: true},
				{option: 2},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			balance := proposalBondAmount + defaultTxFee*(uint64(len(tt.votes))+1) + newFee

			// Prepare vm
			vm := newCaminoVM(api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        caminoPreFundedKeys[0].Address(),
			}, []api.UTXO{
				{
					Amount:  json.Uint64(balance),
					Address: proposerAddrStr,
				},
				{
					Amount:  json.Uint64(defaultTxFee),
					Address: caminoPreFundedKey0AddrStr,
				},
			}, &defaultConfig.BanffTime)
			vm.ctx.Lock.Lock()
			defer func() { require.NoError(vm.Shutdown(context.Background())) }() //nolint:lint
			checkBalance(t, vm.state, proposerAddr,
				balance,          // total
				0, 0, 0, balance, // unlocked
			)

			fee := defaultTxFee
			burnedAmt := uint64(0)

			// Give proposer address role to make proposals
			addrStateTx, err := vm.txBuilder.NewAddressStateTx(
				proposerAddr,
				false,
				as.AddressStateBitCaminoProposer,
				[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
				nil,
			)
			require.NoError(err)
			blk := buildAndAcceptBlock(t, vm, addrStateTx)
			require.Len(blk.Txs(), 1)
			checkTx(t, vm, blk.ID(), addrStateTx.ID())

			// Add proposal
			chainTime := vm.state.GetTimestamp()
			proposalTx := buildBaseFeeProposalTx(t, vm, proposerKey, proposalBondAmount, fee,
				proposerKey, tt.feeOptions, chainTime.Add(100*time.Second), chainTime.Add(200*time.Second))
			proposalState, nextProposalIDsToExpire, nexExpirationTime, proposalIDsToFinish := makeProposalWithTx(t, vm, proposalTx)
			baseFeeProposalState, ok := proposalState.(*dac.BaseFeeProposalState)
			require.True(ok)
			require.EqualValues(5, baseFeeProposalState.TotalAllowedVoters)   // all 5 validators must vote
			require.Equal([]ids.ID{proposalTx.ID()}, nextProposalIDsToExpire) // we have only one proposal
			require.Equal(proposalState.EndTime(), nexExpirationTime)
			require.Empty(proposalIDsToFinish) // no early-finished proposals
			burnedAmt += fee
			checkBalance(t, vm.state, proposerAddr,
				balance-burnedAmt,                          // total
				proposalBondAmount,                         // bonded
				0, 0, balance-proposalBondAmount-burnedAmt, // unlocked
			)

			// Fast-forward clock to time a bit forward, but still before proposals start
			// Try to vote on proposal, expect to fail
			vm.clock.Set(baseFeeProposalState.StartTime().Add(-time.Second))
			addVoteTx := buildSimpleVoteTx(t, vm, proposerKey, fee, proposalTx.ID(), caminoPreFundedKeys[0], 0)
			require.Error(vm.Builder.AddUnverifiedTx(addVoteTx))
			vm.clock.Set(baseFeeProposalState.StartTime())

			optionWeights := make([]uint32, len(baseFeeProposalState.Options))
			for i, vote := range tt.votes {
				optionWeights[vote.option]++
				voteTx := buildSimpleVoteTx(t, vm, proposerKey, fee, proposalTx.ID(), caminoPreFundedKeys[i], vote.option)
				proposalState = voteWithTx(t, vm, voteTx, proposalTx.ID(), optionWeights)
				proposalIDsToFinish, err = vm.state.GetProposalIDsToFinish()
				require.NoError(err)
				if tt.earlyFinish && i == len(tt.votes)-1 {
					require.Equal([]ids.ID{proposalTx.ID()}, proposalIDsToFinish) // proposal has finished early
				} else {
					require.Empty(proposalIDsToFinish) // no early-finished proposals
				}
				nextProposalIDsToExpire, nexExpirationTime, err := vm.state.GetNextToExpireProposalIDsAndTime(nil)
				require.NoError(err)
				require.Equal([]ids.ID{proposalTx.ID()}, nextProposalIDsToExpire)
				require.Equal(proposalState.EndTime(), nexExpirationTime)
				require.Equal(tt.earlyFinish && i == len(tt.votes)-1, proposalState.CanBeFinished())
				require.Equal(vote.success, proposalState.IsSuccessful())
				burnedAmt += fee
				checkBalance(t, vm.state, proposerAddr,
					balance-burnedAmt,                          // total
					proposalBondAmount,                         // bonded
					0, 0, balance-proposalBondAmount-burnedAmt, // unlocked
				)
			}

			if !tt.earlyFinish { // no early finish
				vm.clock.Set(proposalState.EndTime())
			}

			blk = buildAndAcceptBlock(t, vm, nil)
			require.Len(blk.Txs(), 1)
			checkTx(t, vm, blk.ID(), blk.Txs()[0].ID())
			_, err = vm.state.GetProposal(proposalTx.ID())
			require.ErrorIs(err, database.ErrNotFound)
			_, _, err = vm.state.GetNextToExpireProposalIDsAndTime(nil)
			require.ErrorIs(err, database.ErrNotFound)
			proposalIDsToFinish, err = vm.state.GetProposalIDsToFinish()
			require.NoError(err)
			require.Empty(proposalIDsToFinish)
			checkBalance(t, vm.state, proposerAddr,
				balance-burnedAmt,          // total
				0, 0, 0, balance-burnedAmt, // unlocked
			)

			if len(tt.votes) != 0 && tt.votes[len(tt.votes)-1].success { // last vote
				fee = tt.feeOptions[tt.winningOption]
				baseFee, err := vm.state.GetBaseFee()
				require.NoError(err)
				require.Equal(fee, baseFee) // fee has changed
			}

			// Create arbitrary tx to verify which fee is used
			buildAndAcceptBaseTx(t, vm, proposerKey, fee)
			burnedAmt += fee
			checkBalance(t, vm.state, proposerAddr,
				balance-burnedAmt,          // total
				0, 0, 0, balance-burnedAmt, // unlocked
			)
		})
	}
}

func TestAdminProposals(t *testing.T) {
	require := require.New(t)

	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)
	proposerAddrStr, err := address.FormatBech32(constants.NetworkIDToHRP[testNetworkID], proposerAddr.Bytes())
	require.NoError(err)
	caminoPreFundedKey0AddrStr, err := address.FormatBech32(constants.NetworkIDToHRP[testNetworkID], caminoPreFundedKeys[0].Address().Bytes())
	require.NoError(err)

	applicantAddr := proposerAddr

	defaultConfig := defaultCaminoConfig(true)
	proposalBondAmount := defaultConfig.CaminoConfig.DACProposalBondAmount
	balance := proposalBondAmount + defaultTxFee

	// Prepare vm
	vm := newCaminoVM(api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        caminoPreFundedKeys[0].Address(),
	}, []api.UTXO{
		{
			Amount:  json.Uint64(balance),
			Address: proposerAddrStr,
		},
		{
			Amount:  json.Uint64(defaultTxFee),
			Address: caminoPreFundedKey0AddrStr,
		},
	}, &defaultConfig.BanffTime)
	vm.ctx.Lock.Lock()
	defer func() { require.NoError(vm.Shutdown(context.Background())) }() //nolint:lint
	checkBalance(t, vm.state, proposerAddr,
		balance,          // total
		0, 0, 0, balance, // unlocked
	)

	fee := defaultTxFee
	burnedAmt := uint64(0)

	// Give proposer address role to make admin proposals
	addrStateTx, err := vm.txBuilder.NewAddressStateTx(
		proposerAddr,
		false,
		as.AddressStateBitRoleConsortiumAdminProposer,
		[]*secp256k1.PrivateKey{caminoPreFundedKeys[0]},
		nil,
	)
	require.NoError(err)
	blk := buildAndAcceptBlock(t, vm, addrStateTx)
	require.Len(blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), addrStateTx.ID())
	applicantAddrState, err := vm.state.GetAddressStates(applicantAddr)
	require.NoError(err)
	require.True(applicantAddrState.IsNot(as.AddressStateConsortiumMember))

	// Add admin proposal
	chainTime := vm.state.GetTimestamp()
	proposalTx := buildAddMemberProposalTx(t, vm, proposerKey, proposalBondAmount, fee,
		proposerKey, applicantAddr, chainTime.Add(100*time.Second), true)
	proposalState, nextProposalIDsToExpire, nexExpirationTime, proposalIDsToFinish := makeProposalWithTx(t, vm, proposalTx)
	addMemberProposalState, ok := proposalState.(*dac.AddMemberProposalState)
	require.True(ok)
	require.EqualValues(0, addMemberProposalState.TotalAllowedVoters) // its admin proposal
	require.Equal([]ids.ID{proposalTx.ID()}, nextProposalIDsToExpire) // we have only one proposal
	require.Equal(proposalState.EndTime(), nexExpirationTime)
	require.Equal([]ids.ID{proposalTx.ID()}, proposalIDsToFinish) // admin proposal must be immediately finished
	burnedAmt += fee
	checkBalance(t, vm.state, proposerAddr,
		balance-burnedAmt,                          // total
		proposalBondAmount,                         // bonded
		0, 0, balance-proposalBondAmount-burnedAmt, // unlocked
	)

	// build block that will execute proposal
	blk = buildAndAcceptBlock(t, vm, nil)
	require.Len(blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), blk.Txs()[0].ID())
	_, err = vm.state.GetProposal(proposalTx.ID())
	require.ErrorIs(err, database.ErrNotFound)
	_, _, err = vm.state.GetNextToExpireProposalIDsAndTime(nil)
	require.ErrorIs(err, database.ErrNotFound)
	proposalIDsToFinish, err = vm.state.GetProposalIDsToFinish()
	require.NoError(err)
	require.Empty(proposalIDsToFinish)
	checkBalance(t, vm.state, proposerAddr,
		balance-burnedAmt,          // total
		0, 0, 0, balance-burnedAmt, // unlocked
	)

	// check that applicant became c-member
	applicantAddrState, err = vm.state.GetAddressStates(applicantAddr)
	require.NoError(err)
	require.True(applicantAddrState.Is(as.AddressStateConsortiumMember))
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

func getBalance(t *testing.T, db avax.UTXOReader, addr ids.ShortID) (total, bonded, deposited, depositBonded, unlocked uint64) {
	t.Helper()
	utxos, err := avax.GetAllUTXOs(db, set.Set[ids.ShortID]{addr: struct{}{}})
	require.NoError(t, err)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
			unlocked += out.Amount()
			total += out.Amount()
		} else {
			out, ok := utxo.Out.(*locked.Out)
			require.True(t, ok)
			switch out.LockState() {
			case locked.StateDepositedBonded:
				depositBonded += out.Amount()
			case locked.StateDeposited:
				deposited += out.Amount()
			case locked.StateBonded:
				bonded += out.Amount()
			}
			total += out.Amount()
		}
	}
	return
}

func checkBalance(
	t *testing.T, db avax.UTXOReader, addr ids.ShortID,
	expectedTotal, expectedBonded, expectedDeposited, expectedDepositBonded, expectedUnlocked uint64, //nolint:unparam
) {
	t.Helper()
	total, bonded, deposited, depositBonded, unlocked := getBalance(t, db, addr)
	require.Equal(t, expectedTotal, total)
	require.Equal(t, expectedBonded, bonded)
	require.Equal(t, expectedDeposited, deposited)
	require.Equal(t, expectedDepositBonded, depositBonded)
	require.Equal(t, expectedUnlocked, unlocked)
}

func checkTx(t *testing.T, vm *VM, blkID, txID ids.ID) {
	t.Helper()
	state, ok := vm.manager.GetState(blkID)
	require.True(t, ok)
	_, txStatus, err := state.GetTx(txID)
	require.NoError(t, err)
	require.Equal(t, status.Committed, txStatus)
	_, txStatus, err = vm.state.GetTx(txID)
	require.NoError(t, err)
	require.Equal(t, status.Committed, txStatus)
}

func buildBaseFeeProposalTx(
	t *testing.T,
	vm *VM,
	fundsKey *secp256k1.PrivateKey,
	amountToBond uint64,
	amountToBurn uint64,
	proposerKey *secp256k1.PrivateKey,
	options []uint64,
	startTime time.Time,
	endTime time.Time,
) *txs.Tx {
	t.Helper()
	ins, outs, signers, _, err := vm.txBuilder.Lock(
		vm.state,
		[]*secp256k1.PrivateKey{fundsKey},
		amountToBond,
		amountToBurn,
		locked.StateBonded,
		nil, nil, 0,
	)
	require.NoError(t, err)
	proposal := &txs.ProposalWrapper{Proposal: &dac.BaseFeeProposal{
		Start:   uint64(startTime.Unix()),
		End:     uint64(endTime.Unix()),
		Options: options,
	}}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, proposal)
	require.NoError(t, err)
	proposalTx, err := txs.NewSigned(&txs.AddProposalTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		ProposalPayload: proposalBytes,
		ProposerAddress: proposerKey.Address(),
		ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
	}, txs.Codec, append(signers, []*secp256k1.PrivateKey{proposerKey}))
	require.NoError(t, err)
	return proposalTx
}

func buildAddMemberProposalTx(
	t *testing.T,
	vm *VM,
	fundsKey *secp256k1.PrivateKey,
	amountToBond uint64,
	amountToBurn uint64,
	proposerKey *secp256k1.PrivateKey,
	applicantAddress ids.ShortID,
	startTime time.Time,
	adminProposal bool,
) *txs.Tx {
	t.Helper()
	ins, outs, signers, _, err := vm.txBuilder.Lock(
		vm.state,
		[]*secp256k1.PrivateKey{fundsKey},
		amountToBond,
		amountToBurn,
		locked.StateBonded,
		nil, nil, 0,
	)
	require.NoError(t, err)
	var proposal dac.Proposal = &dac.AddMemberProposal{
		Start:            uint64(startTime.Unix()),
		End:              uint64(startTime.Unix()) + dac.AddMemberProposalDuration,
		ApplicantAddress: applicantAddress,
	}
	if adminProposal {
		proposal = &dac.AdminProposal{Proposal: proposal}
	}
	wrapper := &txs.ProposalWrapper{Proposal: proposal}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, wrapper)
	require.NoError(t, err)
	proposalTx, err := txs.NewSigned(&txs.AddProposalTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		ProposalPayload: proposalBytes,
		ProposerAddress: proposerKey.Address(),
		ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
	}, txs.Codec, append(signers, []*secp256k1.PrivateKey{proposerKey}))
	require.NoError(t, err)
	return proposalTx
}

func makeProposalWithTx(
	t *testing.T,
	vm *VM,
	tx *txs.Tx,
) (
	proposal dac.ProposalState,
	nextProposalIDsToExpire []ids.ID,
	nexExpirationTime time.Time,
	proposalIDsToFinish []ids.ID,
) {
	t.Helper()
	blk := buildAndAcceptBlock(t, vm, tx)
	require.Len(t, blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), tx.ID())
	proposalState, err := vm.state.GetProposal(tx.ID())
	require.NoError(t, err)
	nextProposalIDsToExpire, nexExpirationTime, err = vm.state.GetNextToExpireProposalIDsAndTime(nil)
	require.NoError(t, err)
	proposalIDsToFinish, err = vm.state.GetProposalIDsToFinish()
	require.NoError(t, err)
	return proposalState, nextProposalIDsToExpire, nexExpirationTime, proposalIDsToFinish
}

func buildSimpleVoteTx(
	t *testing.T,
	vm *VM,
	fundsKey *secp256k1.PrivateKey,
	amountToBurn uint64,
	proposalID ids.ID,
	voterKey *secp256k1.PrivateKey,
	votedOption uint32,
) *txs.Tx {
	t.Helper()
	ins, outs, signers, _, err := vm.txBuilder.Lock(
		vm.state,
		[]*secp256k1.PrivateKey{fundsKey},
		0,
		amountToBurn,
		locked.StateUnlocked,
		nil, nil, 0,
	)
	require.NoError(t, err)
	voteBytes, err := txs.Codec.Marshal(txs.Version, &txs.VoteWrapper{Vote: &dac.SimpleVote{OptionIndex: votedOption}})
	require.NoError(t, err)
	addVoteTx, err := txs.NewSigned(&txs.AddVoteTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.ctx.NetworkID,
			BlockchainID: vm.ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		ProposalID:   proposalID,
		VotePayload:  voteBytes,
		VoterAddress: voterKey.Address(),
		VoterAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
	}, txs.Codec, append(signers, []*secp256k1.PrivateKey{voterKey}))
	require.NoError(t, err)
	return addVoteTx
}

func voteWithTx(
	t *testing.T,
	vm *VM,
	tx *txs.Tx,
	proposalID ids.ID,
	expectedVoteWeights []uint32,
) (proposalState dac.ProposalState) {
	t.Helper()
	blk := buildAndAcceptBlock(t, vm, tx)
	require.Len(t, blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), tx.ID())
	proposalState, err := vm.state.GetProposal(proposalID)
	require.NoError(t, err)
	switch proposalState := proposalState.(type) {
	case *dac.BaseFeeProposalState:
		require.Len(t, proposalState.Options, len(expectedVoteWeights))
		for i := range proposalState.Options {
			require.Equal(t, expectedVoteWeights[i], proposalState.Options[i].Weight)
		}
	default:
		require.Fail(t, "unexpected proposalState type")
	}
	return proposalState
}

func buildAndAcceptBaseTx(
	t *testing.T,
	vm *VM,
	fundsKey *secp256k1.PrivateKey,
	amountToBurn uint64,
) {
	t.Helper()
	ins, outs, signers, _, err := vm.txBuilder.Lock(
		vm.state,
		[]*secp256k1.PrivateKey{fundsKey},
		0,
		amountToBurn,
		locked.StateUnlocked,
		nil, nil, 0,
	)
	require.NoError(t, err)
	feeTestingTx, err := txs.NewSigned(&txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		Ins:          ins,
		Outs:         outs,
	}}, txs.Codec, signers)
	require.NoError(t, err)
	blk := buildAndAcceptBlock(t, vm, feeTestingTx)
	require.Len(t, blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), feeTestingTx.ID())
}
