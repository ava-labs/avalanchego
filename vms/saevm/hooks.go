// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/hook"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/vms/components/gas"
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
		Time:       uint64(now.Unix()),
	}
}

func (*Hooks) BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) *types.Block {
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil))
}

func (*Hooks) BlockRebuilderFrom(b *types.Block) hook.BlockBuilder {
	return &Hooks{
		Now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
	}
}

func (*Hooks) GasTargetAfter(*types.Header) gas.Gas {
	return 1_000_000
}

func (*Hooks) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (*Hooks) EndOfBlockOps(*types.Block) []hook.Op {
	return nil
}

func (*Hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*Hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {}

func (*Hooks) ApplyNetworkUpgrades(config *params.ChainConfig, upgrades upgrade.Config) error {
	// Populate the Avalanche config extras to enable EVM fork activation
	configExtra := params.GetExtra(config)
	configExtra.NetworkUpgrades = extras.GetNetworkUpgrades(upgrades)

	if err := configExtra.Verify(); err != nil {
		return fmt.Errorf("invalid chain config: %w", err)
	}

	// Align Ethereum upgrades to Avalanche upgrades
	// This maps: ApricotPhase3Plus->Berlin, ApricotPhase5->London,
	//            Cortina->Shanghai, Durango->Cancun
	if err := params.SetEthUpgrades(config); err != nil {
		return fmt.Errorf("setting eth upgrades: %w", err)
	}

	return nil
}
