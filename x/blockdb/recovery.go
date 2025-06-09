package blockdb

import (
	"fmt"

	"go.uber.org/zap"
)

const (
	// maxRecoverBlockSize is a sanity limit for block sizes encountered during the recovery scan.
	// It prevents attempts to read/allocate excessively large blocks due to data corruption in a block header.
	maxRecoverBlockSize uint64 = 50 * 1024 * 1024 // 50MB
)

// recover attempts to restore the store to a consistent state by scanning the data file
// for blocks that may not be correctly indexed, usually after an unclean shutdown.
// It reconciles the data file with the index file header and entries.
func (s *Store) recover() error {
	dataFileInfo, err := s.dataFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get data file stats for recovery: %w", err)
	}
	dataFileActualSize := uint64(dataFileInfo.Size())
	nextDataWriteOffset := s.nextDataWriteOffset.Load()

	// If the data file size matches the size recorded in the index header, then no recovery is needed.
	if dataFileActualSize == nextDataWriteOffset {
		// TODO: Do we need to validate that the max contiguous height is correct?
		// it might not be correct if the previous shutdown was not clean and
		// only the new datafile size was persisted somehow. In this case, we need
		// to fix the max contiguous height otherwise it will never be updated.
		return nil
	}

	// If the data file is smaller than the index header indicates, this is a fatal inconsistency.
	// The index file claims more data than actually exists, which cannot be recovered automatically.
	if dataFileActualSize < nextDataWriteOffset {
		return fmt.Errorf("%w: data file is smaller than index header claims (data file: %d bytes, index header: %d bytes) -- possible corruption or incomplete flush",
			ErrCorrupted, dataFileActualSize, nextDataWriteOffset)
	}

	// Data file is larger than the index header indicates.
	s.log.Info("Data file larger than indexed size; recovering blocks",
		zap.Uint64("dataFileSize", dataFileActualSize),
		zap.Uint64("indexedSize", nextDataWriteOffset),
		zap.Uint64("scanStartOffset", nextDataWriteOffset),
	)

	// Start scan from where the index left off.
	currentScanOffset := nextDataWriteOffset
	var recoveredBlocksCount int = 0
	var maxRecoveredHeightSeen uint64 = s.maxBlockHeight.Load()
	for currentScanOffset < dataFileActualSize {
		bh, err := s.recoverBlockAtOffset(currentScanOffset, dataFileActualSize)
		if err != nil {
			s.log.Error("Recovery: scan stopped due to invalid block data",
				zap.Uint64("offset", currentScanOffset),
				zap.Error(err),
			)
			break
		}
		s.log.Debug("Recovery: Successfully validated and indexed block",
			zap.Uint64("height", bh.Height),
			zap.Uint64("size", bh.Size),
			zap.Uint64("offset", currentScanOffset),
		)
		recoveredBlocksCount++
		if bh.Height > maxRecoveredHeightSeen {
			maxRecoveredHeightSeen = bh.Height
		}
		currentScanOffset += sizeOfBlockHeader + bh.Size
	}
	s.nextDataWriteOffset.Store(currentScanOffset)
	s.maxBlockHeight.Store(maxRecoveredHeightSeen)

	// Recalculate MCH if we recovered any blocks
	if recoveredBlocksCount > 0 {
		s.updateMaxContiguousHeightOnRecovery()
	}

	if err := s.persistIndexHeader(true); err != nil {
		return fmt.Errorf("recovery: failed to save index header after recovery scan: %w", err)
	}

	s.log.Info("Recovery: Scan finished",
		zap.Int("recoveredBlocks", recoveredBlocksCount),
		zap.Uint64("dataFileSize", nextDataWriteOffset),
		zap.Uint64("maxContiguousBlockHeight", s.maxContiguousHeight.Load()),
		zap.Uint64("maxBlockHeight", s.maxBlockHeight.Load()),
	)

	return nil
}

// recoverBlockAtOffset attempts to read, validate, and index a block at the given offset.
// Returns the blockHeader and an error if the block is invalid or incomplete.
func (s *Store) recoverBlockAtOffset(offset, dataFileActualSize uint64) (blockHeader, error) {
	var bh blockHeader
	if dataFileActualSize-offset < sizeOfBlockHeader {
		return bh, fmt.Errorf("not enough data for block header at offset %d", offset)
	}
	bhBuf := make([]byte, sizeOfBlockHeader)
	_, readErr := s.dataFile.ReadAt(bhBuf, int64(offset))
	if readErr != nil {
		return bh, fmt.Errorf("error reading block header at offset %d: %w", offset, readErr)
	}
	if err := bh.UnmarshalBinary(bhBuf); err != nil {
		return bh, fmt.Errorf("error deserializing block header at offset %d: %w", offset, err)
	}
	if bh.Size == 0 || bh.Size > maxRecoverBlockSize {
		return bh, fmt.Errorf("invalid block size in header at offset %d: %d", offset, bh.Size)
	}
	if bh.Height < s.header.MinBlockHeight {
		return bh, fmt.Errorf(
			"invalid block height in header at offset %d: found %d, expected >= %d",
			offset, bh.Height, s.header.MinBlockHeight,
		)
	}
	expectedBlockEndOffset := offset + sizeOfBlockHeader + bh.Size
	if expectedBlockEndOffset < offset || expectedBlockEndOffset > dataFileActualSize {
		return bh, fmt.Errorf("block data out of bounds at offset %d", offset)
	}
	blockData := make([]byte, bh.Size)
	_, readErr = s.dataFile.ReadAt(blockData, int64(offset+sizeOfBlockHeader))
	if readErr != nil {
		return bh, fmt.Errorf("failed to read block data at offset %d: %w", offset, readErr)
	}
	calculatedChecksum := calculateChecksum(blockData)
	if calculatedChecksum != bh.Checksum {
		return bh, fmt.Errorf("checksum mismatch for block at offset %d", offset)
	}

	// Write index entry for this block
	indexFileOffset, idxErr := s.indexEntryOffset(bh.Height)
	if idxErr != nil {
		return bh, fmt.Errorf("cannot get index offset for recovered block %d: %w", bh.Height, idxErr)
	}
	if err := s.writeIndexEntryAt(indexFileOffset, offset, bh.Size); err != nil {
		return bh, fmt.Errorf("failed to update index for recovered block %d: %w", bh.Height, err)
	}
	return bh, nil
}

// updateMaxContiguousHeightOnRecovery extends the max contiguous height from the value in the header,
// incrementing as long as contiguous blocks exist.
func (s *Store) updateMaxContiguousHeightOnRecovery() {
	currentMCH := s.header.MaxContiguousBlockHeight
	highestKnown := s.maxBlockHeight.Load()

	for nextHeight := currentMCH + 1; nextHeight <= highestKnown; nextHeight++ {
		entry, err := s.readIndexEntry(nextHeight)
		if err != nil || entry.IsEmpty() {
			break
		}
		currentMCH = nextHeight
	}
	s.maxContiguousHeight.Store(currentMCH)
}
