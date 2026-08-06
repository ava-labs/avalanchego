// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go tool canoto $GOFILE

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync/atomic"

	"github.com/ava-labs/simplex"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_               simplex.Storage = (*Storage)(nil)
	genesisMetadata                 = simplex.ProtocolMetadata{
		Version: 0,
		Epoch:   0,
		Round:   0,
		Seq:     0,
	}

	errUnexpectedSeq    = errors.New("unexpected sequence number")
	errInvalidQC        = errors.New("invalid quorum certificate")
	errMismatchedDigest = errors.New("mismatched digest in finalization")

	finalizationPrefix = []byte("f")
	blacklistPrefix    = []byte("b")
)

type Storage struct {
	// numBlocks represents the number of blocks indexed in storage, also known as the height of the chain
	numBlocks atomic.Uint64

	// db is the underlying database used to store finalizations.
	db database.KeyValueReaderWriter

	// genesisBlock is the genesis block data. It is stored as the first block in the storage.
	genesisBlock *Block

	// lastIndexed is the last indexed block digest.
	lastIndexedDigest simplex.Digest

	// deserializer is used to deserialize quorum certificates from bytes.
	deserializer *QCDeserializer

	// blockTracker is used to manage blocks that have been indexed.
	blockTracker *blockTracker

	vm block.ChainVM

	log logging.Logger
}

// newStorage creates a new prefixed database to store
// finalizations according to their sequence numbers.
// The VM is assumed to be initialized before calling this function.
func newStorage(ctx context.Context, config *Config, qcDeserializer *QCDeserializer, blockTracker *blockTracker) (*Storage, error) {
	genesisBlock, err := getGenesisBlock(ctx, config, blockTracker)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:           config.DB,
		genesisBlock: genesisBlock,
		vm:           config.VM,
		deserializer: qcDeserializer,
		blockTracker: blockTracker,
		log:          config.Log,
	}

	lastAccepted, err := config.VM.LastAccepted(ctx)
	if err != nil {
		return nil, err
	}
	lastAcceptedBlock, err := config.VM.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}
	s.numBlocks.Store(lastAcceptedBlock.Height() + 1)

	// set the last accepted digest by retrieving the last accepted simplex block
	lastAcceptedSimplexBlock, _, err := s.Retrieve(lastAcceptedBlock.Height())
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve last accepted simplex block: %w", err)
	}
	s.lastIndexedDigest = lastAcceptedSimplexBlock.BlockHeader().Digest

	return s, nil
}

func (s *Storage) NumBlocks() uint64 {
	return s.numBlocks.Load()
}

// Retrieve returns the block and finalization at [seq].
// If [seq] is not found, returns simplex.ErrBlockNotFound.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.Finalization, error) {
	// The genesis block doesn't have a finalization, so we need to handle it specifically.
	if seq == 0 {
		return s.genesisBlock, simplex.Finalization{}, nil
	}

	block, err := getBlock(context.TODO(), s.vm, seq)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, simplex.Finalization{}, simplex.ErrBlockNotFound
		}
		s.log.Error("Error retrieving block from storage", zap.Uint64("seq", seq), zap.Error(err))
		return nil, simplex.Finalization{}, err
	}

	finalization, err := s.retrieveFinalization(seq)
	if err != nil {
		return nil, simplex.Finalization{}, err
	}

	blacklist, err := s.retrieveBlacklist(seq)
	if err != nil {
		return nil, simplex.Finalization{}, err
	}

	vb, err := newBlock(finalization.Finalization.ProtocolMetadata, blacklist, block, s.blockTracker)
	if err != nil {
		s.log.Error("failed to create simplex block", zap.Uint64("seq", seq), zap.Error(err))
		return nil, simplex.Finalization{}, err
	}

	return vb, finalization, nil
}

// Index indexes the finalization in the storage.
// It stores the finalization bytes and increments numBlocks.
func (s *Storage) Index(ctx context.Context, block simplex.VerifiedBlock, finalization simplex.Finalization) error {
	bh := block.BlockHeader()
	numBlocks := s.numBlocks.Load()
	if numBlocks != bh.Seq {
		s.log.Error("Attempted to index block with mismatched sequence number",
			zap.Uint64("expected", numBlocks),
			zap.Uint64("got", bh.Seq))
		return fmt.Errorf("%w: expected %d, got %d", errUnexpectedSeq, numBlocks, bh.Seq)
	}

	if s.lastIndexedDigest != bh.Prev {
		s.log.Error("Attempted to index block with mismatched previous digest",
			zap.Stringer("expected", s.lastIndexedDigest),
			zap.Stringer("got", bh.Prev))

		return fmt.Errorf("%w: expected %s, got %s", errMismatchedPrevDigest, s.lastIndexedDigest, bh.Prev)
	}

	if bh.Digest != finalization.Finalization.Digest {
		s.log.Error("Attempted to index block with mismatched digest",
			zap.Stringer("expected", bh.Digest),
			zap.Stringer("got", finalization.Finalization.Digest))
		return fmt.Errorf("%w: expected %d, got %d", errMismatchedDigest, bh.Digest, finalization.Finalization.Digest)
	}

	if finalization.QC == nil {
		s.log.Error("Attempted to index block with no quorum certificate", zap.Stringer("blockID", bh.Digest))
		return errInvalidQC
	}

	finalizationBytes := finalizationToBytes(finalization)
	if err := s.db.Put(finalizationKey(bh.Seq), finalizationBytes); err != nil {
		return fmt.Errorf("failed to store finalization: %w", err)
	}

	bl := block.Blacklist()
	if err := s.db.Put(blacklistKey(bh.Seq), bl.Bytes()); err != nil {
		return fmt.Errorf("failed to store blacklist: %w", err)
	}

	err := s.blockTracker.indexBlock(ctx, bh.Digest)
	if err != nil {
		return fmt.Errorf("failed to index block: %w", err)
	}

	s.numBlocks.Add(1) // only increment numBlocks after successful indexing
	s.lastIndexedDigest = bh.Digest
	return nil
}

func finalizationKey(seq uint64) []byte {
	seqBuff := make([]byte, len(finalizationPrefix)+8)
	copy(seqBuff, finalizationPrefix)
	binary.BigEndian.PutUint64(seqBuff[len(finalizationPrefix):], seq)
	return seqBuff
}

func blacklistKey(seq uint64) []byte {
	seqBuff := make([]byte, len(blacklistPrefix)+8)
	copy(seqBuff, blacklistPrefix)
	binary.BigEndian.PutUint64(seqBuff[len(blacklistPrefix):], seq)
	return seqBuff
}

// getGenesisBlock returns the genesis block wrapped as a Block instance.
func getGenesisBlock(ctx context.Context, config *Config, blockTracker *blockTracker) (*Block, error) {
	snowmanGenesis, err := getBlock(ctx, config.VM, 0)
	if err != nil {
		return nil, err
	}

	genesis := &Block{
		metadata:     genesisMetadata,
		blockTracker: blockTracker,
		vmBlock:      snowmanGenesis,
		blacklist:    simplex.NewBlacklist(uint16(len(config.Params.InitialValidators))),
	}

	// set the digest
	bytes, err := genesis.Bytes()
	if err != nil {
		return nil, err
	}
	genesis.digest = computeDigest(bytes)

	return genesis, nil
}

// retrieveFinalization retrieves the finalization at [seq].
// If the finalization is not found, it returns false.
func (s *Storage) retrieveFinalization(seq uint64) (simplex.Finalization, error) {
	finalizationBytes, err := s.db.Get(finalizationKey(seq))
	if err != nil {
		if err == database.ErrNotFound {
			return simplex.Finalization{}, simplex.ErrBlockNotFound
		}
		s.log.Debug("Failed to retrieve finalization", zap.Uint64("seq", seq), zap.Error(err))
		return simplex.Finalization{}, err
	}

	var canotoFinalization canotoFinalization
	if err := canotoFinalization.UnmarshalCanoto(finalizationBytes); err != nil {
		return simplex.Finalization{}, err
	}

	return canotoFinalization.toFinalization(s.deserializer)
}

func (s *Storage) retrieveBlacklist(seq uint64) (simplex.Blacklist, error) {
	blacklistBytes, err := s.db.Get(blacklistKey(seq))
	if err != nil {
		if err == database.ErrNotFound {
			return simplex.Blacklist{}, nil
		}
		s.log.Debug("Failed to retrieve blacklist", zap.Uint64("seq", seq), zap.Error(err))
		return simplex.Blacklist{}, err
	}

	var blacklist simplex.Blacklist
	if err := blacklist.FromBytes(blacklistBytes); err != nil {
		return simplex.Blacklist{}, fmt.Errorf("failed to parse blacklist: %w", err)
	}
	return blacklist, nil
}

// locateLastNonSimplexBlock returns the highest block that was accepted before Simplex was activated.
// It returns the block, a boolean indicating if a non-simplex block was found, and an error if any occurred during the search.
// The parameter numBlocksInChain is the number of blocks in the chain, not the number of blocks on disk.
func locateLastNonSimplexBlock(
	ctx context.Context,
	genesisBlock snowman.Block,
	proposerVM block.ChainVM,
	db database.KeyValueReader,
	log logging.Logger,
	numBlocksInChain uint64,
) (snowman.Block, bool, error) {
	if numBlocksInChain == 0 {
		// This is a sanity check, as the genesis block should always be present in storage.
		return nil, false, errors.New("no blocks in storage")
	}

	if numBlocksInChain == 1 {
		// If there's only one block in the chain, it must be the genesis block, which is a non-simplex block.
		return genesisBlock, true, nil
	}

	// We first check if the last block is a non-simplex block. If it is, we can return it immediately.
	lastBlock, isNonSimplexBlock, err := isLastBlockNonSimplexBlock(ctx, proposerVM, log, numBlocksInChain)
	if err != nil {
		return nil, false, err
	}

	if isNonSimplexBlock {
		return lastBlock, true, nil
	}

	// Else, the last block is a simplex block, so we need to find the lowest simplex block in the chain and then return the block before it,
	// which is the last non-simplex block.

	if numBlocksInChain > math.MaxInt {
		// This cannot happen, but we check to avoid potential overflow issues with sort.Search.
		return nil, false, errors.New("too many blocks in storage")
	}

	lowestSimplexBlockSeq, simplexBlockExists, err := findLowestSimplexBlockSeq(db, log, int(numBlocksInChain))
	if err != nil {
		return nil, false, err
	}

	if !simplexBlockExists {
		// This is a sanity check, as we should have found at least one simplex block if the last block was a simplex block.
		return nil, false, errors.New("no simplex blocks found in storage")
	}

	if err := validateLowestSimplexBlock(db, log, lowestSimplexBlockSeq); err != nil {
		return nil, false, err
	}

	blockBeforeLowestSimplexSeq := lowestSimplexBlockSeq - 1

	if blockBeforeLowestSimplexSeq == 0 {
		// If the block before the lowest simplex block is the genesis block, we return it as the last non-simplex block.
		return genesisBlock, true, nil
	}

	// Retrieve the last non-simplex block from the proposerVM and return it.
	lastNonSimplexBlock, err := getBlock(ctx, proposerVM, uint64(blockBeforeLowestSimplexSeq))
	if errors.Is(err, database.ErrNotFound) {
		// The block before the lowest simplex block isn't in the proposerVM, so we only
		// have simplex blocks, which can happen if we have bootstrapped with state sync.
		return nil, false, nil
	}
	if err != nil {
		log.Error("Failed to retrieve last non-simplex block", zap.Int("seq", blockBeforeLowestSimplexSeq), zap.Error(err))
		return nil, false, fmt.Errorf("failed getting block %d: %w", blockBeforeLowestSimplexSeq, err)
	}

	return lastNonSimplexBlock, true, nil
}

// isLastBlockNonSimplexBlock returns the last block of the chain, and whether it was
// accepted before Simplex was activated. Blocks accepted by Simplex are not retrievable
// from the proposerVM, so a block that is missing from it is a Simplex block.
// [numBlocksInChain] is assumed to be positive.
func isLastBlockNonSimplexBlock(
	ctx context.Context,
	proposerVM block.ChainVM,
	log logging.Logger,
	numBlocksInChain uint64,
) (snowman.Block, bool, error) {
	lastBlockSeq := numBlocksInChain - 1

	// If the last block is not found in the proposerVM, it is a Simplex block, so we return false.
	lastBlock, err := getBlock(ctx, proposerVM, lastBlockSeq)
	if errors.Is(err, database.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		log.Error("Failed to retrieve last block", zap.Uint64("lastBlockSeq", lastBlockSeq), zap.Error(err))
		return nil, false, fmt.Errorf("failed getting block %d: %w", lastBlockSeq, err)
	}

	return lastBlock, true, nil
}

// findLowestSimplexBlockSeq binary searches for the lowest sequence number in
// [0, searchUpperBound) that has a finalization, which is the first block that was
// accepted by Simplex. If no such sequence exists, false is returned.
func findLowestSimplexBlockSeq(db database.KeyValueReader, log logging.Logger, searchUpperBound int) (int, bool, error) {
	var internalError error

	// The below binary search searches for the lowest sequence number that has a finalization,
	// which is the first block that was accepted by Simplex.
	lowestSimplexBlockSeq := sort.Search(searchUpperBound, func(seq int) bool {
		if internalError != nil {
			return false
		}
		_, err := db.Get(finalizationKey(uint64(seq)))
		// If the finalization is not found, this block is not a Simplex block, so we return false.
		if errors.Is(err, database.ErrNotFound) {
			return false
		}
		if err != nil {
			internalError = err
			log.Error("Failed to get finalization for block", zap.Int("seq", seq), zap.Error(err))
			return false
		}
		// Else err == nil therefore the finalization exists in the storage, so it's a Simplex block.
		return true
	})

	if internalError != nil {
		return 0, false, internalError
	}

	// sort.Search returns the upper bound if the predicate is false for every sequence,
	// which means there are no simplex blocks in the storage.
	if lowestSimplexBlockSeq == searchUpperBound {
		return 0, false, nil
	}

	return lowestSimplexBlockSeq, true, nil
}

// validateLowestSimplexBlock sanity checks the lowest sequence number that was accepted
// by Simplex: it must not be the genesis block, and the block preceding it must not have
// a finalization of its own, which contradicts it being the simplex block with the lowest sequence.
func validateLowestSimplexBlock(db database.KeyValueReader, log logging.Logger, lowestSimplexBlockSeq int) error {
	// Sanity check I - make sure lowest simplex block is not the genesis block,
	// as the genesis block should never be a simplex block.
	if lowestSimplexBlockSeq == 0 {
		log.Error("Found simplex block at genesis block sequence number")
		return errors.New("found simplex block at genesis block sequence number")
	}

	// Sanity check II - check that the block before the lowest simplex block doesn't have a finalization.
	blockBeforeLowestSimplexSeq := lowestSimplexBlockSeq - 1

	_, err := db.Get(finalizationKey(uint64(blockBeforeLowestSimplexSeq)))
	if err == nil {
		log.Error("Found finalization for block that should be a non-simplex block", zap.Int("seq", blockBeforeLowestSimplexSeq))
		return fmt.Errorf("found finalization for block %d that should be a non-simplex block", blockBeforeLowestSimplexSeq)
	}
	if !errors.Is(err, database.ErrNotFound) {
		log.Error("Failed to get finalization for block", zap.Int("seq", blockBeforeLowestSimplexSeq), zap.Error(err))
		return fmt.Errorf("failed getting finalization for block %d: %w", blockBeforeLowestSimplexSeq, err)
	}

	return nil
}

func getBlock(ctx context.Context, vm block.ChainVM, height uint64) (snowman.Block, error) {
	id, err := vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return nil, err
	}

	return vm.GetBlock(ctx, id)
}

// finalizationToBytes serializes the simplex.Finalization into bytes.
func finalizationToBytes(finalization simplex.Finalization) []byte {
	cFinalization := canotoFinalization{
		Finalization: finalization.Finalization.Bytes(),
		QC:           finalization.QC.Bytes(),
	}
	return cFinalization.MarshalCanoto()
}

type canotoFinalization struct {
	Finalization []byte `canoto:"bytes,1"`
	QC           []byte `canoto:"bytes,2"`

	canotoData canotoData_canotoFinalization
}

// finalizationFromBytes deserialized the bytes into a simplex.Finalization.
func (c *canotoFinalization) toFinalization(d *QCDeserializer) (simplex.Finalization, error) {
	var finalization simplex.Finalization
	if err := finalization.Finalization.FromBytes(c.Finalization); err != nil {
		return simplex.Finalization{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(c.QC)
	if err != nil {
		return simplex.Finalization{}, err
	}

	finalization.QC = qc
	return finalization, nil
}
