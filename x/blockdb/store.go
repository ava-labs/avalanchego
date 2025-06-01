package blockdb

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	indexFileName = "blockdb.idx"
	dataFileName  = "blockdb.dat"
)

// BlockHeight defines the type for block heights.
type BlockHeight = uint64

// Block defines the type for block data.
type Block = []byte

// Store is a collection of blockchain blocks. It provides methods to read, write, and manage blocks on disk.
type Store struct {
	indexFile *os.File
	dataFile  *os.File
	options   StoreOptions
	header    IndexFileHeader
	log       logging.Logger

	// syncToDisk determines if fsync is called after each write for durability.
	syncToDisk bool
	// maxBlockHeight tracks the highest block height that has been written to the store, even if there are gaps in the sequence.
	maxBlockHeight atomic.Uint64
	// closed indicates if the store has been closed.
	closed bool
	// mu synchronizes access to the store.
	mu sync.RWMutex
	// nextDataWriteOffset tracks the next position to write new data in the data file.
	nextDataWriteOffset atomic.Uint64
	// maxContiguousHeight tracks the highest block height known to be contiguously stored.
	maxContiguousHeight atomic.Uint64
}

// MaxContiguousHeight returns the highest block height known to be contiguously stored.
func (s *Store) MaxContiguousHeight() BlockHeight {
	return s.maxContiguousHeight.Load()
}

// MinHeight returns the minimum block height configured for this store.
func (s *Store) MinHeight() uint64 {
	return s.header.MinBlockHeight
}
