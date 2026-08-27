// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package blocks

import (
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
)

// CmpOpt returns a configuration for [cmp.Diff] to compare [Block] instances in
// tests.
func CmpOpt() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(Block{}, ancestry{}),
		cmpopts.IgnoreFields(
			Block{},
			"bounds",
			"hooks",
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
		return e.byGas.Rate() == f.byGas.Rate() &&
			e.byGas.Compare(f.byGas.Time) == 0 &&
			e.receiptRoot == f.receiptRoot &&
			cmp.Equal(e.receipts, f.receipts, cmputils.CmpByMerkleRoots[types.Receipts]()) &&
			e.stateRootPost == f.stateRootPost
	})
	return fn(e, f)
}

// IgnoreLastSettledExecutionArtefacts returns an option for [cmp.Diff] that
// ignores execution artefacts of the last-settled ancestor of a [Block]. This
// SHOULD only be used for testing database recovery, during which blocks older
// than the chain's last settled do not have [Block.RestoreExecutionArtefacts]
// called (to simplify state sync).
func IgnoreLastSettledExecutionArtefacts(tb testing.TB) cmp.Option {
	// [cmputils.IfIn] filters by type, but we need to ignore based on a field
	// name. To avoid hard-to-debug failures due to a name mismatch, we fail
	// early if the field no longer exists. Although a bit clunky, it's cleaner
	// than introducing a marker type in [ancestry] purely to detect
	// last-settled blocks.
	const field = "lastSettled"
	if _, ok := reflect.TypeFor[ancestry]().FieldByName(field); !ok {
		tb.Fatalf("Type %T does not contain field named %q to ignore last-settled execution artefacts", ancestry{}, field)
	}

	return cmputils.IfIn[ancestry](
		cmp.FilterPath(
			func(p cmp.Path) bool {
				for _, s := range p {
					const pathStep = "." + field
					if s.String() == pathStep {
						return true
					}
				}
				return false
			},
			cmpopts.IgnoreTypes(
				atomic.Pointer[executionResults]{},
			),
		),
	)
}
