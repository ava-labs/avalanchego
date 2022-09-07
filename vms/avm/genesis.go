// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"strings"

	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

type Genesis struct {
	Txs []*GenesisAsset `serialize:"true"`
}

type GenesisAsset struct {
	Alias             string `serialize:"true"`
	txs.CreateAssetTx `serialize:"true"`
}

func (g *GenesisAsset) Less(other *GenesisAsset) bool {
	return strings.Compare(g.Alias, other.Alias) == -1
}
