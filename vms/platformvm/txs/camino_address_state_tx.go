// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

type (
	AddressState    uint64
	AddressStateBit uint8
)

// AddressState flags, max 63
const (
	AddressStateBitRoleAdmin    AddressStateBit = 0
	AddressStateBitRoleKYC      AddressStateBit = 1
	AddressStateBitKYCVerified  AddressStateBit = 32
	AddressStateBitKYCExpired   AddressStateBit = 33
	AddressStateBitConsortium   AddressStateBit = 38
	AddressStateBitNodeDeferred AddressStateBit = 39
	AddressStateBitMax          AddressStateBit = 63

	AddressStateEmpty AddressState = 0

	AddressStateRoleAdmin AddressState = 0b1
	AddressStateRoleKYC   AddressState = 0b10
	AddressStateRoleAll   AddressState = 0b11

	AddressStateKYCVerified AddressState = 0b0100000000000000000000000000000000
	AddressStateKYCExpired  AddressState = 0b1000000000000000000000000000000000
	AddressStateKYCAll      AddressState = AddressStateKYCVerified | AddressStateKYCExpired

	AddressStateConsortiumMember AddressState = 0b0100000000000000000000000000000000000000
	AddressStateNodeDeferred     AddressState = 0b1000000000000000000000000000000000000000
	AddressStateVoteBits         AddressState = 0b1100000000000000000000000000000000000000 // TODO @evlekht rename ?

	AddressStateValidBits = AddressStateRoleAll | AddressStateKYCAll | AddressStateVoteBits
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
	State AddressStateBit `serialize:"true" json:"state"`
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
	case tx.State > AddressStateBitMax || AddressStateValidBits&AddressState(uint64(1)<<tx.State) == 0:
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
