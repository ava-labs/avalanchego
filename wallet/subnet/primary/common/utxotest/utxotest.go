// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utxotest

import (
	"context"
	"slices"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

func NewDeterministicChainUTXOs(require *require.Assertions, utxoSets map[ids.ID][]*avax.UTXO) *DeterministicChainUTXOs {
	globalUTXOs := common.NewUTXOs()
	for subnetID, utxos := range utxoSets {
		for _, utxo := range utxos {
			require.NoError(
				globalUTXOs.AddUTXO(context.Background(), subnetID, constants.PlatformChainID, utxo),
			)
		}
	}
	return &DeterministicChainUTXOs{
		ChainUTXOs: common.NewChainUTXOs(constants.PlatformChainID, globalUTXOs),
	}
}

type DeterministicChainUTXOs struct {
	common.ChainUTXOs
}

func (c *DeterministicChainUTXOs) UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error) {
	utxos, err := c.ChainUTXOs.UTXOs(ctx, sourceChainID)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(utxos, func(a, b *avax.UTXO) int {
		return a.Compare(&b.UTXOID)
	})
	return utxos, nil
}
