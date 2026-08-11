// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// This file prototypes an answer to one question: the ancestors walk is bound
// by random-read latency against the block store, so would issuing those reads
// concurrently help?
//
// The walk cannot be parallelised as written, because a block's parent ID is
// only known once that block has been read. The height index breaks that
// dependency: heights are contiguous, so the whole range of IDs can be resolved
// with a single ordered scan and the block reads then issued together.
//
// A local test database is served from the page cache, so it exhibits none of
// the read latency the question is about. [slowDB] injects that latency
// explicitly, which is what makes the comparison meaningful.
//
// This exercises the production implementation, [GetAncestorBytes]. The height
// of the requested block is resolved once per request by the caller
// ([VM.getAncestorsIndexed]) and is not part of the per-block cost measured
// here; that one lookup is a single decoded block read.

// readLatency approximates the per-get latency observed on the Fuji C-Chain
// base database after Helicon (~232us measured; rounded down here).
const readLatency = 200 * time.Microsecond

// slowDB adds a fixed latency to every point read, modelling storage that is
// not resident in memory. Iteration is left untouched: an ordered scan over
// adjacent keys costs a seek plus streaming, not a seek per entry.
type slowDB struct {
	database.Database
	delay time.Duration
}

func (db *slowDB) Get(key []byte) ([]byte, error) {
	if db.delay > 0 {
		time.Sleep(db.delay)
	}
	return db.Database.Get(key)
}

func buildIndexedState(b *testing.B, blks []block.Block, delay time.Duration) *versiondb.Database {
	b.Helper()

	raw, err := pebbledb.New(b.TempDir(), storages[1].config, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(b, err)
	b.Cleanup(func() {
		require.NoError(b, raw.Close())
	})

	// Write through the raw database so that building the fixture is not
	// slowed by the injected latency.
	warm := versiondb.New(raw)
	st := New(warm)
	for i, blk := range blks {
		require.NoError(b, st.PutBlock(blk))
		require.NoError(b, st.SetBlockIDAtHeight(uint64(i), blk.ID()))
	}
	require.NoError(b, warm.Commit())
	require.NoError(b, raw.Compact(nil, nil))

	return versiondb.New(&slowDB{Database: raw, delay: delay})
}

// BenchmarkParallelAncestorsFetch compares the serial parent-chasing walk
// against resolving the height range with one scan and fetching the blocks
// concurrently. Both use the bytes-only read, so read concurrency is the only
// difference being measured.
//
// "noLatency" is a page-cache-resident database, where there is nothing to
// overlap. "storageLatency" injects [readLatency] per point read, which is the
// regime the production numbers indicate.
func BenchmarkParallelAncestorsFetch(b *testing.B) {
	blks := buildChain(b, chainLen, innerBlockSize)

	tops := make([]uint64, 0, (chainLen-walkLen)/walkLen)
	for i := walkLen; i < chainLen; i += walkLen {
		tops = append(tops, uint64(i))
	}

	for _, regime := range []struct {
		name  string
		delay time.Duration
	}{
		{"noLatency", 0},
		{"storageLatency", readLatency},
	} {
		vdb := buildIndexedState(b, blks, regime.delay)

		b.Run(regime.name+"/serial", func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				st := New(vdb)
				n, err := walkBytes(st, blks[tops[i%len(tops)]].ID())
				if err != nil {
					b.Fatal(err)
				}
				if n == 0 {
					b.Fatal("walked no bytes")
				}
				i++
			}
			b.ReportMetric(float64(walkLen), "blocks/op")
		})

		b.Run(regime.name+"/resolveOnly", func(b *testing.B) {
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				st := New(vdb)
				top := tops[i%len(tops)]
				blkIDs, err := st.GetBlockIDsInRange(top+1-walkLen, top)
				if err != nil {
					b.Fatal(err)
				}
				if len(blkIDs) != walkLen {
					b.Fatalf("got %d ids, want %d", len(blkIDs), walkLen)
				}
				i++
			}
			b.ReportMetric(float64(walkLen), "blocks/op")
		})

		for _, workers := range []int{1, 4, 16, 32, 64} {
			b.Run(regime.name+"/parallel/"+strconv.Itoa(workers), func(b *testing.B) {
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					st := New(vdb)
					out, err := GetAncestorBytes(
						st, st,
						tops[i%len(tops)],
						walkLen,
						1<<30,
						time.Now().Add(time.Hour),
						time.Now,
						workers,
					)
					if err != nil {
						b.Fatal(err)
					}
					if len(out) != walkLen {
						b.Fatalf("got %d blocks, want %d", len(out), walkLen)
					}
					total := 0
					for _, blkBytes := range out {
						total += len(blkBytes)
					}
					if total == 0 {
						b.Fatal("fetched no bytes")
					}
					i++
				}
				b.ReportMetric(float64(walkLen), "blocks/op")
			})
		}
	}
}
