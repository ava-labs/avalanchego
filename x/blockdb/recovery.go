// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"fmt"
	"io"
	"os"

	"go.uber.org/zap"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// recover detects and recovers unindexed blocks by scanning data files and updating the index.
// It compares the actual data file sizes on disk with the indexed data size to detect
// blocks that were written but not properly indexed.
// For each unindexed block found, it validates the block, then
// writes the corresponding index entry and updates block height tracking.
func (s *Database) recover() error {
	dataFiles, maxIndex, err := s.listDataFiles()
	if err != nil {
		return fmt.Errorf("failed to list data files for recovery: %w", err)
	}

	if len(dataFiles) == 0 {
		return nil
	}

	// ensure no data files are missing
	// If any data files are missing, we would need to recalculate the max height
	// and max contiguous height. This can be supported in the future but for now
	// to keep things simple, we will just error if the data files are not as expected.
	if s.header.MaxDataFileSize > 0 {
		// Ensure data files are sequential starting from 0
		for i := 0; i <= maxIndex; i++ {
			if _, exists := dataFiles[i]; !exists {
				return fmt.Errorf("%w: data file at index %d is missing", ErrCorrupted, i)
			}
		}
	} else if len(dataFiles) > 1 || maxIndex > 1 {
		return fmt.Errorf("%w: expect only 1 data file at index 0, got %d files with max index %d", ErrCorrupted, len(dataFiles), maxIndex)
	}

	// Calculate the expected next write offset based on the data on disk.
	var calculatedNextDataWriteOffset uint64
	if s.header.MaxDataFileSize > 0 {
		// All data files before the last should be full.
		fullFilesCount := maxIndex
		fileSizeContribution, err := safemath.Mul(uint64(fullFilesCount), s.header.MaxDataFileSize)
		if err != nil {
			return fmt.Errorf("calculating file size contribution would overflow: %w", err)
		}
		calculatedNextDataWriteOffset = fileSizeContribution

		lastFileInfo, err := os.Stat(dataFiles[maxIndex])
		if err != nil {
			return fmt.Errorf("failed to get stats for last data file %s: %w", dataFiles[maxIndex], err)
		}
		calculatedNextDataWriteOffset, err = safemath.Add(calculatedNextDataWriteOffset, uint64(lastFileInfo.Size()))
		if err != nil {
			return fmt.Errorf("adding last file size would overflow: %w", err)
		}
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
		// this happens when the index claims to have more data than is actually on disk
		return fmt.Errorf("%w: index header claims to have more data than is actually on disk "+
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
		for currentScanOffset < calculatedNextDataWriteOffset {
			bh, err := s.recoverBlockAtOffset(currentScanOffset, calculatedNextDataWriteOffset)
			if err != nil {
				if errors.Is(err, io.EOF) && s.header.MaxDataFileSize > 0 {
					// reach end of this file, try to read the next file
					currentFileIndex := int(currentScanOffset / s.header.MaxDataFileSize)
					nextFileIndex, err := safemath.Add(uint64(currentFileIndex), 1)
					if err != nil {
						return fmt.Errorf("recovery: overflow in file index calculation: %w", err)
					}
					if currentScanOffset, err = safemath.Mul(nextFileIndex, s.header.MaxDataFileSize); err != nil {
						return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
					}
					continue
				}
				return err
			}
			s.log.Debug("Recovery: Successfully validated and indexed block",
				zap.Uint64("height", bh.Height),
				zap.Uint32("size", bh.Size),
				zap.Uint64("offset", currentScanOffset),
			)
			recoveredBlocksCount++
			if bh.Height > maxRecoveredHeightSeen || maxRecoveredHeightSeen == unsetHeight {
				maxRecoveredHeightSeen = bh.Height
			}
			blockTotalSize, err := safemath.Add(uint64(sizeOfBlockHeader), uint64(bh.Size))
			if err != nil {
				return fmt.Errorf("recovery: overflow in block size calculation: %w", err)
			}
			currentScanOffset, err = safemath.Add(currentScanOffset, blockTotalSize)
			if err != nil {
				return fmt.Errorf("recovery: overflow in scan offset calculation: %w", err)
			}
		}
		s.nextDataWriteOffset.Store(currentScanOffset)

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

func (s *Database) recoverBlockAtOffset(offset, totalDataSize uint64) (blockHeader, error) {
	var bh blockHeader
	if totalDataSize-offset < uint64(sizeOfBlockHeader) {
		return bh, fmt.Errorf("%w: not enough data for block header at offset %d", ErrCorrupted, offset)
	}

	dataFile, localOffset, err := s.getDataFileAndOffset(offset)
	if err != nil {
		return bh, fmt.Errorf("recovery: failed to get data file for offset %d: %w", offset, err)
	}
	bhBuf := make([]byte, sizeOfBlockHeader)
	if _, err := dataFile.ReadAt(bhBuf, int64(localOffset)); err != nil {
		return bh, fmt.Errorf("%w: error reading block header at offset %d: %w", ErrCorrupted, offset, err)
	}
	if err := bh.UnmarshalBinary(bhBuf); err != nil {
		return bh, fmt.Errorf("%w: error deserializing block header at offset %d: %w", ErrCorrupted, offset, err)
	}
	if bh.Size == 0 || bh.Size > MaxBlockDataSize {
		return bh, fmt.Errorf("%w: invalid block size in header at offset %d: %d", ErrCorrupted, offset, bh.Size)
	}
	if bh.Height < s.header.MinHeight || bh.Height == unsetHeight {
		return bh, fmt.Errorf(
			"%w: invalid block height in header at offset %d: found %d, expected >= %d",
			ErrCorrupted, offset, bh.Height, s.header.MinHeight,
		)
	}
	if bh.HeaderSize > bh.Size {
		return bh, fmt.Errorf("%w: invalid block header size in header at offset %d: %d > %d", ErrCorrupted, offset, bh.HeaderSize, bh.Size)
	}
	expectedBlockEndOffset, err := safemath.Add(offset, uint64(sizeOfBlockHeader))
	if err != nil {
		return bh, fmt.Errorf("calculating block end offset would overflow at offset %d: %w", offset, err)
	}
	expectedBlockEndOffset, err = safemath.Add(expectedBlockEndOffset, uint64(bh.Size))
	if err != nil {
		return bh, fmt.Errorf("calculating block end offset would overflow at offset %d: %w", offset, err)
	}
	if expectedBlockEndOffset > totalDataSize {
		return bh, fmt.Errorf("%w: block data out of bounds at offset %d", ErrCorrupted, offset)
	}
	blockData := make([]byte, bh.Size)
	blockDataOffset, err := safemath.Add(localOffset, uint64(sizeOfBlockHeader))
	if err != nil {
		return bh, fmt.Errorf("calculating block data offset would overflow at offset %d: %w", offset, err)
	}
	if _, err := dataFile.ReadAt(blockData, int64(blockDataOffset)); err != nil {
		return bh, fmt.Errorf("%w: failed to read block data at offset %d: %w", ErrCorrupted, offset, err)
	}
	calculatedChecksum := calculateChecksum(blockData)
	if calculatedChecksum != bh.Checksum {
		return bh, fmt.Errorf("%w: checksum mismatch for block at offset %d", ErrCorrupted, offset)
	}

	// Write index entry for this block
	indexFileOffset, idxErr := s.indexEntryOffset(bh.Height)
	if idxErr != nil {
		return bh, fmt.Errorf("cannot get index offset for recovered block %d: %w", bh.Height, idxErr)
	}
	if err := s.writeIndexEntryAt(indexFileOffset, offset, bh.Size, bh.HeaderSize); err != nil {
		return bh, fmt.Errorf("failed to update index for recovered block %d: %w", bh.Height, err)
	}

	if err := s.updateBlockHeights(bh.Height); err != nil {
		return bh, fmt.Errorf("failed to update block heights for recovered block %d: %w", bh.Height, err)
	}

	return bh, nil
}
