// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

// TestGeneratorStateDecodesLibevmEntry asserts that [generatorState] stays
// RLP-compatible with the journalGenerator entry libevm persists: a synchronous
// rebuild over the empty root writes a completed generator, which must decode
// with Done set.
func TestGeneratorStateDecodesLibevmEntry(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)

	// No persisted snapshot exists, so New rebuilds; NoBuild/AsyncBuild are
	// unset so it blocks until the (empty) generation completes.
	snaps, err := snapshot.New(snapshot.Config{CacheSize: 16}, db, tdb, types.EmptyRootHash)
	require.NoError(t, err, "snapshot.New()")
	t.Cleanup(snaps.Release)

	blob := rawdb.ReadSnapshotGenerator(db)
	require.NotEmpty(t, blob, "rawdb.ReadSnapshotGenerator() after rebuild")

	var gen generatorState
	require.NoError(t, rlp.DecodeBytes(blob, &gen), "rlp.DecodeBytes() of libevm generator entry")
	require.True(t, gen.Done, "generator Done after synchronous rebuild")

	rec := loggingtest.NewRecorder(logging.Trace)
	logSnapshotState(rec, db, snaps, types.EmptyRootHash)
	require.Empty(t, rec.AtLeast(logging.Warn), "logSnapshotState() warnings on a healthy snapshot")

	infos := rec.At(logging.Info)
	require.Len(t, infos, 1, "logSnapshotState() Info records")
	require.True(t, hasBoolField(t, infos[0].Fields, "generationDone"), "generationDone field value")
}

func TestLogSnapshotStateInProgress(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)
	snaps, err := snapshot.New(snapshot.Config{CacheSize: 16}, db, tdb, types.EmptyRootHash)
	require.NoError(t, err, "snapshot.New()")
	t.Cleanup(snaps.Release)

	// Overwrite the generator entry with an in-progress marker halfway
	// through the keyspace.
	halfway := generatorState{
		Done:   false,
		Marker: []byte{0x80, 0x00, 0x00, 0x00},
	}
	blob, err := rlp.EncodeToBytes(halfway)
	require.NoError(t, err, "rlp.EncodeToBytes(generatorState)")
	rawdb.WriteSnapshotGenerator(db, blob)

	rec := loggingtest.NewRecorder(logging.Trace)
	logSnapshotState(rec, db, snaps, types.EmptyRootHash)
	require.Empty(t, rec.AtLeast(logging.Warn), "logSnapshotState() warnings")

	infos := rec.At(logging.Info)
	require.Len(t, infos, 1, "logSnapshotState() Info records")
	require.False(t, hasBoolField(t, infos[0].Fields, "generationDone"), "generationDone field value")

	var pct float64
	for _, f := range infos[0].Fields {
		if f.Key == "generationPct" {
			require.Equal(t, zapcore.Float64Type, f.Type, "generationPct field type")
			pct = math.Float64frombits(uint64(f.Integer)) //#nosec G115 -- zap stores float64 bits
		}
	}
	require.InDelta(t, 50.0, pct, 0.1, "generationPct for a halfway marker")
}

// hasBoolField returns the value of the named bool field, failing the test if
// it is absent.
func hasBoolField(t *testing.T, fields []zapcore.Field, key string) bool {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			require.Equal(t, zapcore.BoolType, f.Type, "field %q type", key)
			return f.Integer == 1
		}
	}
	t.Fatalf("field %q not logged", key)
	return false
}
