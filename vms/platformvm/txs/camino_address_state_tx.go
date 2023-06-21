// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

type (
	AddressState    uint64
	AddressStateBit uint8
)

// AddressState flags, max 63
const (
	// Bits

	AddressStateBitRoleAdmin         AddressStateBit = 0
	AddressStateBitRoleKYC           AddressStateBit = 1
	AddressStateBitRoleOffersAdmin   AddressStateBit = 2
	AddressStateBitRoleOffersCreator AddressStateBit = 3

	AddressStateBitKYCVerified  AddressStateBit = 32
	AddressStateBitKYCExpired   AddressStateBit = 33
	AddressStateBitConsortium   AddressStateBit = 38
	AddressStateBitNodeDeferred AddressStateBit = 39
	AddressStateBitMax          AddressStateBit = 63

	// States

	AddressStateEmpty AddressState = 0

	AddressStateRoleAdmin         AddressState = AddressState(1) << AddressStateBitRoleAdmin                                                               // 0b1
	AddressStateRoleKYC           AddressState = AddressState(1) << AddressStateBitRoleKYC                                                                 // 0b10
	AddressStateRoleOffersAdmin   AddressState = AddressState(1) << AddressStateBitRoleOffersAdmin                                                         // 0b100
	AddressStateRoleOffersCreator AddressState = AddressState(1) << AddressStateBitRoleOffersCreator                                                       // 0b1000
	AddressStateRoleAll           AddressState = AddressStateRoleAdmin | AddressStateRoleKYC | AddressStateRoleOffersAdmin | AddressStateRoleOffersCreator // 0b1111

	AddressStateKYCVerified AddressState = AddressState(1) << AddressStateBitKYCVerified    // 0b0100000000000000000000000000000000
	AddressStateKYCExpired  AddressState = AddressState(1) << AddressStateBitKYCExpired     // 0b1000000000000000000000000000000000
	AddressStateKYCAll      AddressState = AddressStateKYCVerified | AddressStateKYCExpired // 0b1100000000000000000000000000000000

	AddressStateConsortiumMember AddressState = AddressState(1) << AddressStateBitConsortium            // 0b0100000000000000000000000000000000000000
	AddressStateNodeDeferred     AddressState = AddressState(1) << AddressStateBitNodeDeferred          // 0b1000000000000000000000000000000000000000
	AddressStateVotableBits      AddressState = AddressStateConsortiumMember | AddressStateNodeDeferred // 0b1100000000000000000000000000000000000000

	AddressStateValidBits = AddressStateRoleAll | AddressStateKYCAll | AddressStateVotableBits // 0b1100001100000000000000000000000000001111
)

var (
	_ UnsignedTx = (*AddressStateTx)(nil)

	ErrEmptyAddress = errors.New("address is empty")
	ErrInvalidState = errors.New("invalid state")
)

// AddressStateTx is an unsigned AddressStateTx
type AddressStateTx struct {
	// We upgrade this struct beginning SP1
	UpgradeVersionID codec.UpgradeVersionID
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The address to add / remove state
	Address ids.ShortID `serialize:"true" json:"address"`
	// The state to set / unset
	State AddressStateBit `serialize:"true" json:"state"`
	// Remove or add the flag ?
	Remove bool `serialize:"true" json:"remove"`
	// The executor of this TX (needs access role)
	Executor ids.ShortID `serialize:"true" json:"executor" upgradeVersion:"1"`
	// Signature(s) to authenticate executor
	ExecutorAuth verify.Verifiable `serialize:"true" json:"executorAuth" upgradeVersion:"1"`
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

	if tx.UpgradeVersionID.Version() > 0 {
		if err := tx.ExecutorAuth.Verify(); err != nil {
			return err
		}
		if tx.Executor == ids.ShortEmpty {
			return ErrEmptyAddress
		}
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	return tx.BaseTx.SyntacticVerify(ctx)
}

func (tx *AddressStateTx) Visit(visitor Visitor) error {
	return visitor.AddressStateTx(tx)
}
