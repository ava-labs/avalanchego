// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"crypto"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// buildChain returns [n] linked blocks, oldest first, each carrying an inner
// block of [innerSize] bytes. All blocks are signed, so each one carries a
// staking certificate - the thing a full parse would have to decode.
func buildChain(tb testing.TB, n int, innerSize int) []block.Block {
	tb.Helper()

	tlsCert, err := staking.NewTLSCert()
	require.NoError(tb, err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(tb, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	chainID := ids.GenerateTestID()
	innerBytes := make([]byte, innerSize)
	for i := range innerBytes {
		innerBytes[i] = byte(i)
	}

	blks := make([]block.Block, n)
	parentID := ids.GenerateTestID()
	for i := range blks {
		blk, err := block.Build(
			parentID,
			time.Unix(int64(i), 0),
			uint64(i),
			block.Epoch{},
			cert,
			innerBytes,
			chainID,
			key,
		)
		require.NoError(tb, err)
		blks[i] = blk
		parentID = blk.ID()
	}
	return blks
}

// TestInnerBlockBytes pins the layout assumption that [innerBlockBytes] relies
// on: that [blockWrapper.Block] is the wrapper's first serialized field and so
// begins at a fixed offset.
func TestInnerBlockBytes(t *testing.T) {
	require := require.New(t)

	for _, size := range []int{0, 1, 100, 10_000} {
		inner := make([]byte, size)
		for i := range inner {
			inner[i] = byte(i)
		}

		wrapperBytes, err := Codec.Marshal(CodecVersion, &blockWrapper{
			Block:  inner,
			Status: choices.Accepted,
		})
		require.NoError(err)

		got, err := innerBlockBytes(wrapperBytes)
		require.NoError(err)
		require.Equal(inner, got, "size %d", size)
	}
}

func TestInnerBlockBytesErrors(t *testing.T) {
	require := require.New(t)

	wrapperBytes, err := Codec.Marshal(CodecVersion, &blockWrapper{
		Block:  []byte{1, 2, 3},
		Status: choices.Accepted,
	})
	require.NoError(err)

	for n := range innerBlockOffset {
		_, err := innerBlockBytes(wrapperBytes[:n])
		require.ErrorIs(err, errTruncatedBlockWrapper, "truncated to %d bytes", n)
	}

	// A length prefix that runs past the end of the buffer must be rejected
	// rather than panicking or aliasing out of bounds. The prefix claims 3
	// bytes of block, so cut the buffer to leave only 1.
	truncated := wrapperBytes[:innerBlockOffset+1]
	_, err = innerBlockBytes(truncated)
	require.ErrorIs(err, errTruncatedBlockWrapper)

	// Trailing bytes after the block field are the wrapper's other fields and
	// must not be mistaken for truncation.
	_, err = innerBlockBytes(wrapperBytes[:innerBlockOffset+3])
	require.NoError(err)

	corrupt := make([]byte, len(wrapperBytes))
	copy(corrupt, wrapperBytes)
	corrupt[0] = 0xff
	_, err = innerBlockBytes(corrupt)
	require.ErrorIs(err, errBlockWrongVersion)
}

// TestGetBlockBytesAndParent checks the new accessor agrees with GetBlock on
// both a cold cache (read from disk) and a warm one.
func TestGetBlockBytesAndParent(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	bs := NewBlockState(db)

	blks := buildChain(t, 8, 128)
	for _, blk := range blks {
		require.NoError(bs.PutBlock(blk))
	}

	// Warm cache: PutBlock populates it.
	for _, blk := range blks {
		gotBytes, gotParent, err := bs.GetBlockBytesAndParent(blk.ID())
		require.NoError(err)
		require.Equal(blk.Bytes(), gotBytes)
		require.Equal(blk.ParentID(), gotParent)
	}

	// Cold cache: a fresh BlockState over the same database reads from disk.
	cold := NewBlockState(db)
	for _, blk := range blks {
		gotBytes, gotParent, err := cold.GetBlockBytesAndParent(blk.ID())
		require.NoError(err)
		require.Equal(blk.Bytes(), gotBytes)
		require.Equal(blk.ParentID(), gotParent)

		// And it must agree with the decoding path it replaces.
		viaGetBlock, err := cold.GetBlock(blk.ID())
		require.NoError(err)
		require.Equal(viaGetBlock.Bytes(), gotBytes)
		require.Equal(viaGetBlock.ParentID(), gotParent)
	}

	_, _, err := cold.GetBlockBytesAndParent(ids.GenerateTestID())
	require.ErrorIs(err, database.ErrNotFound)
}

// TestGetBlockBytesAndParentDoesNotPolluteCache checks that serving a
// historical walk does not evict blocks consensus is relying on.
func TestGetBlockBytesAndParentDoesNotPolluteCache(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	writer := NewBlockState(db)
	blks := buildChain(t, 4, 128)
	for _, blk := range blks {
		require.NoError(writer.PutBlock(blk))
	}

	reader := NewBlockState(db).(*blockState)
	for _, blk := range blks {
		_, _, err := reader.GetBlockBytesAndParent(blk.ID())
		require.NoError(err)
	}
	for _, blk := range blks {
		_, found := reader.blkCache.Get(blk.ID())
		require.False(found, "walk populated the block cache")
	}
}

// walkLen is the number of blocks returned per GetAncestors response. Before
// Helicon, a response on the Fuji C-Chain held roughly this many blocks before
// hitting the 1.6 MB byte cap.
const walkLen = 250

// chainLen is the number of blocks in the benchmark database. It is much larger
// than walkLen so that successive walks start at different heights and the
// working set is the whole database rather than one hot range.
const chainLen = 4000

// innerBlockSize approximates a Fuji C-Chain block.
const innerBlockSize = 6 * 1024

// storage describes how much of the database pebble is allowed to keep in
// memory. Real nodes walk history far larger than any cache, so the "onDisk"
// case is the one that matches production; "cached" isolates the CPU cost by
// removing storage from the measurement entirely.
type storage struct {
	name   string
	config []byte
}

var storages = []storage{
	{name: "cached", config: nil}, // pebbledb default: 512 MiB cache
	{name: "onDisk", config: []byte(`{"cacheSize":4194304,"memTableSize":1048576}`)},
}

func newBenchDB(b *testing.B, cfg []byte, blks []block.Block) database.Database {
	b.Helper()

	db, err := pebbledb.New(b.TempDir(), cfg, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(b, err)
	b.Cleanup(func() {
		require.NoError(b, db.Close())
	})

	writer := NewBlockState(db)
	for _, blk := range blks {
		require.NoError(b, writer.PutBlock(blk))
	}
	// Force the memtable out to disk so that reads exercise the LSM rather than
	// finding everything still in memory.
	require.NoError(b, db.Compact(nil, nil))
	return db
}

func walkDecode(bs BlockState, from ids.ID) (int, error) {
	total := 0
	for range walkLen {
		blk, err := bs.GetBlock(from)
		if err != nil {
			return total, err
		}
		total += len(blk.Bytes())
		from = blk.ParentID()
	}
	return total, nil
}

func walkBytes(bs BlockState, from ids.ID) (int, error) {
	total := 0
	for range walkLen {
		blkBytes, parentID, err := bs.GetBlockBytesAndParent(from)
		if err != nil {
			return total, err
		}
		total += len(blkBytes)
		from = parentID
	}
	return total, nil
}

// BenchmarkAncestorsWalk measures the loop in [proposervm.VM.GetAncestors],
// which walks parent links and collects each block's bytes.
//
// "decode" is the existing path: GetBlock decodes the wrapper, decodes the
// block, hashes it to compute its ID, and parses its staking certificate.
// "bytes" is GetBlockBytesAndParent, which does none of that.
//
// Every iteration uses a fresh BlockState, so the proposervm block cache is
// always cold. That is the case that matters: a historical walk touches each
// block once and reuses nothing.
func BenchmarkAncestorsWalk(b *testing.B) {
	blks := buildChain(b, chainLen, innerBlockSize)

	// Starting points spread across the chain so that consecutive iterations
	// read different blocks, as independently bootstrapping peers would.
	starts := make([]ids.ID, 0, (chainLen-walkLen)/walkLen)
	for i := walkLen; i < chainLen; i += walkLen {
		starts = append(starts, blks[i].ID())
	}

	for _, st := range storages {
		db := newBenchDB(b, st.config, blks)

		for _, walk := range []struct {
			name string
			fn   func(BlockState, ids.ID) (int, error)
		}{
			{"decode", walkDecode},
			{"bytes", walkBytes},
		} {
			b.Run(st.name+"/"+walk.name, func(b *testing.B) {
				b.ReportAllocs()
				i := 0
				for b.Loop() {
					bs := NewBlockState(db)
					n, err := walk.fn(bs, starts[i%len(starts)])
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
		}
	}
}

// BenchmarkParentIDExtraction isolates the decoding work, with no database
// involved, so the saving can be read independently of storage speed.
func BenchmarkParentIDExtraction(b *testing.B) {
	blk := buildChain(b, 1, innerBlockSize)[0]
	blkBytes := blk.Bytes()
	want := blk.ParentID()

	b.Run("parse", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			parsed, err := block.ParseWithoutVerification(blkBytes)
			if err != nil {
				b.Fatal(err)
			}
			if parsed.ParentID() != want {
				b.Fatal("wrong parent")
			}
		}
	})

	b.Run("offset", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			got, err := block.ParentID(blkBytes)
			if err != nil {
				b.Fatal(err)
			}
			if got != want {
				b.Fatal("wrong parent")
			}
		}
	})
}
