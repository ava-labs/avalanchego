// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/json"
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
	"github.com/ava-labs/avalanchego/vms/platformvm/test"
	"github.com/ava-labs/avalanchego/vms/platformvm/test/generate"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

func TestRemoveDeferredValidator(t *testing.T) {
	require := require.New(t)

	nodeKey, nodeID := test.Keys[0], ids.NodeID(test.Keys[0].Address())
	rootAdminKey := test.FundedKeys[0]
	consortiumSecretaryKey := test.FundedKeys[0]
	kycAdminKey := test.FundedKeys[0]
	validatorAdminKey := test.FundedKeys[0]
	fundsKey := test.FundedKeys[0]
	consortiumMemberKey := test.Keys[1]

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        rootAdminKey.Address(),
	}

	vm := newCaminoVM(t, caminoGenesisConf, test.PhaseLast, []api.UTXO{{
		Amount:  json.Uint64(test.ValidatorWeight * 2),
		Address: test.FundedKeysBech32[0],
	}})
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Set consortium member
	// set ConsortiumSecretary role
	tx, err := vm.txBuilder.NewAddressStateTx(
		consortiumSecretaryKey.Address(),
		false,
		as.AddressStateBitRoleConsortiumSecretary,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	// set kyc admin role
	tx, err = vm.txBuilder.NewAddressStateTx(
		kycAdminKey.Address(),
		false,
		as.AddressStateBitRoleKYCAdmin,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	// set kyc flag for test consortium member (not member yet)
	addrStateTx, err := vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		as.AddressStateBitKYCVerified,
		kycAdminKey.Address(),
		[]*secp256k1.PrivateKey{kycAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, addrStateTx)
	// make admin proposal to add consortium member
	proposalTx := buildAddMemberProposalTx(t, vm, consortiumSecretaryKey, vm.Config.CaminoConfig.DACProposalBondAmount, defaultTxFee,
		consortiumSecretaryKey, consortiumMemberKey.Address(), vm.clock.Time(), true)
	_, _, _, _ = makeProposalWithTx(t, vm, proposalTx) // add admin proposal
	_ = buildAndAcceptBlock(t, vm, nil)                // execute admin proposal

	// Register node for new consortium member
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*secp256k1.PrivateKey{fundsKey, nodeKey, consortiumMemberKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

	// Add validator for new consortium member and registered node
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
		[]*secp256k1.PrivateKey{fundsKey, consortiumMemberKey},
		ids.ShortEmpty,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, addValidatorTx)

	// Fast-forward clock to time for validator to be moved from pending to current
	vm.clock.Set(startTime)

	// Defer the validator
	// set validator admin role
	tx, err = vm.txBuilder.NewAddressStateTx(
		validatorAdminKey.Address(),
		false,
		as.AddressStateBitRoleValidatorAdmin,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	// defer validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		as.AddressStateBitNodeDeferred,
		validatorAdminKey.Address(),
		[]*secp256k1.PrivateKey{validatorAdminKey},
		nil,
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
	require.Equal(ownerState, as.AddressStateNodeDeferred|as.AddressStateConsortium|as.AddressStateKYCVerified)

	// Fast-forward clock to time for validator to be rewarded
	vm.clock.Set(endTime)
	blk, err := vm.Builder.BuildBlock(context.Background())
	require.NoError(err)
	err = blk.Verify(context.Background())
	require.NoError(err)

	// Assert preferences are correct
	options, err := blk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)

	commit := options[1].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[0].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
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
	require.Equal(ownerState, as.AddressStateConsortium|as.AddressStateKYCVerified)

	timestamp := vm.state.GetTimestamp()
	require.Equal(endTime.Unix(), timestamp.Unix())
}

func TestRemoveReactivatedValidator(t *testing.T) {
	require := require.New(t)

	nodeKey, nodeID := test.Keys[0], ids.NodeID(test.Keys[0].Address())
	fundsKey := test.FundedKeys[0]
	rootAdminKey := test.FundedKeys[0]
	kycAdminKey := test.FundedKeys[0]
	validatorAdminKey := test.FundedKeys[0]
	consortiumSecretaryKey := test.FundedKeys[0]
	consortiumMemberKey := test.Keys[1]

	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        rootAdminKey.Address(),
	}
	genesisUTXOs := []api.UTXO{
		{
			Amount:  json.Uint64(test.ValidatorWeight * 2),
			Address: test.FundedKeysBech32[0],
		},
	}

	vm := newCaminoVM(t, caminoGenesisConf, test.PhaseLast, genesisUTXOs)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Set consortium member
	// set consortium secretary role
	tx, err := vm.txBuilder.NewAddressStateTx(
		consortiumSecretaryKey.Address(),
		false,
		as.AddressStateBitRoleConsortiumSecretary,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	// set kyc admin role
	addrStateTx, err := vm.txBuilder.NewAddressStateTx(
		kycAdminKey.Address(),
		false,
		as.AddressStateBitRoleKYCAdmin,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, addrStateTx)
	// set kyc flag for test consortium member (not member yet)
	addrStateTx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		as.AddressStateBitKYCVerified,
		kycAdminKey.Address(),
		[]*secp256k1.PrivateKey{kycAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, addrStateTx)
	// make admin proposal to add consortium member
	proposalTx := buildAddMemberProposalTx(t, vm, consortiumSecretaryKey, vm.Config.CaminoConfig.DACProposalBondAmount, defaultTxFee,
		consortiumSecretaryKey, consortiumMemberKey.Address(), vm.clock.Time(), true)
	_, _, _, _ = makeProposalWithTx(t, vm, proposalTx) // add admin proposal
	_ = buildAndAcceptBlock(t, vm, nil)                // execute admin proposal

	// Register node for new consortium member
	tx, err = vm.txBuilder.NewRegisterNodeTx(
		ids.EmptyNodeID,
		nodeID,
		consortiumMemberKey.Address(),
		[]*secp256k1.PrivateKey{fundsKey, nodeKey, consortiumMemberKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)

	// Add validator for new consortium member and registered node
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
		[]*secp256k1.PrivateKey{fundsKey, nodeKey, consortiumMemberKey},
		ids.ShortEmpty,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, addValidatorTx)

	// Fast-forward clock to time for validator to be moved from pending to current
	vm.clock.Set(startTime)

	// Set validator admin role
	tx, err = vm.txBuilder.NewAddressStateTx(
		validatorAdminKey.Address(),
		false,
		as.AddressStateBitRoleValidatorAdmin,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	_ = buildAndAcceptBlock(t, vm, tx)
	// Defer the validator
	tx, err = vm.txBuilder.NewAddressStateTx(
		consortiumMemberKey.Address(),
		false,
		as.AddressStateBitNodeDeferred,
		validatorAdminKey.Address(),
		[]*secp256k1.PrivateKey{validatorAdminKey},
		nil,
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
		validatorAdminKey.Address(),
		[]*secp256k1.PrivateKey{validatorAdminKey},
		nil,
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
	options, err := blk.(snowman.OracleBlock).Options(context.Background())
	require.NoError(err)

	commit := options[1].(*blockexecutor.Block)
	_, ok := commit.Block.(*blocks.BanffCommitBlock)
	require.True(ok)

	abort := options[0].(*blockexecutor.Block)
	_, ok = abort.Block.(*blocks.BanffAbortBlock)
	require.True(ok)

	require.NoError(blk.Accept(context.Background()))
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

	depositOwnerKey, depositOwnerAddr, depositOwner := generate.KeyAndOwner(t, test.Keys[0])
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

	vm := newCaminoVM(t, caminoGenesisConf, test.PhaseLast, []api.UTXO{{
		Amount:  json.Uint64(depositOffer.MinAmount + defaultTxFee),
		Address: depositOwnerAddrBech32,
	}})
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

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
	proposerKey, proposerAddr := test.Keys[0], test.Keys[0].Address()

	defaultConfig := test.Config(t, test.PhaseLast)
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
			vm := newCaminoVM(t, api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        test.FundedKeys[0].Address(),
			}, test.PhaseCairo, []api.UTXO{ // TODO @evlekht replace with PhaseLast when cairo is added
				{
					Amount:  json.Uint64(balance),
					Address: test.KeysBech32[0],
				},
				{
					Amount:  json.Uint64(defaultTxFee),
					Address: test.FundedKeysBech32[0],
				},
			})
			vm.ctx.Lock.Lock()
			defer vm.ctx.Lock.Unlock()
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
				as.AddressStateBitFoundationAdmin,
				test.FundedKeys[0].Address(),
				[]*secp256k1.PrivateKey{test.FundedKeys[0]},
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
			addVoteTx := buildSimpleVoteTx(t, vm, proposerKey, fee, proposalTx.ID(), test.FundedKeys[0], 0)
			err = vm.Builder.AddUnverifiedTx(addVoteTx)
			require.ErrorIs(err, txexecutor.ErrProposalInactive)
			vm.clock.Set(baseFeeProposalState.StartTime())

			optionWeights := make([]uint32, len(baseFeeProposalState.Options))
			for i, vote := range tt.votes {
				optionWeights[vote.option]++
				voteTx := buildSimpleVoteTx(t, vm, proposerKey, fee, proposalTx.ID(), test.FundedKeys[i], vote.option)
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

	rootAdminKey := test.FundedKeys[0]
	kycAdminKey := test.FundedKeys[0]
	consortiumSecretaryKey := test.Keys[0]
	applicantAddr := consortiumSecretaryKey.Address()

	defaultConfig := test.Config(t, test.PhaseLast)
	proposalBondAmount := defaultConfig.CaminoConfig.DACProposalBondAmount
	balance := proposalBondAmount + defaultTxFee

	// Prepare vm
	vm := newCaminoVM(t, api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
		InitialAdmin:        rootAdminKey.Address(),
	}, test.PhaseLast, []api.UTXO{
		{
			Amount:  json.Uint64(balance),
			Address: test.KeysBech32[0],
		},
		{
			Amount:  json.Uint64(defaultTxFee * 2),
			Address: test.FundedKeysBech32[0],
		},
	})
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	checkBalance(t, vm.state, consortiumSecretaryKey.Address(),
		balance,          // total
		0, 0, 0, balance, // unlocked
	)

	fee := defaultTxFee
	burnedAmt := uint64(0)

	// Give proposer address role to make admin proposals
	addrStateTx, err := vm.txBuilder.NewAddressStateTx(
		consortiumSecretaryKey.Address(),
		false,
		as.AddressStateBitRoleConsortiumSecretary,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	blk := buildAndAcceptBlock(t, vm, addrStateTx)
	require.Len(blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), addrStateTx.ID())
	applicantAddrState, err := vm.state.GetAddressStates(applicantAddr)
	require.NoError(err)
	require.True(applicantAddrState.IsNot(as.AddressStateConsortium))

	// Set kyc admin role
	addrStateTx, err = vm.txBuilder.NewAddressStateTx(
		kycAdminKey.Address(),
		false,
		as.AddressStateBitRoleKYCAdmin,
		rootAdminKey.Address(),
		[]*secp256k1.PrivateKey{rootAdminKey},
		nil,
	)
	require.NoError(err)
	blk = buildAndAcceptBlock(t, vm, addrStateTx)
	require.Len(blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), addrStateTx.ID())
	// Make applicant (see admin proposal below) kyc-verified
	addrStateTx, err = vm.txBuilder.NewAddressStateTx(
		applicantAddr,
		false,
		as.AddressStateBitKYCVerified,
		kycAdminKey.Address(),
		[]*secp256k1.PrivateKey{kycAdminKey},
		nil,
	)
	require.NoError(err)
	blk = buildAndAcceptBlock(t, vm, addrStateTx)
	require.Len(blk.Txs(), 1)
	checkTx(t, vm, blk.ID(), addrStateTx.ID())
	applicantAddrState, err = vm.state.GetAddressStates(applicantAddr)
	require.NoError(err)
	require.True(applicantAddrState.Is(as.AddressStateKYCVerified))

	// Add admin proposal
	chainTime := vm.state.GetTimestamp()
	proposalTx := buildAddMemberProposalTx(t, vm, consortiumSecretaryKey, proposalBondAmount, fee,
		consortiumSecretaryKey, applicantAddr, chainTime.Add(100*time.Second), true)
	proposalState, nextProposalIDsToExpire, nexExpirationTime, proposalIDsToFinish := makeProposalWithTx(t, vm, proposalTx)
	addMemberProposalState, ok := proposalState.(*dac.AddMemberProposalState)
	require.True(ok)
	require.EqualValues(0, addMemberProposalState.TotalAllowedVoters) // its admin proposal
	require.Equal([]ids.ID{proposalTx.ID()}, nextProposalIDsToExpire) // we have only one proposal
	require.Equal(proposalState.EndTime(), nexExpirationTime)
	require.Equal([]ids.ID{proposalTx.ID()}, proposalIDsToFinish) // admin proposal must be immediately finished
	burnedAmt += fee
	checkBalance(t, vm.state, consortiumSecretaryKey.Address(),
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
	checkBalance(t, vm.state, consortiumSecretaryKey.Address(),
		balance-burnedAmt,          // total
		0, 0, 0, balance-burnedAmt, // unlocked
	)

	// check that applicant became c-member
	applicantAddrState, err = vm.state.GetAddressStates(applicantAddr)
	require.NoError(err)
	require.True(applicantAddrState.Is(as.AddressStateConsortium))
}

func TestExcludeMemberProposals(t *testing.T) {
	// member to exclude
	memberToExcludeKey, memberToExcludeAddr := test.Keys[0], test.Keys[0].Address()
	memberToExcludeNodeKey, memberToExcludeNodeShortID := test.Keys[1], test.Keys[1].Address()
	memberToExcludeNodeID := ids.NodeID(memberToExcludeNodeShortID)

	rootAdminKey := test.FundedKeys[0]
	kycAdminKey := test.FundedKeys[0]
	consortiumSecretaryKey := test.FundedKeys[0]
	proposerMemberKey := test.FundedKeys[0]
	fundsKey := test.FundedKeys[0]
	fundsAddr := test.FundedKeys[0].Address()

	defaultConfig := test.Config(t, test.PhaseLast)
	fee := defaultConfig.TxFee
	addValidatorFee := defaultConfig.AddPrimaryNetworkValidatorFee
	proposalBondAmount := defaultConfig.CaminoConfig.DACProposalBondAmount
	validatorBondAmount := defaultConfig.MaxValidatorStake // is equal to min
	initialBalance := test.PreFundedBalance + test.ValidatorWeight
	initialHeight := uint64(1)

	// for simplicity: member == member that will be excluded
	//                 proposer == member that creates excludeMember proposal
	tests := map[string]struct {
		moreExclude      bool // try to exclude member with additional proposal
		registerNode     bool // member has registered node
		currentValidator bool // member has current validator
		pendingValidator bool // member has pending validator
		expire           bool // means that proposal should expire, not early finish
		success          bool // doesn't mean that most voted option is "yes", just means that proposal was successfully voted with some option
		excluded         bool // means that most voted option is "yes", proposal is successful and member was excluded
	}{
		"Failed: tried to exclude with another proposal": {
			moreExclude: true,
		},
		"Excluded: no registered node": {
			success:  true,
			excluded: true,
		},
		"Excluded: no validators": {
			registerNode: true,
			success:      true,
			excluded:     true,
		},
		"Excluded: has pending validator": {
			registerNode:     true,
			pendingValidator: true,
			success:          true,
			excluded:         true,
		},
		"Excluded: has active validator": {
			registerNode:     true,
			currentValidator: true,
			success:          true,
			excluded:         true,
		},
		"Not excluded: no registered node": {
			success:  true,
			excluded: false,
		},
		"Not excluded: no validators": {
			registerNode: true,
			success:      true,
			excluded:     false,
		},
		"Not excluded: has pending validator": {
			registerNode:     true,
			pendingValidator: true,
			success:          true,
			excluded:         false,
		},
		"Not excluded: has active validator": {
			registerNode:     true,
			currentValidator: true,
			success:          true,
			excluded:         false,
		},
		"Not excluded: expire": {
			registerNode: true,
			expire:       true,
			excluded:     false,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			numberOfValidatorsInNetwork := 5
			if tt.currentValidator {
				numberOfValidatorsInNetwork++
			}
			balance := initialBalance
			burnedAmt := uint64(0)
			bondedAmt := validatorBondAmount // proposer validator's bond from genesis
			expectedHeight := initialHeight

			// Prepare vm
			vm := newCaminoVM(t, api.Camino{
				VerifyNodeSignature: true,
				LockModeBondDeposit: true,
				InitialAdmin:        rootAdminKey.Address(),
			}, test.PhaseLast, nil)
			vm.ctx.Lock.Lock()
			defer vm.ctx.Lock.Unlock()
			height, err := vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			checkBalance(t, vm.state, fundsAddr,
				balance,                 // total     // nothing is burned yet
				bondedAmt,               // bonded
				0, 0, balance-bondedAmt, // unlocked
			)

			// set ConsortiumSecretary role
			addrStateTx, err := vm.txBuilder.NewAddressStateTx(
				consortiumSecretaryKey.Address(),
				false,
				as.AddressStateBitRoleConsortiumSecretary,
				rootAdminKey.Address(),
				[]*secp256k1.PrivateKey{rootAdminKey, fundsKey},
				nil,
			)
			require.NoError(err)
			_ = buildAndAcceptBlock(t, vm, addrStateTx)
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			burnedAmt += fee
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-bondedAmt-burnedAmt, // unlocked
			)

			// Prepare member that will be excluded
			memberAddrState, err := vm.state.GetAddressStates(memberToExcludeAddr)
			require.NoError(err)
			require.Equal(as.AddressStateEmpty, memberAddrState)
			_, err = vm.state.GetShortIDLink(memberToExcludeAddr, state.ShortLinkKeyRegisterNode)
			require.ErrorIs(err, database.ErrNotFound)
			addrStateTx, err = vm.txBuilder.NewAddressStateTx(
				kycAdminKey.Address(),
				false,
				as.AddressStateBitRoleKYCAdmin,
				rootAdminKey.Address(),
				[]*secp256k1.PrivateKey{rootAdminKey},
				nil,
			)
			require.NoError(err)
			_ = buildAndAcceptBlock(t, vm, addrStateTx)
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			burnedAmt += fee
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-bondedAmt-burnedAmt, // unlocked
			)
			addrStateTx, err = vm.txBuilder.NewAddressStateTx(
				memberToExcludeAddr,
				false,
				as.AddressStateBitKYCVerified,
				kycAdminKey.Address(),
				[]*secp256k1.PrivateKey{kycAdminKey},
				nil,
			)
			require.NoError(err)
			_ = buildAndAcceptBlock(t, vm, addrStateTx)
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			burnedAmt += fee
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-bondedAmt-burnedAmt, // unlocked
			)

			addMemberProposalTx := buildAddMemberProposalTx(t, vm, fundsKey, proposalBondAmount, defaultTxFee,
				consortiumSecretaryKey, memberToExcludeAddr, vm.clock.Time(), true)
			_, _, _, _ = makeProposalWithTx(t, vm, addMemberProposalTx) // add admin proposal
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			burnedAmt += fee
			bondedAmt += proposalBondAmount
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-bondedAmt-burnedAmt, // unlocked
			)

			_ = buildAndAcceptBlock(t, vm, nil) // execute admin proposal
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			memberAddrState, err = vm.state.GetAddressStates(memberToExcludeAddr)
			require.NoError(err)
			require.Equal(as.AddressStateConsortium|as.AddressStateKYCVerified, memberAddrState)
			bondedAmt -= proposalBondAmount
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-bondedAmt-burnedAmt, // unlocked
			)

			// Register member's node
			if tt.registerNode {
				registerNodeTx, err := vm.txBuilder.NewRegisterNodeTx(
					ids.EmptyNodeID,
					memberToExcludeNodeID,
					memberToExcludeAddr,
					[]*secp256k1.PrivateKey{memberToExcludeKey, memberToExcludeNodeKey, fundsKey},
					nil,
				)
				require.NoError(err)
				blk := buildAndAcceptBlock(t, vm, registerNodeTx)
				expectedHeight++
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				require.Len(blk.Txs(), 1)
				checkTx(t, vm, blk.ID(), registerNodeTx.ID())
				registeredMemberNodeID, err := vm.state.GetShortIDLink(memberToExcludeAddr, state.ShortLinkKeyRegisterNode)
				require.NoError(err)
				require.Equal(memberToExcludeNodeShortID, registeredMemberNodeID)
				burnedAmt += fee
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-bondedAmt-burnedAmt, // unlocked
				)
			}

			chainTime := vm.state.GetTimestamp()
			pendingValidatorStartTime := chainTime.Add(120 * time.Second)
			currentValidatorStartTime := chainTime.Add(240 * time.Second)
			validatorsEndTime := currentValidatorStartTime.Add(defaultConfig.MinStakeDuration)

			// Add pending validator
			if tt.pendingValidator {
				addValidatorTx, err := vm.txBuilder.NewCaminoAddValidatorTx(
					validatorBondAmount,
					uint64(pendingValidatorStartTime.Unix()),
					uint64(validatorsEndTime.Unix()),
					memberToExcludeNodeID,
					memberToExcludeAddr,
					memberToExcludeAddr,
					0,
					[]*secp256k1.PrivateKey{memberToExcludeKey, fundsKey},
					fundsAddr,
				)
				require.NoError(err)
				blk := buildAndAcceptBlock(t, vm, addValidatorTx)
				expectedHeight++
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				require.Len(blk.Txs(), 1)
				checkTx(t, vm, blk.ID(), addValidatorTx.ID())
				_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.NoError(err)
				_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.ErrorIs(err, database.ErrNotFound)
				burnedAmt += addValidatorFee
				bondedAmt += validatorBondAmount
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-bondedAmt-burnedAmt, // unlocked
				)
			}

			// Add current validator
			if tt.currentValidator {
				// Add current validator as pending
				addValidatorTx, err := vm.txBuilder.NewCaminoAddValidatorTx(
					validatorBondAmount,
					uint64(currentValidatorStartTime.Unix()),
					uint64(validatorsEndTime.Unix()),
					memberToExcludeNodeID,
					memberToExcludeAddr,
					memberToExcludeAddr,
					0,
					[]*secp256k1.PrivateKey{memberToExcludeKey, fundsKey},
					fundsAddr,
				)
				require.NoError(err)
				blk := buildAndAcceptBlock(t, vm, addValidatorTx)
				expectedHeight++
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				require.Len(blk.Txs(), 1)
				checkTx(t, vm, blk.ID(), addValidatorTx.ID())
				_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.NoError(err)
				_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.ErrorIs(err, database.ErrNotFound)

				// Advance time, so pending validator will become current
				vm.clock.Set(currentValidatorStartTime)
				blk = buildAndAcceptBlock(t, vm, nil)
				expectedHeight++
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				require.Empty(blk.Txs())
				_, err = vm.state.GetPendingValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.ErrorIs(err, database.ErrNotFound)
				_, err = vm.state.GetCurrentValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
				require.NoError(err)
				burnedAmt += addValidatorFee
				bondedAmt += validatorBondAmount
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-bondedAmt-burnedAmt, // unlocked
				)
			}

			chainTime = vm.state.GetTimestamp()

			// Add proposal (one member proposes to exclude another member)
			proposalStartTime := chainTime.Add(100 * time.Second)
			proposalEndTime := proposalStartTime.Add(time.Duration(dac.ExcludeMemberProposalMinDuration) * time.Second)
			excludeMemberProposalTx := buildExcludeMemberProposalTx(t, vm, fundsKey, proposalBondAmount, fee,
				proposerMemberKey, memberToExcludeAddr, proposalStartTime, proposalEndTime, false)
			proposalState, _, _, _ := makeProposalWithTx(t, vm, excludeMemberProposalTx)
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			excludeMemberProposalState, ok := proposalState.(*dac.ExcludeMemberProposalState)
			require.True(ok)
			burnedAmt += fee
			bondedAmt += proposalBondAmount
			checkBalance(t, vm.state, fundsAddr,
				balance-burnedAmt,                 // total
				bondedAmt,                         // bonded
				0, 0, balance-burnedAmt-bondedAmt, // unlocked
			)

			vm.clock.Set(excludeMemberProposalState.StartTime())

			if tt.moreExclude {
				excludeMemberProposalTx := buildExcludeMemberProposalTx(t, vm, fundsKey, proposalBondAmount, fee,
					consortiumSecretaryKey, memberToExcludeAddr, proposalStartTime, proposalStartTime.Add(time.Duration(dac.ExcludeMemberProposalMinDuration)*time.Second), true)
				err = vm.Builder.AddUnverifiedTx(excludeMemberProposalTx)
				require.ErrorIs(err, txexecutor.ErrInvalidProposal)
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				return
			}

			// If we want proposal to succeed, pick option 0, to fail - option 1
			optionIndex := uint32(1)
			if tt.excluded {
				optionIndex = 0
			}
			optionWeights := make([]uint32, len(excludeMemberProposalState.Options))

			// If proposal should be finished early, than we're voting for it with enough votes
			numberOfVotesToSuccess := numberOfValidatorsInNetwork/2 + 1
			if tt.expire {
				numberOfVotesToSuccess = 0
			}

			for i := 0; i < numberOfVotesToSuccess; i++ {
				optionWeights[optionIndex]++
				voteTx := buildSimpleVoteTx(t, vm, fundsKey, fee, excludeMemberProposalTx.ID(), test.FundedKeys[i], optionIndex)
				proposalState = voteWithTx(t, vm, voteTx, excludeMemberProposalTx.ID(), optionWeights)
				expectedHeight++
				height, err = vm.GetCurrentHeight(context.Background())
				require.NoError(err)
				require.Equal(expectedHeight, height)
				burnedAmt += fee
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-burnedAmt-bondedAmt, // unlocked
				)
			}
			require.Equal(tt.success, proposalState.IsSuccessful())

			// If we need proposal to expire, advance time forward
			if tt.expire {
				vm.clock.Set(proposalState.EndTime())
			}

			// Build block with FinishProposalsTx
			blk := buildAndAcceptBlock(t, vm, nil)
			expectedHeight++
			height, err = vm.GetCurrentHeight(context.Background())
			require.NoError(err)
			require.Equal(expectedHeight, height)
			require.Len(blk.Txs(), 1)
			checkTx(t, vm, blk.ID(), blk.Txs()[0].ID())
			_, err = vm.state.GetProposal(excludeMemberProposalTx.ID())
			require.ErrorIs(err, database.ErrNotFound)

			bondedAmt -= proposalBondAmount
			memberAddrState, err = vm.state.GetAddressStates(memberToExcludeAddr)
			require.NoError(err)
			_, pendingValidatorErr := vm.state.GetPendingValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
			_, currentValidatorErr := vm.state.GetCurrentValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
			_, suspendedValidatorErr := vm.state.GetDeferredValidator(constants.PrimaryNetworkID, memberToExcludeNodeID)
			registeredMemberNodeID, registeredNodeErr := vm.state.GetShortIDLink(memberToExcludeAddr, state.ShortLinkKeyRegisterNode)
			if tt.excluded {
				require.Equal(as.AddressStateKYCVerified, memberAddrState)
				require.ErrorIs(pendingValidatorErr, database.ErrNotFound)
				require.ErrorIs(currentValidatorErr, database.ErrNotFound)
				require.ErrorIs(registeredNodeErr, database.ErrNotFound)
				if tt.currentValidator {
					require.NoError(suspendedValidatorErr)
				} else {
					require.ErrorIs(suspendedValidatorErr, database.ErrNotFound)
				}
				if tt.pendingValidator {
					bondedAmt -= validatorBondAmount
				}
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-burnedAmt-bondedAmt, // unlocked
				)
			} else {
				require.Equal(as.AddressStateConsortium|as.AddressStateKYCVerified, memberAddrState)
				if tt.pendingValidator {
					require.NoError(pendingValidatorErr)
				} else {
					require.ErrorIs(pendingValidatorErr, database.ErrNotFound)
				}
				if tt.currentValidator {
					require.NoError(currentValidatorErr)
				} else {
					require.ErrorIs(currentValidatorErr, database.ErrNotFound)
				}
				if tt.registerNode {
					require.NoError(registeredNodeErr)
					require.Equal(memberToExcludeNodeShortID, registeredMemberNodeID)
				} else {
					require.ErrorIs(registeredNodeErr, database.ErrNotFound)
				}
				require.ErrorIs(suspendedValidatorErr, database.ErrNotFound)
				checkBalance(t, vm.state, fundsAddr,
					balance-burnedAmt,                 // total
					bondedAmt,                         // bonded
					0, 0, balance-burnedAmt-bondedAmt, // unlocked
				)
			}
		})
	}
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
	require.Equal(t, expectedTotal, total, "total balance (expected: %d, actual: %d)", expectedTotal, total)
	require.Equal(t, expectedBonded, bonded, "bonded balance (expected: %d, actual: %d)", expectedBonded, bonded)
	require.Equal(t, expectedDeposited, deposited, "deposited balance (expected: %d, actual: %d)", expectedDeposited, deposited)
	require.Equal(t, expectedDepositBonded, depositBonded, "depositBonded balance (expected: %d, actual: %d)", expectedDepositBonded, depositBonded)
	require.Equal(t, expectedUnlocked, unlocked, "unlocked balance (expected: %d, actual: %d)", expectedUnlocked, unlocked)
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
	adminProposal bool, //nolint:unparam
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

func buildExcludeMemberProposalTx(
	t *testing.T,
	vm *VM,
	fundsKey *secp256k1.PrivateKey,
	amountToBond uint64,
	amountToBurn uint64,
	proposerKey *secp256k1.PrivateKey,
	memberAddress ids.ShortID,
	startTime time.Time,
	endTime time.Time,
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
	var proposal dac.Proposal = &dac.ExcludeMemberProposal{
		Start:         uint64(startTime.Unix()),
		End:           uint64(endTime.Unix()),
		MemberAddress: memberAddress,
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
	case *dac.ExcludeMemberProposalState:
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
