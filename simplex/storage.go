package simplex

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"sync/atomic"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ simplex.Storage = (*Storage)(nil)
var simplexPrefix = []byte("simplex")
var errUnexpectedSeq = errors.New("unexpected sequence number")
var errGenesisIndexed = errors.New("genesis block should not be indexed")
var errInvalidQC = errors.New("invalid quorum certificate")
var errMismatchedDigest = errors.New("mismatched digest in finalization")
var genesisMetadata = simplex.ProtocolMetadata{
	Version: 1,
	Epoch:   1,
	Round:   0,
	Seq:     0,
}

type Storage struct {
	// height represents the number of blocks indexed in storage.
	height atomic.Uint64

	// db is the underlying database used to store finalizations.
	db database.Database

	// genesisBlock is the genesis block data. It is stored as the first block in the storage.
	genesisBlock *Block

	// deserializer is used to deserialize quorum certificates from bytes.
	deserializer *QCDeserializer

	// blockTracker is used to manage blocks that have been indexed.
	blockTracker *blockTracker

	vm block.ChainVM

	log logging.Logger
}

// newStorage creates a new Storage instance.
// It creates a new prefixed database to store finalizations according to their sequence numbers.
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

	genesisBlock, err := getGenesisBlock(ctx, config, lastAcceptedBlock)
	if err != nil {
		return nil, err
	}

	s := &Storage{
		db:           prefixdb.New(simplexPrefix, config.DB),
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
			return nil, simplex.Finalization{}, false
		}
		return nil, simplex.Finalization{}, false
	}

	finalization, found, err := s.retrieveFinalization(seq)
	if !found || err != nil {
		return nil, simplex.Finalization{}, false
	}

	vb := &Block{vmBlock: block, metadata: finalization.Finalization.ProtocolMetadata}

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
		return fmt.Errorf("%w: expected %d, got %d", errMismatchedDigest , bh.Digest, finalization.Finalization.Digest)
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

	s.height.Add(1)
	return s.blockTracker.indexBlock(ctx, bh.Digest)
}

// getGenesisBlock returns the genesis block wrapped as a Block instance.
func getGenesisBlock(ctx context.Context, config *Config, lastAcceptedBlock snowman.Block) (*Block, error) {
	genesis := &Block{
		metadata: genesisMetadata,
	}

	// set the genesis's vmBlock
	if lastAcceptedBlock.Height() == 0 {
		genesis.vmBlock = lastAcceptedBlock
	} else {
		snowmanGenesis, err := getBlockAtHeight(ctx, config.VM, 0)
		if err != nil {
			return nil, err
		}
		genesis.vmBlock = snowmanGenesis
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
func (s *Storage) retrieveFinalization(seq uint64) (simplex.Finalization, bool, error) {
	seqBuff := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBuff, seq)
	finalizationBytes, err := s.db.Get(seqBuff)
	if err != nil {
		if err == database.ErrNotFound {
			return simplex.Finalization{}, false, nil
		}
		return simplex.Finalization{}, false, err
	}

	finalization, err := finalizationFromBytes(finalizationBytes, s.deserializer)
	if err != nil {
		return simplex.Finalization{}, false, err
	}

	return finalization, true, nil
}

func getBlockAtHeight(ctx context.Context, vm block.ChainVM, height uint64) (snowman.Block, error) {
	id, err := vm.GetBlockIDAtHeight(context.Background(), height)
	if err != nil {
		return nil, err
	}

	block, err := vm.GetBlock(context.Background(), id)
	if err != nil {
		return nil, err
	}

	return block, nil
}

// finalizationToBytes serializes the simplex.Finalization into bytes.
func finalizationToBytes(finalization simplex.Finalization) []byte {
	blockHeaderBytes := finalization.Finalization.Bytes()
	qcBytes := finalization.QC.Bytes()
	buff := make([]byte, len(blockHeaderBytes)+len(qcBytes))
	copy(buff, blockHeaderBytes)
	copy(buff[len(blockHeaderBytes):], qcBytes)
	return buff
}

// finalizationFromBytes deserialized the bytes into a simplex.Finalization.
func finalizationFromBytes(bytes []byte, d *QCDeserializer) (simplex.Finalization, error) {
	var finalization simplex.Finalization
	if err := finalization.Finalization.FromBytes(bytes[:simplex.BlockHeaderLen]); err != nil {
		return simplex.Finalization{}, err
	}

	qc, err := d.DeserializeQuorumCertificate(bytes[simplex.BlockHeaderLen:])
	if err != nil {
		return simplex.Finalization{}, err
	}

	finalization.QC = qc
	return finalization, nil
}
