// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package backends

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	stdcontext "context"
)

type Backend interface {
	BuilderBackend
	SignerBackend
}

type BuilderBackend interface {
	Context

	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
}

type SignerBackend interface {
	GetUTXO(ctx stdcontext.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
}
