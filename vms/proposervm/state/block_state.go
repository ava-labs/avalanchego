// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const blockCacheSize = 64 * units.MiB

var (
	errBlockWrongVersion     = errors.New("wrong version")
	errShortBlockRecord      = errors.New("block record too short to contain a codec version")
	errInnerBlockUnavailable = errors.New("inner block unavailable for deduplicated block")
	errRestoredBlockMismatch = errors.New("restored block bytes do not match original")

	_ BlockState = (*blockState)(nil)
)

type BlockState interface {
	GetBlock(blkID ids.ID) (block.Block, error)
	// PutBlock persists [blk]. [innerID] is the ID of [blk]'s inner block and is
	// only used when inner-block deduplication is enabled.
	PutBlock(blk block.Block, innerID ids.ID) error
	DeleteBlock(blkID ids.ID) error
}

type blockState struct {
	// Caches BlockID -> Block. If the Block is nil, that means the block is not
	// in storage.
	blkCache cache.Cacher[ids.ID, *blockWrapper]

	db database.Database

	// getInnerBytes, when non-nil, enables inner-block deduplication: accepted
	// blocks are stored without their inner bytes, which are reconstructed on
	// read by looking them up by inner block ID. nil disables deduplication and
	// preserves the legacy full-block storage format.
	getInnerBytes func(innerID ids.ID) ([]byte, error)

	// log, when non-nil, reports every dedup fallback at error level. A
	// fallback has no legitimate data-dependent cause (the write-time
	// round-trip is pure in-memory container mechanics), so it can only be a
	// strip/restore code bug for some container shape and must not go
	// unnoticed.
	log logging.Logger

	// numDedupStored counts blocks written in the deduplicated (inner-bytes
	// stripped) format; numDedupFallback counts blocks for which stripping did
	// not round-trip and were written in the legacy full format instead. Both
	// are nil when deduplication is disabled or the state is unmetered.
	numDedupStored   prometheus.Counter
	numDedupFallback prometheus.Counter
}

// blockWrapper is the legacy (CodecVersion) record: the full block bytes plus
// status. It is also reused as the in-memory cache entry for both formats.
type blockWrapper struct {
	Block  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`

	block block.Block
}

// dedupBlockWrapper is the deduplicated (CodecVersionDedup) record: the block
// bytes with the inner bytes stripped, the inner block ID needed to recover
// them, and status.
type dedupBlockWrapper struct {
	StrippedBlock []byte         `serialize:"true"`
	InnerID       ids.ID         `serialize:"true"`
	Status        choices.Status `serialize:"true"`
}

func cachedBlockSize(_ ids.ID, bw *blockWrapper) int {
	if bw == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(bw.Block) + wrappers.IntLen + 2*constants.PointerOverhead
}

func NewBlockState(db database.Database, getInnerBytes func(ids.ID) ([]byte, error), log logging.Logger) BlockState {
	return &blockState{
		blkCache:      lru.NewSizedCache(blockCacheSize, cachedBlockSize),
		db:            db,
		getInnerBytes: getInnerBytes,
		log:           log,
	}
}

func NewMeteredBlockState(db database.Database, namespace string, metrics prometheus.Registerer, getInnerBytes func(ids.ID) ([]byte, error), log logging.Logger) (BlockState, error) {
	blkCache, err := metercacher.New[ids.ID, *blockWrapper](
		metric.AppendNamespace(namespace, "block_cache"),
		metrics,
		lru.NewSizedCache(blockCacheSize, cachedBlockSize),
	)
	if err != nil {
		return nil, err
	}

	s := &blockState{
		blkCache:      blkCache,
		db:            db,
		getInnerBytes: getInnerBytes,
		log:           log,
	}

	// Only register dedup counters when deduplication is enabled, so the legacy
	// path is byte-for-byte and metric-for-metric unchanged.
	if getInnerBytes != nil {
		s.numDedupStored = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "dedup_blocks_stored",
			Help:      "Number of accepted blocks stored with inner bytes deduplicated",
		})
		s.numDedupFallback = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "dedup_blocks_fallback",
			Help:      "Number of accepted blocks stored full because stripping did not round-trip",
		})
		if err := errors.Join(
			metrics.Register(s.numDedupStored),
			metrics.Register(s.numDedupFallback),
		); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (s *blockState) GetBlock(blkID ids.ID) (block.Block, error) {
	if blk, found := s.blkCache.Get(blkID); found {
		if blk == nil {
			return nil, database.ErrNotFound
		}
		return blk.block, nil
	}

	blkWrapperBytes, err := s.db.Get(blkID[:])
	if err == database.ErrNotFound {
		s.blkCache.Put(blkID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	wrapper, err := s.parseStored(blkID, blkWrapperBytes)
	if err != nil {
		return nil, err
	}

	s.blkCache.Put(blkID, wrapper)
	return wrapper.block, nil
}

// parseStored decodes a stored record (legacy full or deduplicated) into a
// cacheable [blockWrapper] holding the fully reconstructed block.
func (s *blockState) parseStored(blkID ids.ID, storedBytes []byte) (*blockWrapper, error) {
	if len(storedBytes) < wrappers.ShortLen {
		return nil, errShortBlockRecord
	}
	version := binary.BigEndian.Uint16(storedBytes)
	if version == CodecVersionDedup {
		return s.parseDedupBlock(blkID, storedBytes)
	}

	blkWrapper := blockWrapper{}
	parsedVersion, err := Codec.Unmarshal(storedBytes, &blkWrapper)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersion {
		return nil, errBlockWrongVersion
	}

	blk, err := block.ParseWithoutVerification(blkWrapper.Block)
	if err != nil {
		return nil, err
	}
	blkWrapper.block = blk
	return &blkWrapper, nil
}

func (s *blockState) parseDedupBlock(blkID ids.ID, storedBytes []byte) (*blockWrapper, error) {
	if s.getInnerBytes == nil {
		return nil, errInnerBlockUnavailable
	}

	wrapper := dedupBlockWrapper{}
	parsedVersion, err := Codec.Unmarshal(storedBytes, &wrapper)
	if err != nil {
		return nil, err
	}
	if parsedVersion != CodecVersionDedup {
		return nil, errBlockWrongVersion
	}

	innerBytes, err := s.getInnerBytes(wrapper.InnerID)
	if err != nil {
		return nil, fmt.Errorf("%w: inner block %s: %w", errInnerBlockUnavailable, wrapper.InnerID, err)
	}

	fullBytes, err := block.RestoreInnerBytes(wrapper.StrippedBlock, innerBytes)
	if err != nil {
		return nil, err
	}

	blk, err := block.ParseWithoutVerification(fullBytes)
	if err != nil {
		return nil, err
	}

	// Safety net: the reconstructed block ID must match the key it was stored
	// under. This catches any divergence between the stripped bytes / fetched
	// inner bytes and the block originally stored, rather than serving a block
	// that doesn't match its ID.
	if blk.ID() != blkID {
		return nil, fmt.Errorf("%w: reconstructed block %s does not match key %s", errInnerBlockUnavailable, blk.ID(), blkID)
	}

	return &blockWrapper{
		Block:  wrapper.StrippedBlock,
		Status: wrapper.Status,
		block:  blk,
	}, nil
}

func (s *blockState) PutBlock(blk block.Block, innerID ids.ID) error {
	blkID := blk.ID()

	if s.getInnerBytes != nil {
		storedBytes, cached, err := tryStripBlock(blk, innerID)
		if err == nil {
			s.blkCache.Put(blkID, cached)
			if s.numDedupStored != nil {
				s.numDedupStored.Inc()
			}
			return s.db.Put(blkID[:], storedBytes)
		}
		// Could not safely strip the inner bytes; fall through to full storage
		// so correctness is never traded for the saving. This has no legitimate
		// data-dependent cause and can only be a strip/restore code bug for
		// some container shape, so it must not go unnoticed.
		if s.log != nil {
			s.log.Error("failed to deduplicate accepted block; stored full block",
				zap.Stringer("blkID", blkID),
				zap.Error(err),
			)
		}
		if s.numDedupFallback != nil {
			s.numDedupFallback.Inc()
		}
	}

	blkWrapper := blockWrapper{
		Block:  blk.Bytes(),
		Status: choices.Accepted,
		block:  blk,
	}
	bytesValue, err := Codec.Marshal(CodecVersion, &blkWrapper)
	if err != nil {
		return err
	}

	s.blkCache.Put(blkID, &blkWrapper)
	return s.db.Put(blkID[:], bytesValue)
}

// tryStripBlock attempts to build a deduplicated record for [blk]. It returns
// an error describing the cause if stripping the inner bytes does not
// round-trip back to the exact original block bytes, in which case the caller
// must store the full block.
func tryStripBlock(blk block.Block, innerID ids.ID) ([]byte, *blockWrapper, error) {
	fullBytes := blk.Bytes()
	strippedBytes, err := block.StripInnerBytes(fullBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("stripping inner bytes: %w", err)
	}

	restored, err := block.RestoreInnerBytes(strippedBytes, blk.Block())
	if err != nil {
		return nil, nil, fmt.Errorf("restoring inner bytes: %w", err)
	}
	if !bytes.Equal(restored, fullBytes) {
		return nil, nil, errRestoredBlockMismatch
	}

	wrapper := dedupBlockWrapper{
		StrippedBlock: strippedBytes,
		InnerID:       innerID,
		Status:        choices.Accepted,
	}
	storedBytes, err := Codec.Marshal(CodecVersionDedup, &wrapper)
	if err != nil {
		return nil, nil, fmt.Errorf("marshalling deduplicated record: %w", err)
	}

	cached := &blockWrapper{
		Block:  strippedBytes,
		Status: choices.Accepted,
		block:  blk,
	}
	return storedBytes, cached, nil
}

func (s *blockState) DeleteBlock(blkID ids.ID) error {
	s.blkCache.Evict(blkID)
	return s.db.Delete(blkID[:])
}
