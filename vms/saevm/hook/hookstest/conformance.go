// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hookstest

import (
	"context"
	"iter"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// Conformance wraps hooks so that tb fails if, while building or rebuilding a
// block, they change a header value that [hook.BlockBuilder] forbids changing.
func Conformance[T hook.Transaction](
	tb testing.TB,
	hooks hook.PointsG[T],
) hook.PointsG[T] {
	return &conformance[T]{
		Points: hooks,
		checker: &checker[T]{
			tb:     tb,
			points: hooks,
			inner:  hooks,
		},
		rebuild: hooks.BlockRebuilderFrom,
	}
}

type conformance[T hook.Transaction] struct {
	hook.Points
	*checker[T]
	rebuild func(*types.Block) (hook.BlockBuilder[T], error)
}

func (c *conformance[T]) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[T], error) {
	inner, err := c.rebuild(b)
	if err != nil {
		return nil, err
	}
	return &checker[T]{
		tb:     c.tb,
		points: c.Points,
		inner:  inner,
	}, nil
}

// identity holds the header values that settlement is resolved from.
type identity struct {
	ParentHash common.Hash
	Number     uint64
	Time       uint64
	BlockTime  int64
}

type gasView struct {
	Target   gas.Gas
	Scaling  gas.Gas
	MinPrice gas.Price
	Static   bool
}

// assigned holds the header values that SAE sets before final assembly.
type assigned struct {
	Root     common.Hash
	GasLimit uint64
	BaseFee  string
}

type checker[T hook.Transaction] struct {
	tb     testing.TB
	points hook.Points
	inner  hook.BlockBuilder[T]

	stampedID  identity
	stampedGas gasView
	assigned   assigned
}

var _ hook.BlockBuilder[Op] = (*checker[Op])(nil)

func (c *checker[T]) identityOf(hdr *types.Header) identity {
	return identity{
		ParentHash: hdr.ParentHash,
		Number:     hdr.Number.Uint64(),
		Time:       hdr.Time,
		BlockTime:  c.points.BlockTime(hdr).UnixNano(),
	}
}

func (c *checker[T]) gasOf(hdr *types.Header) gasView {
	target, cfg := c.points.GasConfigAfter(hdr)
	return gasView{
		Target:   target,
		Scaling:  cfg.TargetToExcessScaling,
		MinPrice: cfg.MinPrice,
		Static:   cfg.StaticPricing,
	}
}

func assignedOf(hdr *types.Header) assigned {
	return assigned{
		Root:     hdr.Root,
		GasLimit: hdr.GasLimit,
		BaseFee:  hdr.BaseFee.String(),
	}
}

func (c *checker[T]) requireAsStamped(where string, hdr *types.Header) {
	c.tb.Helper()
	if got := c.identityOf(hdr); got != c.stampedID {
		c.tb.Errorf("%s: header identity %+v, stamped as %+v", where, got, c.stampedID)
	}
	if got := c.gasOf(hdr); got != c.stampedGas {
		c.tb.Errorf("%s: gas configuration %+v, stamped as %+v", where, got, c.stampedGas)
	}
}

func (c *checker[T]) BuildHeader(parent *types.Header) (*types.Header, error) {
	return c.inner.BuildHeader(parent)
}

func (c *checker[T]) StampHeader(hdr *types.Header, settled hook.SettledView) error {
	c.tb.Helper()
	before := c.identityOf(hdr)
	err := c.inner.StampHeader(hdr, settled)
	if after := c.identityOf(hdr); after != before {
		c.tb.Errorf("StampHeader changed header identity from %+v to %+v", before, after)
	}
	c.stampedID = before
	c.stampedGas = c.gasOf(hdr)
	return err
}

func (c *checker[T]) PotentialEndOfBlockOps(
	ctx context.Context,
	hdr *types.Header,
	lastSettled common.Hash,
	source saetypes.BlockSource,
) iter.Seq[T] {
	c.tb.Helper()
	c.requireAsStamped("PotentialEndOfBlockOps", hdr)
	c.assigned = assignedOf(hdr)
	return c.inner.PotentialEndOfBlockOps(ctx, hdr, lastSettled, source)
}

func (c *checker[T]) BuildBlock(
	hdr *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	ops []T,
	settled hook.Settled,
) (*types.Block, error) {
	c.tb.Helper()
	c.requireAsStamped("BuildBlock", hdr)
	if got := assignedOf(hdr); got != c.assigned {
		c.tb.Errorf("PotentialEndOfBlockOps changed SAE-assigned fields from %+v to %+v", c.assigned, got)
	}
	gasUsed := hdr.GasUsed

	b, err := c.inner.BuildBlock(hdr, blockCtx, txs, receipts, ops, settled)
	if err != nil {
		return nil, err
	}
	final := b.Header()
	c.requireAsStamped("built block", final)
	if got := assignedOf(final); got != c.assigned {
		c.tb.Errorf("BuildBlock changed SAE-assigned fields from %+v to %+v", c.assigned, got)
	}
	if final.GasUsed != gasUsed {
		c.tb.Errorf("BuildBlock changed gas used from %d to %d", gasUsed, final.GasUsed)
	}
	if got := c.points.SettledBy(final); got != settled {
		c.tb.Errorf("built block settled marker %+v, want %+v", got, settled)
	}
	return b, nil
}
