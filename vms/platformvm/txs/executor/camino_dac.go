// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ dac.VerifierVisitor = (*proposalVerifier)(nil)
	_ dac.ExecutorVisitor = (*proposalExecutor)(nil)

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

func (e *CaminoStandardTxExecutor) proposalVerifier(tx *txs.AddProposalTx) *proposalVerifier {
	return &proposalVerifier{
		state:               e.State,
		fx:                  e.Fx,
		signedAddProposalTx: e.Tx,
		addProposalTx:       tx,
	}
}

func (e *CaminoStandardTxExecutor) proposalExecutor() *proposalExecutor {
	return &proposalExecutor{state: e.State, fx: e.Fx}
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
	if accepted, _, _ := proposal.Result(); accepted {
		addrState, err := e.state.GetAddressStates(proposal.ApplicantAddress)
		if err != nil {
			return err
		}
		newAddrState := addrState | as.AddressStateConsortiumMember
		if newAddrState == addrState { // c-member was already added via admin action after proposal creation
			return nil
		}
		e.state.SetAddressStates(proposal.ApplicantAddress, newAddrState)
	}
	return nil
}
