// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*AddDepositOfferTx)(nil)

	errBadDepositOffer                 = errors.New("bad deposit offer")
	errBadDepositOfferCreatorAuth      = errors.New("bad deposit offer creator auth")
	errEmptyDepositOfferCreatorAddress = errors.New("deposit offer creator address is empty")
	errWrongDepositOfferVersion        = errors.New("wrong deposit offer version")
	errNotZeroDepositOfferAmounts      = errors.New("deposit offer rewardedAmount or depositedAmount isn't zero")
)

// AddDepositOfferTx is an unsigned depositTx
type AddDepositOfferTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// deposit offer that will be added
	DepositOffer *deposit.Offer `serialize:"true" json:"depositOffer"`
	// Address that have "deposit offers creator" role
	DepositOfferCreatorAddress ids.ShortID `serialize:"true" json:"depositOfferCreatorAddress"`
	// Auth that will be used to verify credential for deposit offers creator
	DepositOfferCreatorAuth verify.Verifiable `serialize:"true" json:"depositOfferCreatorAuth"`
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *AddDepositOfferTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.DepositOfferCreatorAddress == ids.ShortEmpty:
		return errEmptyDepositOfferCreatorAddress
	case tx.DepositOffer.UpgradeVersionID.Version() == 0:
		return errWrongDepositOfferVersion
	case tx.DepositOffer.RewardedAmount > 0 || tx.DepositOffer.DepositedAmount > 0:
		return errNotZeroDepositOfferAmounts
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := tx.DepositOffer.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadDepositOffer, err)
	}

	if err := tx.DepositOfferCreatorAuth.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errBadDepositOfferCreatorAuth, err)
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *AddDepositOfferTx) Visit(visitor Visitor) error {
	return visitor.AddDepositOfferTx(tx)
}
