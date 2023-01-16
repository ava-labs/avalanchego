// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

var _ UnsignedTx = (*MultisigAliasTx)(nil)

// MultisigAliasTx is an unsigned multisig alias tx
type MultisigAliasTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of this multisig alias
	Alias ids.ShortID `serialize:"true" json:"alias"`
	// Secp256K1fx owners which need to sign
	Owner fx.Owner `serialize:"true" json:"owner"`
	// Auth that allows existing owners to change an alias
	ChangeAuth verify.Verifiable `serialize:"true" json:"changeAuthorization"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [MultisigAliasTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *MultisigAliasTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.Owner.InitCtx(ctx)
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *MultisigAliasTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(tx.Owner, tx.ChangeAuth); err != nil {
		return fmt.Errorf("failed to verify owner or change auth: %w", err)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (*MultisigAliasTx) Visit(_ Visitor) error {
	return errNonExecutableTx
}
