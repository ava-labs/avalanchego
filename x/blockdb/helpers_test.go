package blockdb

import (
	"crypto/rand"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func newTestDatabase(t *testing.T, syncToDisk bool, opts *DatabaseConfig) (*Database, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "blockdb_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	idxDir := dir + "/idx"
	dataDir := dir + "/dat"
	var config DatabaseConfig
	if opts != nil {
		config = *opts
	} else {
		config = DefaultDatabaseConfig()
	}
	db, err := New(idxDir, dataDir, syncToDisk, true, config, logging.NoLog{})
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create database: %v", err)
	}
	cleanup := func() {
		db.Close()
		os.RemoveAll(dir)
	}
	return db, cleanup
}

// randomBlock generates a random block of size 1KB-50KB.
func randomBlock(t *testing.T) []byte {
	size, err := rand.Int(rand.Reader, big.NewInt(50*1024-1024+1))
	if err != nil {
		t.Fatalf("failed to generate random size: %v", err)
	}
	blockSize := int(size.Int64()) + 1024 // 1KB to 50KB
	b := make([]byte, blockSize)
	_, err = rand.Read(b)
	if err != nil {
		t.Fatalf("failed to fill random block: %v", err)
	}
	return b
}

func checkDatabaseState(t *testing.T, db *Database, maxHeight uint64, maxContiguousHeight uint64) {
	if got := db.maxBlockHeight.Load(); got != maxHeight {
		t.Fatalf("maxBlockHeight: got %d, want %d", got, maxHeight)
	}
	gotMCH, ok := db.MaxContiguousHeight()
	if maxContiguousHeight != unsetHeight && !ok {
		t.Fatalf("MaxContiguousHeight is not set, want %d", maxContiguousHeight)
	}
	if ok && gotMCH != maxContiguousHeight {
		t.Fatalf("maxContiguousHeight: got %d, want %d", gotMCH, maxContiguousHeight)
	}
}
