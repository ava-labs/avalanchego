// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

// AddressState flags, max 63
const (
	AddressStateRoleAdmin    = uint8(0)
	AddressStateRoleAdminBit = uint64(0b1)
	AddressStateRoleKyc      = uint8(1)
	AddressStateRoleKycBit   = uint64(0b10)
	AddressStateRoleBits     = uint64(0b11)

	AddressStateKycVerified    = uint8(32)
	AddressStateKycVerifiedBit = uint64(0b0100000000000000000000000000000000)
	AddressStateKycExpired     = uint8(33)
	AddressStateKycExpiredBit  = uint64(0b1000000000000000000000000000000000)
	AddressStateKycBits        = uint64(0b1100000000000000000000000000000000)

	AddressStateConsortium      = uint8(38)
	AddressStateConsortiumBit   = uint64(0b0100000000000000000000000000000000000000)
	AddressStateNodeDeferred    = uint8(39)
	AddressStateNodeDeferredBit = uint64(0b1000000000000000000000000000000000000000)
	AddressStateVoteBits        = uint64(0b1100000000000000000000000000000000000000)

	AddressStateMax       = uint8(63)
	AddressStateValidBits = AddressStateRoleBits | AddressStateKycBits | AddressStateVoteBits
)

var (
	_ UnsignedTx = (*AddressStateTx)(nil)

	ErrEmptyAddress = errors.New("address is empty")
	ErrInvalidState = errors.New("invalid state")
)

// AddressStateTx is an unsigned AddressStateTx
type AddressStateTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The address to add / remove state
	Address ids.ShortID `serialize:"true" json:"address"`
	// The state to set / unset
	State uint8 `serialize:"true" json:"state"`
	// Remove or add the flag ?
	Remove bool `serialize:"true" json:"remove"`
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *AddressStateTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Address == ids.ShortEmpty:
		return ErrEmptyAddress
	case tx.State > AddressStateMax || AddressStateValidBits&(uint64(1)<<tx.State) == 0:
		return ErrInvalidState
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return tx.BaseTx.SyntacticVerify(ctx)
}

func (tx *AddressStateTx) Visit(visitor Visitor) error {
	return visitor.AddressStateTx(tx)
}
