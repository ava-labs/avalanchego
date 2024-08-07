// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_                            UnsignedTx = (*MultisigAliasTx)(nil)
	errFailedToVerifyAliasOrAuth            = errors.New("failed to verify alias or auth")
)

// MultisigAliasTx is an unsigned multisig alias tx
type MultisigAliasTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Multisig alias definition. MultisigAlias.ID must be empty, if its the new alias
	MultisigAlias multisig.Alias `serialize:"true" json:"multisigAlias"`
	// Auth that allows existing owners to change an alias
	Auth verify.Verifiable `serialize:"true" json:"auth"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [MultisigAliasTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *MultisigAliasTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.MultisigAlias.InitCtx(ctx)
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
	if err := verify.All(&tx.MultisigAlias, tx.Auth); err != nil {
		return fmt.Errorf("%w: %s", errFailedToVerifyAliasOrAuth, err.Error())
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *MultisigAliasTx) Visit(visitor Visitor) error {
	return visitor.MultisigAliasTx(tx)
}
