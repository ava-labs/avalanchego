// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prefixdb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestInterface(t *testing.T) {
	for name, test := range dbtest.Tests {
		t.Run(name, func(t *testing.T) {
			db := memdb.New()
			test(t, New([]byte("hello"), db))
			test(t, New([]byte("world"), db))
			test(t, New([]byte("wor"), New([]byte("ld"), db)))
			test(t, New([]byte("ld"), New([]byte("wor"), db)))
			test(t, NewNested([]byte("wor"), New([]byte("ld"), db)))
			test(t, NewNested([]byte("ld"), New([]byte("wor"), db)))
		})
	}
}

func TestPrefixLimit(t *testing.T) {
	testString := []string{"hello", "world", "a\xff", "\x01\xff\xff\xff\xff"}
	expected := []string{"hellp", "worle", "b\x00", "\x02\x00\x00\x00\x00"}
	for i, str := range testString {
		db := newDB([]byte(str), nil)
		require.Equal(t, db.dbLimit, []byte(expected[i]))
	}
}

func FuzzKeyValue(f *testing.F) {
	dbtest.FuzzKeyValue(f, New([]byte(""), memdb.New()))
}

func FuzzNewIteratorWithPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithPrefix(f, New([]byte(""), memdb.New()))
}

func FuzzNewIteratorWithStartAndPrefix(f *testing.F) {
	dbtest.FuzzNewIteratorWithStartAndPrefix(f, New([]byte(""), memdb.New()))
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("prefixdb_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := New([]byte("hello"), memdb.New())
				bench(b, db, keys, values)
			})
		}
	}
}
