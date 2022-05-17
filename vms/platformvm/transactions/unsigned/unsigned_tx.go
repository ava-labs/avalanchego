// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unsigned

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// Tx is an unsigned transaction
type Tx interface {
	// TODO: Remove this initialization pattern from both the platformvm and the
	// avm.
	snow.ContextInitializable
	Initialize(unsignedBytes, signedBytes []byte)
	ID() ids.ID
	UnsignedBytes() []byte
	Bytes() []byte

	// InputIDs returns the set of inputs this transaction consumes
	InputIDs() ids.Set

	// Attempts to verify this transaction without any provided state.
	SyntacticVerify(ctx *snow.Context) error
}
