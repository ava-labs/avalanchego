// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestProposalVerifierBaseFeeProposal(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, _, feeOwner := generateKeyAndOwner(t)
	bondOwnerKey, _, bondOwner := generateKeyAndOwner(t)
	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)

	proposalBondAmt := uint64(100)
	feeUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	bondUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 6}, ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, ids.Empty)

	proposal := &txs.ProposalWrapper{Proposal: &dac.BaseFeeProposal{End: 1, Options: []uint64{1}}}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, proposal)
	require.NoError(t, err)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generateTestInFromUTXO(feeUTXO, []uint32{0}),
			generateTestInFromUTXO(bondUTXO, []uint32{0}),
		},
		Outs: []*avax.TransferableOutput{
			generateTestOut(ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, locked.ThisTxID),
		},
	}}

	tests := map[string]struct {
		state       func(*gomock.Controller, *txs.AddProposalTx) *state.MockDiff
		utx         func() *txs.AddProposalTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Proposer isn't caminoProposer": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateEmpty, nil) // not AddressStateCaminoProposer
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errNotPermittedToCreateProposal,
		},
		"Already active BaseFeeProposal": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(true)
				proposalsIterator.EXPECT().Value().Return(&dac.BaseFeeProposalState{}, nil)
				proposalsIterator.EXPECT().Release()

				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateCaminoProposer, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errAlreadyActiveProposal,
		},
		"OK": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateCaminoProposer, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			txExecutor := CaminoStandardTxExecutor{StandardTxExecutor{
				Backend: &env.backend,
				State:   tt.state(ctrl, utx),
				Tx:      tx,
			}}

			proposal, err := utx.Proposal()
			require.NoError(t, err)
			err = proposal.Visit(txExecutor.proposalVerifier(utx))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalExecutorBaseFeeProposal(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	tests := map[string]struct {
		state       func(*gomock.Controller) *state.MockDiff
		proposal    dac.ProposalState
		expectedErr error
	}{
		"OK": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().SetBaseFee(uint64(123))
				return s
			},
			proposal: &dac.BaseFeeProposalState{
				SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{Options: []dac.SimpleVoteOption[uint64]{
					{Value: 555, Weight: 0},
					{Value: 123, Weight: 2},
					{Value: 7, Weight: 1},
				}},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			txExecutor := CaminoStandardTxExecutor{StandardTxExecutor{
				Backend: &env.backend,
				State:   tt.state(ctrl),
			}}

			err := tt.proposal.Visit(txExecutor.proposalExecutor())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalVerifierAddMemberProposal(t *testing.T) {
	ctx, _ := defaultCtx(nil)
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	feeOwnerKey, _, feeOwner := generateKeyAndOwner(t)
	bondOwnerKey, _, bondOwner := generateKeyAndOwner(t)
	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)
	applicantAddress := ids.ShortID{1}

	proposalBondAmt := uint64(100)
	feeUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	bondUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 6}, ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, ids.Empty)

	proposal := &txs.ProposalWrapper{Proposal: &dac.AddMemberProposal{End: 1, ApplicantAddress: applicantAddress}}
	proposalBytes, err := txs.Codec.Marshal(txs.Version, proposal)
	require.NoError(t, err)

	baseTx := txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    ctx.NetworkID,
		BlockchainID: ctx.ChainID,
		Ins: []*avax.TransferableInput{
			generateTestInFromUTXO(feeUTXO, []uint32{0}),
			generateTestInFromUTXO(bondUTXO, []uint32{0}),
		},
		Outs: []*avax.TransferableOutput{
			generateTestOut(ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, locked.ThisTxID),
		},
	}}

	tests := map[string]struct {
		state       func(*gomock.Controller, *txs.AddProposalTx) *state.MockDiff
		utx         func() *txs.AddProposalTx
		signers     [][]*secp256k1.PrivateKey
		expectedErr error
	}{
		"Applicant address is consortium member": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateConsortiumMember, nil)
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errConsortiumMember,
		},
		"Already active AddMemberProposal for this applicant": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(true)
				proposalsIterator.EXPECT().Value().Return(&dac.AddMemberProposalState{ApplicantAddress: applicantAddress}, nil)
				proposalsIterator.EXPECT().Release()

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateEmpty, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
			expectedErr: errAlreadyActiveProposal,
		},
		"OK": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateEmpty, nil)
				s.EXPECT().GetProposalIterator().Return(proposalsIterator, nil)
				return s
			},
			utx: func() *txs.AddProposalTx {
				return &txs.AddProposalTx{
					BaseTx:          baseTx,
					ProposalPayload: proposalBytes,
					ProposerAddress: proposerAddr,
					ProposerAuth:    &secp256k1fx.Input{SigIndices: []uint32{0}},
				}
			},
			signers: [][]*secp256k1.PrivateKey{
				{feeOwnerKey}, {bondOwnerKey}, {proposerKey},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			txExecutor := CaminoStandardTxExecutor{StandardTxExecutor{
				Backend: &env.backend,
				State:   tt.state(ctrl, utx),
				Tx:      tx,
			}}

			proposal, err := utx.Proposal()
			require.NoError(t, err)
			err = proposal.Visit(txExecutor.proposalVerifier(utx))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalExecutorAddMemberProposal(t *testing.T) {
	caminoGenesisConf := api.Camino{
		VerifyNodeSignature: true,
		LockModeBondDeposit: true,
	}

	applicantAddress := ids.ShortID{1}
	applicantAddressState := as.AddressStateCaminoProposer // just not empty

	tests := map[string]struct {
		state       func(*gomock.Controller) *state.MockDiff
		proposal    dac.ProposalState
		expectedErr error
	}{
		"OK: rejected": {
			state: state.NewMockDiff,
			proposal: &dac.AddMemberProposalState{
				ApplicantAddress: applicantAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true, Weight: 1},
					{Value: false, Weight: 2},
				}},
			},
		},
		"OK: accepted": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(applicantAddress).Return(applicantAddressState, nil)
				s.EXPECT().SetAddressStates(applicantAddress, applicantAddressState|as.AddressStateConsortiumMember)
				return s
			},
			proposal: &dac.AddMemberProposalState{
				ApplicantAddress: applicantAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true, Weight: 2},
					{Value: false, Weight: 1},
				}},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			env := newCaminoEnvironmentWithMocks(caminoGenesisConf, nil)
			defer func() { require.NoError(t, shutdownCaminoEnvironment(env)) }()

			txExecutor := CaminoStandardTxExecutor{StandardTxExecutor{
				Backend: &env.backend,
				State:   tt.state(ctrl),
			}}

			err := tt.proposal.Visit(txExecutor.proposalExecutor())
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
