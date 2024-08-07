// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ ValidatorTx = (*CaminoAddValidatorTx)(nil)

	errAssetNotAVAX      = errors.New("locked output must be AVAX")
	errStakeOutsNotEmpty = errors.New("stake outputs must be empty")
)

// CaminoAddValidatorTx is an unsigned caminoAddValidatorTx
type CaminoAddValidatorTx struct {
	AddValidatorTx `serialize:"true"`

	// Auth that will be used to verify credential for [NodeOwnerAuth].
	// If node owner address is msig-alias, auth must match real signatures.
	NodeOwnerAuth verify.Verifiable `serialize:"true" json:"nodeOwnerAuth"`
}

func (tx *CaminoAddValidatorTx) Stake() []*avax.TransferableOutput {
	var stake []*avax.TransferableOutput
	for _, out := range tx.Outs {
		if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateBonded) {
			stake = append(stake, out)
		}
	}
	return stake
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *CaminoAddValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.DelegationShares > 0:
		return errTooManyShares
	case tx.Validator.NodeID == ids.EmptyNodeID:
		return errEmptyNodeID
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(&tx.Validator, tx.RewardsOwner, tx.NodeOwnerAuth); err != nil {
		return fmt.Errorf("failed to verify validator, rewards owner or node owner auth: %w", err)
	}

	totalStakeWeight := uint64(0)
	for _, out := range tx.Outs {
		lockedOut, ok := out.Out.(*locked.Out)
		if ok && lockedOut.IsNewlyLockedWith(locked.StateBonded) {
			newWeight, err := math.Add64(totalStakeWeight, lockedOut.Amount())
			if err != nil {
				return err
			}
			totalStakeWeight = newWeight

			assetID := out.AssetID()
			if assetID != ctx.AVAXAssetID {
				return errAssetNotAVAX
			}
		}
	}

	switch {
	case len(tx.StakeOuts) > 0:
		return errStakeOutsNotEmpty
	case totalStakeWeight != tx.Validator.Wght:
		return fmt.Errorf("%w: weight %d != stake %d", errValidatorWeightMismatch, tx.Validator.Wght, totalStakeWeight)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
