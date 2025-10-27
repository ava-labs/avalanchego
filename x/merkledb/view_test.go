// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"encoding/binary"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var hashChangedNodesTests = []struct {
	name             string
	numKeys          uint64
	expectedRootHash string
}{
	{
		name:             "1",
		numKeys:          1,
		expectedRootHash: "2A4DRkSWbTvSxgA1UMGp1Mpt1yzMFaeMMiDnrijVGJXPcRYiD4",
	},
	{
		name:             "10",
		numKeys:          10,
		expectedRootHash: "2PGy7QvbYwVwn5QmLgj4KBgV2BisanZE8Nue2SxK9ffybb4mAn",
	},
	{
		name:             "100",
		numKeys:          100,
		expectedRootHash: "LCeS4DWh6TpNKWH4ke9a2piSiwwLbmxGUj8XuaWx1XDGeCMAv",
	},
	{
		name:             "1000",
		numKeys:          1000,
		expectedRootHash: "2S6f84wdRHmnx51mj35DF2owzf8wio5pzNJXfEWfFYFNxUB64T",
	},
	{
		name:             "10000",
		numKeys:          10000,
		expectedRootHash: "wF6UnhaDoA9fAqiXAcx27xCYBK2aspDBEXkicmC7rs8EzLCD8",
	},
	{
		name:             "100000",
		numKeys:          100000,
		expectedRootHash: "2Dy3RWZeNDUnUvzXpruB5xdp1V7xxb14M53ywdZVACDkdM66M1",
	},
}

func makeViewForHashChangedNodes(t require.TestingT, numKeys uint64, parallelism uint) *view {
	config := NewConfig()
	config.RootGenConcurrency = parallelism
	db, err := newDatabase(
		context.Background(),
		memdb.New(),
		config,
		&mockMetrics{},
	)
	require.NoError(t, err)

	ops := make([]database.BatchOp, 0, numKeys)
	for i := uint64(0); i < numKeys; i++ {
		k := binary.AppendUvarint(nil, i)
		ops = append(ops, database.BatchOp{
			Key:   k,
			Value: hashing.ComputeHash256(k),
		})
	}

	ctx := context.Background()
	viewIntf, err := db.NewView(ctx, ViewChanges{BatchOps: ops})
	require.NoError(t, err)

	view := viewIntf.(*view)
	require.NoError(t, view.calculateNodeChanges(ctx))
	return view
}

func Test_HashChangedNodes(t *testing.T) {
	for _, test := range hashChangedNodesTests {
		t.Run(test.name, func(t *testing.T) {
			view := makeViewForHashChangedNodes(t, test.numKeys, 16)
			ctx := t.Context()
			view.hashChangedNodes(ctx)
			require.Equal(t, test.expectedRootHash, view.changes.rootID.String())
		})
	}
}

func Benchmark_HashChangedNodes(b *testing.B) {
	for _, test := range hashChangedNodesTests {
		view := makeViewForHashChangedNodes(b, test.numKeys, 1)
		ctx := b.Context()
		b.Run(test.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				view.hashChangedNodes(ctx)
			}
		})
	}
}

func BenchmarkView_NewIteratorWithStartAndPrefix(b *testing.B) {
	var (
		keyMaxLen = 20
		numKeys   = uint64(1_000_000)
	)

	rand := rand.New(rand.NewSource(time.Now().Unix())) // #nosec G404

	db, err := getBasicDB()
	require.NoError(b, err)

	ops := make([]database.BatchOp, 0, numKeys)
	for range numKeys {
		key := make([]byte, rand.Intn(keyMaxLen))
		rand.Read(key)

		value := make([]byte, rand.Intn(keyMaxLen))
		rand.Read(value)

		ops = append(ops, database.BatchOp{
			Key:   key,
			Value: value,
		})
	}

	ctx := b.Context()
	view, err := db.NewView(ctx, ViewChanges{BatchOps: ops})
	require.NoError(b, err)

	for range b.N {
		b.StopTimer()
		start := make([]byte, rand.Intn(keyMaxLen))
		rand.Read(start)

		prefix := make([]byte, rand.Intn(keyMaxLen/2))
		rand.Read(prefix)

		b.StartTimer()
		view.NewIteratorWithStartAndPrefix(start, prefix)
	}
}
