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

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/warp"

	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
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

	// coinbase is the fee recipient stamped into `header.Coinbase`
	// in operator-chosen branches of [resolveCoinbase].
	// On a builder, it is the local node's `Config.FeeRecipient`.
	// On a rebuilder it is overridden with the RECEIVED block's
	// Coinbase so the rebuilt header hashes identically to the received
	// header. see [Points.BlockRebuilderFrom] for the determinism
	// rationale.
	coinbase common.Address
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
			ParentHash: parent.Hash(),
			// `Coinbase` is a placeholder; the final value may depend on
			// settled-state-as-of-build-time (rewardmanager precompile)
			// which is not in scope here. [blockBuilder.FinalizeHeader]
			// receives the settled state from SAE and overwrites this field
			// before the block is sealed (see [blockBuilder.resolveCoinbase]).
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(now.Unix()),
			BlobGasUsed:      utils.PointerTo[uint64](0),
			ExcessBlobGas:    utils.PointerTo[uint64](0),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			// BlockGasCost is preserved in the header for layout parity with
			// legacy subnet-evm headers, but is not consumed for any
			// decision-making in SAE (ACP-226 superseded its use). It is
			// always stamped to zero by the SAE block builder.
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: utils.PointerTo[uint64](nowMS),
			MinDelayExcess:   &mde,
			TargetExcess:     &te,
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

var errZeroStoredGasPriceConfig = errors.New("gaspricemanager enabled but storage is zero")

// FinalizeHeader stamps the header fields that depend on the settled block:
// the effective ACP-224 gas-config group (read from gaspricemanager storage
// in the settled state; see [headerGasConfig]) and `header.Coinbase` (see
// [blockBuilder.resolveCoinbase]). Both gates use `settled.Time` (NOT
// `header.Time`): a precompile activation `T` is only reflected in
// `settledState` once a block with `T <= blockTime` has settled; using
// header.Time would read an uninitialised slot.
//
// The gas-config group MUST be stamped here rather than in
// [blockBuilder.BuildBlock] because SAE's worst-case projection reads it off
// the header (via [Points.GasConfigAfter]) before BuildBlock runs.
func (b *blockBuilder) FinalizeHeader(header, settled *types.Header, settledState libevm.StateReader) error {
	configExtra := subnetevmparams.GetExtra(b.chainConfig)
	if configExtra.IsPrecompileEnabled(gaspricemanager.ContractAddress, settled.Time) {
		stored := gaspricemanager.GetStoredGasPriceConfig(settledState, gaspricemanager.ContractAddress)
		if stored == (commontype.GasPriceConfig{}) {
			// Activation runs [gaspricemanager.Configure] which always writes
			// a non-zero config (MinGasPrice > 0), and the only mutator path
			// also enforces non-zero via [commontype.GasPriceConfig.Verify].
			// Reaching here therefore indicates corrupt or missing storage at
			// an activated precompile, which would cause silent divergence if
			// papered over with defaults.
			return fmt.Errorf("%w: block %d settling %d", errZeroStoredGasPriceConfig, header.Number, settled.Number)
		}
		stampGasConfig(customtypes.GetHeaderExtra(header), gasConfigFromStored(stored))
	}

	header.Coinbase = b.resolveCoinbase(settled, settledState)
	return nil
}

var errEmptyBlock = errors.New("empty block")

func (b *blockBuilder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []*Tx,
	settled saehook.Settled,
) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}

	rules := b.chainConfig.Rules(header.Number, subnetevmparams.IsMergeTODO, header.Time)
	rulesExtra := subnetevmparams.GetRulesExtra(rules)
	predicateBytes, err := warp.PredicateBytes(b.ctx, blockCtx, rulesExtra, txs)
	if err != nil {
		return nil, fmt.Errorf("generating predicates: %w", err)
	}
	header.Extra = customheader.SetPredicateBytesInExtra(header.Extra, predicateBytes)

	// Encode the settled marker into the header so [Points.SettledBy] can
	// recover it.
	he := customtypes.GetHeaderExtra(header)
	he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess = settled.AsPointers()

	return types.NewBlock(
		header,
		txs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
	), nil
}

// resolveCoinbase returns the fee recipient for the block being built.
// Branches, in order:
//  1. rewardmanager precompile not enabled at `settled.Time`:
//     `customCoinbase` if `AllowFeeRecipients` is true, else burn.
//  2. precompile enabled, allows fee recipients enabled:
//     `customCoinbase`.
//  3. otherwise: the address stored in the precompile's reward address slot.
//
// Gates use `settled.Time` (not `header.Time`): see
// [blockBuilder.FinalizeHeader].
func (b *blockBuilder) resolveCoinbase(settled *types.Header, settledState libevm.StateReader) common.Address {
	configExtra := subnetevmparams.GetExtra(b.chainConfig)
	if !configExtra.IsPrecompileEnabled(rewardmanager.ContractAddress, settled.Time) {
		if configExtra.AllowFeeRecipients {
			return b.coinbase
		}
		return constants.BlackholeAddr
	}

	addr, allowFeeRecipients := rewardmanager.GetStoredRewardAddress(settledState)
	if allowFeeRecipients {
		return b.coinbase
	}
	return addr
}
