// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package blocks

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ava-labs/strevm/cmputils"
	"github.com/ava-labs/strevm/saetest"
)

// CmpOpt returns a configuration for [cmp.Diff] to compare [Block] instances in
// tests.
func CmpOpt() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(Block{}, ancestry{}),
		cmpopts.IgnoreFields(
			Block{},
			"bounds",
			"interimExecutionTime",
		),
		cmputils.IfIn[Block](cmputils.NilSlicesAreEmpty[types.Transactions]()),
		cmputils.IfIn[Block](cmputils.NilSlicesAreEmpty[[]*types.Header]()),
		cmputils.IfIn[Block](cmpopts.IgnoreTypes(
			make(chan struct{}),
		)),
		cmputils.IfIn[Block](cmpopts.IgnoreInterfaces(
			struct{ logging.Logger }{},
		)),
		cmputils.Blocks(),
		cmputils.Headers(),
		cmputils.LoadAtomicPointers[ancestry](),
		cmputils.LoadAtomicPointers[executionResults](),
		cmp.Comparer((*executionResults).equalForTests),
	}
}

func (e *executionResults) equalForTests(f *executionResults) bool {
	fn := cmputils.WithNilCheck(func(e, f *executionResults) bool {
		return true &&
			e.byGas.Rate() == f.byGas.Rate() &&
			e.byGas.Compare(f.byGas.Time) == 0 &&
			e.receiptRoot == f.receiptRoot &&
			saetest.MerkleRootsEqual(e.receipts, f.receipts) &&
			e.stateRootPost == f.stateRootPost
	})
	return fn(e, f)
}
