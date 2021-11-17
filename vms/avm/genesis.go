// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"sort"
	"strings"

	"github.com/ava-labs/avalanchego/utils"
)

type Genesis struct {
	Txs []*GenesisAsset `serialize:"true"`
}

func (g *Genesis) Less(i, j int) bool { return strings.Compare(g.Txs[i].Alias, g.Txs[j].Alias) == -1 }

func (g *Genesis) Len() int { return len(g.Txs) }

func (g *Genesis) Swap(i, j int) { g.Txs[j], g.Txs[i] = g.Txs[i], g.Txs[j] }

func (g *Genesis) Sort() { sort.Sort(g) }

func (g *Genesis) IsSortedAndUnique() bool { return utils.IsSortedAndUnique(g) }

type GenesisAsset struct {
	Alias         string `serialize:"true"`
	CreateAssetTx `serialize:"true"`
}
