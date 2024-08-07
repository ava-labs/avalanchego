// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*ClaimTx)(nil)

	errNoClaimables         = errors.New("no deposit txs with rewards or claimables to claim")
	errNonUniqueClaimableID = errors.New("non-unique deposit tx id or claimable owner id")
	errZeroClaimedAmount    = errors.New("zero claimed amount")
	errBadClaimableAuth     = errors.New("bad claimable auth")
	ErrWrongClaimType       = errors.New("wrong claim type")
)

type ClaimType uint64

const (
	ClaimTypeValidatorReward ClaimType = iota
	ClaimTypeExpiredDepositReward
	ClaimTypeAllTreasury
	ClaimTypeActiveDepositReward
)

type ClaimAmount struct {
	// If Type is ClaimTypeActiveDepositReward, then it is DepositTxID.
	// Otherwise it is ownerID of claimable owner (validator rewards, expired deposit rewards),
	// where ownerID is hash256 of owners structure (secp256k1fx.OutputOwners, for example).
	ID ids.ID `serialize:"true" json:"id"`
	// Enum defining what is claimed
	Type ClaimType `serialize:"true" json:"type"`
	// Amount that will be claimed
	Amount uint64 `serialize:"true" json:"amount"`
	// Auth that will be used to verify credential for claimable owner.
	OwnerAuth verify.Verifiable `serialize:"true" json:"ownerAuth"`
}

// ClaimTx is an unsigned ClaimTx
type ClaimTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Array, describing what types and amounts of claimables.
	Claimables []ClaimAmount `serialize:"true" json:"claimables"`
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *ClaimTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.Claimables) == 0:
		return errNoClaimables
	}

	uniqueIDs := set.NewSet[ids.ID](len(tx.Claimables))
	for i, claimable := range tx.Claimables {
		if claimable.Amount == 0 {
			return errZeroClaimedAmount
		}
		if claimable.Type > ClaimTypeActiveDepositReward {
			return ErrWrongClaimType
		}
		if _, ok := uniqueIDs[claimable.ID]; ok {
			return errNonUniqueClaimableID
		}
		if err := claimable.OwnerAuth.Verify(); err != nil {
			return fmt.Errorf("%w (claimable[%d].OwnerAuth): %s", errBadClaimableAuth, i, err)
		}
		uniqueIDs.Add(claimable.ID)
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ClaimTx) Visit(visitor Visitor) error {
	return visitor.ClaimTx(tx)
}
