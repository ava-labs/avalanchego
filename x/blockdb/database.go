// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	indexFileName          = "blockdb.idx"
	dataFileNameFormat     = "blockdb_%d.dat"
	defaultFilePermissions = 0o666

	// Since 0 is a valid height, math.MaxUint64 is used to indicate unset height.
	// It is not be possible for block height to be max uint64 as it would overflow the index entry offset
	unsetHeight = math.MaxUint64
)

// Database stores blockchain blocks on disk and provides methods to read, and write blocks.
type Database struct {
	indexFile *os.File
	dataDir   string
	options   DatabaseConfig
	header    indexFileHeader
	log       logging.Logger
	closed    bool
	fileCache *lru.Cache[int, *os.File]

	// closeMu prevents the database from being closed while in use and prevents
	// use of a closed database.
	closeMu sync.RWMutex

	// fileOpenMu prevents race conditions when multiple threads try to open the same data file
	fileOpenMu sync.Mutex

	// maxBlockHeight tracks the highest block height that has been written to the db, even if there are gaps in the sequence.
	maxBlockHeight atomic.Uint64
	// nextDataWriteOffset tracks the next position to write new data in the data file.
	nextDataWriteOffset atomic.Uint64
	// maxContiguousHeight tracks the highest block height known to be contiguously stored.
	maxContiguousHeight atomic.Uint64
}

func (s *Database) listDataFiles() (map[int]string, int, error) {
	files, err := os.ReadDir(s.dataDir)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to read data directory %s: %w", s.dataDir, err)
	}

	dataFiles := make(map[int]string)
	maxIndex := -1
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		var index int
		if n, err := fmt.Sscanf(file.Name(), dataFileNameFormat, &index); n == 1 && err == nil {
			dataFiles[index] = filepath.Join(s.dataDir, file.Name())
			if index > maxIndex {
				maxIndex = index
			}
		}
	}

	return dataFiles, maxIndex, nil
}

func (s *Database) openAndInitializeIndex(indexDir string, truncate bool) error {
	indexPath := filepath.Join(indexDir, indexFileName)
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		return fmt.Errorf("failed to create index directory %s: %w", indexDir, err)
	}
	openFlags := os.O_RDWR | os.O_CREATE
	if truncate {
		openFlags |= os.O_TRUNC
	}
	var err error
	s.indexFile, err = os.OpenFile(indexPath, openFlags, defaultFilePermissions)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", indexPath, err)
	}
	return s.loadOrInitializeHeader(truncate)
}

func (s *Database) initializeDataFiles(dataDir string, truncate bool) error {
	s.dataDir = dataDir
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}

	if truncate {
		dataFiles, _, err := s.listDataFiles()
		if err != nil {
			return fmt.Errorf("failed to list data files for truncation: %w", err)
		}
		for _, filePath := range dataFiles {
			if err := os.Remove(filePath); err != nil {
				return fmt.Errorf("failed to remove old data file %s: %w", filePath, err)
			}
		}
	}

	// Pre-load the data file for the next write offset.
	nextOffset := s.nextDataWriteOffset.Load()
	if nextOffset > 0 {
		_, _, err := s.getDataFileAndOffset(nextOffset)
		if err != nil {
			return fmt.Errorf("failed to pre-load data file for offset %d: %w", nextOffset, err)
		}
	}
	return nil
}

func (s *Database) loadOrInitializeHeader(truncate bool) error {
	fileInfo, err := s.indexFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get index file stats: %w", err)
	}

	// reset index file if its empty or we are truncating
	if truncate || fileInfo.Size() == 0 {
		s.header = indexFileHeader{
			Version:             IndexFileVersion,
			MinHeight:           s.options.MinimumHeight,
			MaxDataFileSize:     s.options.MaxDataFileSize,
			MaxHeight:           unsetHeight,
			MaxContiguousHeight: unsetHeight,
			NextWriteOffset:     0,
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

	headerBuf := make([]byte, sizeOfIndexFileHeader)
	_, readErr := s.indexFile.ReadAt(headerBuf, 0)
	if readErr != nil {
		return fmt.Errorf("failed to read index header (delete index file to reindex): %w", readErr)
	}
	if err := s.header.UnmarshalBinary(headerBuf); err != nil {
		return fmt.Errorf("failed to deserialize index header (delete index file to reindex): %w", err)
	}
	if s.header.Version != IndexFileVersion {
		return fmt.Errorf("mismatched index file version: found %d, expected %d", s.header.Version, IndexFileVersion)
	}
	s.nextDataWriteOffset.Store(s.header.NextWriteOffset)
	s.maxContiguousHeight.Store(s.header.MaxContiguousHeight)
	s.maxBlockHeight.Store(s.header.MaxHeight)

	return nil
}

// New creates a block database.
// Parameters:
//   - indexDir: Directory for the index file
//   - dataDir: Directory for the data file(s)
//   - config: Configuration parameters
//   - log: Logger instance for structured logging
func New(indexDir, dataDir string, config DatabaseConfig, log logging.Logger) (*Database, error) {
	if indexDir == "" || dataDir == "" {
		return nil, errors.New("both indexDir and dataDir must be provided")
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	s := &Database{
		options:   config,
		log:       log,
		fileCache: lru.NewCache[int, *os.File](MaxDataFiles),
	}
	s.fileCache.SetOnEvict(func(_ int, f *os.File) {
		if f != nil {
			f.Close()
		}
	})

	if err := s.openAndInitializeIndex(indexDir, config.Truncate); err != nil {
		return nil, err
	}

	if err := s.initializeDataFiles(dataDir, config.Truncate); err != nil {
		s.closeFiles()
		return nil, err
	}

	if !config.Truncate {
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
	if s.fileCache != nil {
		s.fileCache.Flush()
	}
}

func (s *Database) dataFilePath(index int) string {
	return filepath.Join(s.dataDir, fmt.Sprintf(dataFileNameFormat, index))
}

func (s *Database) getOrOpenDataFile(fileIndex int) (*os.File, error) {
	if handle, ok := s.fileCache.Get(fileIndex); ok {
		return handle, nil
	}

	// Prevent race conditions when multiple threads try to open the same file
	s.fileOpenMu.Lock()
	defer s.fileOpenMu.Unlock()

	// Double-check the cache after acquiring the lock
	if handle, ok := s.fileCache.Get(fileIndex); ok {
		return handle, nil
	}

	filePath := s.dataFilePath(fileIndex)
	handle, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, defaultFilePermissions)
	if err != nil {
		return nil, fmt.Errorf("failed to open data file %s: %w", filePath, err)
	}
	s.fileCache.Put(fileIndex, handle)
	return handle, nil
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
	s.closeMu.Lock()
	defer s.closeMu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	err := s.persistIndexHeader()
	s.closeFiles()
	return err
}
