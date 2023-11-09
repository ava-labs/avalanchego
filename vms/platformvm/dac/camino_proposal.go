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
	errWrongDuration              = errors.New("wrong proposal duration")
	ErrWrongVote                  = errors.New("this proposal can't be voted with this vote")
	ErrNotAllowedToVoteOnProposal = errors.New("this address has already voted or not allowed to vote on this proposal")
)

type VerifierVisitor interface {
	BaseFeeProposal(*BaseFeeProposal) error
	AddMemberProposal(*AddMemberProposal) error
}

type ExecutorVisitor interface {
	BaseFeeProposal(*BaseFeeProposalState) error
	AddMemberProposal(*AddMemberProposalState) error
}

type Proposal interface {
	verify.Verifiable

	StartTime() time.Time
	EndTime() time.Time
	CreateProposalState(allowedVoters []ids.ShortID) ProposalState
	Visit(VerifierVisitor) error

	// Returns proposal options. (used in magellan)
	//
	// We want to keep it in caminogo even if its used only in magellan,
	// cause it could contain internal proposal logic.
	// E.g. Add member proposal doesn't have options, but it hardcode-generates them.
	// We don't want to care about that in magellan.
	GetOptions() any
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

	// Returns modified proposal with added vote ignoring allowed voters, original proposal will not be modified!
	// (used in magellan)
	//
	// We want to keep it in caminogo even if its used only in magellan,
	// cause it contains internal proposal logic that affects success state.
	// We don't want to care about that in magellan.
	ForceAddVote(voteIntf Vote) (ProposalState, error)
}
