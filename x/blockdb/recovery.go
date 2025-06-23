// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"fmt"
	"os"

	"go.uber.org/zap"
)

// recover attempts to restore the store to a consistent state by scanning the data file
// for blocks that may not be correctly indexed, usually after an unclean shutdown.
// It reconciles the data file with the index file header and entries.
func (s *Database) recover() error {
	dataFiles, maxIndex, err := s.listDataFiles()
	if err != nil {
		return fmt.Errorf("failed to list data files for recovery: %w", err)
	}

	if len(dataFiles) == 0 {
		return nil
	}

	// Calculate the expected next write offset based on the data files on disk.
	var calculatedNextDataWriteOffset uint64
	if s.header.MaxDataFileSize > 0 {
		// All data files before the last one are full.
		fullFilesCount := maxIndex
		calculatedNextDataWriteOffset += uint64(fullFilesCount) * s.header.MaxDataFileSize

		lastFileInfo, err := os.Stat(dataFiles[maxIndex])
		if err != nil {
			return fmt.Errorf("failed to get stats for last data file %s: %w", dataFiles[maxIndex], err)
		}
		calculatedNextDataWriteOffset += uint64(lastFileInfo.Size())
	} else {
		lastFileInfo, err := os.Stat(dataFiles[0])
		if err != nil {
			return fmt.Errorf("failed to get stats for data file %s: %w", dataFiles[0], err)
		}
		calculatedNextDataWriteOffset = uint64(lastFileInfo.Size())
	}

	nextDataWriteOffset := s.nextDataWriteOffset.Load()

	switch {
	case calculatedNextDataWriteOffset == nextDataWriteOffset:
		s.log.Debug("Recovery: data files match index header, no recovery needed.")
		return nil

	case calculatedNextDataWriteOffset < nextDataWriteOffset:
		return fmt.Errorf("%w: calculated next write offset is smaller than index header claims "+
			"(calculated: %d bytes, index header: %d bytes)",
			ErrCorrupted, calculatedNextDataWriteOffset, nextDataWriteOffset)
	default:
		// The data on disk is ahead of the index. We need to recover un-indexed blocks.
		s.log.Info("Recovery: data files are ahead of index; recovering un-indexed blocks.",
			zap.Uint64("headerNextWriteOffset", nextDataWriteOffset),
			zap.Uint64("calculatedNextWriteOffset", calculatedNextDataWriteOffset),
		)

		// Start scan from where the index left off.
		currentScanOffset := nextDataWriteOffset
		recoveredBlocksCount := 0
		maxRecoveredHeightSeen := s.maxBlockHeight.Load()

		totalDataFileSize := calculatedNextDataWriteOffset
		for currentScanOffset < totalDataFileSize {
			bh, err := s.recoverBlockAtOffset(currentScanOffset, totalDataFileSize)
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
			if bh.Height > maxRecoveredHeightSeen || maxRecoveredHeightSeen == unsetHeight {
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

		if err := s.persistIndexHeader(); err != nil {
			return fmt.Errorf("recovery: failed to save index header after recovery scan: %w", err)
		}

		s.log.Info("Recovery: Scan finished",
			zap.Int("recoveredBlocks", recoveredBlocksCount),
			zap.Uint64("finalNextWriteOffset", s.nextDataWriteOffset.Load()),
			zap.Uint64("maxContiguousBlockHeight", s.maxContiguousHeight.Load()),
			zap.Uint64("maxBlockHeight", s.maxBlockHeight.Load()),
		)
	}
	return nil
}

func (s *Database) recoverBlockAtOffset(offset, dataFileActualSize uint64) (blockHeader, error) {
	var bh blockHeader
	if dataFileActualSize-offset < sizeOfBlockHeader {
		return bh, fmt.Errorf("not enough data for block header at offset %d", offset)
	}

	dataFile, localOffset, err := s.getDataFileAndOffset(offset)
	if err != nil {
		return bh, fmt.Errorf("recovery: failed to get data file for offset %d: %w", offset, err)
	}
	bhBuf := make([]byte, sizeOfBlockHeader)
	if _, err := dataFile.ReadAt(bhBuf, int64(localOffset)); err != nil {
		return bh, fmt.Errorf("error reading block header at offset %d: %w", offset, err)
	}
	if err := bh.UnmarshalBinary(bhBuf); err != nil {
		return bh, fmt.Errorf("error deserializing block header at offset %d: %w", offset, err)
	}
	if bh.Size == 0 || bh.Size > MaxBlockDataSize {
		return bh, fmt.Errorf("invalid block size in header at offset %d: %d", offset, bh.Size)
	}
	if bh.Height < s.header.MinHeight || bh.Height == unsetHeight {
		return bh, fmt.Errorf(
			"invalid block height in header at offset %d: found %d, expected >= %d",
			offset, bh.Height, s.header.MinHeight,
		)
	}
	if uint64(bh.HeaderSize) > bh.Size {
		return bh, fmt.Errorf("invalid block header size in header at offset %d: %d > %d", offset, bh.HeaderSize, bh.Size)
	}
	expectedBlockEndOffset := offset + sizeOfBlockHeader + bh.Size
	if expectedBlockEndOffset < offset || expectedBlockEndOffset > dataFileActualSize {
		return bh, fmt.Errorf("block data out of bounds at offset %d", offset)
	}
	blockData := make([]byte, bh.Size)
	if _, err := dataFile.ReadAt(blockData, int64(offset+sizeOfBlockHeader)); err != nil {
		return bh, fmt.Errorf("failed to read block data at offset %d: %w", offset, err)
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
	if err := s.writeIndexEntryAt(indexFileOffset, offset, bh.Size, bh.HeaderSize); err != nil {
		return bh, fmt.Errorf("failed to update index for recovered block %d: %w", bh.Height, err)
	}
	return bh, nil
}

func (s *Database) updateMaxContiguousHeightOnRecovery() {
	currentMCH := s.header.MaxContiguousHeight
	highestKnown := s.maxBlockHeight.Load()

	for nextHeight := currentMCH + 1; nextHeight <= highestKnown; nextHeight++ {
		entry, err := s.readIndexEntry(nextHeight)
		if err != nil {
			s.log.Error(
				"error reading index entry when updating max contiguous height on recovery",
				zap.Uint64("height", nextHeight),
				zap.Error(err),
			)
			break
		}
		if entry.IsEmpty() {
			break
		}
		currentMCH = nextHeight
	}
	s.maxContiguousHeight.Store(currentMCH)
}
