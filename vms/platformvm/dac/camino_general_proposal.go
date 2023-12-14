// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package dac

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/types"
	"golang.org/x/exp/slices"
)

const (
	generalProposalMaxOptionsCount = 3
	generalProposalMaxOptionSize   = 256
	generalProposalMinDuration     = uint64(time.Hour * 24 / time.Second)      // 1 day
	generalProposalMaxDuration     = uint64(time.Hour * 24 * 30 / time.Second) // 1 month
)

var (
	_ Proposal      = (*GeneralProposal)(nil)
	_ ProposalState = (*GeneralProposalState)(nil)

	errGeneralProposalOptionIsToBig = errors.New("option size is to big")
	errNotImplemented               = errors.New("not implemented, should never be called")
)

type GeneralProposal struct {
	Options [][]byte `serialize:"true"` // Arbitrary options
	Start   uint64   `serialize:"true"` // Start time of proposal
	End     uint64   `serialize:"true"` // End time of proposal

	// In order to be successful, proposal must have number of votes greater than threshold, which is calculated as:
	//
	// len(allowedVoters) * p.TotalVotedThresholdNominator / fractionDenominatorBig
	TotalVotedThresholdNominator uint64 `serialize:"true"`

	// In order to be successful, most voted option must have more weight than threshold, which is calculated as:
	//
	// voted * MostVotedThresholdNominator / FractionDenominator.
	MostVotedThresholdNominator uint64 `serialize:"true"`

	// Allow this proposal to finish early if it has unambiguos result that cannot be altered by future votes.
	AllowEarlyFinish bool `serialize:"true"`
}

func (p *GeneralProposal) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *GeneralProposal) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (p *GeneralProposal) GetOptions() any {
	return p.Options
}

// This proposal type cannot be admin proposal
func (*GeneralProposal) AdminProposer() as.AddressState {
	return as.AddressStateEmpty
}

func (p *GeneralProposal) Verify() error {
	switch {
	case len(p.Options) == 0:
		return errNoOptions
	case len(p.Options) > generalProposalMaxOptionsCount:
		return fmt.Errorf("%w (expected: no more than %d, actual: %d)", errWrongOptionsCount, generalProposalMaxOptionsCount, len(p.Options))
	case p.Start >= p.End:
		return errEndNotAfterStart
	case p.End-p.Start < generalProposalMinDuration:
		return fmt.Errorf("%w (expected: minimum duration %d, actual: %d)", errWrongDuration, generalProposalMinDuration, p.End-p.Start)
	case p.End-p.Start > generalProposalMaxDuration:
		return fmt.Errorf("%w (expected: maximum duration %d, actual: %d)", errWrongDuration, generalProposalMaxDuration, p.End-p.Start)
	}
	for i := 0; i < len(p.Options); i++ {
		if len(p.Options[i]) > generalProposalMaxOptionSize {
			return fmt.Errorf("%w (expected: no more than %d, actual: %d, option index: %d)",
				errGeneralProposalOptionIsToBig, generalProposalMaxOptionSize, len(p.Options[i]), i)
		}
		for j := i + 1; j < len(p.Options); j++ {
			if bytes.Equal(p.Options[i], p.Options[j]) {
				return errNotUniqueOption
			}
		}
	}
	return nil
}

func (p *GeneralProposal) CreateProposalState(allowedVoters []ids.ShortID) ProposalState {
	// totalVotedThreshold = len(allowedVoters) * p.TotalVotedThresholdNominator / fractionDenominatorBig
	totalAllowedVoters := big.NewInt(int64(len(allowedVoters)))
	totalVotedThreshold := big.NewInt(int64(p.TotalVotedThresholdNominator))
	totalVotedThreshold.Mul(totalVotedThreshold, totalAllowedVoters)
	totalVotedThreshold.Div(totalVotedThreshold, fractionDenominatorBig)

	stateProposal := &GeneralProposalState{
		SimpleVoteOptions: SimpleVoteOptions[[]byte]{
			Options: make([]SimpleVoteOption[[]byte], len(p.Options)),
		},
		Start:                       p.Start,
		End:                         p.End,
		AllowedVoters:               allowedVoters,
		TotalAllowedVoters:          uint32(len(allowedVoters)),
		TotalVotedThreshold:         uint32(totalVotedThreshold.Uint64()),
		MostVotedThresholdNominator: p.MostVotedThresholdNominator,
		AllowEarlyFinish:            p.AllowEarlyFinish,
	}
	for i := range p.Options {
		stateProposal.Options[i].Value = p.Options[i]
	}
	return stateProposal
}

func (*GeneralProposal) CreateFinishedProposalState(uint32) (ProposalState, error) {
	return nil, errNotImplemented
}

func (p *GeneralProposal) VerifyWith(verifier Verifier) error {
	return verifier.GeneralProposal(p)
}

type GeneralProposalState struct {
	// New base fee options
	SimpleVoteOptions[[]byte] `serialize:"true"`

	// Start time of proposal
	Start uint64 `serialize:"true"`

	// End time of proposal
	End uint64 `serialize:"true"`

	// Addresses that are allowed to vote for this proposal
	AllowedVoters []ids.ShortID `serialize:"true"`

	// Number of addresses that were initially allowed to vote for this proposal.
	// This is used to calculate thresholds like "half of total voters".
	TotalAllowedVoters uint32 `serialize:"true"`

	// Proposal must have number of votes greater than this threshold in order to be successful.
	TotalVotedThreshold uint32 `serialize:"true"`

	// In order to be successful, most voted option must have more weight than threshold, which is calculated as:
	//
	// voted * MostVotedThresholdNominator / FractionDenominator.
	MostVotedThresholdNominator uint64 `serialize:"true"`

	// Allow this proposal to finish early if it has unambiguos result that cannot be altered by future votes.
	AllowEarlyFinish bool `serialize:"true"`
}

func (p *GeneralProposalState) StartTime() time.Time {
	return time.Unix(int64(p.Start), 0)
}

func (p *GeneralProposalState) EndTime() time.Time {
	return time.Unix(int64(p.End), 0)
}

func (p *GeneralProposalState) IsActiveAt(time time.Time) bool {
	timestamp := uint64(time.Unix())
	return p.Start <= timestamp && timestamp <= p.End
}

func (p *GeneralProposalState) CanBeFinished() bool {
	if !p.AllowEarlyFinish {
		return false
	}

	mostVotedWeight, mostVotedIndex, unambiguous := p.GetMostVoted()
	voted := p.Voted()
	remainingVotes := p.TotalAllowedVoters - voted

	secondMostVotedWeight := uint32(0)
	if len(p.Options) > 1 {
		for index, option := range p.Options {
			if option.Weight > secondMostVotedWeight && index != int(mostVotedIndex) {
				secondMostVotedWeight = option.Weight
			}
		}
	}

	// mostVotedThreshold = voted * p.MostVotedThresholdNominator / fractionDenominatorBig
	mostVotedThresholdNominator := big.NewInt(int64(p.MostVotedThresholdNominator))
	mostVotedThresholdBig := big.NewInt(int64(voted))
	mostVotedThresholdBig.Mul(mostVotedThresholdBig, mostVotedThresholdNominator)
	mostVotedThresholdBig.Div(mostVotedThresholdBig, fractionDenominatorBig)
	mostVotedThreshold := uint32(mostVotedThresholdBig.Uint64())

	return remainingVotes+mostVotedWeight < mostVotedThreshold+1 || // no option can win
		voted == p.TotalAllowedVoters || // everyone had voted
		unambiguous && // option will inevitably win, because
			(mostVotedWeight > remainingVotes+secondMostVotedWeight || len(p.Options) == 1) && // it has more votes than any other option + remaining votes or its the only option
			mostVotedWeight > mostVotedThreshold && // it has already surpassed mostVotedThreshold
			voted > p.TotalVotedThreshold // it has already surpassed totalVotedThreshold
}

func (p *GeneralProposalState) IsSuccessful() bool {
	mostVotedWeight, _, unambiguous := p.GetMostVoted()
	voted := p.Voted()

	// mostVotedThreshold = voted * p.MostVotedThresholdNominator / fractionDenominatorBig
	mostVotedThresholdNominator := big.NewInt(int64(p.MostVotedThresholdNominator))
	mostVotedThreshold := big.NewInt(int64(voted))
	mostVotedThreshold.Mul(mostVotedThreshold, mostVotedThresholdNominator)
	mostVotedThreshold.Div(mostVotedThreshold, fractionDenominatorBig)

	return unambiguous && voted > p.TotalVotedThreshold && uint64(mostVotedWeight) > mostVotedThreshold.Uint64()
}

func (p *GeneralProposalState) Outcome() any {
	_, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	if !unambiguous {
		return -1
	}
	return mostVotedOptionIndex
}

func (p *GeneralProposalState) Result() (types.JSONByteSlice, uint32, bool) {
	mostVotedWeight, mostVotedOptionIndex, unambiguous := p.GetMostVoted()
	return p.Options[mostVotedOptionIndex].Value, mostVotedWeight, unambiguous
}

// Will return modified proposal with added vote, original proposal will not be modified!
func (p *GeneralProposalState) AddVote(voterAddress ids.ShortID, voteIntf Vote) (ProposalState, error) {
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

	updatedProposal := &GeneralProposalState{
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: make([]ids.ShortID, len(p.AllowedVoters)-1),
		SimpleVoteOptions: SimpleVoteOptions[[]byte]{
			Options: make([]SimpleVoteOption[[]byte], len(p.Options)),
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
func (p *GeneralProposalState) ForceAddVote(voteIntf Vote) (ProposalState, error) {
	vote, ok := voteIntf.(*SimpleVote)
	if !ok {
		return nil, ErrWrongVote
	}
	if int(vote.OptionIndex) >= len(p.Options) {
		return nil, ErrWrongVote
	}

	updatedProposal := &GeneralProposalState{
		Start:         p.Start,
		End:           p.End,
		AllowedVoters: p.AllowedVoters,
		SimpleVoteOptions: SimpleVoteOptions[[]byte]{
			Options: make([]SimpleVoteOption[[]byte], len(p.Options)),
		},
		TotalAllowedVoters: p.TotalAllowedVoters,
	}
	// we can't use the same slice, cause we need to change its element
	copy(updatedProposal.Options, p.Options)
	updatedProposal.Options[vote.OptionIndex].Weight++
	return updatedProposal, nil
}

func (p *GeneralProposalState) ExecuteWith(executor Executor) error {
	return executor.GeneralProposal(p)
}

func (p *GeneralProposalState) GetBondTxIDsWith(bondTxIDsGetter BondTxIDsGetter) ([]ids.ID, error) {
	return bondTxIDsGetter.GeneralProposal(p)
}
