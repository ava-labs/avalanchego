package blockdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
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

// Database is a collection of blockchain blocks. It provides methods to read, write, and manage blocks on disk.
type Database struct {
	indexFile *os.File
	dataFile  *os.File
	options   DatabaseConfig
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
		initialMCH := uint64(0)
		if s.options.MinimumHeight > 1 {
			initialMCH = s.options.MinimumHeight - 1
			s.maxContiguousHeight.Store(initialMCH)
		}

		s.header = IndexFileHeader{
			Version:                  IndexFileVersion,
			MinBlockHeight:           s.options.MinimumHeight,
			MaxDataFileSize:          s.options.MaxDataFileSize,
			MaxBlockHeight:           0,
			MaxContiguousBlockHeight: initialMCH,
			DataFileSize:             0,
		}

		buf := new(bytes.Buffer)
		if err := binary.Write(buf, binary.LittleEndian, &s.header); err != nil {
			return fmt.Errorf("failed to serialize new header: %w", err)
		}
		if uint64(buf.Len()) != sizeOfIndexFileHeader {
			return fmt.Errorf("internal error: serialized new header size %d, expected %d", buf.Len(), sizeOfIndexFileHeader)
		}
		if _, err := s.indexFile.WriteAt(buf.Bytes(), 0); err != nil {
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
	s.maxContiguousHeight.Store(s.header.MaxContiguousBlockHeight)
	s.maxBlockHeight.Store(s.header.MaxBlockHeight)

	return nil
}

// New creates a block database.
// Parameters:
//   - indexDir: Directory for the index file
//   - dataDir: Directory for the data file(s)
//   - syncToDisk: If true, forces fsync after writes for guaranteed recoverability
//   - truncate: If true, truncates existing store files
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
func (s *Database) MaxContiguousHeight() BlockHeight {
	return s.maxContiguousHeight.Load()
}

// MinHeight returns the minimum block height configured for this store.
func (s *Database) MinHeight() uint64 {
	return s.header.MinBlockHeight
}

func (s *Database) MaxBlockHeight() BlockHeight {
	return s.maxBlockHeight.Load()
}

// Close flushes pending writes and closes the store files.
// It is safe to call Close multiple times.
func (s *Database) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	err := s.persistIndexHeader(false)
	s.closeFiles()
	return err
}
