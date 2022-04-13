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

package encdb

import (
	"testing"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/memdb"
)

func TestInterface(t *testing.T) {
	pw := "lol totally a secure password" // #nosec G101
	for _, test := range database.Tests {
		unencryptedDB := memdb.New()
		db, err := New([]byte(pw), unencryptedDB)
		if err != nil {
			t.Fatal(err)
		}

		test(t, db)
	}
}

func BenchmarkInterface(b *testing.B) {
	pw := "lol totally a secure password" // #nosec G101
	for _, size := range database.BenchmarkSizes {
		keys, values := database.SetupBenchmark(b, size[0], size[1], size[2])
		for _, bench := range database.Benchmarks {
			unencryptedDB := memdb.New()
			db, err := New([]byte(pw), unencryptedDB)
			if err != nil {
				b.Fatal(err)
			}
			bench(b, db, "encdb", keys, values)
		}
	}
}
