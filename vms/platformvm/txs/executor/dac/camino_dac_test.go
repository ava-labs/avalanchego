// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestProposalVerifierBaseFeeProposal(t *testing.T) {
	ctx := defaultCtx(nil)

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
		state           func(*gomock.Controller, *txs.AddProposalTx) *state.MockDiff
		utx             func() *txs.AddProposalTx
		signers         [][]*secp256k1.PrivateKey
		isAdminProposal bool
		expectedErr     error
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
			fx := defaultFx(true)

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			proposal, err := utx.Proposal()
			require.NoError(t, err)
			err = proposal.VerifyWith(NewProposalVerifier(tt.state(ctrl, utx), fx, tx, utx, tt.isAdminProposal))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalExecutorBaseFeeProposal(t *testing.T) {
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
			fx := defaultFx(true)
			err := tt.proposal.ExecuteWith(NewProposalExecutor(tt.state(ctrl), fx))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalVerifierAddMemberProposal(t *testing.T) {
	ctx := defaultCtx(nil)

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
		state           func(*gomock.Controller, *txs.AddProposalTx) *state.MockDiff
		utx             func() *txs.AddProposalTx
		signers         [][]*secp256k1.PrivateKey
		isAdminProposal bool
		expectedErr     error
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
		"Applicant address is not kyc verified": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateEmpty, nil)
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
			expectedErr: errNotKYCVerified,
		},
		"Already active AddMemberProposal for this applicant": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(true)
				proposalsIterator.EXPECT().Value().Return(&dac.AddMemberProposalState{ApplicantAddress: applicantAddress}, nil)
				proposalsIterator.EXPECT().Release()

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateKYCVerified, nil)
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

				s.EXPECT().GetAddressStates(applicantAddress).Return(as.AddressStateKYCVerified, nil)
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
			fx := defaultFx(true)

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			proposal, err := utx.Proposal()
			require.NoError(t, err)
			err = proposal.VerifyWith(NewProposalVerifier(tt.state(ctrl, utx), fx, tx, utx, tt.isAdminProposal))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalExecutorAddMemberProposal(t *testing.T) {
	applicantAddress := ids.ShortID{1}
	applicantAddressState := as.AddressStateCaminoProposer // just not empty

	tests := map[string]struct {
		state       func(*gomock.Controller) *state.MockDiff
		proposal    dac.ProposalState
		expectedErr error
	}{
		"OK": {
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
			fx := defaultFx(true)
			err := tt.proposal.ExecuteWith(NewProposalExecutor(tt.state(ctrl), fx))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalVerifierExcludeMemberProposal(t *testing.T) {
	ctx := defaultCtx(nil)

	feeOwnerKey, _, feeOwner := generateKeyAndOwner(t)
	bondOwnerKey, _, bondOwner := generateKeyAndOwner(t)
	proposerKey, proposerAddr, _ := generateKeyAndOwner(t)
	memberAddress := ids.ShortID{1}
	memberNodeShortID := ids.ShortID{2}
	memberNodeID := ids.NodeID(memberNodeShortID)
	memberValidator := &state.Staker{TxID: ids.ID{3}}

	proposalBondAmt := uint64(100)
	feeUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 5}, ctx.AVAXAssetID, defaultTxFee, feeOwner, ids.Empty, ids.Empty)
	bondUTXO := generateTestUTXO(ids.ID{1, 2, 3, 4, 6}, ctx.AVAXAssetID, proposalBondAmt, bondOwner, ids.Empty, ids.Empty)

	proposal := &txs.ProposalWrapper{Proposal: &dac.ExcludeMemberProposal{End: 1, MemberAddress: memberAddress}}
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
		state           func(*gomock.Controller, *txs.AddProposalTx) *state.MockDiff
		utx             func() *txs.AddProposalTx
		signers         [][]*secp256k1.PrivateKey
		isAdminProposal bool
		expectedErr     error
	}{
		"Member-to-exclude is not consortium member": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateEmpty, nil)
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
			expectedErr: errNotConsortiumMember,
		},
		"Proposer is not consortium member": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateEmpty, nil)
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
			expectedErr: errNotConsortiumMember,
		},
		"Proposer doesn't have registered node": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.ProposerAddress, state.ShortLinkKeyRegisterNode).Return(ids.ShortEmpty, database.ErrNotFound)
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
			expectedErr: errNoActiveValidator,
		},
		"Proposer doesn't have active validator": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.ProposerAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(nil, database.ErrNotFound)
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
			expectedErr: errNoActiveValidator,
		},
		"Already active ExcludeMemberProposal for this member": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(true)
				proposalsIterator.EXPECT().Value().Return(&dac.ExcludeMemberProposalState{MemberAddress: memberAddress}, nil)
				proposalsIterator.EXPECT().Release()

				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.ProposerAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
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
		"OK: admin proposal": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
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
			isAdminProposal: true,
		},
		"OK": {
			state: func(c *gomock.Controller, utx *txs.AddProposalTx) *state.MockDiff {
				s := state.NewMockDiff(c)
				proposalsIterator := state.NewMockProposalsIterator(c)
				proposalsIterator.EXPECT().Next().Return(false)
				proposalsIterator.EXPECT().Release()
				proposalsIterator.EXPECT().Error().Return(nil)

				s.EXPECT().GetAddressStates(memberAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetAddressStates(utx.ProposerAddress).Return(as.AddressStateConsortiumMember, nil)
				s.EXPECT().GetShortIDLink(utx.ProposerAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
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
			fx := defaultFx(true)

			utx := tt.utx()
			avax.SortTransferableInputsWithSigners(utx.Ins, tt.signers)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec)
			tx, err := txs.NewSigned(utx, txs.Codec, tt.signers)
			require.NoError(t, err)

			proposal, err := utx.Proposal()
			require.NoError(t, err)
			err = proposal.VerifyWith(NewProposalVerifier(tt.state(ctrl, utx), fx, tx, utx, tt.isAdminProposal))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalExecutorExcludeMemberProposal(t *testing.T) {
	memberAddress := ids.ShortID{1}
	memberAddressState := as.AddressStateCaminoProposer | as.AddressStateConsortiumMember // just not only c-member
	memberNodeShortID := ids.ShortID{2}
	memberNodeID := ids.NodeID(memberNodeShortID)
	memberValidator := &state.Staker{TxID: ids.ID{3}}

	tests := map[string]struct {
		state       func(*gomock.Controller) *state.MockDiff
		proposal    dac.ProposalState
		expectedErr error
	}{
		"OK": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(memberAddressState, nil)
				s.EXPECT().SetAddressStates(memberAddress, memberAddressState^as.AddressStateConsortiumMember)
				s.EXPECT().GetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
				s.EXPECT().DeleteCurrentValidator(memberValidator)
				s.EXPECT().PutDeferredValidator(memberValidator)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
				s.EXPECT().DeletePendingValidator(memberValidator)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{
					Options: []dac.SimpleVoteOption[bool]{{Value: true, Weight: 1}, {Value: false}},
				},
			},
		},
		"OK: no pending validator": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(memberAddressState, nil)
				s.EXPECT().SetAddressStates(memberAddress, memberAddressState^as.AddressStateConsortiumMember)
				s.EXPECT().GetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
				s.EXPECT().DeleteCurrentValidator(memberValidator)
				s.EXPECT().PutDeferredValidator(memberValidator)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, memberNodeID).Return(nil, database.ErrNotFound)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{
					Options: []dac.SimpleVoteOption[bool]{{Value: true, Weight: 1}, {Value: false}},
				},
			},
		},
		"OK: no current validator": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(memberAddressState, nil)
				s.EXPECT().SetAddressStates(memberAddress, memberAddressState^as.AddressStateConsortiumMember)
				s.EXPECT().GetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, memberNodeID).Return(memberValidator, nil)
				s.EXPECT().DeletePendingValidator(memberValidator)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{
					Options: []dac.SimpleVoteOption[bool]{{Value: true, Weight: 1}, {Value: false}},
				},
			},
		},
		"OK: no validators": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(memberAddressState, nil)
				s.EXPECT().SetAddressStates(memberAddress, memberAddressState^as.AddressStateConsortiumMember)
				s.EXPECT().GetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode).Return(memberNodeShortID, nil)
				s.EXPECT().SetShortIDLink(memberNodeShortID, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().SetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode, nil)
				s.EXPECT().GetCurrentValidator(constants.PrimaryNetworkID, memberNodeID).Return(nil, database.ErrNotFound)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, memberNodeID).Return(nil, database.ErrNotFound)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{
					Options: []dac.SimpleVoteOption[bool]{{Value: true, Weight: 1}, {Value: false}},
				},
			},
		},
		"OK: no registered node": {
			state: func(c *gomock.Controller) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetAddressStates(memberAddress).Return(memberAddressState, nil)
				s.EXPECT().SetAddressStates(memberAddress, memberAddressState^as.AddressStateConsortiumMember)
				s.EXPECT().GetShortIDLink(memberAddress, state.ShortLinkKeyRegisterNode).Return(ids.ShortEmpty, database.ErrNotFound)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{
					Options: []dac.SimpleVoteOption[bool]{{Value: true, Weight: 1}, {Value: false}},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			fx := defaultFx(true)
			err := tt.proposal.ExecuteWith(NewProposalExecutor(tt.state(ctrl), fx))
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestProposalBondTxIDsGetterExcludeMemberProposal(t *testing.T) {
	memberAddress := ids.ShortID{1}
	memberNodeShortID := ids.ShortID{2}
	memberValidatorTxID := ids.ID{3}

	tests := map[string]struct {
		state             func(*gomock.Controller, *dac.ExcludeMemberProposalState) *state.MockDiff
		proposal          *dac.ExcludeMemberProposalState
		expectedBondTxIDs []ids.ID
		expectedErr       error
	}{
		"OK: not excluded": {
			state: func(c *gomock.Controller, proposal *dac.ExcludeMemberProposalState) *state.MockDiff {
				s := state.NewMockDiff(c)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true},
					{Value: false, Weight: 1},
				}},
			},
		},
		"OK": {
			state: func(c *gomock.Controller, proposal *dac.ExcludeMemberProposalState) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID)).
					Return(&state.Staker{TxID: memberValidatorTxID}, nil)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true, Weight: 1},
					{Value: false},
				}},
			},
			expectedBondTxIDs: []ids.ID{memberValidatorTxID},
		},
		"OK: no pending validator": {
			state: func(c *gomock.Controller, proposal *dac.ExcludeMemberProposalState) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(memberNodeShortID, nil)
				s.EXPECT().GetPendingValidator(constants.PrimaryNetworkID, ids.NodeID(memberNodeShortID)).
					Return(nil, database.ErrNotFound)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true, Weight: 1},
					{Value: false},
				}},
			},
		},
		"OK: no registered node": {
			state: func(c *gomock.Controller, proposal *dac.ExcludeMemberProposalState) *state.MockDiff {
				s := state.NewMockDiff(c)
				s.EXPECT().GetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode).
					Return(ids.ShortEmpty, database.ErrNotFound)
				return s
			},
			proposal: &dac.ExcludeMemberProposalState{
				MemberAddress: memberAddress,
				SimpleVoteOptions: dac.SimpleVoteOptions[bool]{Options: []dac.SimpleVoteOption[bool]{
					{Value: true, Weight: 1},
					{Value: false},
				}},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			bondTxIDs, err := tt.proposal.GetBondTxIDsWith(&proposalBondTxIDsGetter{tt.state(ctrl, tt.proposal)})
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedBondTxIDs, bondTxIDs)
		})
	}
}

func TestGetBondTxIDs(t *testing.T) {
	earlyFinishedSuccessfulProposalWithAdditionalBondID := ids.ID{1}
	earlyFinishedFailedProposalWithAdditionalBondID := ids.ID{2}
	expiredSuccessfulProposalWithAdditionalBondID := ids.ID{3}
	expiredFailedProposalWithAdditionalBondID := ids.ID{4}

	earlyFinishedSuccessfulProposalID := ids.ID{5}
	earlyFinishedFailedProposalID := ids.ID{6}
	expiredSuccessfulProposalID := ids.ID{7}
	expiredFailedProposalID := ids.ID{8}

	additionalBondTxID1 := ids.ID{9}
	additionalBondTxID2 := ids.ID{10}
	additionalBondTxID3 := ids.ID{11}

	earlyFinishedSuccessfulProposalWithAdditionalBond := &dac.ExcludeMemberProposalState{TotalAllowedVoters: 1}
	expiredSuccessfulProposalWithAdditionalBond := &dac.ExcludeMemberProposalState{TotalAllowedVoters: 3}
	earlyFinishedSuccessfulProposal := &dac.ExcludeMemberProposalState{TotalAllowedVoters: 5}
	expiredSuccessfulProposal := &dac.ExcludeMemberProposalState{TotalAllowedVoters: 7}

	finishProposalsTx := &txs.FinishProposalsTx{
		EarlyFinishedSuccessfulProposalIDs: []ids.ID{earlyFinishedSuccessfulProposalWithAdditionalBondID, earlyFinishedSuccessfulProposalID},
		EarlyFinishedFailedProposalIDs:     []ids.ID{earlyFinishedFailedProposalWithAdditionalBondID, earlyFinishedFailedProposalID},
		ExpiredSuccessfulProposalIDs:       []ids.ID{expiredSuccessfulProposalWithAdditionalBondID, expiredSuccessfulProposalID},
		ExpiredFailedProposalIDs:           []ids.ID{expiredFailedProposalWithAdditionalBondID, expiredFailedProposalID},
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := state.NewMockDiff(ctrl)

	state.EXPECT().GetProposal(earlyFinishedSuccessfulProposalWithAdditionalBondID).Return(earlyFinishedSuccessfulProposalWithAdditionalBond, nil)
	state.EXPECT().GetProposal(earlyFinishedSuccessfulProposalID).Return(earlyFinishedSuccessfulProposal, nil)
	state.EXPECT().GetProposal(expiredSuccessfulProposalWithAdditionalBondID).Return(expiredSuccessfulProposalWithAdditionalBond, nil)
	state.EXPECT().GetProposal(expiredSuccessfulProposalID).Return(expiredSuccessfulProposal, nil)

	getter := dac.NewMockBondTxIDsGetter(ctrl)
	getter.EXPECT().ExcludeMemberProposal(earlyFinishedSuccessfulProposalWithAdditionalBond).Return([]ids.ID{additionalBondTxID1, additionalBondTxID2}, nil)
	getter.EXPECT().ExcludeMemberProposal(earlyFinishedSuccessfulProposal).Return([]ids.ID{}, nil)
	getter.EXPECT().ExcludeMemberProposal(expiredSuccessfulProposalWithAdditionalBond).Return([]ids.ID{additionalBondTxID3}, nil)
	getter.EXPECT().ExcludeMemberProposal(expiredSuccessfulProposal).Return([]ids.ID{}, nil)

	bondTxIDs, err := getBondTxIDs(getter, state, finishProposalsTx)
	require.NoError(t, err)
	require.Equal(
		t,
		append(finishProposalsTx.ProposalIDs(), additionalBondTxID1, additionalBondTxID2, additionalBondTxID3),
		bondTxIDs)
}
