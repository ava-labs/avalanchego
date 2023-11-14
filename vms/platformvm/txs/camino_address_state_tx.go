// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
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
	State as.AddressStateBit `serialize:"true" json:"state"`
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
	case tx.State > as.AddressStateBitMax || as.AddressStateValidBits&as.AddressState(uint64(1)<<tx.State) == 0:
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
