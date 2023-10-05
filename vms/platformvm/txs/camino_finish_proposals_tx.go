// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*FinishProposalsTx)(nil)

	errNoFinishedProposals          = errors.New("no expired or successful proposals")
	errNotUniqueProposalID          = errors.New("not unique proposal id")
	errNotSortedOrUniqueProposalIDs = errors.New("not sorted or not unique proposal ids")
)

// FinishProposalsTx is an unsigned removeExpiredProposalsTx
type FinishProposalsTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Proposals that were finished early.
	EarlyFinishedSuccessfulProposalIDs []ids.ID `serialize:"true" json:"earlyFinishedSuccessfulProposalIDs"`
	// Proposals that were finished early.
	EarlyFinishedFailedProposalIDs []ids.ID `serialize:"true" json:"earlyFinishedFailedProposalIDs"`
	// Proposals that were expired.
	ExpiredSuccessfulProposalIDs []ids.ID `serialize:"true" json:"expiredSuccessfulProposalIDs"`
	// Proposals that were expired.
	ExpiredFailedProposalIDs []ids.ID `serialize:"true" json:"expiredFailedProposalIDs"`
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *FinishProposalsTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.EarlyFinishedSuccessfulProposalIDs) == 0 &&
		len(tx.EarlyFinishedFailedProposalIDs) == 0 &&
		len(tx.ExpiredSuccessfulProposalIDs) == 0 &&
		len(tx.ExpiredFailedProposalIDs) == 0:
		return errNoFinishedProposals
	case !utils.IsSortedAndUniqueSortable(tx.EarlyFinishedSuccessfulProposalIDs) ||
		!utils.IsSortedAndUniqueSortable(tx.EarlyFinishedFailedProposalIDs) ||
		!utils.IsSortedAndUniqueSortable(tx.ExpiredSuccessfulProposalIDs) ||
		!utils.IsSortedAndUniqueSortable(tx.ExpiredFailedProposalIDs):
		return errNotSortedOrUniqueProposalIDs
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	totalProposalsCount := len(tx.EarlyFinishedSuccessfulProposalIDs) + len(tx.EarlyFinishedFailedProposalIDs) +
		len(tx.ExpiredSuccessfulProposalIDs) + len(tx.ExpiredFailedProposalIDs)
	uniqueProposals := set.NewSet[ids.ID](totalProposalsCount)
	for _, proposalID := range tx.EarlyFinishedSuccessfulProposalIDs {
		uniqueProposals.Add(proposalID)
	}
	for _, proposalID := range tx.EarlyFinishedFailedProposalIDs {
		if uniqueProposals.Contains(proposalID) {
			return errNotUniqueProposalID
		}
		uniqueProposals.Add(proposalID)
	}
	for _, proposalID := range tx.ExpiredSuccessfulProposalIDs {
		if uniqueProposals.Contains(proposalID) {
			return errNotUniqueProposalID
		}
		uniqueProposals.Add(proposalID)
	}
	for _, proposalID := range tx.ExpiredFailedProposalIDs {
		if uniqueProposals.Contains(proposalID) {
			return errNotUniqueProposalID
		}
		uniqueProposals.Add(proposalID)
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, true); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *FinishProposalsTx) Visit(visitor Visitor) error {
	return visitor.FinishProposalsTx(tx)
}

func (tx *FinishProposalsTx) ProposalIDs() []ids.ID {
	lockTxIDs := tx.EarlyFinishedSuccessfulProposalIDs
	lockTxIDs = append(lockTxIDs, tx.EarlyFinishedFailedProposalIDs...)
	lockTxIDs = append(lockTxIDs, tx.ExpiredSuccessfulProposalIDs...)
	return append(lockTxIDs, tx.ExpiredFailedProposalIDs...)
}
