// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ dac.VerifierVisitor = (*proposalVerifier)(nil)
	_ dac.ExecutorVisitor = (*proposalExecutor)(nil)

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

	if proposerAddressState.IsNot(txs.AddressStateCaminoProposer) {
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

// should never error
func (e *proposalExecutor) BaseFeeProposal(proposal *dac.BaseFeeProposalState) error {
	_, mostVotedOptionIndex, _ := proposal.GetMostVoted()
	e.state.SetBaseFee(proposal.Options[mostVotedOptionIndex].Value)
	return nil
}
