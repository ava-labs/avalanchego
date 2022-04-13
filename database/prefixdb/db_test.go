// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"testing"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/memdb"
)

func TestInterface(t *testing.T) {
	for _, test := range database.Tests {
		db := memdb.New()
		test(t, New([]byte("hello"), db))
		test(t, New([]byte("world"), db))
		test(t, New([]byte("wor"), New([]byte("ld"), db)))
		test(t, New([]byte("ld"), New([]byte("wor"), db)))
		test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
		test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
	}
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			db := New([]byte("hello"), memdb.New())
			bench(b, db, "prefixdb", keys, values)
		}
	}
}
