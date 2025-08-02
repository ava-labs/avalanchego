// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
	errGenesisIndexed   = errors.New("genesis block should not be indexed")
	errInvalidQC        = errors.New("invalid quorum certificate")
	errMismatchedDigest = errors.New("mismatched digest in finalization")
)

type Storage struct {
	// height represents the number of blocks indexed in storage.
	height atomic.Uint64

	// db is the underlying database used to store finalizations.
	db KeyValueReaderWriter

	// genesisBlock is the genesis block data. It is stored as the first block in the storage.
	genesisBlock *Block

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
	lastAccepted, err := config.VM.LastAccepted(ctx)
	if err != nil {
		return nil, err
	}

	lastAcceptedBlock, err := config.VM.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}

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
	s.height.Store(lastAcceptedBlock.Height() + 1)

	return s, nil
}

func (s *Storage) Height() uint64 {
	return s.height.Load()
}

// Retrieve returns the block and finalization at [seq].
// If [seq] is not found, returns false.
func (s *Storage) Retrieve(seq uint64) (simplex.VerifiedBlock, simplex.Finalization, bool) {
	if seq == 0 {
		return s.genesisBlock, simplex.Finalization{}, true
	}

	block, err := getBlockAtHeight(context.TODO(), s.vm, seq)
	if err != nil {
		if err == database.ErrNotFound {
			s.log.Error("block not found for sequence", zap.Uint64("seq", seq), zap.Error(err))
		}
		return nil, simplex.Finalization{}, false
	}

	finalization, found, err := s.retrieveFinalization(seq)
	if !found || err != nil {
		return nil, simplex.Finalization{}, false
	}

	vb := &Block{vmBlock: block, metadata: finalization.Finalization.ProtocolMetadata, blockTracker: s.blockTracker}
	bytes, err := vb.Bytes()
	if err != nil {
		s.log.Error("failed to serialize block", zap.Error(err))
		return nil, simplex.Finalization{}, false
	}
	vb.digest = computeDigest(bytes)

	return vb, finalization, true
}

// Index indexes the finalization in the storage.
// It stores the finalization bytes at the current height and increments the height.
func (s *Storage) Index(ctx context.Context, block simplex.VerifiedBlock, finalization simplex.Finalization) error {
	bh := block.BlockHeader()
	if bh.Seq == 0 {
		s.log.Warn("attempted to index genesis block, which should not be indexed")
		return errGenesisIndexed
	}

	currentHeight := s.height.Load()
	if currentHeight != bh.Seq {
		return fmt.Errorf("%w: expected %d, got %d", errUnexpectedSeq, currentHeight, bh.Seq)
	}

	if !bytes.Equal(bh.Digest[:], finalization.Finalization.Digest[:]) {
		return fmt.Errorf("%w: expected %d, got %d", errMismatchedDigest, bh.Digest, finalization.Finalization.Digest)
	}

	if finalization.QC == nil {
		return errInvalidQC
	}

	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, currentHeight)

	finalizationBytes := finalizationToBytes(finalization)
	if err := s.db.Put(seqBuff, finalizationBytes); err != nil {
		return fmt.Errorf("failed to store finalization: %w", err)
	}

	err := s.blockTracker.indexBlock(ctx, bh.Digest)
	if err != nil {
		return fmt.Errorf("failed to index block: %w", err)
	}

	s.height.Add(1) // only increment height after successful indexing
	return nil
}

// getGenesisBlock returns the genesis block wrapped as a Block instance.
func getGenesisBlock(ctx context.Context, config *Config, blockTracker *blockTracker) (*Block, error) {
	genesis := &Block{
		metadata:     genesisMetadata,
		blockTracker: blockTracker,
	}

	snowmanGenesis, err := getBlockAtHeight(ctx, config.VM, 0)
	if err != nil {
		return nil, err
	}
	genesis.vmBlock = snowmanGenesis

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
func (s *Storage) retrieveFinalization(seq uint64) (simplex.Finalization, bool, error) {
	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	finalizationBytes, err := s.db.Get(seqBuff)
	if err != nil {
		if err == database.ErrNotFound {
			s.log.Error("finalization not found for sequence", zap.Uint64("seq", seq), zap.Error(err))
			return simplex.Finalization{}, false, nil
		}
		return simplex.Finalization{}, false, err
	}

	var canotoFinalization canotoFinalization
	if err := canotoFinalization.UnmarshalCanoto(finalizationBytes); err != nil {
		return simplex.Finalization{}, false, err
	}

	finalization, err := canotoFinalization.finalization(s.deserializer)
	if err != nil {
		return simplex.Finalization{}, false, err
	}

	return finalization, true, nil
}

func getBlockAtHeight(ctx context.Context, vm block.ChainVM, height uint64) (snowman.Block, error) {
	id, err := vm.GetBlockIDAtHeight(ctx, height)
	if err != nil {
		return nil, err
	}

	block, err := vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}

	return block, nil
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
func (c *canotoFinalization) finalization(d *QCDeserializer) (simplex.Finalization, error) {
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
