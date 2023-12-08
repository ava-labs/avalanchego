// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/types"
)

const maxProposalDescriptionSize = 2048

var (
	_ UnsignedTx = (*AddProposalTx)(nil)

	errBadProposal               = errors.New("bad proposal")
	errBadProposerAuth           = errors.New("bad proposer auth")
	errTooBigBond                = errors.New("too big bond")
	errTooBigProposalDescription = errors.New("too big proposal description")
)

// AddProposalTx is an unsigned addProposalTx
type AddProposalTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Contains arbitrary bytes, up to maxProposalDescriptionSize
	ProposalDescription types.JSONByteSlice `serialize:"true" json:"proposalDescription"`
	// Proposal bytes
	ProposalPayload []byte `serialize:"true" json:"proposalPayload"`
	// Address that can create proposals of this type
	ProposerAddress ids.ShortID `serialize:"true" json:"proposerAddress"`
	// Auth that will be used to verify credential for proposal
	ProposerAuth verify.Verifiable `serialize:"true" json:"proposerAuth"`

	bondAmount *uint64
	proposal   dac.Proposal
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *AddProposalTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case len(tx.ProposalDescription) > maxProposalDescriptionSize:
		return errTooBigProposalDescription
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	proposal, err := tx.Proposal()
	if err != nil {
		return err
	}

	if err := proposal.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadProposal, err)
	}

	if err := tx.ProposerAuth.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadProposerAuth, err)
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, true); err != nil {
		return err
	}

	bondAmount := uint64(0)
	for _, out := range tx.Outs {
		if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateBonded) {
			newBondAmount, err := math.Add64(bondAmount, lockedOut.Amount())
			if err != nil {
				return fmt.Errorf("%w: %s", errTooBigBond, err)
			}
			bondAmount = newBondAmount
		}
	}
	tx.bondAmount = &bondAmount

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

type ProposalWrapper struct {
	dac.Proposal `serialize:"true"`
}

func (tx *AddProposalTx) Proposal() (dac.Proposal, error) {
	if tx.proposal == nil {
		proposal := &ProposalWrapper{}
		if _, err := Codec.Unmarshal(tx.ProposalPayload, proposal); err != nil {
			return nil, fmt.Errorf("%w: %s", errBadProposal, err)
		}
		tx.proposal = proposal.Proposal
	}
	return tx.proposal, nil
}

func (tx *AddProposalTx) BondAmount() uint64 {
	if tx.bondAmount == nil {
		bondAmount := uint64(0)
		for _, out := range tx.Outs {
			if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateBonded) {
				bondAmount += lockedOut.Amount()
			}
		}
		tx.bondAmount = &bondAmount
	}
	return *tx.bondAmount
}

func (tx *AddProposalTx) Visit(visitor Visitor) error {
	return visitor.AddProposalTx(tx)
}
