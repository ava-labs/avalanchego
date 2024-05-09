// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type AVMBuilderBackend interface {
	UTXOs(addrs set.Set[ids.ShortID], sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetUTXO(addrs set.Set[ids.ShortID], chainID, utxoID ids.ID) (*avax.UTXO, error)
}

type walletBackendAdapter struct {
	b     AVMBuilderBackend
	addrs set.Set[ids.ShortID]
}

func (wa *walletBackendAdapter) UTXOs(_ context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	return wa.b.UTXOs(wa.addrs, sourceChainID)
}

func (wa *walletBackendAdapter) GetUTXO(_ context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error) {
	return wa.b.GetUTXO(wa.addrs, chainID, utxoID)
}
