// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
)

var (
	errWrongOptionIndex           = errors.New("wrong option index")
	errEndNotAfterStart           = errors.New("proposal end-time is not after start-time")
	errWrongDuration              = errors.New("wrong proposal duration")
	ErrWrongVote                  = errors.New("this proposal can't be voted with this vote")
	ErrNotAllowedToVoteOnProposal = errors.New("this address has already voted or not allowed to vote on this proposal")

	_ Proposal = (*AdminProposal)(nil)
)

type Verifier interface {
	BaseFeeProposal(*BaseFeeProposal) error
	AddMemberProposal(*AddMemberProposal) error
	ExcludeMemberProposal(*ExcludeMemberProposal) error
}

type Executor interface {
	BaseFeeProposal(*BaseFeeProposalState) error
	AddMemberProposal(*AddMemberProposalState) error
	ExcludeMemberProposal(*ExcludeMemberProposalState) error
}

type BondTxIDsGetter interface {
	BaseFeeProposal(*BaseFeeProposalState) ([]ids.ID, error)
	AddMemberProposal(*AddMemberProposalState) ([]ids.ID, error)
	ExcludeMemberProposal(*ExcludeMemberProposalState) ([]ids.ID, error)
}

type Proposal interface {
	verify.Verifiable

	StartTime() time.Time
	EndTime() time.Time
	// AddressStateEmpty means that this proposal can't be used as admin proposal
	AdminProposer() as.AddressState
	CreateProposalState(allowedVoters []ids.ShortID) ProposalState
	CreateFinishedProposalState(optionIndex uint32) (ProposalState, error)
	VerifyWith(Verifier) error

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
	ExecuteWith(Executor) error
	// Visits getter and returns additional lock tx ids, that should be unbonded when this proposal is successfully finished.
	GetBondTxIDsWith(BondTxIDsGetter) ([]ids.ID, error)
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

type AdminProposal struct {
	OptionIndex uint32 `serialize:"true"`
	Proposal    `serialize:"true"`
}
