// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ dac.Verifier        = (*proposalVerifier)(nil)
	_ dac.Executor        = (*proposalExecutor)(nil)
	_ dac.BondTxIDsGetter = (*proposalBondTxIDsGetter)(nil)

	errNotConsortiumMember          = errors.New("address isn't consortium member")
	errConsortiumMember             = errors.New("address is consortium member")
	errNotPermittedToCreateProposal = errors.New("don't have permission to create proposal of this type")
	errAlreadyActiveProposal        = errors.New("there is already active proposal of this type")
)

type proposalVerifier struct {
	state               state.Chain
	fx                  fx.Fx
	signedAddProposalTx *txs.Tx
	addProposalTx       *txs.AddProposalTx
}

// Executor calls should never error.
// We should always mind possible proposals conflict, when implementing proposal execution logic.
// Because when proposal is semantically verified, state doesn't know about changes
// that already existing proposals will bring into state on their execution.
// And proposal execution is a system tx, so it should always succeed.
type proposalExecutor struct {
	state state.Chain
	fx    fx.Fx
}

// We should always mind possible proposals conflict, when implementing proposal execution logic.
// Because when proposal is semantically verified, state doesn't know about changes
// that already existing proposals will bring into state on their execution.
// And proposal execution is a system tx, so it should always succeed.
type proposalBondTxIDsGetter struct {
	state state.Chain
}

func NewProposalVerifier(state state.Chain, fx fx.Fx, signedTx *txs.Tx, tx *txs.AddProposalTx) dac.Verifier {
	return &proposalVerifier{
		state:               state,
		fx:                  fx,
		signedAddProposalTx: signedTx,
		addProposalTx:       tx,
	}
}

func NewProposalExecutor(state state.Chain, fx fx.Fx) dac.Executor {
	return &proposalExecutor{state: state, fx: fx}
}

func GetBondTxIDs(state state.Chain, tx *txs.FinishProposalsTx) ([]ids.ID, error) {
	return getBondTxIDs(&proposalBondTxIDsGetter{state: state}, state, tx)
}

// so we can test it with mock
func getBondTxIDs(bondTxIDsGetter dac.BondTxIDsGetter, state state.Chain, tx *txs.FinishProposalsTx) ([]ids.ID, error) {
	bondTxIDs := tx.ProposalIDs()
	successfulProposalIDs := tx.SuccessfulProposalIDs()
	for _, proposalID := range successfulProposalIDs {
		proposal, err := state.GetProposal(proposalID)
		if err != nil {
			return nil, err
		}
		lockTxIDs, err := proposal.GetBondTxIDsWith(bondTxIDsGetter)
		if err != nil {
			return nil, err
		}
		bondTxIDs = append(bondTxIDs, lockTxIDs...)
	}
	return bondTxIDs, nil
}

// BaseFeeProposal

func (e *proposalVerifier) BaseFeeProposal(*dac.BaseFeeProposal) error {
	// verify address state (role)
	proposerAddressState, err := e.state.GetAddressStates(e.addProposalTx.ProposerAddress)
	if err != nil {
		return err
	}

	if proposerAddressState.IsNot(as.AddressStateCaminoProposer) {
		return errNotPermittedToCreateProposal
	}

	// verify that there is no existing base fee proposal
	proposalsIterator, err := e.state.GetProposalIterator()
	if err != nil {
		return err
	}
	defer proposalsIterator.Release()
	for proposalsIterator.Next() {
		proposal, err := proposalsIterator.Value()
		if err != nil {
			return err
		}
		if _, ok := proposal.(*dac.BaseFeeProposalState); ok {
			return errAlreadyActiveProposal
		}
	}

	if err := proposalsIterator.Error(); err != nil {
		return err
	}

	return nil
}

func (e *proposalExecutor) BaseFeeProposal(proposal *dac.BaseFeeProposalState) error {
	_, mostVotedOptionIndex, _ := proposal.GetMostVoted()
	e.state.SetBaseFee(proposal.Options[mostVotedOptionIndex].Value)
	return nil
}

func (*proposalBondTxIDsGetter) BaseFeeProposal(*dac.BaseFeeProposalState) ([]ids.ID, error) {
	return nil, nil
}

// AddMemberProposal

func (e *proposalVerifier) AddMemberProposal(proposal *dac.AddMemberProposal) error {
	// verify that address isn't consortium member
	applicantAddress, err := e.state.GetAddressStates(proposal.ApplicantAddress)
	if err != nil {
		return err
	}

	if applicantAddress.Is(as.AddressStateConsortiumMember) {
		return errConsortiumMember
	}

	// verify that there is no existing add member proposal for this address
	proposalsIterator, err := e.state.GetProposalIterator()
	if err != nil {
		return err
	}
	defer proposalsIterator.Release()
	for proposalsIterator.Next() {
		existingProposal, err := proposalsIterator.Value()
		if err != nil {
			return err
		}
		addMemberProposal, ok := existingProposal.(*dac.AddMemberProposalState)
		if ok && addMemberProposal.ApplicantAddress == proposal.ApplicantAddress {
			return errAlreadyActiveProposal
		}
	}

	if err := proposalsIterator.Error(); err != nil {
		return err
	}

	return nil
}

func (e *proposalExecutor) AddMemberProposal(proposal *dac.AddMemberProposalState) error {
	if accepted, _, _ := proposal.Result(); !accepted {
		return nil
	}

	addrState, err := e.state.GetAddressStates(proposal.ApplicantAddress)
	if err != nil {
		return err
	}
	newAddrState := addrState | as.AddressStateConsortiumMember
	if newAddrState == addrState {
		// c-member was somehow already added
		// this should never happen, cause adding only done via addMemberProposal
		// and only one addMemberProposal per applicant can exist at the same time
		return nil
	}
	e.state.SetAddressStates(proposal.ApplicantAddress, newAddrState)
	return nil
}

func (*proposalBondTxIDsGetter) AddMemberProposal(*dac.AddMemberProposalState) ([]ids.ID, error) {
	return nil, nil
}

// ExcludeMemberProposal

func (e *proposalVerifier) ExcludeMemberProposal(proposal *dac.ExcludeMemberProposal) error {
	// verify that address is consortium member
	memberAddressState, err := e.state.GetAddressStates(proposal.MemberAddress)
	if err != nil {
		return err
	}

	if memberAddressState.IsNot(as.AddressStateConsortiumMember) {
		return errNotConsortiumMember
	}

	// verify that there is no existing exclude member proposal for this address
	proposalsIterator, err := e.state.GetProposalIterator()
	if err != nil {
		return err
	}
	defer proposalsIterator.Release()
	for proposalsIterator.Next() {
		existingProposal, err := proposalsIterator.Value()
		if err != nil {
			return err
		}
		excludeMemberProposal, ok := existingProposal.(*dac.ExcludeMemberProposalState)
		if ok && excludeMemberProposal.MemberAddress == proposal.MemberAddress {
			return errAlreadyActiveProposal
		}
	}

	if err := proposalsIterator.Error(); err != nil {
		return err
	}

	return nil
}

func (e *proposalExecutor) ExcludeMemberProposal(proposal *dac.ExcludeMemberProposalState) error {
	if accepted, _, _ := proposal.Result(); !accepted {
		return nil
	}

	// set address state
	addrState, err := e.state.GetAddressStates(proposal.MemberAddress)
	if err != nil {
		return err
	}
	newAddrState := addrState ^ as.AddressStateConsortiumMember
	if newAddrState == addrState {
		// c-member was somehow already excluded
		// this should never happen, cause excluding only done via excludeMemberProposal
		// and only one excludeMemberProposal per member can exist at the same time
		return nil
	}
	e.state.SetAddressStates(proposal.MemberAddress, newAddrState)

	// get member nodeID
	nodeShortID, err := e.state.GetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode)
	switch {
	case err == database.ErrNotFound:
		// member doesn't have node, so we don't need to do anything with it
		return nil
	case err != nil:
		return nil
	}
	nodeID := ids.NodeID(nodeShortID)

	// remove member node registration
	e.state.SetShortIDLink(nodeShortID, state.ShortLinkKeyRegisterNode, nil)
	e.state.SetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode, nil)

	// transfer validator to from current to deferred validator set
	validator, err := e.state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
	switch {
	case err == nil:
		e.state.DeleteCurrentValidator(validator)
		e.state.PutDeferredValidator(validator)
	case err != database.ErrNotFound:
		return err
	}

	// remove pending validator
	pendingValidator, err := e.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	switch {
	case err == nil:
		e.state.DeletePendingValidator(pendingValidator)
	case err != database.ErrNotFound:
		return err
	}

	return nil
}

func (e *proposalBondTxIDsGetter) ExcludeMemberProposal(proposal *dac.ExcludeMemberProposalState) ([]ids.ID, error) {
	if accepted, _, _ := proposal.Result(); !accepted {
		return nil, nil
	}

	nodeShortID, err := e.state.GetShortIDLink(proposal.MemberAddress, state.ShortLinkKeyRegisterNode)
	switch {
	case err == database.ErrNotFound: // member doesn't have node
		return nil, nil
	case err != nil:
		return nil, err
	}
	nodeID := ids.NodeID(nodeShortID)

	pendingValidator, err := e.state.GetPendingValidator(constants.PrimaryNetworkID, nodeID)
	switch {
	case err == database.ErrNotFound: // member doesn't have pending validator
		return nil, nil
	case err != nil:
		return nil, err
	}

	return []ids.ID{pendingValidator.TxID}, nil
}
