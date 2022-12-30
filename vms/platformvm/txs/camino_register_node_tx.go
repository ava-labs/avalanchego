// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ UnsignedTx = (*RegisterNodeTx)(nil)

	errNoNodeID                  = errors.New("no nodeID specified")
	errConsortiumMemberAddrEmpty = errors.New("consortium member address is empty")
)

// RegisterNodeTx is an unsigned registerNodeTx
type RegisterNodeTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Node id that will be unregistered for consortium member
	OldNodeID ids.NodeID `serialize:"true" json:"oldNodeID"`
	// Node id that will be registered for consortium member
	NewNodeID ids.NodeID `serialize:"true" json:"newNodeID"`
	// Auth that will be used to verify credential for [ConsortiumMemberAddress].
	// If [ConsortiumMemberAddress] is msig-alias, auth must match real signatures.
	ConsortiumMemberAuth verify.Verifiable `serialize:"true" json:"consortiumMemberAuth"`
	// Address of consortium member to which node id will be registered
	ConsortiumMemberAddress ids.ShortID `serialize:"true" json:"consortiumMemberAddress"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [RegisterNodeTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *RegisterNodeTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *RegisterNodeTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.NewNodeID == ids.EmptyNodeID && tx.OldNodeID == ids.EmptyNodeID:
		return errNoNodeID
	case tx.ConsortiumMemberAddress == ids.ShortEmpty:
		return errConsortiumMemberAddrEmpty
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := tx.ConsortiumMemberAuth.Verify(); err != nil {
		return fmt.Errorf("failed to verify consortium member auth: %w", err)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *RegisterNodeTx) Visit(visitor Visitor) error {
	return visitor.RegisterNodeTx(tx)
}
