//go:build !windows
// +build !windows

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/dbtest"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func newBenchDB(b testing.TB) database.Database {
	dbPath := filepath.Join(b.TempDir(), "bench.fw")
	db, err := New(dbPath, nil, logging.NoLog{})
	if err != nil {
		b.Fatal(err)
	}
	return db
}

func BenchmarkInterface(b *testing.B) {
	for _, size := range dbtest.BenchmarkSizes {
		keys, values := dbtest.SetupBenchmark(b, size[0], size[1], size[2])
		for name, bench := range dbtest.Benchmarks {
			b.Run(fmt.Sprintf("firewood_%d_pairs_%d_keys_%d_values_%s", size[0], size[1], size[2], name), func(b *testing.B) {
				db := newBenchDB(b)
				bench(b, db, keys, values)
				_ = db.Close()
			})
		}
	}
}
