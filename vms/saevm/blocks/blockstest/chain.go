// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockstest

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// A ChainBuilder builds a chain of blocks, maintaining necessary invariants.
//
// It is not safe for concurrent use.
type ChainBuilder struct {
	config         *params.ChainConfig
	chain          []*blocks.Block
	blocksByHash   sync.Map
	acceptedBlocks event.FeedOf[*blocks.Block]

	defaultOpts []ChainOption
}

// NewChainBuilder returns a new ChainBuilder starting from the provided block,
// which MUST NOT be nil.
func NewChainBuilder(config *params.ChainConfig, genesis *blocks.Block, defaultOpts ...ChainOption) *ChainBuilder {
	c := &ChainBuilder{
		config: config,
		chain:  []*blocks.Block{genesis},
	}
	c.SetDefaultOptions(defaultOpts...)
	return c
}

// A ChainOption configures [ChainBuilder.NewBlock].
type ChainOption = options.Option[chainOptions]

// SetDefaultOptions sets the default options upon which all
// additional options passed to [ChainBuilder.NewBlock] are appended.
func (cb *ChainBuilder) SetDefaultOptions(opts ...ChainOption) {
	cb.defaultOpts = opts
}

type chainOptions struct {
	eth []EthBlockOption
	sae []BlockOption
}

// WithEthBlockOptions wraps the options that [ChainBuilder.NewBlock] propagates
// to [NewEthBlock].
func WithEthBlockOptions(opts ...EthBlockOption) ChainOption {
	return options.Func[chainOptions](func(co *chainOptions) {
		co.eth = append(co.eth, opts...)
	})
}

// WithBlockOptions wraps the options that [ChainBuilder.NewBlock] propagates to
// [NewBlock].
func WithBlockOptions(opts ...BlockOption) ChainOption {
	return options.Func[chainOptions](func(co *chainOptions) {
		co.sae = append(co.sae, opts...)
	})
}

// NewBlock constructs a new block and appends it to the chain.
func (cb *ChainBuilder) NewBlock(tb testing.TB, txs []*types.Transaction, opts ...ChainOption) *blocks.Block {
	tb.Helper()

	allOpts := new(chainOptions)
	options.ApplyTo(allOpts, cb.defaultOpts...)
	options.ApplyTo(allOpts, opts...)

	last := cb.Last()
	eth := NewEthBlock(tb, last.EthBlock(), txs, allOpts.eth...)
	b := NewBlock(tb, eth, last, nil, allOpts.sae...)

	cb.chain = append(cb.chain, b)
	cb.blocksByHash.Store(b.Hash(), b)
	cb.acceptedBlocks.Send(b)

	return b
}

// Last returns the last block to be built by the builder, which MAY be the
// genesis block passed to the constructor.
func (cb *ChainBuilder) Last() *blocks.Block {
	return cb.chain[len(cb.chain)-1]
}

// AllBlocks returns all blocks, including the genesis passed to
// [NewChainBuilder].
func (cb *ChainBuilder) AllBlocks() []*blocks.Block {
	return slices.Clone(cb.chain)
}

// AllExceptGenesis returns all blocks created with [ChainBuilder.NewBlock].
func (cb *ChainBuilder) AllExceptGenesis() []*blocks.Block {
	return slices.Clone(cb.chain[1:])
}

var _ blocks.Source = (*ChainBuilder)(nil).GetBlock

// GetBlock returns the block with specified hash and height, and a flag
// indicating if it was found. If either argument does not match, it returns
// `nil, false`.
func (cb *ChainBuilder) GetBlock(h common.Hash, num uint64) (*blocks.Block, bool) {
	ifc, _ := cb.blocksByHash.Load(h)
	b, ok := ifc.(*blocks.Block)
	if !ok || b.NumberU64() != num {
		return nil, false
	}
	return b, true
}

// ErrBlockNotFound is returned by [ChainBuilder.ResolveBlockNumber] and
// [ChainBuilder.BlockByNumber] when the requested block number exceeds the
// chain height.
var ErrBlockNotFound = errors.New("block not found")

// SubscribeAcceptedBlocks subscribes to accepted block events fired by
// [ChainBuilder.NewBlock].
func (cb *ChainBuilder) SubscribeAcceptedBlocks(ch chan<- *blocks.Block) event.Subscription {
	return cb.acceptedBlocks.Subscribe(ch)
}

// LastAcceptedBlock returns the last block in the chain.
func (cb *ChainBuilder) LastAcceptedBlock() *blocks.Block {
	return cb.Last()
}

// ResolveBlockNumber resolves special block number aliases to concrete numbers.
func (cb *ChainBuilder) ResolveBlockNumber(bn rpc.BlockNumber) (uint64, error) {
	head := cb.LastAcceptedBlock().NumberU64()
	switch bn {
	case rpc.EarliestBlockNumber:
		return 0, nil
	case rpc.FinalizedBlockNumber, rpc.SafeBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		return head, nil
	default:
		if bn < 0 {
			return 0, fmt.Errorf("%s block unsupported", bn)
		}
		n := uint64(bn) //nolint:gosec // Non-negative checked above
		if n > head {
			return 0, fmt.Errorf("%w: %d", ErrBlockNotFound, n)
		}
		return n, nil
	}
}

// BlockByNumber returns the accepted block at the specified height.
func (cb *ChainBuilder) BlockByNumber(bn rpc.BlockNumber) (*types.Block, error) {
	n, err := cb.ResolveBlockNumber(bn)
	if err != nil {
		return nil, err
	}
	return cb.chain[n].EthBlock(), nil
}
