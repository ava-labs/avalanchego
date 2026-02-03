// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/hook"
)

var _ hook.Points = (*Hooks)(nil)

type Hooks struct {
	Now func() time.Time
}

func (h *Hooks) BuildHeader(parent *types.Header) *types.Header {
	var now time.Time
	if h.Now != nil {
		now = h.Now()
	} else {
		now = time.Now()
	}
	return &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       uint64(now.Unix()), //nolint:gosec // Known non-negative
	}
}

func (*Hooks) BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) *types.Block {
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil))
}

func (h *Hooks) BlockRebuilderFrom(b *types.Block) hook.BlockBuilder {
	return &Hooks{
		Now: func() time.Time {
			return time.Unix(int64(b.Time()), 0) //nolint:gosec // Won't overflow for a few millennia
		},
	}
}

func (*Hooks) GasTargetAfter(*types.Header) gas.Gas {
	return 1_000_000
}

func (*Hooks) SubSecondBlockTime(hdr *types.Header) time.Duration {
	return 0
}

func (*Hooks) EndOfBlockOps(*types.Block) []hook.Op {
	return nil
}

func (*Hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*Hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {}

// ParseGenesis processes genesis bytes to apply Avalanche network upgrades
// that activate EVM forks (Berlin, London, Shanghai, Cancun, etc.).
func (*Hooks) ParseGenesis(snowCtx *snow.Context, genesisBytes []byte) (*core.Genesis, error) {
	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return nil, fmt.Errorf("parsing genesis: %w", err)
	}

	// snowCtx.Log.Info(fmt.Sprintf("Original genesis config: chainID=%s berlinBlock=%v londonBlock=%v shanghaiTime=%v cancunTime=%v",
	// 	genesis.Config.ChainID, genesis.Config.BerlinBlock, genesis.Config.LondonBlock,
	// 	genesis.Config.ShanghaiTime, genesis.Config.CancunTime))

	// // Populate the Avalanche config extras to enable EVM fork activation
	// configExtra := params.GetExtra(genesis.Config)
	// configExtra.AvalancheContext = extras.AvalancheContext{
	// 	SnowCtx: snowCtx,
	// }
	// configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(snowCtx.NetworkUpgrades)

	// snowCtx.Log.Info(fmt.Sprintf("Network upgrades: %+v", snowCtx.NetworkUpgrades))

	// if err := configExtra.Verify(); err != nil {
	// 	return nil, fmt.Errorf("invalid chain config: %w", err)
	// }

	// // Align Ethereum upgrades to Avalanche upgrades
	// // This maps: ApricotPhase3Plus->Berlin, ApricotPhase5->London,
	// //            Cortina->Shanghai, Durango->Cancun
	// if err := params.SetEthUpgrades(genesis.Config); err != nil {
	// 	return nil, fmt.Errorf("setting eth upgrades: %w", err)
	// }

	// snowCtx.Log.Info(fmt.Sprintf("Modified genesis config: chainID=%s berlinBlock=%v londonBlock=%v shanghaiTime=%v cancunTime=%v",
	// 	genesis.Config.ChainID, genesis.Config.BerlinBlock, genesis.Config.LondonBlock,
	// 	genesis.Config.ShanghaiTime, genesis.Config.CancunTime))

	// Marshal back to bytes
	return genesis, nil
}
