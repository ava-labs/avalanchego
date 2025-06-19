package blockdb

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	indexFileName = "blockdb.idx"
	dataFileName  = "blockdb.dat"

	// Since 0 is a valid height, math.MaxUint64 is used to indicate unset height.
	// It is not be possible for block height to be max uint64 as it would overflow the index entry offset
	unsetHeight = math.MaxUint64
)

// Database stores blockchain blocks on disk and provides methods to read, and write blocks.
type Database struct {
	indexFile *os.File
	dataFile  *os.File
	options   DatabaseConfig
	header    indexFileHeader
	log       logging.Logger
	mu        sync.RWMutex
	closed    bool

	// syncToDisk determines if fsync is called after each write for durability.
	syncToDisk bool
	// maxBlockHeight tracks the highest block height that has been written to the db, even if there are gaps in the sequence.
	maxBlockHeight atomic.Uint64
	// nextDataWriteOffset tracks the next position to write new data in the data file.
	nextDataWriteOffset atomic.Uint64
	// maxContiguousHeight tracks the highest block height known to be contiguously stored.
	maxContiguousHeight atomic.Uint64
}

func (s *Database) openOrCreateFiles(indexDir, dataDir string, truncate bool) error {
	indexPath := filepath.Join(indexDir, indexFileName)
	dataPath := filepath.Join(dataDir, dataFileName)

	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return fmt.Errorf("failed to create index directory %s: %w", indexDir, err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	openFlags := os.O_RDWR | os.O_CREATE
	if truncate {
		openFlags |= os.O_TRUNC
	}

	var err error
	s.indexFile, err = os.OpenFile(indexPath, openFlags, 0666)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	s.dataFile, err = os.OpenFile(dataPath, openFlags, 0666)
	if err != nil {
		// Clean up partially opened resources
		s.indexFile.Close()
		return fmt.Errorf("failed to open data file %s: %w", dataPath, err)
	}
	return nil
}

func (s *Database) loadOrInitializeHeader(truncate bool) error {
	if truncate {
		s.header = indexFileHeader{
			Version:             IndexFileVersion,
			MinHeight:           s.options.MinimumHeight,
			MaxDataFileSize:     s.options.MaxDataFileSize,
			MaxHeight:           unsetHeight,
			MaxContiguousHeight: unsetHeight,
			DataFileSize:        0,
		}
		s.maxContiguousHeight.Store(unsetHeight)
		s.maxBlockHeight.Store(unsetHeight)

		headerBytes, err := s.header.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to serialize new header: %w", err)
		}
		if uint64(len(headerBytes)) != sizeOfIndexFileHeader {
			return fmt.Errorf("internal error: serialized new header size %d, expected %d", len(headerBytes), sizeOfIndexFileHeader)
		}
		if _, err := s.indexFile.WriteAt(headerBytes, 0); err != nil {
			return fmt.Errorf("failed to write initial index header: %w", err)
		}

		return nil
	}

	// Not truncating, load existing header
	headerBuf := make([]byte, sizeOfIndexFileHeader)
	_, readErr := s.indexFile.ReadAt(headerBuf, 0)
	if readErr != nil {
		return fmt.Errorf("failed to read index header: %w", readErr)
	}
	if err := s.header.UnmarshalBinary(headerBuf); err != nil {
		return fmt.Errorf("failed to deserialize index header: %w", err)
	}
	if s.header.Version != IndexFileVersion {
		return fmt.Errorf("mismatched index file version: found %d, expected %d", s.header.Version, IndexFileVersion)
	}
	s.nextDataWriteOffset.Store(s.header.DataFileSize)
	s.maxContiguousHeight.Store(s.header.MaxContiguousHeight)
	s.maxBlockHeight.Store(s.header.MaxHeight)

	return nil
}

// New creates a block database.
// Parameters:
//   - indexDir: Directory for the index file
//   - dataDir: Directory for the data file(s)
//   - syncToDisk: If true, forces fsync after writes
//   - truncate: If true, truncates the index file
//   - config: Optional configuration parameters
//   - log: Logger instance for structured logging
func New(indexDir, dataDir string, syncToDisk bool, truncate bool, config DatabaseConfig, log logging.Logger) (*Database, error) {
	if indexDir == "" || dataDir == "" {
		return nil, fmt.Errorf("both indexDir and dataDir must be provided")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	s := &Database{
		options:    config,
		syncToDisk: syncToDisk,
		log:        log,
	}

	if err := s.openOrCreateFiles(indexDir, dataDir, truncate); err != nil {
		return nil, err
	}

	if err := s.loadOrInitializeHeader(truncate); err != nil {
		s.closeFiles()
		return nil, err
	}

	if !truncate {
		if err := s.recover(); err != nil {
			s.closeFiles()
			return nil, fmt.Errorf("recovery failed: %w", err)
		}
	}
	return s, nil
}

func (s *Database) closeFiles() {
	if s.indexFile != nil {
		s.indexFile.Close()
	}
	if s.dataFile != nil {
		s.dataFile.Close()
	}
}

// MaxContiguousHeight returns the highest block height known to be contiguously stored.
func (s *Database) MaxContiguousHeight() (height BlockHeight, found bool) {
	if s.maxContiguousHeight.Load() == unsetHeight {
		return 0, false
	}
	return s.maxContiguousHeight.Load(), true
}

// Close flushes pending writes and closes the store files.
func (s *Database) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	err := s.persistIndexHeader()
	s.closeFiles()
	return err
}
