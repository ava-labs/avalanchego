// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

package cmputils

import (
	"fmt"
	"math/big"
	"reflect"
	"sync/atomic"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// BigInts returns a [cmp.Comparer] for [big.Int] pointers. A nil pointer is not
// equal to zero.
func BigInts() cmp.Option {
	return ComparerWithNilCheck(func(a, b *big.Int) bool {
		return a.Cmp(b) == 0
	})
}

// HexutilBigs returns a [cmp.Comparer] for [hexutil.Big] pointers. A nil
// pointer is not equal to zero.
func HexutilBigs() cmp.Option {
	return ComparerWithNilCheck(func(a, b *hexutil.Big) bool {
		return (*big.Int)(a).Cmp((*big.Int)(b)) == 0
	})
}

// BlocksByHash returns a [cmp.Comparer] for [types.Block] pointers, equating
// them by hash alone.
func BlocksByHash() cmp.Option {
	return ComparerWithNilCheck(func(b, c *types.Block) bool {
		return b.Hash() == c.Hash()
	})
}

// TransactionsByHash returns a [cmp.Comparer] for [types.Transaction] pointers,
// equating them by hash alone.
func TransactionsByHash() cmp.Option {
	return ComparerWithNilCheck(func(t, u *types.Transaction) bool {
		return t.Hash() == u.Hash()
	})
}

// Receipts returns a set of [cmp.Options] for comparing [types.Receipt] values.
func Receipts() cmp.Option {
	return cmp.Options{
		IfIn[types.Receipt](BigInts()),
	}
}

// ReceiptsByTxHash returns a [cmp.Comparer] for [types.Receipt] pointers,
// equating them by transaction hash alone.
func ReceiptsByTxHash() cmp.Option {
	return ComparerWithNilCheck(func(r, s *types.Receipt) bool {
		return r.TxHash == s.TxHash
	})
}

// Blocks returns a set of [cmp.Options] for comparing [types.Block] values.
// The [Headers] option MUST be used alongside this but isn't included
// automatically, to avoid duplication.
func Blocks() cmp.Option {
	return cmp.Options{
		cmp.AllowUnexported(types.Block{}),
		cmpopts.IgnoreFields(types.Block{}, "hash", "size", "extra"),
		IfIn[types.Block](TransactionsByHash()),
	}
}

// Headers returns a set of [cmp.Options] for comparing [type.Headers] values.
func Headers() cmp.Option {
	return cmp.Options{
		cmpopts.IgnoreFields(types.Header{}, "extra"),
		// Without the [IfIn] filter, any other use of [BigInts] will result in
		// ambiguous comparers as [cmp] can't deduplicate them.
		IfIn[types.Header](BigInts()),
	}
}

// LoadAtomicPointers returns a set of [cmp.Transformer] options that convert
// [atomic.Pointer] instances of `T` into their underlying `*T`. If the atomic
// under test is not itself a pointer (i.e. not *atomic.Pointer) then the
// returned options are NOT safe for concurrent use with said atomic as its lock
// is copied when passed as an argument to the transformer.
func LoadAtomicPointers[T any]() cmp.Options {
	return cmp.Options{
		// Although accepting an [atomic.Pointer] value copies a lock, this is
		// unavoidable but OK in tests given the non-concurrency documentation
		// above.
		cmp.Transformer(fmt.Sprintf("atomicOf_%s", typeName[T]()), func(p atomic.Pointer[T]) *T { //nolint:govet
			return p.Load()
		}),
		cmp.Transformer(fmt.Sprintf("pointerOfAtomicOf_%s", typeName[T]()), func(p *atomic.Pointer[T]) *T {
			return p.Load()
		}),
	}
}

// NilSlicesAreEmpty returns a [cmp.Transformer] that converts `S(nil)` values
// into `S{}`, for use when [cmpopts.EquateEmpty] is too general.
func NilSlicesAreEmpty[S ~[]E, E any]() cmp.Option {
	name := fmt.Sprintf("nilSliceOf_%s_isEmpty", typeName[E]())
	return cmp.Transformer(name, func(s S) S {
		if s == nil {
			return S{}
		}
		return s
	})
}

func typeName[T any]() string {
	t := reflect.TypeFor[T]()
	if t.Kind() == reflect.Pointer {
		return fmt.Sprintf("pointerTo_%s", t.Elem().String())
	}
	return t.String()
}

// StateDBs returns a [cmp.Transformer] that converts [state.StateDB] instances
// into [state.Dump] equivalents.
func StateDBs() cmp.Option {
	return cmp.Transformer("StateDB.RawDump", func(db *state.StateDB) state.Dump {
		return db.RawDump(&state.DumpConfig{})
	})
}
