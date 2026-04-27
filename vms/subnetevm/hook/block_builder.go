// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"time"

	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/corethvm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ saehook.BlockBuilder[*Tx] = (*blockBuilder)(nil)

type params struct {
	delayExcess  *acp226.DelayExcess
	targetExcess *acp176.TargetExcess
}

type blockBuilder struct {
	ctx         *snow.Context
	chainConfig *ethparams.ChainConfig

	now func() time.Time
	// When fields in params are set, the block builder will build blocks that
	// move the network values towards their desired values.
	desired params
}

func (b *blockBuilder) BuildHeader(parent *types.Header) (*types.Header, error) {
	var now time.Time
	if b.now != nil {
		now = b.now()
	} else {
		now = time.Now()
	}
	nowMS := uint64(now.UnixMilli())

	mde := acp226.InitialDelayExcess
	if pmde := customtypes.GetHeaderExtra(parent).MinDelayExcess; pmde != nil {
		mde = *pmde
	}

	{
		parentTimeMS := customtypes.HeaderTimeMilliseconds(parent)
		if nowMS < parentTimeMS {
			return nil, fmt.Errorf("current time is before parent timestamp: now=%d parentTime=%d", nowMS, parentTimeMS)
		}

		delay := nowMS - parentTimeMS
		minDelay := mde.Delay()
		if delay < minDelay {
			return nil, fmt.Errorf("block building separation not satisfied: delay=%d minDelay=%d", delay, minDelay)
		}
	}

	if b.desired.delayExcess != nil {
		mde.UpdateDelayExcess(*b.desired.delayExcess)
	}

	te := targetExcess(parent)
	if b.desired.targetExcess != nil {
		te.UpdateTargetExcess(*b.desired.targetExcess)
	}
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash:       parent.Hash(),
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(now.Unix()),
			BlobGasUsed:      utils.PointerTo[uint64](0),
			ExcessBlobGas:    utils.PointerTo[uint64](0),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			ExtDataGasUsed:   big.NewInt(0),
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: utils.PointerTo[uint64](nowMS),
			MinDelayExcess:   &mde,
			TargetExcess:     &te,
			SettledHeight:    utils.PointerTo[uint64](0), // Populated in BuildBlock
		},
	), nil
}

// PotentialEndOfBlockOps returns the iterator of end-of-block transactions to
// consider for inclusion.
//
// Subnet-EVM has no end-of-block ops, so this is always empty. See [Tx] for
// the rationale.
func (*blockBuilder) PotentialEndOfBlockOps(_ context.Context, _ *types.Header, _ common.Hash, _ saetypes.BlockSource) iter.Seq[*Tx] {
	return func(_ func(*Tx) bool) {}
}

var errEmptyBlock = errors.New("empty block")

func (b *blockBuilder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []*Tx,
	settledHeight uint64,
) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}

	rules := b.chainConfig.Rules(header.Number, corethparams.IsMergeTODO, header.Time)
	rulesExtra := corethparams.GetRulesExtra(rules)
	predicateBytes, err := warp.PredicateBytes(b.ctx, blockCtx, rulesExtra, txs)
	if err != nil {
		return nil, fmt.Errorf("generating predicates: %w", err)
	}
	// TODO: Do not use [customheader.SetPredicateBytesInExtra] it assumes fee
	// information is included in [types.Header.Extra].
	header.Extra = customheader.SetPredicateBytesInExtra(rulesExtra.AvalancheRules, header.Extra, predicateBytes)

	headerExtra := customtypes.GetHeaderExtra(header)
	headerExtra.SettledHeight = &settledHeight
	return customtypes.NewBlockWithExtData(
		header,
		txs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
		nil,  // ExtData: subnet-evm has no atomic txs
		true, // update [customtypes.HeaderExtra.ExtDataHash]
	), nil
}
