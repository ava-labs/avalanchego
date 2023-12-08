// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"golang.org/x/exp/slices"
)

const (
	ExcludeMemberProposalMinDuration = uint64(time.Hour * 24 * 7 / time.Second)  // 7 days
	ExcludeMemberProposalMaxDuration = uint64(time.Hour * 24 * 30 / time.Second) // 30 days
)

var (
	_ Proposal      = (*ExcludeMemberProposal)(nil)
	_ ProposalState = (*ExcludeMemberProposalState)(nil)
)

type ExcludeMemberProposal struct {
	MemberAddress ids.ShortID `serialize:"true"`
	Start         uint64      `serialize:"true"`
	End           uint64      `serialize:"true"`
}

func (p *ExcludeMemberProposal) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *ExcludeMemberProposal) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (*ExcludeMemberProposal) GetOptions() any {
	return []bool{true, false}
}

func (p *ExcludeMemberProposal) GetData() any {
	return p.MemberAddress
}

func (*ExcludeMemberProposal) AdminProposer() as.AddressState {
	return as.AddressStateRoleConsortiumAdminProposer
}

func (p *ExcludeMemberProposal) Verify() error {
	switch {
	case p.Start >= p.End:
		return errEndNotAfterStart
	case p.End-p.Start < ExcludeMemberProposalMinDuration:
		return fmt.Errorf("%w (expected: minimum duration %d, actual: %d)", errWrongDuration, ExcludeMemberProposalMinDuration, p.End-p.Start)
	case p.End-p.Start > ExcludeMemberProposalMaxDuration:
		return fmt.Errorf("%w (expected: maximum duration %d, actual: %d)", errWrongDuration, ExcludeMemberProposalMaxDuration, p.End-p.Start)
	}
	return nil
}

func (p *ExcludeMemberProposal) CreateProposalState(allowedVoters []ids.ShortID) ProposalState {
	stateProposal := &ExcludeMemberProposalState{
		SimpleVoteOptions: SimpleVoteOptions[bool]{
			Options: []SimpleVoteOption[bool]{
				{Value: true},
				{Value: false},
			},
		},
		MemberAddress:      p.MemberAddress,
		Start:              p.Start,
		End:                p.End,
		AllowedVoters:      allowedVoters,
		TotalAllowedVoters: uint32(len(allowedVoters)),
	}
	return stateProposal
}

func (p *ExcludeMemberProposal) CreateFinishedProposalState(optionIndex uint32) (ProposalState, error) {
	if optionIndex >= 2 {
		return nil, fmt.Errorf("%w (expected: less than 2, actual: %d)", errWrongOptionIndex, optionIndex)
	}
	proposalState := p.CreateProposalState([]ids.ShortID{}).(*ExcludeMemberProposalState)
	proposalState.Options[optionIndex].Weight++
	return proposalState, nil
}

func (p *ExcludeMemberProposal) VerifyWith(verifier Verifier) error {
	return verifier.ExcludeMemberProposal(p)
}

type ExcludeMemberProposalState struct {
	SimpleVoteOptions[bool] `serialize:"true"`

	MemberAddress      ids.ShortID   `serialize:"true"`
	Start              uint64        `serialize:"true"`
	End                uint64        `serialize:"true"`
	AllowedVoters      []ids.ShortID `serialize:"true"`
	TotalAllowedVoters uint32        `serialize:"true"`
}

func (p *ExcludeMemberProposalState) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *ExcludeMemberProposalState) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (p *ExcludeMemberProposalState) IsActiveAt(time time.Time) bool {
	timestamp := uint64(time.Unix())
	return p.Start <= timestamp && timestamp <= p.End
}

func (p *ExcludeMemberProposalState) CanBeFinished() bool {
	mostVotedWeight, _, unambiguous := p.GetMostVoted()
	voted := p.Voted()
	// We don't check for 'no option can reach 50%+ of votes' for this proposal type, cause its impossible with just 2 options
	return voted == p.TotalAllowedVoters ||
		unambiguous && mostVotedWeight > p.TotalAllowedVoters/2
}

func (p *ExcludeMemberProposalState) IsSuccessful() bool {
	mostVotedWeight, _, unambiguous := p.GetMostVoted()
	voted := p.Voted()
	return unambiguous && voted > p.TotalAllowedVoters/2 && mostVotedWeight > voted/2
}

func (p *ExcludeMemberProposalState) Outcome() any {
	_, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	if !unambiguous {
		return -1
	}
	return mostVotedOptionIndex
}

func (p *ExcludeMemberProposalState) Result() (bool, uint32, bool) {
	mostVotedWeight, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	return p.Options[mostVotedOptionIndex].Value, mostVotedWeight, unambiguous
}

// Will return modified proposal with added vote, original proposal will not be modified!
func (p *ExcludeMemberProposalState) AddVote(voterAddress ids.ShortID, voteIntf Vote) (ProposalState, error) {
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

	updatedProposal := &ExcludeMemberProposalState{
		MemberAddress: p.MemberAddress,
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: make([]ids.ShortID, len(p.AllowedVoters)-1),
		SimpleVoteOptions: SimpleVoteOptions[bool]{
			Options: make([]SimpleVoteOption[bool], len(p.Options)),
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
func (p *ExcludeMemberProposalState) ForceAddVote(voteIntf Vote) (ProposalState, error) {
	vote, ok := voteIntf.(*SimpleVote)
	if !ok {
		return nil, ErrWrongVote
	}
	if int(vote.OptionIndex) >= len(p.Options) {
		return nil, ErrWrongVote
	}

	updatedProposal := &ExcludeMemberProposalState{
		MemberAddress: p.MemberAddress,
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: p.AllowedVoters,
		SimpleVoteOptions: SimpleVoteOptions[bool]{
			Options: make([]SimpleVoteOption[bool], len(p.Options)),
		},
		TotalAllowedVoters: p.TotalAllowedVoters,
	}
	// we can't use the same slice, cause we need to change its element
	copy(updatedProposal.Options, p.Options)
	updatedProposal.Options[vote.OptionIndex].Weight++
	return updatedProposal, nil
}

func (p *ExcludeMemberProposalState) ExecuteWith(executor Executor) error {
	return executor.ExcludeMemberProposal(p)
}

func (p *ExcludeMemberProposalState) GetBondTxIDsWith(bondTxIDsGetter BondTxIDsGetter) ([]ids.ID, error) {
	return bondTxIDsGetter.ExcludeMemberProposal(p)
}
