// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*AddVoteTx)(nil)

	errBadVote      = errors.New("bad vote")
	errBadVoterAuth = errors.New("bad voter auth")
)

// AddVoteTx is an unsigned AddVoteTx
type AddVoteTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Proposal id
	ProposalID ids.ID `serialize:"true" json:"proposalID"`
	// Vote bytes
	VotePayload []byte `serialize:"true" json:"votePayload"`
	// Address that is voting
	VoterAddress ids.ShortID `serialize:"true" json:"proposerAddress"`
	// Auth that will be used to verify credential for vote
	VoterAuth verify.Verifiable `serialize:"true" json:"VoterAuth"`

	vote dac.Vote
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *AddVoteTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	vote, err := tx.Vote()
	if err != nil {
		return err
	}

	if err := vote.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadVote, err)
	}

	if err := tx.VoterAuth.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadVoterAuth, err)
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

type VoteWrapper struct {
	dac.Vote `serialize:"true"`
}

func (tx *AddVoteTx) Vote() (dac.Vote, error) {
	if tx.vote == nil {
		vote := &VoteWrapper{}
		if _, err := Codec.Unmarshal(tx.VotePayload, vote); err != nil {
			return nil, fmt.Errorf("%w: %s", errBadVote, err)
		}
		tx.vote = vote.Vote
	}
	return tx.vote, nil
}

func (tx *AddVoteTx) Visit(visitor Visitor) error {
	return visitor.AddVoteTx(tx)
}
