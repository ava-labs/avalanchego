// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errEndNotAfterStart           = errors.New("proposal end-time is not after start-time")
	ErrWrongVote                  = errors.New("this proposal can't be voted with this vote")
	ErrNotAllowedToVoteOnProposal = errors.New("this address has already voted or not allowed to vote on this proposal")
)

type VerifierVisitor interface {
	BaseFeeProposal(*BaseFeeProposal) error
}

type ExecutorVisitor interface {
	BaseFeeProposal(*BaseFeeProposalState) error
}

type Proposal interface {
	verify.Verifiable

	StartTime() time.Time
	EndTime() time.Time
	GetOptions() any
	CreateProposalState(allowedVoters []ids.ShortID) ProposalState
	Visit(VerifierVisitor) error
}

type ProposalState interface {
	verify.Verifiable

	EndTime() time.Time
	IsActiveAt(time time.Time) bool
	// Once a proposal has become Finishable, it cannot be undone by adding more votes.
	CanBeFinished() bool
	IsSuccessful() bool // should be called only for finished proposals
	Outcome() any       // should be called only for finished successful proposals
	Visit(ExecutorVisitor) error
	// Will return modified ProposalState with added vote, original ProposalState will not be modified!
	AddVote(voterAddress ids.ShortID, vote Vote) (ProposalState, error)

	// Will return modified proposal with added vote ignoring allowed voters, original proposal will not be modified!
	// (used in magellan)
	ForceAddVote(voterAddress ids.ShortID, voteIntf Vote) (ProposalState, error)
}
