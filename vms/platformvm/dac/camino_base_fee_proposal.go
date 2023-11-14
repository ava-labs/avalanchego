// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"golang.org/x/exp/slices"
)

const baseFeeProposalMaxOptionsCount = 3

var (
	_ Proposal      = (*BaseFeeProposal)(nil)
	_ ProposalState = (*BaseFeeProposalState)(nil)

	errZeroFee           = errors.New("base-fee option is zero")
	errWrongOptionsCount = errors.New("wrong options count")
)

type BaseFeeProposal struct {
	Options []uint64 `serialize:"true"` // New base fee options
	Start   uint64   `serialize:"true"` // Start time of proposal
	End     uint64   `serialize:"true"` // End time of proposal
}

func (p *BaseFeeProposal) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *BaseFeeProposal) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (p *BaseFeeProposal) GetOptions() any {
	return p.Options
}

func (*BaseFeeProposal) AdminProposer() as.AddressState {
	return as.AddressStateEmpty // for now its forbidden, until we'll introduce dedicated role for it
}

func (p *BaseFeeProposal) Verify() error {
	switch {
	case len(p.Options) > baseFeeProposalMaxOptionsCount:
		return fmt.Errorf("%w (expected: no more than %d, actual: %d)", errWrongOptionsCount, baseFeeProposalMaxOptionsCount, len(p.Options))
	case p.Start >= p.End:
		return errEndNotAfterStart
	}
	for _, fee := range p.Options {
		if fee == 0 {
			return errZeroFee
		}
	}
	return nil
}

func (p *BaseFeeProposal) CreateProposalState(allowedVoters []ids.ShortID) ProposalState {
	stateProposal := &BaseFeeProposalState{
		SimpleVoteOptions: SimpleVoteOptions[uint64]{
			Options: make([]SimpleVoteOption[uint64], len(p.Options)),
		},
		Start:              p.Start,
		End:                p.End,
		AllowedVoters:      allowedVoters,
		TotalAllowedVoters: uint32(len(allowedVoters)),
	}
	for i := range p.Options {
		stateProposal.Options[i].Value = p.Options[i]
	}
	return stateProposal
}

func (p *BaseFeeProposal) CreateFinishedProposalState(optionIndex uint32) (ProposalState, error) {
	if optionIndex >= uint32(len(p.Options)) {
		return nil, fmt.Errorf("%w (expected: less than %d, actual: %d)", errWrongOptionIndex, len(p.Options), optionIndex)
	}
	proposalState := p.CreateProposalState([]ids.ShortID{}).(*BaseFeeProposalState)
	proposalState.Options[optionIndex].Weight++
	return proposalState, nil
}

func (p *BaseFeeProposal) Visit(visitor VerifierVisitor) error {
	return visitor.BaseFeeProposal(p)
}

type BaseFeeProposalState struct {
	SimpleVoteOptions[uint64] `serialize:"true"` // New base fee options
	// Start time of proposal
	Start uint64 `serialize:"true"`
	// End time of proposal
	End uint64 `serialize:"true"`
	// Addresses that are allowed to vote for this proposal
	AllowedVoters []ids.ShortID `serialize:"true"`
	// Number of addresses that were initially allowed to vote for this proposal.
	// This is used to calculate thresholds like "half of total voters".
	TotalAllowedVoters uint32 `serialize:"true"`
}

func (p *BaseFeeProposalState) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *BaseFeeProposalState) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (p *BaseFeeProposalState) IsActiveAt(time time.Time) bool {
	timestamp := uint64(time.Unix())
	return p.Start <= timestamp && timestamp <= p.End
}

func (p *BaseFeeProposalState) CanBeFinished() bool {
	mostVotedWeight, _, unambiguous := p.GetMostVoted()
	voted := p.Voted()
	return voted == p.TotalAllowedVoters || unambiguous && mostVotedWeight > p.TotalAllowedVoters/2
}

func (p *BaseFeeProposalState) IsSuccessful() bool {
	mostVotedWeight, _, unambiguous := p.GetMostVoted()
	voted := p.Voted()
	return unambiguous && voted > p.TotalAllowedVoters/2 && mostVotedWeight > voted/2
}

func (p *BaseFeeProposalState) Outcome() any {
	_, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	if !unambiguous {
		return -1
	}
	return mostVotedOptionIndex
}

// Votes must be valid for this proposal, could panic otherwise.
func (p *BaseFeeProposalState) Result() (uint64, uint32, bool) {
	mostVotedWeight, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	return p.Options[mostVotedOptionIndex].Value, mostVotedWeight, unambiguous
}

// Will return modified proposal with added vote, original proposal will not be modified!
func (p *BaseFeeProposalState) AddVote(voterAddress ids.ShortID, voteIntf Vote) (ProposalState, error) {
	vote, ok := voteIntf.(*SimpleVote)
	if !ok {
		return nil, ErrWrongVote
	}
	if int(vote.OptionIndex) >= len(p.Options) {
		return nil, ErrWrongVote
	}

	voterAddrPos, allowedToVote := slices.BinarySearchFunc(p.AllowedVoters, voterAddress, func(id, other ids.ShortID) int {
		return bytes.Compare(id[:], other[:])
	})
	if !allowedToVote {
		return nil, ErrNotAllowedToVoteOnProposal
	}

	updatedProposal := &BaseFeeProposalState{
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: make([]ids.ShortID, len(p.AllowedVoters)-1),
		SimpleVoteOptions: SimpleVoteOptions[uint64]{
			Options: make([]SimpleVoteOption[uint64], len(p.Options)),
		},
		TotalAllowedVoters: p.TotalAllowedVoters,
	}
	// we can't use the same slice, cause we need to change its elements
	copy(updatedProposal.AllowedVoters, p.AllowedVoters[:voterAddrPos])
	updatedProposal.AllowedVoters = append(updatedProposal.AllowedVoters[:voterAddrPos], p.AllowedVoters[voterAddrPos+1:]...)
	// we can't use the same slice, cause we need to change its element
	copy(updatedProposal.Options, p.Options)
	updatedProposal.Options[vote.OptionIndex].Weight++
	return updatedProposal, nil
}

// Will return modified proposal with added vote ignoring allowed voters, original proposal will not be modified!
func (p *BaseFeeProposalState) ForceAddVote(voteIntf Vote) (ProposalState, error) {
	vote, ok := voteIntf.(*SimpleVote)
	if !ok {
		return nil, ErrWrongVote
	}
	if int(vote.OptionIndex) >= len(p.Options) {
		return nil, ErrWrongVote
	}

	updatedProposal := &BaseFeeProposalState{
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: p.AllowedVoters,
		SimpleVoteOptions: SimpleVoteOptions[uint64]{
			Options: make([]SimpleVoteOption[uint64], len(p.Options)),
		},
		TotalAllowedVoters: p.TotalAllowedVoters,
	}
	// we can't use the same slice, cause we need to change its element
	copy(updatedProposal.Options, p.Options)
	updatedProposal.Options[vote.OptionIndex].Weight++
	return updatedProposal, nil
}

func (p *BaseFeeProposalState) Visit(visitor ExecutorVisitor) error {
	return visitor.BaseFeeProposal(p)
}
